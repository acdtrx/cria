package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"cria/internal/config"
	"cria/internal/serve"
)

// measuredSize is one size a scripted sweep came back with.
func measuredSize(tokens int, prefill, decode float64) serve.BenchSize {
	return serve.BenchSize{
		Tokens: tokens,
		Runs: []serve.BenchRun{{
			PromptTokens: tokens + 1,
			GenTokens:    256,
			TTFT:         120 * time.Millisecond,
			Decode:       3 * time.Second,
			PrefillRate:  prefill,
			DecodeRate:   decode,
		}},
		Mean: serve.BenchMean{
			PromptTokens: float64(tokens + 1),
			GenTokens:    256,
			TTFT:         120 * time.Millisecond,
			PrefillRate:  prefill,
			DecodeRate:   decode,
		},
	}
}

// refusedSize is a size the server had no room for.
func refusedSize(tokens int) serve.BenchSize {
	return serve.BenchSize{
		Tokens: tokens,
		Err:    errors.New("16384 tokens: http://127.0.0.1:8080/v1/completions answered 400 Bad Request: exceeds the available context size"),
	}
}

// benchedResult is one finished sweep in the session's log.
func benchedResult(id string, at time.Time, sizes ...serve.BenchSize) serve.BenchResult {
	return serve.BenchResult{
		Record: serve.Record{
			EntryID: id, Backend: config.BackendLlama,
			Repo: "unsloth/Qwen3-30B-A3B-GGUF", Quant: "UD-Q4_K_XL", Host: "0.0.0.0", Port: 8080,
		},
		StartedAt: at,
		Spec:      serve.DefaultBenchSpec(),
		Sizes:     sizes,
	}
}

// benchPane is a frame with the pane open over a box holding these servers.
func benchPane(t *testing.T, servers ...serve.Status) (model, *fakeServers) {
	t.Helper()
	frame, fake := pickFrame(t, servers...)
	frame, _ = press(t, frame, typed('b'))
	if !frame.benchOpen {
		t.Fatal("the bench key did not open the pane")
	}
	return frame, fake
}

// took is one message through Update, back as a model — a command's answer, the
// way the program delivers it.
func took(t *testing.T, frame model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := frame.Update(msg)
	taken, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned a %T, want the frame's own model", next)
	}
	return taken, cmd
}

// benchCommands is what ⏎ fired: the sweep, and the reader that follows its
// progress. They come back as commands rather than as answers so a test can
// deliver a step before the result, the way they actually arrive.
func benchCommands(t *testing.T, cmd tea.Cmd) (sweep, reader tea.Cmd) {
	t.Helper()
	batch, ok := run(t, cmd).(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("⏎ fired %T, want the sweep and the command that reads its progress", run(t, cmd))
	}
	return batch[0], batch[1]
}

// An empty pane says what it is and what starts one: the log is this session's,
// so a fresh cria always opens on this.
func TestBenchPaneIsEmptyUntilSomethingIsMeasured(t *testing.T) {
	frame, _ := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))

	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "no benches yet — ⏎ starts one") {
		t.Errorf("the empty pane does not say how to fill it:\n%s", drawn)
	}
	// The pane is over the view, and the box above it still says what is running.
	if !strings.Contains(drawn, statusTitle) {
		t.Errorf("the bench pane took the status box with it:\n%s", drawn)
	}
	if strings.Contains(drawn, detailTitle) {
		t.Errorf("the bench pane was drawn beside the view rather than over it:\n%s", drawn)
	}
}

// A finished sweep is a heading and a table: which server, which model, when it
// was taken, and one row per size.
func TestBenchPaneDrawsAResult(t *testing.T) {
	frame, _ := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))
	frame.benchLog = []serve.BenchResult{benchedResult("qwen", time.Date(2026, 8, 19, 9, 30, 15, 0, time.UTC),
		measuredSize(16, 1200, 78.4), measuredSize(4096, 3800, 72.1))}

	drawn := plain(frame.View().Content)
	for _, want := range []string{
		"qwen  llama  unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL  09:30:15",
		"size  tokens  prefill t/s  ttft   decode t/s",
		"16    17      1200         120ms  78.4",
		"4096  4097    3800         120ms  72.1",
	} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the pane does not carry %q:\n%s", want, drawn)
		}
	}
}

