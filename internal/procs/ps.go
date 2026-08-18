package procs

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// psProgram is resolved on PATH like every other tool cria drives; unlike those
// it carries no config override, because a host without a working `ps` is not a
// host cria runs on (docs/TECH-STACK.md).
const psProgram = "ps"

// Every `ps` here selects its columns explicitly and puts pid first, so each row
// is attributable and nothing depends on the default table. The flags are the
// set macOS and Linux `ps` share:
//
//	-A   every process on the host — not -e, which macOS reads as "print the
//	     environment too" while Linux reads it as "every process"
//	-ww  never cut a row to the terminal width; Linux `ps` does that by default
//	-o   the named columns, each with an "=" header so no header row is printed
const (
	psAllProcesses = "-A"
	psWide         = "-ww"
	psColumns      = "-o"
	psSelectPID    = "-p"
)

// psIdentityColumns is the row an identity is read from: who, when it started,
// and what it runs. command stays last — it is the one column that can contain
// spaces.
const psIdentityColumns = "pid=,lstart=,command="

// psStatsColumns is the row a cost is read from. rss is kilobytes on both macOS
// and Linux; %cpu is a percentage of one core.
const psStatsColumns = "pid=,rss=,%cpu="

// lstartWords is how many words `ps -o lstart` prints: weekday, month, day,
// time, year — "Tue Aug 18 14:57:30 2026". The day is space-padded, so two of
// those words can be separated by two spaces; counting words rather than
// columns is what makes the split exact either way.
const lstartWords = 5

// serverPrograms are the two programs whose processes serve wants to hear about
// (docs/specs/SERVE.md).
var serverPrograms = []string{"llama-server", "mlx_lm.server"}

// Identify asks `ps` what one pid is running:
//
//	ps -ww -o pid=,lstart=,command= -p PID
func (System) Identify(pid int) (Identity, bool, error) {
	out, err := run(psProgram, psWide, psColumns, psIdentityColumns, psSelectPID, strconv.Itoa(pid))
	for _, process := range parseProcesses(out) {
		if process.PID == pid {
			return process.Identity, true, nil
		}
	}
	if !answered(err) {
		return Identity{}, false, fmt.Errorf("identifying pid %d: %w", pid, err)
	}
	return Identity{}, false, nil
}

// Stats asks `ps` what one pid costs:
//
//	ps -ww -o pid=,rss=,%cpu= -p PID
func (System) Stats(pid int) (Stats, bool, error) {
	out, err := run(psProgram, psWide, psColumns, psStatsColumns, psSelectPID, strconv.Itoa(pid))
	if stats, ok := parseStats(out, pid); ok {
		return stats, true, nil
	}
	if !answered(err) {
		return Stats{}, false, fmt.Errorf("reading the stats of pid %d: %w", pid, err)
	}
	return Stats{}, false, nil
}

// Servers walks the whole process table for the servers cria manages:
//
//	ps -A -ww -o pid=,lstart=,command=
//
// A `ps` that could not run gives back no servers and the reason: the caller
// loses foreign-server detection, not its own records (docs/specs/SERVE.md).
func (System) Servers() ([]Process, error) {
	out, err := run(psProgram, psAllProcesses, psWide, psColumns, psIdentityColumns)
	if !answered(err) {
		return nil, fmt.Errorf("scanning the process table for servers: %w", err)
	}
	var servers []Process
	for _, process := range parseProcesses(out) {
		if isManagedServer(process.Command) {
			servers = append(servers, process)
		}
	}
	return servers, nil
}

// parseProcesses reads every row of a `pid=,lstart=,command=` listing. A line
// that does not have the shape those columns produce is skipped rather than
// guessed at: `ps` writes its warnings to stderr, which run() drops, so anything
// here that does not parse is not a process (CODING-RULES §4).
func parseProcesses(out string) []Process {
	var processes []Process
	for _, line := range strings.Split(out, "\n") {
		if process, ok := parseProcessRow(line); ok {
			processes = append(processes, process)
		}
	}
	return processes
}

// parseProcessRow reads one row: the pid, the five words of lstart, then the
// command line. The command keeps the rest of the line as it stands — a run of
// spaces inside it is an argument that contained one, not a column separator.
func parseProcessRow(line string) (Process, bool) {
	fields, command, ok := cutFields(line, 1+lstartWords)
	if !ok || command == "" {
		return Process{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return Process{}, false
	}
	return Process{
		PID: pid,
		Identity: Identity{
			Command: command,
			// The words of lstart are rejoined with single spaces, so the
			// space-padded day gives one instant one spelling whichever day of
			// the month it fell on.
			StartedAt: strings.Join(fields[1:], " "),
		},
	}, true
}

// parseStats reads a `pid=,rss=,%cpu=` row for the pid that was asked about. The
// pid is checked rather than assumed: every `ps` this package runs prints it
// first precisely so no answer is attributed to a process by position alone.
func parseStats(out string, pid int) (Stats, bool) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		rowPID, err := strconv.Atoi(fields[0])
		if err != nil || rowPID != pid {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		cpu, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		return Stats{RSSBytes: kilobytes * 1024, CPUPercent: cpu}, true
	}
	return Stats{}, false
}

// isManagedServer reports whether a command line runs one of the servers cria
// manages.
//
// argv[0] is the program for a compiled server. It is not for a server
// installed as a script — which is what mlx-lm ships: the kernel runs the
// interpreter named in the shebang, so a real mlx_lm.server appears as
// ".../Python /Users/x/.local/bin/mlx_lm.server --model org/repo" (verified on
// macOS 26.6). argv[1] therefore counts too, but only when it is a path: the
// kernel writes a path it resolved to reach the script, where a person types a
// bare word — which is what keeps `grep mlx_lm.server` out of the answer.
//
// `ps -o comm=` would be the obvious column to match on and cannot be used: it
// names the interpreter for a script-installed server, and macOS truncates it to
// 16 characters when the process was started by a relative path
// ("./bin/llama-server" comes back as "./bin/llama-ser").
func isManagedServer(command string) bool {
	args := strings.Fields(command)
	if len(args) == 0 {
		return false
	}
	if isServerProgram(args[0]) {
		return true
	}
	return len(args) > 1 && strings.ContainsRune(args[1], filepath.Separator) && isServerProgram(args[1])
}

// isServerProgram reports whether one argument names a managed server.
func isServerProgram(arg string) bool {
	return slices.Contains(serverPrograms, filepath.Base(arg))
}

// cutFields takes count whitespace-separated words off the front of a line and
// returns them with the rest of the line untouched. A line that runs out of
// words early is not the row it claimed to be.
func cutFields(line string, count int) ([]string, string, bool) {
	fields := make([]string, 0, count)
	rest := line
	for range count {
		rest = strings.TrimLeft(rest, " \t")
		end := strings.IndexAny(rest, " \t")
		if end < 0 {
			return nil, "", false
		}
		fields = append(fields, rest[:end])
		rest = rest[end:]
	}
	return fields, strings.TrimLeft(rest, " \t"), true
}
