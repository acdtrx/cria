package serve

import (
	"slices"
	"strings"
	"testing"
)

// The stop escalation, in the order docs/specs/SERVE.md settles it: SIGTERM
// first, SIGKILL only once the grace period has passed, and never a signal to a
// server that is already gone.
func TestStopEscalation(t *testing.T) {
	cases := []struct {
		name      string
		dieOnTerm bool
		dieOnKill bool
		stop      func(*Manager, Record) error
		want      []string
		fails     bool
	}{
		{
			name:      "a server that answers SIGTERM is never killed",
			dieOnTerm: true,
			stop:      (*Manager).Stop,
			want:      []string{"TERM 4242"},
		},
		{
			name:      "a server that ignores SIGTERM is killed after the grace",
			dieOnKill: true,
			stop:      (*Manager).Stop,
			want:      []string{"TERM 4242", "KILL 4242"},
		},
		{
			name:      "a kill skips the grace",
			dieOnKill: true,
			stop:      (*Manager).Kill,
			want:      []string{"KILL 4242"},
		},
		{
			name:  "a server nothing ends keeps its record",
			stop:  (*Manager).Stop,
			want:  []string{"TERM 4242", "KILL 4242"},
			fails: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			host := &fakeHost{dieOnTerm: test.dieOnTerm, dieOnKill: test.dieOnKill}
			manager := newManager(t, host)
			record, _ := startOne(t, manager, host, llamaEntry(), 4242)

			err := test.stop(manager, record)
			if !slices.Equal(host.sent, test.want) {
				t.Errorf("cria sent %v, want %v", host.sent, test.want)
			}

			if test.fails {
				if err == nil {
					t.Fatal("stopping a server that never exited reported success")
				}
				if !strings.Contains(err.Error(), "4242") {
					t.Errorf("the failure is %v, want it to name the pid", err)
				}
				// The process is still holding whatever it holds; forgetting it
				// would hide it from cria entirely.
				if names := records(t, manager); !slices.Equal(names, []string{"qwen.json"}) {
					t.Errorf("a failed stop left %v, want the record kept", names)
				}
				return
			}

			if err != nil {
				t.Fatalf("stopping: %v", err)
			}
			// A deliberate stop leaves no record: a record that survives is
			// always a crash (docs/specs/SERVE.md).
			if _, found, err := manager.loadRecord(record.EntryID); found || err != nil {
				t.Errorf("a stopped server left a record: found=%v, err=%v", found, err)
			}
		})
	}
}

// Stopping a server that has already crashed is not a failure: the record goes,
// which is the state the caller asked for, and no signal is sent to a pid that
// may now belong to something else.
func TestStoppingACrashedServer(t *testing.T) {
	host := &fakeHost{dieOnTerm: true}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)
	delete(host.alive, 4242)

	if err := manager.Stop(record); err != nil {
		t.Fatalf("stopping a crashed server: %v", err)
	}
	if len(host.sent) != 0 {
		t.Errorf("cria signalled %v for a server that had already exited", host.sent)
	}
	if _, found, err := manager.loadRecord(record.EntryID); found || err != nil {
		t.Errorf("the record survived: found=%v, err=%v", found, err)
	}
}

// Dismiss clears a crash report. It refuses a live server: dropping a running
// server out of cria's view would leave it holding its port with nothing naming
// it (docs/specs/SERVE.md).
func TestDismiss(t *testing.T) {
	host := &fakeHost{}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)

	err := manager.Dismiss(record)
	if err == nil {
		t.Fatal("a running server was dismissed")
	}
	if !strings.Contains(err.Error(), "stop it") {
		t.Errorf("the refusal is %v, want it to name the operation that applies", err)
	}
	if names := records(t, manager); !slices.Equal(names, []string{"qwen.json"}) {
		t.Fatalf("the refused dismiss left %v", names)
	}
	if len(host.sent) != 0 {
		t.Errorf("a dismiss signalled %v", host.sent)
	}

	delete(host.alive, 4242)
	if err := manager.Dismiss(record); err != nil {
		t.Fatalf("dismissing an exited server: %v", err)
	}
	if _, found, err := manager.loadRecord(record.EntryID); found || err != nil {
		t.Errorf("the dismissed record survived: found=%v, err=%v", found, err)
	}
}

// A pid that was handed to something else after the server died is never
// signalled: the identity check is what stands between a stop and an unrelated
// process (docs/specs/SERVE.md).
func TestStopNeverSignalsAReusedPID(t *testing.T) {
	host := &fakeHost{dieOnTerm: true, dieOnKill: true}
	manager := newManager(t, host)
	record, _ := startOne(t, manager, host, llamaEntry(), 4242)

	host.alive[4242] = identityOf("/usr/bin/vim notes.txt")
	if err := manager.Stop(record); err != nil {
		t.Fatalf("stopping: %v", err)
	}
	if len(host.sent) != 0 {
		t.Errorf("cria sent %v to a pid that is no longer its server", host.sent)
	}
	if _, found, _ := manager.loadRecord(record.EntryID); found {
		t.Error("the record of a server whose pid was reused survived the stop")
	}
}
