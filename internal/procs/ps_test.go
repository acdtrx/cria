package procs

import (
	"strings"
	"testing"
)

// The rows here are real `ps -ww -o pid=,lstart=,command=` output from macOS
// 26.6: pid right-aligned in a padded column, lstart in five words with the day
// of the month space-padded, then the command line separated by a run of spaces.
// Only the command column can contain spaces of its own, which is why it is
// selected last and taken as the rest of the line.
func TestParseProcessRow(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantPID     int
		wantStarted string
		wantCommand string
		wantOK      bool
	}{
		{
			name:        "a real row",
			line:        "79189 Tue Aug 18 14:57:30 2026     /bin/zsh -i",
			wantPID:     79189,
			wantStarted: "Tue Aug 18 14:57:30 2026",
			wantCommand: "/bin/zsh -i",
			wantOK:      true,
		},
		{
			name:        "pid padded to the column width",
			line:        "    1 Thu Jul 30 00:25:55 2026     /sbin/launchd",
			wantPID:     1,
			wantStarted: "Thu Jul 30 00:25:55 2026",
			wantCommand: "/sbin/launchd",
			wantOK:      true,
		},
		{
			// A single-digit day is space-padded, so the words of lstart are two
			// spaces apart there. Rejoining on single spaces gives one instant one
			// spelling whichever day of the month it fell on.
			name:        "space-padded day of the month",
			line:        "  523 Thu Jul  9 00:26:51 2026     /usr/libexec/logd",
			wantPID:     523,
			wantStarted: "Thu Jul 9 00:26:51 2026",
			wantCommand: "/usr/libexec/logd",
			wantOK:      true,
		},
		{
			// `ps` joins argv with single spaces, so a run of them inside the
			// command is an argument that contained one. The command has to come
			// back as it stands.
			name:        "spaces inside an argument survive",
			line:        "79596 Tue Aug 18 15:01:12 2026     /usr/bin/python3 -c import time;  time.sleep(20)",
			wantPID:     79596,
			wantStarted: "Tue Aug 18 15:01:12 2026",
			wantCommand: "/usr/bin/python3 -c import time;  time.sleep(20)",
			wantOK:      true,
		},
		{
			name:        "unicode arguments",
			line:        "79601 Tue Aug 18 15:02:03 2026     /usr/bin/python3 --модель=日本語 модель",
			wantPID:     79601,
			wantStarted: "Tue Aug 18 15:02:03 2026",
			wantCommand: "/usr/bin/python3 --модель=日本語 модель",
			wantOK:      true,
		},
		{
			// macOS `ps` escapes a literal newline in an argument as \012 rather
			// than emitting it, so one process is always one line (verified on
			// macOS 26.6).
			name:        "an escaped newline stays on one line",
			line:        `79610 Tue Aug 18 15:03:44 2026     /usr/bin/python3 first\012second --flag`,
			wantPID:     79610,
			wantStarted: "Tue Aug 18 15:03:44 2026",
			wantCommand: `/usr/bin/python3 first\012second --flag`,
			wantOK:      true,
		},
		{
			name:        "tabs separate the columns",
			line:        "700\tThu Jul 30 00:26:51 2026\t/usr/libexec/smd",
			wantPID:     700,
			wantStarted: "Thu Jul 30 00:26:51 2026",
			wantCommand: "/usr/libexec/smd",
			wantOK:      true,
		},
		{
			name:   "a row with no command at all",
			line:   "79189 Tue Aug 18 14:57:30 2026",
			wantOK: false,
		},
		{
			name:   "a pid that is not a number",
			line:   "PID Tue Aug 18 14:57:30 2026     /bin/zsh",
			wantOK: false,
		},
		{
			name:   "an error `ps` would have written to stderr",
			line:   "ps: process id too large: 999999",
			wantOK: false,
		},
		{
			name:   "empty",
			line:   "",
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process, ok := parseProcessRow(test.line)
			if ok != test.wantOK {
				t.Fatalf("parseProcessRow(%q) ok is %v, want %v", test.line, ok, test.wantOK)
			}
			if !ok {
				return
			}
			if process.PID != test.wantPID {
				t.Errorf("pid is %d, want %d", process.PID, test.wantPID)
			}
			if process.StartedAt != test.wantStarted {
				t.Errorf("started at %q, want %q", process.StartedAt, test.wantStarted)
			}
			if process.Command != test.wantCommand {
				t.Errorf("command is %q, want %q", process.Command, test.wantCommand)
			}
		})
	}
}

