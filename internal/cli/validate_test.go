package cli

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"cria/internal/config"
	"cria/internal/serve"
	"cria/internal/tools"
)

// heldServer is the server a validation finds on the target's port: a record of
// cria's own, which is what makes it displaceable rather than a refusal.
func heldServer(entryID string, selection config.Selection) *serve.Server {
	record := testRecord()
	record.EntryID = entryID
	record.Selection = selection
	return &serve.Server{Record: record, Live: true}
}

// greenTarget is a fake whose target starts, answers its health endpoint and
// takes a completion — a swap where nothing goes wrong.
func greenTarget(holder *serve.Server) *fakeServers {
	return &fakeServers{
		record: testRecord(),
		use:    serve.PortUse{Managed: holder},
		phases: []serve.Phase{serve.PhaseRunning},
		health: serve.Health{URL: "http://127.0.0.1:8080/health", Green: true, Status: 200, Detail: "200 OK"},
	}
}

// printedInOrder checks that each line appears in the output after everything
// before it: the stage lines are a sequence, and a swap that printed them in
// another order ran another protocol.
func printedInOrder(t *testing.T, printed string, lines ...string) {
	t.Helper()
	rest := printed
	for _, line := range lines {
		_, after, found := strings.Cut(rest, line)
		if !found {
			t.Fatalf("cria printed %q, want %q after everything before it", printed, line)
		}
		rest = after
	}
}

// lastLine is what an agent reads as cria's answer: every non-zero exit ends
// with one line naming the reason (docs/specs/CLI.md).
func lastLine(printed string) string {
	lines := strings.Split(strings.TrimRight(printed, "\n"), "\n")
	return lines[len(lines)-1]
}

