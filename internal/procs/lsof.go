package procs

import (
	"fmt"
	"strconv"
	"strings"
)

// lsofProgram, like `ps`, is resolved on PATH and carries no config override.
const lsofProgram = "lsof"

// `lsof` is always asked in its machine format (-F) — the human table aligns
// columns for reading, and a file name in it can contain the very spaces that
// would have to separate the columns (docs/TECH-STACK.md). In -F output every
// line is one field: the first byte names the field, the rest is its value.
// `lsof` always emits the process field (p) and the file-descriptor field (f)
// whatever else is asked for, and a file's fields follow that file's f line —
// which is what makes "the working directory of this process" and "the pid
// holding this port" readable without guessing.
const (
	lsofFields    = "-F"
	lsofAND       = "-a"  // AND the filters; without it `lsof` ORs them and answers about everything
	lsofSelectPID = "-p"  // one process
	lsofSelectFD  = "-d"  // one file-descriptor slot
	lsofNumeric   = "-nP" // no host lookup, no /etc/services lookup: never wait on the network to describe this host
	lsofListening = "-sTCP:LISTEN"
)

// The -F field letters this package reads.
const (
	lsofPID  = 'p'
	lsofFD   = 'f'
	lsofName = 'n'
)

// cwdFD is what `lsof` puts in the file-descriptor field for a process's working
// directory. It is a slot, not a number.
const cwdFD = "cwd"

// WorkingDir asks `lsof` where a process is running:
//
//	lsof -a -p PID -d cwd -F n
func (System) WorkingDir(pid int) (string, bool, error) {
	out, err := run(lsofProgram, lsofAND, lsofSelectPID, strconv.Itoa(pid), lsofSelectFD, cwdFD, lsofFields, string(lsofName))
	if dir, ok := parseWorkingDir(out); ok {
		return dir, true, nil
	}
	if !answered(err) {
		return "", false, fmt.Errorf("reading the working directory of pid %d: %w", pid, err)
	}
	return "", false, nil
}

// Listeners asks `lsof` who holds a TCP port:
//
//	lsof -nP -iTCP:PORT -sTCP:LISTEN -F pn
//
// -sTCP:LISTEN keeps established connections *to* that port out of the answer,
// so a browser talking to a server is never mistaken for the server.
func (System) Listeners(port int) ([]int, error) {
	out, err := run(lsofProgram, lsofNumeric, "-iTCP:"+strconv.Itoa(port), lsofListening, lsofFields, string(lsofPID)+string(lsofName))
	if !answered(err) {
		return nil, fmt.Errorf("finding what listens on port %d: %w", port, err)
	}
	return parseListeners(out, port), nil
}

// parseWorkingDir reads the working directory out of -F output: the name of the
// file occupying the cwd slot. Taking the first name line instead would report
// whichever file `lsof` happened to list first as the directory
// (CODING-RULES §4).
func parseWorkingDir(out string) (string, bool) {
	fd := ""
	for _, line := range strings.Split(out, "\n") {
		field, value, ok := parseField(line)
		if !ok {
			continue
		}
		switch field {
		case lsofFD:
			fd = value
		case lsofName:
			if fd == cwdFD {
				return value, true
			}
		}
	}
	return "", false
}

// parseListeners reads the pids holding a port out of -F output, each pid once.
// One process can hold a port through several sockets — a server bound to both
// address families has two — and a server that forked shares them with its
// children, which `lsof` reports as further p sections for the same port
// (verified on macOS).
//
// A pid counts only once one of its sockets is actually named on the port. The
// filter already asked for that port; reading it back off the name field is what
// keeps a socket `lsof` selected for some other reason from being reported as
// the port's holder.
func parseListeners(out string, port int) []int {
	suffix := ":" + strconv.Itoa(port)
	var pids []int
	counted := map[int]bool{}
	current, haveCurrent := 0, false
	for _, line := range strings.Split(out, "\n") {
		field, value, ok := parseField(line)
		if !ok {
			continue
		}
		switch field {
		case lsofPID:
			pid, err := strconv.Atoi(value)
			current, haveCurrent = pid, err == nil
		case lsofName:
			if !haveCurrent || counted[current] || !strings.HasSuffix(value, suffix) {
				continue
			}
			counted[current] = true
			pids = append(pids, current)
		}
	}
	return pids
}

// parseField splits one -F line into the byte naming the field and its value.
func parseField(line string) (byte, string, bool) {
	if line == "" {
		return 0, "", false
	}
	return line[0], line[1:], true
}
