package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// logPath names the file one launch of an entry writes to. Everything the server
// prints lands there raw and is never parsed — the tail is what a person reads
// when a server dies (docs/cria.md, principle 6).
func (m *Manager) logPath(entryID string, at time.Time) string {
	return filepath.Join(m.logsRoot(), entryID+"-"+at.Format(logStamp)+logExt)
}

// createLog opens a launch's log file. It is opened for appending rather than
// truncating: two launches of one entry inside the same second would otherwise
// have the second erase the first one's crash.
func (m *Manager) createLog(path string) (*os.File, error) {
	if err := os.MkdirAll(m.logsRoot(), 0o755); err != nil {
		return nil, fmt.Errorf("cannot create the server log directory %s: %w", m.logsRoot(), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot create the log file %s: %w", path, err)
	}
	return file, nil
}

// pruneLogs keeps the newest keep logs of one entry and deletes the rest —
// retention by count, at launch, with no rotation machinery
// (docs/specs/SERVE.md). It runs before the launch it makes room for, so a
// directory cria cannot tidy fails the start rather than a server that is
// already running.
//
// Only this entry's logs are considered, and only files whose name carries a
// launch stamp: an entry id can be the prefix of another one, and the stamp is
// what tells "qwen-30b-20260818-150405.log" from any log of entry "qwen".
func (m *Manager) pruneLogs(entryID string, keep int) error {
	files, err := os.ReadDir(m.logsRoot())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read the server log directory %s: %w", m.logsRoot(), err)
	}

	type launchLog struct {
		name string
		at   time.Time
	}
	var logs []launchLog
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		at, ok := launchStamp(file.Name(), entryID)
		if !ok {
			continue
		}
		logs = append(logs, launchLog{name: file.Name(), at: at})
	}

	// Newest first, so what is dropped is the tail. Names break a tie between two
	// launches inside one second, which keeps the order total.
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].at.Equal(logs[j].at) {
			return logs[i].name > logs[j].name
		}
		return logs[i].at.After(logs[j].at)
	})

	for _, log := range logs[min(keep, len(logs)):] {
		path := filepath.Join(m.logsRoot(), log.name)
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cannot remove the old log %s: %w", path, err)
		}
	}
	return nil
}

// launchStamp reads the launch time out of a log file's name, and reports
// whether the name is a log of this entry at all.
func launchStamp(name, entryID string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, entryID+"-")
	if !ok {
		return time.Time{}, false
	}
	rest, ok = strings.CutSuffix(rest, logExt)
	if !ok {
		return time.Time{}, false
	}
	at, err := time.ParseInLocation(logStamp, rest, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}