// The whole protocol on a machine that was already serving: the port holder is
// stopped with its record held, the target is started, proved and stopped, and
// the holder goes back — in that order, with the verdict last.
func TestValidateSwapsTheHolderOutAndPutsItBack(t *testing.T) {
	fake := greenTarget(heldServer("gemma", config.Selection{"quant": "q6"}))
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.validate([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if want := []string{"gemma", "qwen"}; !slices.Equal(fake.stopped, want) {
		t.Errorf("cria stopped %v, want %v: the holder first, the target once it was proved", fake.stopped, want)
	}
	if len(fake.started) != 1 || fake.started[0].ID != "qwen" {
		t.Errorf("cria started %+v, want the target alone", fake.started)
	}
	if want := []string{"qwen"}; !slices.Equal(fake.proved, want) {
		t.Errorf("cria proved %v, want %v: a completion is what validation means", fake.proved, want)
	}
	if len(fake.restored) != 1 || fake.restored[0].EntryID != "gemma" {
		t.Fatalf("cria put back %+v, want the record it held of gemma", fake.restored)
	}
	if !maps.Equal(fake.restored[0].Selection, config.Selection{"quant": "q6"}) {
		t.Errorf("gemma went back under the picks %v, want the ones its record was composed with", fake.restored[0].Selection)
	}

	printedInOrder(t, out.String(),
		"stopping gemma (holding its record)",
		"starting qwen…",
		"proving qwen…",
		"stopping qwen",
		"restoring gemma…",
		"restored gemma as pid 4242 on 0.0.0.0:8080",
		"validated qwen: it served on port 8080 and answered a completion",
	)
	if got := lastLine(out.String()); !strings.HasPrefix(got, "validated qwen") {
		t.Errorf("cria's last line is %q, want the verdict", got)
	}
	if errOut.Len() != 0 {
		t.Errorf("cria wrote %q to stderr, want a swap that went as planned to say nothing there", errOut)
	}
}

// A target whose port nobody holds is started, proved and stopped: there is
// nothing to displace and nothing to put back, and "left as found" includes
// leaving nothing running.
func TestValidateOnAFreePortDisplacesNothing(t *testing.T) {
	fake := greenTarget(nil)
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.validate([]string{"qwen"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.displaced) != 0 || len(fake.restored) != 0 {
		t.Errorf("cria displaced %v and restored %+v on a port nobody held", fake.displaced, fake.restored)
	}
	if want := []string{"qwen"}; !slices.Equal(fake.stopped, want) {
		t.Errorf("cria stopped %v, want %v: the target it started, and nothing else", fake.stopped, want)
	}
	if !strings.Contains(out.String(), "validated qwen") {
		t.Errorf("cria printed %q, want the verdict", out)
	}
}

// Validating the entry that is already serving on the port is the same protocol:
// it is displaced and restarted like any other holder — and what goes back is
// the combination its record was launched under, not the one being validated.
func TestValidateReplaysTheHolderOwnCombination(t *testing.T) {
	tree := testTree()
	tree.Entries = append(tree.Entries, choicesEntry())

	record := testRecord()
	record.EntryID = "qwen-choices"
	fake := greenTarget(heldServer("qwen-choices", config.Selection{"quant": "q6"}))
	fake.record = record
	app, out, errOut := newTestApp(tree, fake)

	if code := app.validate([]string{"qwen-choices", "quant=q4"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if len(fake.picked) != 1 || !maps.Equal(fake.picked[0], config.Selection{"quant": "q4"}) {
		t.Errorf("the validation launched under %v, want the picks it was asked for", fake.picked)
	}
	if len(fake.restored) != 1 || !maps.Equal(fake.restored[0].Selection, config.Selection{"quant": "q6"}) {
		t.Errorf("the record put back carried %+v, want the combination that was running", fake.restored)
	}
	if want := []string{"qwen-choices", "qwen-choices"}; !slices.Equal(fake.stopped, want) {
		t.Errorf("cria stopped %v, want %v: the holder, then the target it validated", fake.stopped, want)
	}
	printedInOrder(t, out.String(), "stopping qwen-choices (holding its record)", "restoring qwen-choices…")
}

// A target that does not serve is the answer validate exists to give: exit 1,
// one reason line — and the machine is back as it was found, whichever stage
// the target failed at.
func TestValidateReportsATargetThatFailed(t *testing.T) {
	cases := []struct {
		name       string
		tune       func(*fakeServers)
		reason     string
		wantStops  []string
		wantProved bool
	}{
		{
			name:      "the spawn was refused",
			tune:      func(f *fakeServers) { f.startErr = errors.New("cannot spawn llama-server: permission denied") },
			reason:    "cannot spawn llama-server: permission denied",
			wantStops: []string{"gemma"},
		},
		{
			name:      "it exited before it served",
			tune:      func(f *fakeServers) { f.phases = []serve.Phase{serve.PhaseExited} },
			reason:    "it exited after",
			wantStops: []string{"gemma", "qwen"},
		},
		{
			name: "it never answered a completion",
			tune: func(f *fakeServers) {
				f.proveErr = errors.New("qwen did not answer a completion: 500 Internal Server Error")
			},
			reason:     "did not answer a completion",
			wantStops:  []string{"gemma", "qwen"},
			wantProved: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := greenTarget(heldServer("gemma", nil))
			test.tune(fake)
			app, out, errOut := newTestApp(testTree(), fake)

			if code := app.validate([]string{"qwen"}); code != exitFailure {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
			}
			if !slices.Equal(fake.stopped, test.wantStops) {
				t.Errorf("cria stopped %v, want %v", fake.stopped, test.wantStops)
			}
			if proved := len(fake.proved) == 1; proved != test.wantProved {
				t.Errorf("cria proved %v, want the prove to be %v at this stage", fake.proved, test.wantProved)
			}
			if len(fake.restored) != 1 || fake.restored[0].EntryID != "gemma" {
				t.Fatalf("cria put back %+v, want gemma back on its port whatever the target did", fake.restored)
			}
			if !strings.Contains(out.String(), "restored gemma") {
				t.Errorf("cria printed %q, want the restore it performed", out)
			}
			if got := lastLine(errOut.String()); !strings.Contains(got, test.reason) || !strings.HasPrefix(got, "cria validate qwen:") {
				t.Errorf("cria's last line is %q, want one line naming %q", got, test.reason)
			}
		})
	}
}

// A displaced server that cannot be put back is the one outcome a person has to
// act on: exit 3, and the line says what is serving now.
func TestValidateReportsAFailedRestore(t *testing.T) {
	fake := greenTarget(heldServer("gemma", nil))
	fake.restoreErr = errors.New("cannot put gemma back on port 8080: gemma is not an entry cria can read any more")
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.validate([]string{"qwen"}); code != exitUnrestored {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitUnrestored, errOut)
	}
	got := lastLine(errOut.String())
	for _, want := range []string{
		"cannot put gemma back on port 8080",
		"is not an entry cria can read any more",
		"nothing is serving on port 8080 now",
		"`cria start gemma` once that is fixed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cria's last line is %q, want it to contain %q", got, want)
		}
	}
}

// A target that will not stop leaves its port taken, so the holder cannot go
// back onto it: the restore is not attempted at all, and the line names what is
// serving now and the two commands that undo it.
func TestValidateReportsATargetThatWouldNotStop(t *testing.T) {
	fake := greenTarget(heldServer("gemma", nil))
	fake.stopErr = errors.New("qwen did not exit: pid 4242 is still running after 5s")
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.validate([]string{"qwen"}); code != exitUnrestored {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitUnrestored, errOut)
	}
	if len(fake.restored) != 0 {
		t.Errorf("cria tried to put gemma back onto a port qwen still holds: %+v", fake.restored)
	}
	got := lastLine(errOut.String())
	for _, want := range []string{"did not exit", "qwen still holds port 8080", "gemma was not put back", "`cria stop qwen`", "`cria start gemma`"} {
		if !strings.Contains(got, want) {
			t.Errorf("cria's last line is %q, want it to contain %q", got, want)
		}
	}
}