// Every sweep of the session is kept, newest at the bottom: the pane is read
// down to the measurement that was just taken.
func TestBenchPaneKeepsTheSessionsSweepsInOrder(t *testing.T) {
	frame, _ := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))
	frame.benchLog = []serve.BenchResult{
		benchedResult("qwen", time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC), measuredSize(16, 1200, 78.4)),
		benchedResult("gemma", time.Date(2026, 8, 19, 9, 41, 0, 0, time.UTC), measuredSize(16, 900, 41.2)),
	}

	drawn := plain(frame.View().Content)
	first, second := strings.Index(drawn, "qwen  llama"), strings.Index(drawn, "gemma  llama")
	if first < 0 || second < 0 {
		t.Fatalf("the pane does not carry both sweeps:\n%s", drawn)
	}
	if second < first {
		t.Errorf("the newest sweep is drawn above the older one:\n%s", drawn)
	}
	if !strings.Contains(drawn, "09:41:00") {
		t.Errorf("the pane does not say when the newest sweep was taken:\n%s", drawn)
	}
}

// A size the server refused keeps its row with the numbers left out, and its
// reason underneath — the pane is where a partial answer is read.
func TestBenchPaneCarriesTheReasonASizeWasNotMeasured(t *testing.T) {
	frame, _ := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))
	frame.benchLog = []serve.BenchResult{benchedResult("qwen", time.Now(),
		measuredSize(16, 1200, 78.4), refusedSize(16384))}

	drawn := plain(frame.View().Content)
	for _, want := range []string{"16384  —", "exceeds the available context size"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the pane does not carry %q:\n%s", want, drawn)
		}
	}
}

// ⏎ with one server running measures that one, and the pane draws the sweep as
// it goes: the frame keeps redrawing while minutes of work run behind it.
func TestBenchPaneRunsTheSweepAndDrawsItsProgress(t *testing.T) {
	frame, fake := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))
	fake.benchAt = time.Date(2026, 8, 19, 9, 30, 15, 0, time.UTC)
	fake.benchSizes = []serve.BenchSize{measuredSize(16, 1200, 78.4)}
	fake.benchSteps = []serve.BenchStep{
		{Warmup: true},
		{Size: 4096, Nth: 2, Sizes: 3, Run: 2, Runs: 3},
	}

	frame, cmd := press(t, frame, enter)
	if frame.benching == nil || frame.benching.entryID != "qwen" {
		t.Fatalf("⏎ left %+v in flight, want the only running server being measured", frame.benching)
	}
	if drawn := plain(frame.View().Content); !strings.Contains(drawn, "qwen  benchmarking…") {
		t.Errorf("the pane does not draw the sweep it just started:\n%s", drawn)
	}

	sweep, reader := benchCommands(t, cmd)
	done := sweep()

	// The steps arrive one at a time, each arming the read of the next.
	frame, next := took(t, frame, reader())
	if drawn := plain(frame.View().Content); !strings.Contains(drawn, "warming up (unmeasured)") {
		t.Errorf("the pane does not draw the warmup:\n%s", drawn)
	}
	frame, next = took(t, frame, run(t, next))
	if drawn := plain(frame.View().Content); !strings.Contains(drawn, "4096 tokens (size 2/3), run 2/3") {
		t.Errorf("the pane does not draw where the sweep has got to:\n%s", drawn)
	}

	frame, _ = took(t, frame, done)
	if frame.benching != nil {
		t.Errorf("the sweep is still drawn as running after its result landed: %+v", frame.benching)
	}
	if len(fake.benched) != 1 || fake.benched[0] != "qwen" {
		t.Fatalf("cria measured %v, want the running server once", fake.benched)
	}
	// The pane always runs the default sweep: choosing sizes is what the CLI's
	// flags are for.
	if got := fake.specs[0]; len(got.Sizes) != len(serve.DefaultBenchSpec().Sizes) || got.Runs != serve.DefaultBenchSpec().Runs {
		t.Errorf("the pane ran %+v, want the default sweep", got)
	}

	drawn := plain(frame.View().Content)
	if !strings.Contains(drawn, "qwen  llama  unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL  09:30:15") {
		t.Errorf("the result is not in the log:\n%s", drawn)
	}
	if strings.Contains(drawn, "benchmarking…") {
		t.Errorf("the pane still draws the sweep as running:\n%s", drawn)
	}
	// The result is on screen, so nothing is said under the status box about it
	// (docs/specs/TUI.md).
	if frame.alert.text != "" {
		t.Errorf("the finished sweep left %q under the box while its table is on screen", frame.alert.text)
	}
}

