package tui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// The log screen is the crash evidence and the only thing cria shows that it did
// not derive: raw lines, exactly as the server printed them, never parsed for a
// single fact (docs/cria.md, principle 6). Everything cria *says* about a server
// comes from documented endpoints and the filesystem — this is what is left when
// those say a server died.
const (
	// logTailLines is how much of the file is read back. A crash explains itself
	// in the last screenful; this is that with room to spare, and it bounds the
	// read on a log a long run has grown.
	logTailLines = 200

	// logTailBytes is how far back the read reaches for those lines. Nothing in
	// a server's log wraps anywhere near this, so it is the last few hundred
	// lines with margin, and a gigabyte log costs one seek.
	logTailBytes = 256 * 1024
)

// logScreen is the log tail while it is up: which entry's log, where the file
// is, and the lines the last read came back with. The zero value is closed.
type logScreen struct {
	open    bool
	entryID string
	path    string
	lines   []string
	err     error
}

// openLog is l: show the log of what the status box is targeting — the running
// server's, or the log of the record that crashed, which is the whole point of
// keeping an exited record around (docs/specs/SERVE.md).
func (m model) openLog() (tea.Model, tea.Cmd) {
	record, live := m.liveRecord()
	if !live {
		exited, kept := m.exitedRecord()
		if !kept {
			return m, nil
		}
		record = exited
	}

	m.log = logScreen{open: true, entryID: record.EntryID, path: record.LogPath}
	m.alert = alert{}
	return m, m.readLog
}

// pressInLog is the keyboard while the log is up: leave, or quit. The server
// keys are deliberately not live here — a stop pressed while reading a crash
// would act on a server the reader is no longer looking at.
func (m model) pressInLog(pressed tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(pressed, m.keys.quit):
		return m, tea.Quit
	case key.Matches(pressed, m.keys.leaveLog):
		m.log = logScreen{}
		return m, nil
	}
	return m, nil
}

// readLog re-reads the tail. It runs on the same ticker as everything else: a
// log file is written by another process, so following it is reading it again,
// and this is the read (CODING-RULES §6).
func (m model) readLog() tea.Msg {
	lines, err := tailLines(m.log.path, logTailLines)
	return logMsg{lines: lines, err: err}
}

// logMsg is one read of the log file.
type logMsg struct {
	lines []string
	err   error
}

// read takes it. A file that has gone — pruned by a new launch, or deleted —
// keeps the lines already on screen and says what happened to the file: they are
// still the last thing that server printed.
func (l logScreen) read(msg logMsg) logScreen {
	if msg.err != nil {
		l.err = msg.err
		return l
	}
	l.lines, l.err = msg.lines, nil
	return l
}

// panel draws the tail: the last lines of the file, and nothing said about them.
//
// The view is the end of the log and only the end — no scrollback. A crash
// explains itself at the bottom, and the file itself is one `less` away; a
// scroller earns its place when daily use asks for one (CLAUDE.md, Scope).
func (l logScreen) panel(width, rows int) string {
	title := "log · " + l.entryID
	capacity := rows - 2

	var lines []string
	if l.err != nil {
		lines = append(lines, wrapped("cannot read the log: "+l.err.Error(), width-4, alarmStyle)...)
	}
	if len(l.lines) == 0 && l.err == nil {
		lines = append(lines, wrapped("nothing printed yet: "+l.path, width-4, quietStyle)...)
	}

	tail := l.lines
	if room := capacity - len(lines); room > 0 && len(tail) > room {
		tail = tail[len(tail)-room:]
	}
	for _, line := range tail {
		lines = append(lines, factStyle.Render(line))
	}
	return pane(paneTitle(title), width, sizeLines(lines, capacity))
}

// tailLines reads the last count lines of a file.
//
// The read starts at most logTailBytes from the end, so following a log costs
// the same whatever it has grown to, and the first line of that window is
// dropped when the window began mid-line — half a line is not something a server
// printed.
func tailLines(path string, count int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	at, size := int64(0), info.Size()
	if size > logTailBytes {
		at = size - logTailBytes
	}
	data := make([]byte, size-at)
	read, err := file.ReadAt(data, at)
	// A short read is what a file being appended to while it is read looks like;
	// what came back is still what the server printed.
	if err != nil && read == 0 {
		return nil, err
	}

	printed := strings.TrimRight(string(data[:read]), "\n")
	if printed == "" {
		return nil, nil
	}
	lines := strings.Split(printed, "\n")
	if at > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return lines, nil
}