// A holder that will not stop ends the validation before it began: nothing was
// started, and the line says the machine may already have changed — the stop was
// under way when it failed.
func TestValidateReportsAHolderThatWouldNotStop(t *testing.T) {
	fake := greenTarget(heldServer("gemma", nil))
	fake.displaceErr = errors.New("gemma did not exit: pid 4242 is still running after 5s")
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.validate([]string{"qwen"}); code != exitUnrestored {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitUnrestored, errOut)
	}
	if len(fake.started) != 0 || len(fake.restored) != 0 {
		t.Errorf("cria started %+v and restored %+v after a stop that failed", fake.started, fake.restored)
	}
	got := lastLine(errOut.String())
	for _, want := range []string{"cannot stop gemma on port 8080", "nothing was validated", "gemma may already be down", "`cria status`"} {
		if !strings.Contains(got, want) {
			t.Errorf("cria's last line is %q, want it to contain %q", got, want)
		}
	}
}

// Every refusal is answered before the machine changes: exit 2 means nothing was
// stopped, nothing was started, and the reason is on the last line.
func TestValidateRefusesWithoutTouchingTheMachine(t *testing.T) {
	choices := testTree()
	choices.Entries = append(choices.Entries, choicesEntry())

	elsewhere := testRecord()
	elsewhere.Port = 9999

	cases := []struct {
		name string
		tree *config.Tree
		args []string
		fake *fakeServers
		tune func(*app)
		want []string
	}{
		{
			name: "the id names no entry",
			tree: testTree(),
			args: []string{"qwn"},
			fake: greenTarget(heldServer("gemma", nil)),
			want: []string{`no entry named "qwn"`, "available entries: qwen"},
		},
		{
			name: "a pick names no option",
			tree: choices,
			args: []string{"qwen-choices", "quant=q8"},
			fake: greenTarget(heldServer("gemma", nil)),
			want: []string{`has no option named "q8"`, "q4, q6"},
		},
		{
			name: "the backend's tool is missing",
			tree: testTree(),
			args: []string{"qwen"},
			fake: greenTarget(heldServer("gemma", nil)),
			tune: func(a *app) {
				a.tools = func(config.Settings) tools.Report {
					report := usableReport()
					report.LlamaServer = tools.Tool{Name: tools.LlamaServer, Status: tools.StatusMissing, Fix: "install llama.cpp so llama-server is on PATH"}
					return report
				}
			},
			want: []string{"llama-server is missing", "install llama.cpp"},
		},
		{
			name: "the port is held by a process cria did not start",
			tree: testTree(),
			args: []string{"qwen"},
			fake: &fakeServers{
				record: testRecord(),
				use: serve.PortUse{Holders: []serve.Holder{{
					PID:        9001,
					Command:    "/opt/homebrew/bin/llama-server -m gemma.gguf --port 8080",
					WorkingDir: "/Users/someone/models",
				}}},
			},
			want: []string{"port 8080 is held by a process cria did not start", "pid 9001", "llama-server -m gemma.gguf"},
		},
		{
			name: "the holder is answering a request right now",
			tree: testTree(),
			args: []string{"qwen"},
			fake: func() *fakeServers {
				fake := greenTarget(heldServer("gemma", nil))
				fake.generation = serve.Generation{Busy: serve.BusyGenerating}
				return fake
			}(),
			want: []string{
				"gemma is answering a request on port 8080 right now",
				"stopping it would cut that answer off",
				"ask the user to let it finish or to stop gemma",
			},
		},
		{
			name: "the target is already running on another port",
			tree: testTree(),
			args: []string{"qwen"},
			fake: func() *fakeServers {
				fake := greenTarget(nil)
				fake.listing = serve.Listing{Servers: []serve.Server{{Record: elsewhere, Live: true}}}
				return fake
			}(),
			want: []string{
				"qwen is already running as pid 4242 on port 9999",
				"not the port it launches on now (8080)",
				"`cria stop qwen` first",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			app, out, errOut := newTestApp(test.tree, test.fake)
			if test.tune != nil {
				test.tune(app)
			}

			if code := app.validate(test.args); code != exitRefused {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitRefused, errOut)
			}
			if len(test.fake.stopped) != 0 || len(test.fake.started) != 0 || len(test.fake.restored) != 0 {
				t.Errorf("a refused validation stopped %v, started %+v and restored %+v",
					test.fake.stopped, test.fake.started, test.fake.restored)
			}
			if out.Len() != 0 {
				t.Errorf("a refused validation printed %q on stdout, want the refusal on stderr alone", out)
			}
			got := lastLine(errOut.String())
			for _, want := range test.want {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if got == "" {
				t.Errorf("cria printed %q, want a reason on its last line", errOut)
			}
		})
	}
}

