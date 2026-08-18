package serve

import (
	"fmt"
	"time"
)

// Stop ends a server the way docs/specs/SERVE.md settles it: SIGTERM, a grace
// period for it to release its model and close its port, then SIGKILL. The
// record is removed once the process is confirmed gone — a deliberate stop
// leaves nothing behind, so a record that survives is always a crash.
//
// Stopping a server that has already exited is not a failure: it removes the
// record, which is the state the caller asked for.
func (m *Manager) Stop(record Record) error { return m.end(record, m.grace) }

// Kill is Stop without the grace: SIGKILL straight away, for a server that is
// wedged or that the user does not want to wait for.
func (m *Manager) Kill(record Record) error { return m.end(record, 0) }

// Dismiss clears the record of a server that has exited — the crash report, once
// the user is done with it (docs/specs/SERVE.md). It refuses a live server:
// dropping a running server out of cria's view would leave it holding its port
// with nothing naming it.
func (m *Manager) Dismiss(record Record) error {
	live, err := m.Live(record)
	if err != nil {
		return err
	}
	if live {
		return fmt.Errorf("%s is running as pid %d; stop it rather than dismissing it", record.EntryID, record.PID)
	}
	return m.removeRecord(record.EntryID)
}

// end is the whole escalation. grace is how long SIGTERM is given before SIGKILL
// follows; zero skips SIGTERM entirely.
func (m *Manager) end(record Record, grace time.Duration) error {
	live, err := m.Live(record)
	if err != nil {
		return err
	}

	if live && grace > 0 {
		if err := m.host.Terminate(record.PID); err != nil {
			return err
		}
		gone, err := m.waitGone(record, grace)
		if err != nil {
			return err
		}
		live = !gone
	}

	if live {
		if err := m.host.Kill(record.PID); err != nil {
			return err
		}
		gone, err := m.waitGone(record, m.confirm)
		if err != nil {
			return err
		}
		if !gone {
			// The record stays: it names a process that is still holding whatever
			// it holds, and cria must keep showing it rather than forget it.
			return fmt.Errorf("%s did not exit: pid %d is still running %s after SIGKILL",
				record.EntryID, record.PID, m.confirm)
		}
	}
	return m.removeRecord(record.EntryID)
}

// waitGone watches one pid until it stops being the server the record names, or
// until the window runs out.
//
// There is no event to wait on: the lifetime belongs to another process, one
// cria deliberately never waits on (docs/specs/SERVE.md). So this is
// observation — the same `ps` question liveness asks, repeated at a short
// interval and bounded by the caller's window (CODING-RULES §6).
func (m *Manager) waitGone(record Record, window time.Duration) (bool, error) {
	deadline := time.Now().Add(window)
	for {
		time.Sleep(m.poll)
		live, err := m.Live(record)
		if err != nil {
			return false, err
		}
		if !live {
			return true, nil
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
	}
}
