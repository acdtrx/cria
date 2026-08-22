package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
)

// Record is what cria wrote down when it launched a server: everything status
// and stop need, so neither ever has to read the config tree back
// (docs/specs/SERVE.md). One record per entry, replaced by the entry's next
// start.
//
// Identity is the pid's own identity as the process table spelled it at launch.
// It is what stops a reused pid from impersonating a dead server, and it is the
// one field that may be absent: a server that was already gone by the time cria
// looked has no identity to record, and a record with none matches nothing —
// which is the truth about it.
//
// Repo and Quant are the resolved ones — what this launch actually serves after
// its picks were applied — and Selection is the picks themselves, so a record can
// say which combination is running and a restart can replay it. A flat entry picks
// nothing and records nothing.
type Record struct {
	EntryID    string           `json:"entry_id"`
	Backend    config.Backend   `json:"backend"`
	Repo       string           `json:"repo"`
	Quant      string           `json:"quant,omitempty"`
	Selection  config.Selection `json:"selection,omitempty"` // choice → picked option; absent for a flat entry
	Host       string           `json:"host"`
	Port       int              `json:"port"`
	PID        int              `json:"pid"`
	Identity   procs.Identity   `json:"identity"`
	Command    []string         `json:"command"` // the composed argv, program first
	LogPath    string           `json:"log_path"`
	LaunchedAt time.Time        `json:"launched_at"`
}

// picksOf is the selection as a record holds it: a copy, so nothing a caller does
// to its map afterwards rewrites what cria wrote down, and nothing at all when
// there was nothing to pick — a flat entry's record carries no selection key, and
// a record without one is a flat entry's.
func picksOf(selection config.Selection) config.Selection {
	if len(selection) == 0 {
		return nil
	}
	return maps.Clone(selection)
}

// Server is one record next to what the process table says about its pid right
// now. Live is the whole judgement docs/specs/SERVE.md defines: the pid exists
// and it is still this process.
type Server struct {
	Record
	Live bool
}

// Listing is every record cria holds: the servers it can act on, and the record
// files it refused. A broken record is reported rather than skipped — it names a
// pid cria started, and silently dropping it would leave a server nobody can see
// (CODING-RULES §4).
type Listing struct {
	Servers []Server       // ordered by entry id
	Broken  []BrokenRecord // ordered by entry id
}

// BrokenRecord is a record file cria refused, with the reason. Its manual fix is
// always the same one line: delete the file, and stop the pid it named if it is
// still running (CLAUDE.md, feature-building mode).
type BrokenRecord struct {
	EntryID string
	Path    string
	Err     error
}

// writeRecord saves one record. The write lands through a temporary file and a
// rename so a reader never meets a half-written record: this file is the only
// thing standing between a later cria invocation and a running server.
func (m *Manager) writeRecord(record Record) error {
	if err := os.MkdirAll(m.recordsRoot(), 0o755); err != nil {
		return fmt.Errorf("cannot create the server records directory %s: %w", m.recordsRoot(), err)
	}

	// Indented: this file is machine-owned, but a person reading it while
	// debugging a server is exactly when it gets read by hand.
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode the record of %s: %w", record.EntryID, err)
	}
	data = append(data, '\n')

	path := m.recordPath(record.EntryID)
	temp := path + ".writing"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("cannot write the record of %s: %w", record.EntryID, err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("cannot write the record of %s: %w", record.EntryID, err)
	}
	return nil
}

// readRecord loads one record file. The id is the filename minus .json, and it
// has to be the entry the record names.
func readRecord(path, id string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}

	// Strict decoding, like the config tree: an unknown key or a wrong type is an
	// error, never a silent default. Records are cria's own format, so a file
	// that does not match it was either hand-edited or written by a cria that no
	// longer exists — both are the author's to fix (CLAUDE.md, feature-building
	// mode).
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, err
	}
	if decoder.More() {
		return Record{}, errors.New("the file holds more than one JSON document")
	}
	if err := record.validate(id); err != nil {
		return Record{}, err
	}
	return record, nil
}

// validate holds every rule a record must satisfy to be acted on. A record is
// read to signal a pid and to render a status, so each field it drives is
// checked here rather than at the moment it is used.
func (r Record) validate(id string) error {
	switch {
	case r.EntryID == "":
		return missing("entry_id")
	case r.EntryID != id:
		return fmt.Errorf("entry_id is %q, but the file is named after entry %q", r.EntryID, id)
	case r.Backend != config.BackendLlama && r.Backend != config.BackendMLX:
		return fmt.Errorf("backend is %q, want %q or %q", r.Backend, config.BackendLlama, config.BackendMLX)
	case r.Repo == "":
		return missing("repo")
	case r.Quant != "" && r.Backend != config.BackendLlama:
		return fmt.Errorf("quant is set on a %q server; only %q servers take one", r.Backend, config.BackendLlama)
	case r.Host == "":
		return missing("host")
	case r.Port < 1 || r.Port > 65535:
		return fmt.Errorf("port is %d, want a port between 1 and 65535", r.Port)
	case r.PID < 1:
		return fmt.Errorf("pid is %d, which is not a process", r.PID)
	case len(r.Command) == 0:
		return missing("command")
	case r.LogPath == "":
		return missing("log_path")
	case r.LaunchedAt.IsZero():
		return missing("launched_at")
	}
	return nil
}

func missing(field string) error {
	return fmt.Errorf("%s is missing", field)
}

// loadRecord reads the record of one entry. found is false when the entry has no
// record — it has never been started, or its record was removed.
func (m *Manager) loadRecord(entryID string) (Record, bool, error) {
	record, err := readRecord(m.recordPath(entryID), entryID)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("%s: %w", m.recordPath(entryID), err)
	}
	return record, true, nil
}

// removeRecord deletes one entry's record. A record that is already gone is the
// state this asks for, not a failure.
func (m *Manager) removeRecord(entryID string) error {
	if err := os.Remove(m.recordPath(entryID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot remove the record of %s: %w", entryID, err)
	}
	return nil
}

// recordFiles lists the record files under the state root, ordered by entry id —
// os.ReadDir yields filename order, and a record's filename is its entry id. A
// state root that does not exist holds no servers, which is a fresh host rather
// than an error.
func (m *Manager) recordFiles() ([]string, error) {
	files, err := os.ReadDir(m.recordsRoot())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the server records at %s: %w", m.recordsRoot(), err)
	}

	var paths []string
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), recordExt) {
			continue
		}
		paths = append(paths, filepath.Join(m.recordsRoot(), file.Name()))
	}
	return paths, nil
}

// entryOf reads the entry id back off a record path.
func entryOf(path string) string {
	return strings.TrimSuffix(filepath.Base(path), recordExt)
}