// A holder cria cannot ask about its own work is warned about, not refused: the
// swap goes ahead, and the warning names what could not be checked and what it
// would cost if the guess is wrong. Which servers cannot be asked is serve's
// (an mlx holder, a llama holder whose slot endpoint did not answer); what the
// command does with either answer is the same, and it is this.
func TestValidateWarnsWhenTheHolderCannotBeAsked(t *testing.T) {
	cases := []struct {
		name   string
		detail string
	}{
		{
			name:   "an mlx holder publishes no such signal",
			detail: "mlx_lm.server publishes no per-slot signal, so cria cannot tell whether it is generating",
		},
		{
			name:   "a llama holder's slot endpoint refused",
			detail: "http://127.0.0.1:8080/slots answered 501 Not Implemented: slots endpoint is disabled",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := greenTarget(heldServer("gemma", nil))
			fake.generation = serve.Generation{Busy: serve.BusyUnverifiable, Detail: test.detail}
			app, out, errOut := newTestApp(testTree(), fake)

			if code := app.validate([]string{"qwen"}); code != exitOK {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
			}
			for _, want := range []string{"cria cannot tell whether gemma is generating right now", test.detail, "would die with it"} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("cria printed %q, want it to contain %q", errOut, want)
				}
			}
			if len(fake.displaced) != 1 || len(fake.restored) != 1 {
				t.Errorf("cria displaced %v and restored %+v, want the swap to have gone ahead", fake.displaced, fake.restored)
			}
			// The warning is an aside; the answer a script reads stays on stdout.
			if strings.Contains(out.String(), "cannot tell") {
				t.Errorf("cria printed the warning on stdout: %q", out)
			}
		})
	}
}

