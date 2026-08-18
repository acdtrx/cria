package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"cria/internal/serve"
)

// serverNamed is one live-or-exited server in a state directory, for the tests
// that drive `cria stop` and `cria status` off a listing.
func serverNamed(id string, pid int, live bool) serve.Server {
	record := testRecord()
	record.EntryID = id
	record.PID = pid
	return serve.Server{Record: record, Live: live}
}

// `cria stop` with nothing named has three cases, and only one of them acts
// (docs/specs/SERVE.md).
func TestStopWithNoEntryNamed(t *testing.T) {
	cases := []struct {
		name     string
		listing  serve.Listing
		want     int
		stopped  []string
		contains string
	}{
		{
			name:     "one server running: it is stopped",
			listing:  serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
			want:     exitOK,
			stopped:  []string{"qwen"},
			contains: "stopped qwen (pid 4242 on 0.0.0.0:8080)",
		},
		{
			name: "several running: the id is required",
			listing: serve.Listing{Servers: []serve.Server{
				serverNamed("gemma", 11, true),
				serverNamed("qwen", 4242, true),
			}},
			want:     exitFailure,
			contains: "2 servers are running (gemma, qwen); name the one to stop",
		},
		{
			name:     "nothing running: nothing to stop",
			listing:  serve.Listing{},
			want:     exitFailure,
			contains: "nothing is running",
		},
		{
			name:     "nothing running, but crash reports remain",
			listing:  serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, false)}},
			want:     exitFailure,
			contains: "nothing is running (1 exited record(s) remain; `cria status` shows them, `cria stop <id>` clears one)",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{listing: test.listing}
			app, out, errOut := newTestApp(testTree(), fake)

			if code := app.stop(nil); code != test.want {
				t.Errorf("exit code %d, want %d (stderr: %s)", code, test.want, errOut)
			}
			if !slices.Equal(fake.stopped, test.stopped) {
				t.Errorf("cria stopped %v, want %v", fake.stopped, test.stopped)
			}
			printed := out.String() + errOut.String()
			if !strings.Contains(printed, test.contains) {
				t.Errorf("cria printed %q, want it to contain %q", printed, test.contains)
			}
		})
	}
}

// Naming an entry stops that entry, whichever others are running.
func TestStopNamesItsEntry(t *testing.T) {
	fake := &fakeServers{listing: serve.Listing{Servers: []serve.Server{
		serverNamed("gemma", 11, true),
		serverNamed("qwen", 4242, true),
	}}}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.stop([]string{"gemma"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if !slices.Equal(fake.stopped, []string{"gemma"}) {
		t.Errorf("cria stopped %v, want the entry that was named", fake.stopped)
	}
	if !strings.Contains(out.String(), "stopped gemma") {
		t.Errorf("cria printed %q, want what it stopped", out)
	}
}

// Stopping a server that has already crashed removes its record and succeeds:
// that is the state the caller asked for (docs/specs/SERVE.md). The report says
// what cria actually judged — the pid is not the process it launched — rather
// than claiming the server exited, which a record written without an identity
// would make untrue.
func TestStopClearsAnExitedRecord(t *testing.T) {
	fake := &fakeServers{listing: serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, false)}}}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.stop([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if !slices.Equal(fake.stopped, []string{"qwen"}) {
		t.Errorf("cria stopped %v, want the exited entry's record cleared", fake.stopped)
	}
	for _, want := range []string{"had already exited", "pid 4242 is no longer the process cria launched", "removed its record"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", out, want)
		}
	}
}

// An entry with no record was never started, or was already stopped: there is
// nothing to do, and the exit code says so. The refusal names what cria does
// hold, because a caller whose server is still answering needs to hear that cria
// is not tracking it.
func TestStopRefusesAnEntryWithNoRecord(t *testing.T) {
	cases := []struct {
		name     string
		listing  serve.Listing
		contains []string
	}{
		{
			name:     "cria holds nothing at all",
			contains: []string{"no server record for qwen", "holds no server records at all", "not cria's to stop"},
		},
		{
			name: "cria holds records for other entries",
			listing: serve.Listing{
				Servers: []serve.Server{serverNamed("gemma", 11, true)},
				Broken:  []serve.BrokenRecord{{EntryID: "mistral", Path: "/s/mistral.json", Err: errors.New("pid is 0")}},
			},
			contains: []string{"no server record for qwen", "holds records for: gemma, mistral"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{listing: test.listing}
			app, _, errOut := newTestApp(testTree(), fake)

			if code := app.stop([]string{"qwen"}); code != exitFailure {
				t.Fatalf("exit code %d, want %d", code, exitFailure)
			}
			for _, want := range test.contains {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if len(fake.stopped) != 0 {
				t.Errorf("cria stopped %v for an entry with no record", fake.stopped)
			}
		})
	}
}

// A record cria cannot read names a pid cria started, so stopping it reports the
// file and the one line that clears it (CLAUDE.md, feature-building mode).
func TestStopReportsAnUnreadableRecord(t *testing.T) {
	fake := &fakeServers{listing: serve.Listing{Broken: []serve.BrokenRecord{{
		EntryID: "qwen",
		Path:    "/home/u/.local/state/cria/servers/qwen.json",
		Err:     errors.New("port is 0, want a port between 1 and 65535"),
	}}}}
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.stop([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	for _, want := range []string{"servers/qwen.json", "port is 0", "delete that file"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", errOut, want)
		}
	}
}