// Several servers running make ⏎ ask which one, like every other key that could
// mean more than one (docs/specs/TUI.md).
func TestBenchPaneAsksWhichServerWhenSeveralAreRunning(t *testing.T) {
	frame, fake := benchPane(t,
		serverNamed("qwen", 8080, serve.PhaseRunning),
		serverNamed("gemma", 8081, serve.PhaseRunning),
		serverNamed("phi", 8082, serve.PhaseExited))
	fake.benchSizes = []serve.BenchSize{measuredSize(16, 1200, 78.4)}

	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Fatal("⏎ with two servers to choose between measured one of them")
	}
	if frame.pick == nil || frame.pick.action != pickBench {
		t.Fatalf("⏎ armed %+v, want the bench waiting for its server", frame.pick)
	}
	if want := "which server to bench"; frame.alert.text != want {
		t.Errorf("the line under the box reads %q, want %q", frame.alert.text, want)
	}
	bar := plain(renderKeybar(200, frame.groups()...))
	for _, hint := range []string{"bench which", "⏎ bench", "esc cancel"} {
		if !strings.Contains(bar, hint) {
			t.Errorf("the keybar reads %q, want it to offer %q", bar, hint)
		}
	}

	// A crash report is not something to measure: the cursor moves between the
	// servers cria can still see.
	if got := pickedID(t, frame); got != "qwen" {
		t.Fatalf("the cursor started on %q, want the first running server", got)
	}
	frame, _ = press(t, frame, typed('j'))
	if got := pickedID(t, frame); got != "gemma" {
		t.Errorf("j moved the cursor to %q, want the other running server", got)
	}
	frame, _ = press(t, frame, typed('j'))
	if got := pickedID(t, frame); got != "gemma" {
		t.Errorf("the cursor moved onto %q; a crash report cannot answer a completion", got)
	}

	frame, cmd = press(t, frame, enter)
	if frame.pick != nil || frame.alert.text != "" {
		t.Errorf("the question outlived its answer: %+v, %q", frame.pick, frame.alert.text)
	}
	if frame.benching == nil || frame.benching.entryID != "gemma" {
		t.Fatalf("the pick started %+v, want the server it landed on", frame.benching)
	}
	if !frame.benchOpen {
		t.Error("answering the pick closed the pane it was asked from")
	}

	sweep, _ := benchCommands(t, cmd)
	frame, _ = took(t, frame, sweep())
	if len(fake.benched) != 1 || fake.benched[0] != "gemma" {
		t.Errorf("cria measured %v, want the picked server once", fake.benched)
	}
}

// One sweep at a time: two against one host would each be timing the other's
// work, so the second ⏎ is refused rather than queued.
func TestBenchPaneRefusesASecondSweep(t *testing.T) {
	frame, fake := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))

	frame, _ = press(t, frame, enter)
	running := frame.benching
	if running == nil {
		t.Fatal("⏎ started nothing")
	}

	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Error("the second ⏎ started another sweep")
	}
	if frame.alert.text != "a bench is already running" {
		t.Errorf("the line under the box reads %q, want the refusal", frame.alert.text)
	}
	if frame.benching != running {
		t.Errorf("the refused ⏎ replaced the sweep in flight: %+v", frame.benching)
	}
	if len(fake.benched) != 0 {
		t.Errorf("the refused ⏎ measured %v", fake.benched)
	}
}

// With nothing running there is nothing to measure, and the key says so rather
// than doing nothing: the pane exists to start benches.
func TestBenchPaneSaysWhenThereIsNothingToMeasure(t *testing.T) {
	frame, fake := benchPane(t)

	frame, cmd := press(t, frame, enter)
	if cmd != nil {
		t.Error("⏎ ran something with no server to measure")
	}
	if !strings.Contains(frame.alert.text, "start a server first") {
		t.Errorf("the line under the box reads %q, want it to say what is missing", frame.alert.text)
	}
	if len(fake.benched) != 0 {
		t.Errorf("cria measured %v with nothing running", fake.benched)
	}

	// An exited record is no more measurable than an empty state directory.
	frame = frame.observed(snapshotMsg{listing: serve.StatusListing{
		Servers: []serve.Status{serverNamed("qwen", 8080, serve.PhaseExited)},
	}})
	frame, _ = press(t, frame, enter)
	if !strings.Contains(frame.alert.text, "start a server first") {
		t.Errorf("the line under the box reads %q with only a crash report on screen", frame.alert.text)
	}
}