// --ignore-busy is the operator's word that a generation may die: the busy
// verdict is still read and reported, and the swap goes ahead over it.
func TestIgnoreBusyStopsAHolderMidAnswer(t *testing.T) {
	for _, args := range [][]string{{"qwen", ignoreBusyFlag}, {ignoreBusyFlag, "qwen"}} {
		fake := greenTarget(heldServer("gemma", nil))
		fake.generation = serve.Generation{Busy: serve.BusyGenerating}
		app, out, errOut := newTestApp(testTree(), fake)

		if code := app.validate(args); code != exitOK {
			t.Fatalf("`cria validate %s` exited %d, want %d (stderr: %s)",
				strings.Join(args, " "), code, exitOK, errOut)
		}
		if want := []string{"gemma", "qwen"}; !slices.Equal(fake.stopped, want) {
			t.Errorf("cria stopped %v, want %v: the busy holder goes off the port anyway", fake.stopped, want)
		}
		if len(fake.restored) != 1 || fake.restored[0].EntryID != "gemma" {
			t.Errorf("cria put back %+v, want gemma back on its port", fake.restored)
		}
		for _, want := range []string{"gemma is answering a request on port 8080 right now", ignoreBusyFlag, "stops it mid-answer"} {
			if !strings.Contains(errOut.String(), want) {
				t.Errorf("cria printed %q, want it to contain %q", errOut, want)
			}
		}
		if !strings.Contains(out.String(), "validated qwen") {
			t.Errorf("cria printed %q, want the verdict", out)
		}
	}
}

// The override lifts the busy gate and nothing else: the two refusals that are
// not about anybody's patience stand under it, and the machine is untouched.
func TestIgnoreBusyLiftsTheBusyGateAlone(t *testing.T) {
	elsewhere := testRecord()
	elsewhere.Port = 9999

	cases := []struct {
		name string
		fake *fakeServers
		want string
	}{
		{
			name: "the port is held by a process cria did not start",
			fake: &fakeServers{
				record: testRecord(),
				use: serve.PortUse{Holders: []serve.Holder{{
					PID:        9001,
					Command:    "/opt/homebrew/bin/llama-server -m gemma.gguf --port 8080",
					WorkingDir: "/Users/someone/models",
				}}},
			},
			want: "port 8080 is held by a process cria did not start",
		},
		{
			name: "the target is already running on another port",
			fake: func() *fakeServers {
				fake := greenTarget(nil)
				fake.listing = serve.Listing{Servers: []serve.Server{{Record: elsewhere, Live: true}}}
				return fake
			}(),
			want: "qwen is already running as pid 4242 on port 9999",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			app, _, errOut := newTestApp(testTree(), test.fake)

			if code := app.validate([]string{"qwen", ignoreBusyFlag}); code != exitRefused {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitRefused, errOut)
			}
			if len(test.fake.stopped) != 0 || len(test.fake.started) != 0 {
				t.Errorf("a refused validation stopped %v and started %+v", test.fake.stopped, test.fake.started)
			}
			if !strings.Contains(errOut.String(), test.want) {
				t.Errorf("cria printed %q, want it to contain %q", errOut, test.want)
			}
		})
	}
}

// Ctrl-C during the wait abandons the target and still puts the holder back: the
// half-swapped machine is the one state validate must never leave behind.
func TestValidateInterruptedMidWaitStillRestores(t *testing.T) {
	fake := greenTarget(heldServer("gemma", nil))
	fake.phases = []serve.Phase{serve.PhaseStarting}
	app, out, errOut := newTestApp(testTree(), fake)

	// The operator hits Ctrl-C once the wait is under way: the watch is asked
	// once before the start and once per observation.
	asked, disarmed := 0, false
	app.interrupts = func() (func() bool, func()) {
		return func() bool {
			asked++
			return asked > 3
		}, func() { disarmed = true }
	}

	if code := app.validate([]string{"qwen"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
	}
	if fake.observed == 0 {
		t.Error("the wait never observed the target before it was interrupted")
	}
	if len(fake.proved) != 0 {
		t.Errorf("cria proved %v after the operator interrupted it", fake.proved)
	}
	if want := []string{"gemma", "qwen"}; !slices.Equal(fake.stopped, want) {
		t.Errorf("cria stopped %v, want %v: the target it abandoned goes off the port", fake.stopped, want)
	}
	if len(fake.restored) != 1 || fake.restored[0].EntryID != "gemma" {
		t.Fatalf("cria put back %+v, want gemma back on its port", fake.restored)
	}
	if !strings.Contains(out.String(), "restored gemma") {
		t.Errorf("cria printed %q, want the restore it performed", out)
	}
	if got := lastLine(errOut.String()); !strings.Contains(got, "interrupted while waiting for it to serve") {
		t.Errorf("cria's last line is %q, want it to say the validation was interrupted", got)
	}
	if !disarmed {
		t.Error("cria kept the interrupt watch armed after the protocol ended")
	}
}
