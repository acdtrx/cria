package serve

import "fmt"

// Live reports whether a record still names a running server. The rule is the
// one docs/specs/SERVE.md settles: the pid exists *and* the process identity
// matches what cria recorded at launch. A pid the operating system has since
// handed to something else fails the second half, so it can never impersonate a
// dead server.
//
// A record that fails either half is exited. Exited is a state, not an error:
// it is the crash report, and the record is kept until the entry starts again or
// the user dismisses it.
func (m *Manager) Live(record Record) (bool, error) {
	identity, found, err := m.host.Identify(record.PID)
	if err != nil {
		return false, fmt.Errorf("cannot tell whether %s is still running as pid %d: %w", record.EntryID, record.PID, err)
	}
	if !found {
		return false, nil
	}
	return record.Identity.SameProcess(identity), nil
}

// Running is whether an entry has a live server right now — the first question
// a start asks (docs/specs/SERVE.md): it costs one record read and one process
// lookup, where the checks behind it exec programs, so an entry that is already
// up is refused before anything else runs.
func (m *Manager) Running(entryID string) (Server, bool, error) {
	record, found, err := m.loadRecord(entryID)
	if err != nil || !found {
		// A record cria cannot read is List's to report; this entry simply has
		// no live server to refuse over.
		return Server{}, false, nil
	}
	live, err := m.Live(record)
	if err != nil || !live {
		return Server{}, false, err
	}
	return Server{Record: record, Live: true}, true, nil
}

// List reads every state record and judges it against the process table. It is
// how a fresh cria invocation re-attaches to the servers a previous one started
// (docs/specs/SERVE.md).
//
// A process table that cannot be read fails the whole listing rather than
// reporting servers as exited: "everything stopped" is a plausible-looking
// answer, and it would be a lie (CODING-RULES §4).
func (m *Manager) List() (Listing, error) {
	paths, err := m.recordFiles()
	if err != nil {
		return Listing{}, err
	}

	var listing Listing
	for _, path := range paths {
		id := entryOf(path)
		record, err := readRecord(path, id)
		if err != nil {
			listing.Broken = append(listing.Broken, BrokenRecord{EntryID: id, Path: path, Err: err})
			continue
		}
		live, err := m.Live(record)
		if err != nil {
			return Listing{}, err
		}
		listing.Servers = append(listing.Servers, Server{Record: record, Live: live})
	}
	return listing, nil
}