// Closing the pane does not stop the sweep — it is minutes of work and it is not
// the pane's — and its result is announced under the box, which is the one thing
// the boxes cannot show (docs/specs/TUI.md).
func TestASweepOutLivesThePaneAndSaysSoWhenItLands(t *testing.T) {
	for _, closing := range []tea.KeyPressMsg{escape, typed('b')} {
		t.Run(closing.String(), func(t *testing.T) {
			frame, fake := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))
			fake.benchSizes = []serve.BenchSize{measuredSize(16, 1200, 78.4)}

			frame, cmd := press(t, frame, enter)
			frame, _ = press(t, frame, closing)
			if frame.benchOpen {
				t.Fatal("the pane stayed up")
			}
			if frame.benching == nil {
				t.Fatal("closing the pane abandoned the sweep")
			}

			sweep, _ := benchCommands(t, cmd)
			frame, _ = took(t, frame, sweep())

			if len(frame.benchLog) != 1 {
				t.Fatalf("the log holds %d results, want the one that finished off screen", len(frame.benchLog))
			}
			if !strings.Contains(frame.alert.text, "benchmarked qwen") || frame.alert.bad {
				t.Errorf("the line under the box reads %q, want the completion announced", frame.alert.text)
			}
			if drawn := plain(frame.View().Content); !strings.Contains(drawn, "benchmarked qwen") {
				t.Errorf("the frame does not announce the finished sweep:\n%s", drawn)
			}
		})
	}
}

// A sweep that finished off screen with a size it could not measure says that
// too, and says it as a failure.
func TestASweepThatCouldNotMeasureEverythingSaysSo(t *testing.T) {
	frame, fake := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))
	fake.benchSizes = []serve.BenchSize{measuredSize(16, 1200, 78.4), refusedSize(16384)}

	frame, cmd := press(t, frame, enter)
	frame, _ = press(t, frame, escape)
	sweep, _ := benchCommands(t, cmd)
	frame, _ = took(t, frame, sweep())

	if !strings.Contains(frame.alert.text, "1 size(s) could not be measured") || !frame.alert.bad {
		t.Errorf("the line under the box reads %q, want the partial answer reported as one", frame.alert.text)
	}
}

// The pane holds the keyboard while it is up, and the bar says exactly what
// works there (docs/specs/TUI.md).
func TestBenchPaneHoldsTheKeyboard(t *testing.T) {
	frame, fake := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))

	bar := plain(renderKeybar(200, frame.groups()...))
	if !strings.Contains(bar, benchScope+" ⏎ bench · esc close") {
		t.Errorf("the keybar reads %q, want the pane's own keys while it is up", bar)
	}
	for _, hidden := range []string{"s stop", "⏎ start", "c cache", "t tools"} {
		if strings.Contains(bar, hidden) {
			t.Errorf("the keybar offers %q from under the bench pane: %q", hidden, bar)
		}
	}

	for _, ignored := range []tea.KeyPressMsg{typed('s'), typed('c'), {Code: tea.KeyTab}, typed('t')} {
		var cmd tea.Cmd
		frame, cmd = press(t, frame, ignored)
		if cmd != nil {
			t.Errorf("%q fired a command from under the bench pane", ignored.String())
		}
	}
	if len(fake.stopped) != 0 || frame.toolsOpen || frame.view != viewServe {
		t.Errorf("a key acted from under the pane: stopped %v, tools %v, view %v", fake.stopped, frame.toolsOpen, frame.view)
	}
	if !frame.benchOpen {
		t.Error("the pane went away by itself")
	}
}

// b is a global key: it works in every view, and it is drawn in the bar that
// says so.
func TestTheBenchKeyIsGlobal(t *testing.T) {
	frame, _ := testFrame(t, &fakeServers{})

	if bar := plain(renderKeybar(200, frame.groups()...)); !strings.Contains(bar, "b bench") {
		t.Errorf("the keybar reads %q, want the bench key in the global scope", bar)
	}

	frame, _ = press(t, frame, typed('c'))
	frame, _ = press(t, frame, typed('b'))
	if !frame.benchOpen {
		t.Fatal("the bench key did not open the pane over the cache view")
	}
	frame, _ = press(t, frame, escape)
	if frame.benchOpen {
		t.Error("esc left the pane up")
	}
	if frame.view != viewCache {
		t.Errorf("esc closed the pane and left the view too: %v", frame.view)
	}
}

// The pane is over both lists, so a tick with it up walks no cache: the walk
// sizes every blob on disk and nothing on screen reads its answer.
func TestATickWithTheBenchPaneUpDoesNotWalkTheCache(t *testing.T) {
	frame, _ := benchPane(t, serverNamed("qwen", 8080, serve.PhaseRunning))
	if frame.walksTheCache() {
		t.Error("a refresh tick walked the cache with the bench pane up")
	}
}