// A listing is many rows, and the answer to "is this pid still there" is the
// absence of one.
func TestParseProcesses(t *testing.T) {
	tests := []struct {
		name     string
		out      string
		wantPIDs []int
	}{
		{
			name: "a whole listing",
			out: "    1 Thu Jul 30 00:25:55 2026     /sbin/launchd\n" +
				"  523 Thu Jul 30 00:26:51 2026     /usr/libexec/logd\n" +
				"79189 Tue Aug 18 14:57:30 2026     /bin/zsh -i\n",
			wantPIDs: []int{1, 523, 79189},
		},
		{
			// What `ps -p PID` prints for a pid nothing holds: nothing at all,
			// alongside an exit status. That is the answer serve reads as exited.
			name:     "a pid that is gone",
			out:      "",
			wantPIDs: nil,
		},
		{
			name:     "output that is only a trailing newline",
			out:      "\n",
			wantPIDs: nil,
		},
		{
			name: "a broken row does not take the listing down with it",
			out: "ps: some complaint\n" +
				"79189 Tue Aug 18 14:57:30 2026     /bin/zsh -i\n",
			wantPIDs: []int{79189},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processes := parseProcesses(test.out)
			if len(processes) != len(test.wantPIDs) {
				t.Fatalf("parsed %d processes, want %d (%v)", len(processes), len(test.wantPIDs), processes)
			}
			for i, want := range test.wantPIDs {
				if processes[i].PID != want {
					t.Errorf("process %d has pid %d, want %d", i, processes[i].PID, want)
				}
			}
		})
	}
}

// A long command line is the case worth proving on real output rather than on a
// fixture; this checks only that nothing in the parse imposes a length of its
// own. TestSystemIdentifiesARealProcess runs the same string through a real `ps`.
func TestParseProcessRowLongCommand(t *testing.T) {
	long := strings.Repeat("a", 60000)
	line := "79700 Tue Aug 18 15:10:00 2026     /usr/bin/python3 " + long

	process, ok := parseProcessRow(line)
	if !ok {
		t.Fatal("a long row did not parse")
	}
	if process.Command != "/usr/bin/python3 "+long {
		t.Errorf("command is %d bytes, want %d", len(process.Command), len(long)+17)
	}
}

// `ps -o rss=` is kilobytes on macOS and Linux alike, and %cpu is a share of one
// core — a threaded server passes 100.
func TestParseStats(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		pid     int
		wantRSS int64
		wantCPU float64
		wantOK  bool
	}{
		{
			name:    "a real row",
			out:     "79189   3136   4.2\n",
			pid:     79189,
			wantRSS: 3136 * 1024,
			wantCPU: 4.2,
			wantOK:  true,
		},
		{
			name:    "a busy server past one core",
			out:     "  801 8419328 412.7\n",
			pid:     801,
			wantRSS: 8419328 * 1024,
			wantCPU: 412.7,
			wantOK:  true,
		},
		{
			name:    "a process that has touched nothing yet",
			out:     "  802      0   0.0\n",
			pid:     802,
			wantRSS: 0,
			wantCPU: 0,
			wantOK:  true,
		},
		{
			name:   "a pid that is gone",
			out:    "",
			pid:    79189,
			wantOK: false,
		},
		{
			// The pid column is printed first for exactly this: an answer is
			// never attributed to a process by position alone.
			name:   "a row about a different process",
			out:    "79190   3136   4.2\n",
			pid:    79189,
			wantOK: false,
		},
		{
			name:   "a row that is not numbers",
			out:    "79189   RSS   CPU\n",
			pid:    79189,
			wantOK: false,
		},
		{
			name:   "a row missing a column",
			out:    "79189   3136\n",
			pid:    79189,
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stats, ok := parseStats(test.out, test.pid)
			if ok != test.wantOK {
				t.Fatalf("parseStats(%q, %d) ok is %v, want %v", test.out, test.pid, ok, test.wantOK)
			}
			if !ok {
				return
			}
			if stats.RSSBytes != test.wantRSS {
				t.Errorf("rss is %d bytes, want %d", stats.RSSBytes, test.wantRSS)
			}
			if stats.CPUPercent != test.wantCPU {
				t.Errorf("cpu is %v%%, want %v%%", stats.CPUPercent, test.wantCPU)
			}
		})
	}
}

