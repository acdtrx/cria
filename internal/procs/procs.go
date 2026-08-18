// Package procs is cria's one route to the host's process table: it owns every
// exec of `ps` and `lsof`, and every signal cria sends (CODING-RULES §7). macOS
// has no /proc, so exec is the only cgo-free way to ask these questions
// (docs/TECH-STACK.md).
//
// The questions belong to serve: is the pid in a state record still the process
// that record was written for, what is it costing, which llama-server and
// mlx_lm.server processes is the host running, who holds a port, and where does
// a process think it is (docs/specs/SERVE.md). Judging the answers — live
// versus exited, foreign versus managed — is serve's; this package reports what
// the operating system says and nothing more.
//
// `ps` and `lsof` ship with macOS and are not configurable: they are assumed
// present. A program that cannot run degrades its answer — the caller gets the
// failure and decides what it disables — rather than failing cria.
package procs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// probeTimeout bounds every program this package runs. These probes sit on the
// TUI's refresh tick and on `cria status`, so a `ps` or `lsof` that wedges has
// to become an error quickly instead of holding the display. The real calls
// answer in tens of milliseconds.
const probeTimeout = 3 * time.Second

// Identity is what makes a pid the process cria recorded: the command line it
// runs and the moment it started, both exactly as `ps` spells them. serve
// captures an identity when it writes a record and compares it back later, so a
// pid the operating system has since handed to something else cannot
// impersonate a dead server (docs/specs/SERVE.md).
//
// Nothing here is parsed into a time. The comparison is `ps` output against
// `ps` output, and a clock conversion is exactly where two spellings of one
// instant stop being equal.
//
// An identity outlives the cria that captured it: serve writes it into a state
// record and a later invocation reads it back (docs/specs/SERVE.md), which is
// why the two fields carry the spelling that record file uses.
type Identity struct {
	Command   string `json:"command"`    // the process's full argv, joined the way `ps` prints it
	StartedAt string `json:"started_at"` // `ps -o lstart`, verbatim: "Tue Aug 18 14:57:30 2026"
}

// SameProcess reports whether two identities name one process. A zero identity
// matches nothing: it says the answer was never obtained, which must never read
// as agreement.
//
// The start time has to match exactly — it is what stops a recycled pid from
// impersonating a dead server. The command line has to match on its arguments,
// not on its program path, because a running process can rewrite its own argv[0]
// without becoming another process: macOS re-execs framework Python through
// Python.app within a few milliseconds of launch, which is what every
// mlx_lm.server does just after cria has recorded it. The arguments survive that
// rewrite, and a pid handed to some other program does not share them.
func (i Identity) SameProcess(other Identity) bool {
	if (i == Identity{}) || i.StartedAt != other.StartedAt {
		return false
	}
	if i.Command == other.Command {
		return true
	}
	args := arguments(i.Command)
	return args != "" && args == arguments(other.Command)
}

// arguments is a command line without the program path `ps` prints in front of
// it. A command with nothing after the program has no arguments to be judged on,
// and the caller falls back to the whole command rather than treating "no
// arguments" as agreement.
func arguments(command string) string {
	_, args, _ := strings.Cut(command, " ")
	return args
}

// Process is one process on the host: which pid, and what it is.
type Process struct {
	PID int
	Identity
}

// Stats is what a process costs right now.
type Stats struct {
	RSSBytes   int64   // resident set size; `ps` reports kilobytes, this is bytes
	CPUPercent float64 // `ps -o %cpu`: a share of one core, so it passes 100 on a busy multi-threaded server
}

// Host is the process table as cria asks about it — the seam serve is written
// against, so its liveness and stop logic can be driven without a single live
// process. One interface, one real implementation: System.
type Host interface {
	// Identify reports what a pid is running. found is false when nothing holds
	// the pid — the answer serve reads as "exited", not a failure.
	Identify(pid int) (identity Identity, found bool, err error)
	// Stats reports what a pid costs. found is false when the pid is gone.
	Stats(pid int) (stats Stats, found bool, err error)
	// Servers lists every llama-server and mlx_lm.server process on the host,
	// the ones cria started included; serve filters its own records out.
	Servers() ([]Process, error)
	// WorkingDir reports the directory a pid runs in. found is false when the
	// pid is gone or its working directory cannot be read.
	WorkingDir(pid int) (dir string, found bool, err error)
	// Listeners names the pids listening on a TCP port, each one once. Empty
	// means the port is free.
	Listeners(port int) ([]int, error)
	// Terminate asks a pid to stop (SIGTERM).
	Terminate(pid int) error
	// Kill ends a pid outright (SIGKILL).
	Kill(pid int) error
}

// System is the real Host: every answer comes from `ps`, `lsof`, or a signal
// delivered to a live pid. It holds nothing, so the zero value is the value.
type System struct{}

// Terminate asks a process to stop. What happens next — the grace period, the
// escalation to Kill, when the record goes away — is serve's policy
// (docs/specs/SERVE.md); this is only the signal.
func (System) Terminate(pid int) error { return signal(pid, syscall.SIGTERM, "SIGTERM") }

// Kill ends a process that did not answer SIGTERM.
func (System) Kill(pid int) error { return signal(pid, syscall.SIGKILL, "SIGKILL") }

// signal delivers one signal to one pid. A signal is a syscall, not an exec:
// nothing is spawned here and nothing is parsed. The name is carried alongside
// the number because a syscall.Signal prints as its description ("terminated"),
// which does not read as the thing that was sent.
func signal(pid int, number syscall.Signal, name string) error {
	// kill(2) reads 0 as "every process in my own process group" and -1 as
	// "everything this user can signal", so a pid that arrived wrong has to be
	// refused here — delivering it would take cria's own session down with it.
	if pid < 1 {
		return fmt.Errorf("refusing to send %s to pid %d: that is not a process", name, pid)
	}
	if err := syscall.Kill(pid, number); err != nil {
		return fmt.Errorf("sending %s to pid %d: %w", name, pid, err)
	}
	return nil
}

// run executes one of this package's two programs and returns what it wrote to
// stdout. stderr is dropped deliberately: `lsof` warns there about processes it
// is not allowed to inspect, and a warning is not the answer to any question
// asked here.
func run(program string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, program, args...)
	// `ps -o lstart` prints its weekday and month through the locale
	// ("mar. 18 août 15:05:28 2026" under LC_TIME=fr_FR.UTF-8), and a locale
	// with a comma decimal separator would do the same to %cpu. Both feed
	// comparisons and parses that must not depend on what the user's shell
	// happened to export.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	// Without WaitDelay a child that leaked its output pipe to a grandchild keeps
	// the read alive past the kill, and the timeout would not bound this call.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("%s did not answer within %s", program, probeTimeout)
	}
	return string(out), err
}

// answered reports whether a program ran all the way to a verdict. `ps` and
// `lsof` exit non-zero with nothing on stdout when the thing asked about does
// not exist, so an exit status is an answer — absence — while a program that
// could not start, or that the timeout cut off, is a failure the caller has to
// hear about.
func answered(err error) bool {
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	return errors.As(err, &exit)
}