// The command lines here are the real shapes the two managed servers take, and
// the real shapes that must not be mistaken for them.
func TestIsManagedServer(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "llama-server as cria launches it, by resolved path",
			command: "/opt/homebrew/bin/llama-server -hf unsloth/Qwen3-30B-GGUF:UD-Q4_K_XL --port 8080",
			want:    true,
		},
		{
			name:    "llama-server as a shell hands it over, by bare name",
			command: "llama-server -hf org/repo:Q4_K_M --port 8080",
			want:    true,
		},
		{
			// The real shape of a running mlx_lm.server, captured on macOS 26.6:
			// the interpreter from the script's shebang is argv[0], the script
			// itself argv[1].
			name:    "mlx_lm.server through its interpreter",
			command: "/opt/homebrew/Cellar/python@3.14/3.14.7/Frameworks/Python.framework/Versions/3.14/Resources/Python.app/Contents/MacOS/Python /Users/acdtrx/.local/bin/mlx_lm.server --model org/repo --port 18999",
			want:    true,
		},
		{
			name:    "a server installed as a shell script",
			command: "/bin/sh /usr/local/bin/mlx_lm.server --model org/repo",
			want:    true,
		},
		{
			name:    "a relative path to a server",
			command: "./build/bin/llama-server --port 8080",
			want:    true,
		},
		{
			name:    "no arguments at all",
			command: "/opt/homebrew/bin/llama-server",
			want:    true,
		},
		{
			// The reason argv[1] has to be a path: a bare word there is what a
			// person typed, not what the kernel resolved.
			name:    "someone grepping for a server",
			command: "grep llama-server",
			want:    false,
		},
		{
			name:    "someone looking one up",
			command: "which mlx_lm.server",
			want:    false,
		},
		{
			name:    "a log file named after a server",
			command: "tail -f /var/log/llama-server.log",
			want:    false,
		},
		{
			name:    "the name buried deeper in the arguments",
			command: "/usr/bin/env python3 -m mlx_lm.server --model org/repo",
			want:    false,
		},
		{
			name:    "an unrelated process",
			command: "/usr/libexec/logd",
			want:    false,
		},
		{
			name:    "an empty command line",
			command: "",
			want:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isManagedServer(test.command); got != test.want {
				t.Errorf("isManagedServer(%q) is %v, want %v", test.command, got, test.want)
			}
		})
	}
}

// An identity is compared with an identity, and a missing answer is not
// agreement.
func TestIdentitySameProcess(t *testing.T) {
	recorded := Identity{Command: "/opt/homebrew/bin/llama-server --port 8080", StartedAt: "Tue Aug 18 14:57:30 2026"}

	tests := []struct {
		name  string
		other Identity
		want  bool
	}{
		{
			name:  "the same process",
			other: recorded,
			want:  true,
		},
		{
			name:  "a reused pid running something else",
			other: Identity{Command: "/usr/bin/vim notes.md", StartedAt: "Tue Aug 18 15:30:00 2026"},
			want:  false,
		},
		{
			// The case the start time exists for: the same program restarted on
			// the same pid.
			name:  "the same command started again",
			other: Identity{Command: recorded.Command, StartedAt: "Tue Aug 18 15:30:00 2026"},
			want:  false,
		},
		{
			name:  "nothing was found",
			other: Identity{},
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := recorded.SameProcess(test.other); got != test.want {
				t.Errorf("SameProcess is %v, want %v", got, test.want)
			}
		})
	}

	if (Identity{}).SameProcess(Identity{}) {
		t.Error("two answers that were never obtained matched each other")
	}
}

// A process that rewrites its own argv[0] is still that process. mlx_lm.server
// does it on every launch: the shebang runs the venv's python, and macOS re-execs
// framework Python through Python.app a few milliseconds later — same pid, same
// start time, a different program path in `ps`.
func TestIdentitySameProcessAcrossReexec(t *testing.T) {
	const started = "Tue Aug 18 18:04:08 2026"
	const args = "/Users/me/.local/bin/mlx_lm.server --model mlx-community/Qwen2.5-0.5B-Instruct-4bit --host 0.0.0.0 --port 18081"

	recorded := Identity{Command: "/Users/me/.local/share/uv/tools/mlx-lm/bin/python " + args, StartedAt: started}
	reexeced := Identity{
		Command:   "/opt/homebrew/Cellar/python@3.14/3.14.7/Frameworks/Python.framework/Versions/3.14/Resources/Python.app/Contents/MacOS/Python " + args,
		StartedAt: started,
	}

	if !recorded.SameProcess(reexeced) {
		t.Error("a re-exec through Python.app read as a different process")
	}
	if !reexeced.SameProcess(recorded) {
		t.Error("the comparison is not symmetric")
	}

	restarted := reexeced
	restarted.StartedAt = "Tue Aug 18 18:09:11 2026"
	if recorded.SameProcess(restarted) {
		t.Error("the same arguments started again matched; the start time must still decide")
	}

	otherArgs := reexeced
	otherArgs.Command = "/opt/homebrew/bin/llama-server --host 0.0.0.0 --port 18081"
	if recorded.SameProcess(otherArgs) {
		t.Error("a pid running other arguments matched")
	}
}

// Two programs that take no arguments share no arguments to be judged on, so the
// whole command has to agree.
func TestIdentitySameProcessWithoutArguments(t *testing.T) {
	const started = "Tue Aug 18 14:57:30 2026"

	shell := Identity{Command: "/bin/zsh", StartedAt: started}
	if !shell.SameProcess(Identity{Command: "/bin/zsh", StartedAt: started}) {
		t.Error("a command with no arguments did not match itself")
	}
	if shell.SameProcess(Identity{Command: "/usr/bin/vim", StartedAt: started}) {
		t.Error("two argument-less commands matched on having no arguments")
	}
}
