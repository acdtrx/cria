package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"cria/internal/serve"
)

// measuredSize is one size a scripted sweep came back with: three runs around
// the rates given, so the table has a mean and a spread to draw.
func measuredSize(tokens int, prefill, decode float64) serve.BenchSize {
	size := serve.BenchSize{Tokens: tokens}
	for _, offset := range []float64{-10, 0, 10} {
		size.Runs = append(size.Runs, serve.BenchRun{
			PromptTokens: tokens + 1,
			GenTokens:    256,
			TTFT:         120 * time.Millisecond,
			Decode:       3 * time.Second,
			PrefillRate:  prefill + offset,
			DecodeRate:   decode + offset/100,
		})
	}
	size.Mean = serve.BenchMean{
		PromptTokens: float64(tokens + 1),
		GenTokens:    256,
		TTFT:         120 * time.Millisecond,
		PrefillRate:  prefill,
		DecodeRate:   decode,
	}
	return size
}

// refusedSize is a size the server had no room for.
func refusedSize(tokens int) serve.BenchSize {
	return serve.BenchSize{
		Tokens: tokens,
		Err:    errors.New("16384 tokens: http://127.0.0.1:8080/v1/completions answered 400 Bad Request: exceeds the available context size"),
	}
}

// The table is one row per size: what was asked for, what the server counted,
// and the two rates with the spread of the runs behind each.
func TestBenchReportsATable(t *testing.T) {
	fake := &fakeServers{
		listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
		benchSizes: []serve.BenchSize{measuredSize(16, 1200, 78.4), measuredSize(4096, 3800, 72.1)},
	}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.bench(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	for _, want := range []string{
		"qwen  llama  unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL  pid 4242 on 0.0.0.0:8080",
		fmt.Sprintf("%d/4096/16384 tokens of prompt, 3 runs each, 256 tokens generated per run", serve.BenchMinSize),
		"size  tokens  prefill t/s       ttft   decode t/s",
		"16    17      1200 (1190–1210)  120ms  78.4 (78.3–78.5)",
		"4096  4097    3800 (3790–3810)  120ms  72.1 (72.0–72.2)",
		"120ms",
		"78.4 (78.3–78.5)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("cria printed:\n%s\nwant it to contain %q", out, want)
		}
	}

	// Nothing about a benchmark's own timings comes from the server's
	// instrumentation: the table is what cria measured on its own socket.
	if strings.Contains(out.String(), "timings") {
		t.Errorf("the table quotes the server's own timings:\n%s", out)
	}
}

// The wait's progress is stderr's, and the table is stdout's: a script reading
// `cria bench` reads the answer alone (docs/specs/CLI.md).
func TestBenchProgressGoesToStderr(t *testing.T) {
	fake := &fakeServers{
		listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
		benchSizes: []serve.BenchSize{measuredSize(16, 1200, 78.4)},
		benchSteps: []serve.BenchStep{
			{Warmup: true},
			{Size: 4096, Nth: 2, Sizes: 3, Run: 2, Runs: 3},
		},
	}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.bench(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	for _, want := range []string{"benchmarking qwen", "warming up", "4096 tokens (size 2/3), run 2/3"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("cria said %q on stderr, want it to contain %q", errOut, want)
		}
	}
	for _, leaked := range []string{"benchmarking qwen", "warming up", "run 2/3"} {
		if strings.Contains(out.String(), leaked) {
			t.Errorf("the progress leaked onto stdout:\n%s", out)
		}
	}
}

// A size the server refused is printed with the rest — the sweep measured what
// it could — and the exit code says the answer is partial (docs/specs/CLI.md).
func TestBenchExitsNonZeroWhenASizeWasNotMeasured(t *testing.T) {
	fake := &fakeServers{
		listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
		benchSizes: []serve.BenchSize{measuredSize(16, 1200, 78.4), refusedSize(16384)},
	}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.bench(nil); code != exitFailure {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
	}
	for _, want := range []string{"16     17", "16384  —", "not measured — 16384 tokens:", "exceeds the available context size"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("cria printed:\n%s\nwant it to contain %q", out, want)
		}
	}
}

// `cria bench` with nothing named has the same three cases `cria stop` has, and
// only one of them measures anything (docs/specs/SERVE.md).
func TestBenchWithNoEntryNamed(t *testing.T) {
	cases := []struct {
		name     string
		listing  serve.Listing
		want     int
		benched  []string
		contains string
	}{
		{
			name:     "one server running: it is measured",
			listing:  serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
			want:     exitOK,
			benched:  []string{"qwen"},
			contains: "qwen",
		},
		{
			name: "several running: the id is required",
			listing: serve.Listing{Servers: []serve.Server{
				serverNamed("gemma", 11, true),
				serverNamed("qwen", 4242, true),
			}},
			want:     exitFailure,
			contains: "2 servers are running (gemma, qwen); name the one to measure: cria bench <id>",
		},
		{
			name:     "nothing running: nothing to measure",
			listing:  serve.Listing{},
			want:     exitFailure,
			contains: "nothing is running",
		},
		{
			name:     "nothing running, but a crash report remains",
			listing:  serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, false)}},
			want:     exitFailure,
			contains: "nothing is running (1 exited record(s) remain",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{listing: test.listing, benchSizes: []serve.BenchSize{measuredSize(16, 1200, 78.4)}}
			app, out, errOut := newTestApp(testTree(), fake)

			if code := app.bench(nil); code != test.want {
				t.Errorf("exit code %d, want %d (stderr: %s)", code, test.want, errOut)
			}
			if !slices.Equal(fake.benched, test.benched) {
				t.Errorf("cria measured %v, want %v", fake.benched, test.benched)
			}
			printed := out.String() + errOut.String()
			if !strings.Contains(printed, test.contains) {
				t.Errorf("cria printed %q, want it to contain %q", printed, test.contains)
			}
		})
	}
}

// A named server is measured whatever else is running; one cria never started,
// or one whose process is gone, is refused rather than guessed at.
func TestBenchNamingAServer(t *testing.T) {
	listing := serve.Listing{Servers: []serve.Server{
		serverNamed("gemma", 11, true),
		serverNamed("qwen", 4242, true),
		serverNamed("phi", 77, false),
	}}

	fake := &fakeServers{listing: listing, benchSizes: []serve.BenchSize{measuredSize(16, 1200, 78.4)}}
	app, _, errOut := newTestApp(testTree(), fake)
	if code := app.bench([]string{"gemma"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if !slices.Equal(fake.benched, []string{"gemma"}) {
		t.Errorf("cria measured %v, want the server that was named", fake.benched)
	}

	fake = &fakeServers{listing: listing}
	app, _, errOut = newTestApp(testTree(), fake)
	if code := app.bench([]string{"phi"}); code != exitFailure {
		t.Errorf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "phi is not running") {
		t.Errorf("cria printed %q, want it to say the named server is not running", errOut)
	}

	fake = &fakeServers{listing: listing}
	app, _, errOut = newTestApp(testTree(), fake)
	if code := app.bench([]string{"nope"}); code != exitFailure {
		t.Errorf("exit code %d, want %d", code, exitFailure)
	}
	if !strings.Contains(errOut.String(), "cria has no server record for nope") {
		t.Errorf("cria printed %q, want it to say there is no such record", errOut)
	}
	if len(fake.benched) != 0 {
		t.Errorf("a refused bench measured %v", fake.benched)
	}
}

// The flags decide what is measured, in either spelling, and a size too small
// to mean anything is raised to the smallest rung with the correction said out
// loud rather than sent as a degenerate prompt.
func TestBenchFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want serve.BenchSpec
		note string
	}{
		{
			name: "no flags run the default sweep",
			args: nil,
			want: serve.DefaultBenchSpec(),
		},
		{
			name: "the sweep is the caller's to choose",
			args: []string{"--sizes", "512,8192", "--runs", "5", "--gen", "64"},
			want: serve.BenchSpec{Sizes: []int{512, 8192}, Runs: 5, GenTokens: 64},
		},
		{
			name: "a flag joined by = is the same flag",
			args: []string{"--sizes=512", "--runs=1"},
			want: serve.BenchSpec{Sizes: []int{512}, Runs: 1, GenTokens: 256},
		},
		{
			name: "a size below the smallest rung is raised to it",
			args: []string{"--sizes", "0,4096"},
			want: serve.BenchSpec{Sizes: []int{serve.BenchMinSize, 4096}, Runs: 3, GenTokens: 256},
			note: fmt.Sprintf("measuring the smallest rung (%d tokens) instead", serve.BenchMinSize),
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{
				listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
				benchSizes: []serve.BenchSize{measuredSize(16, 1200, 78.4)},
			}
			app, _, errOut := newTestApp(testTree(), fake)

			if code := app.bench(test.args); code != exitOK {
				t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
			}
			if len(fake.specs) != 1 {
				t.Fatalf("cria ran %d sweeps, want one", len(fake.specs))
			}
			got := fake.specs[0]
			if !slices.Equal(got.Sizes, test.want.Sizes) || got.Runs != test.want.Runs || got.GenTokens != test.want.GenTokens {
				t.Errorf("cria measured %+v, want %+v", got, test.want)
			}
			if test.note != "" && !strings.Contains(errOut.String(), test.note) {
				t.Errorf("cria said %q, want it to carry %q", errOut, test.note)
			}
		})
	}
}

// A command line cria cannot route is refused with the flag and what it takes,
// and nothing is measured.
func TestBenchRefusesACommandLineItCannotRoute(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		contains string
	}{
		{name: "an unknown flag", args: []string{"--fast"}, contains: "unknown flag --fast"},
		{name: "a flag with no value", args: []string{"--runs"}, contains: "--runs takes a value"},
		{name: "a size that is not a number", args: []string{"--sizes", "big"}, contains: `--sizes: "big" is not a number of tokens`},
		{name: "an empty size list", args: []string{"--sizes", ""}, contains: "--sizes takes prompt sizes in tokens"},
		{name: "no runs at all", args: []string{"--runs", "0"}, contains: "--runs: 0 is less than 1"},
		{name: "too few tokens to time a decode", args: []string{"--gen", "1"}, contains: "--gen: 1 is less than 2"},
		{name: "two servers named", args: []string{"qwen", "gemma"}, contains: "one server at a time (got qwen, gemma)"},
		{name: "a value on --json", args: []string{"--json=yes"}, contains: "--json takes no value"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeServers{listing: serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}}}
			app, _, errOut := newTestApp(testTree(), fake)

			if code := app.bench(test.args); code != exitUsage {
				t.Errorf("exit code %d, want %d", code, exitUsage)
			}
			if !strings.Contains(errOut.String(), test.contains) {
				t.Errorf("cria printed %q, want it to contain %q", errOut, test.contains)
			}
			if !strings.Contains(errOut.String(), benchUsage) {
				t.Errorf("cria printed %q, want the usage line with it", errOut)
			}
			if len(fake.benched) != 0 {
				t.Errorf("a refused command line measured %v", fake.benched)
			}
		})
	}
}

// The JSON document is the machine contract (docs/specs/CLI.md): stable field
// names, every field present, both lists always lists — and a size that failed
// carries its reason in the same shape as one that did not.
func TestBenchJSONDocument(t *testing.T) {
	fake := &fakeServers{
		listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
		benchSizes: []serve.BenchSize{measuredSize(4096, 3800, 72.5), refusedSize(16384)},
	}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.bench([]string{"--json"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(out.String()), &document); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, out)
	}
	for field, want := range map[string]any{
		"entry":   "qwen",
		"backend": "llama",
		"repo":    "unsloth/Qwen3-30B-A3B-GGUF",
		"quant":   "UD-Q4_K_XL",
		"host":    "0.0.0.0",
		"port":    float64(8080),
		"pid":     float64(4242),
	} {
		if got := document[field]; got != want {
			t.Errorf("%s is %#v, want %#v", field, got, want)
		}
	}

	spec, ok := document["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec is %#v, want an object", document["spec"])
	}
	if spec["runs"] != float64(3) || spec["gen_tokens"] != float64(256) {
		t.Errorf("spec is %#v, want the sweep that ran", spec)
	}

	sizes, ok := document["sizes"].([]any)
	if !ok || len(sizes) != 2 {
		t.Fatalf("sizes is %#v, want the two sizes the sweep held", document["sizes"])
	}

	measured, ok := sizes[0].(map[string]any)
	if !ok {
		t.Fatalf("the size is %#v, want an object", sizes[0])
	}
	if measured["size"] != float64(4096) || measured["error"] != "" {
		t.Errorf("the measured size is %#v, want it named with no error", measured)
	}
	runs, ok := measured["runs"].([]any)
	if !ok || len(runs) != 3 {
		t.Fatalf("runs is %#v, want the three runs behind the mean", measured["runs"])
	}
	for _, shape := range append([]any{measured["mean"]}, runs...) {
		row, ok := shape.(map[string]any)
		if !ok {
			t.Fatalf("a run is %#v, want an object", shape)
		}
		for _, field := range []string{
			"prompt_tokens", "cached_tokens", "gen_tokens", "ttft_seconds",
			"prefill_tokens_per_second", "decode_tokens_per_second",
		} {
			if _, present := row[field]; !present {
				t.Errorf("a run has no %q; the document's fields are always present", field)
			}
		}
	}
	if mean := measured["mean"].(map[string]any); mean["prefill_tokens_per_second"] != 3800.0 {
		t.Errorf("the mean prefill rate is %#v, want 3800", mean["prefill_tokens_per_second"])
	}

	refused, ok := sizes[1].(map[string]any)
	if !ok {
		t.Fatalf("the refused size is %#v, want an object", sizes[1])
	}
	if refused["error"] == "" {
		t.Errorf("the refused size carries no reason: %#v", refused)
	}
	// A size with nothing to report is still a list, never a null: a script
	// iterates it rather than testing for one.
	if runs, ok := refused["runs"].([]any); !ok || runs == nil {
		t.Errorf("the refused size's runs are %#v, want an empty list", refused["runs"])
	}

	// cria's own bookkeeping stays out of the machine contract.
	if _, leaked := document["identity"]; leaked {
		t.Errorf("the document carries the process identity: %#v", document["identity"])
	}
}

// A run the model ended after a single token has no decode rate, so it is
// neither averaged nor drawn as one — and a model that stopped short of what
// was asked for is said out loud, because that is what makes a thin number
// thin.
func TestBenchReportsWhatTheModelActuallyWrote(t *testing.T) {
	short := measuredSize(4096, 3800, 72.5)
	short.Runs[0] = serve.BenchRun{PromptTokens: 4097, GenTokens: 1, TTFT: 120 * time.Millisecond, PrefillRate: 3790}
	short.Mean.GenTokens = 171
	short.Mean.DecodeRate = 72.5

	stopped := serve.BenchSize{
		Tokens: 16,
		Runs:   []serve.BenchRun{{PromptTokens: 17, GenTokens: 1, TTFT: 12 * time.Millisecond, PrefillRate: 1400}},
		Mean:   serve.BenchMean{PromptTokens: 17, GenTokens: 1, TTFT: 12 * time.Millisecond, PrefillRate: 1400},
	}

	fake := &fakeServers{
		listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
		benchSizes: []serve.BenchSize{stopped, short},
	}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.bench(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	// The size where nothing decoded shows no decode rate rather than a zero.
	if !strings.Contains(out.String(), "16    17      1400              12ms   —") {
		t.Errorf("cria printed:\n%s\nwant the size with no decode measurement to show none", out)
	}
	// The size where one of three runs stopped shows the two that did.
	if !strings.Contains(out.String(), "72.5 (72.5–72.6)") {
		t.Errorf("cria printed:\n%s\nwant the decode spread over the runs that measured one", out)
	}
	if !strings.Contains(errOut.String(), "the model ended its answer early on the 4096-token size (171 of 256 tokens on average)") {
		t.Errorf("cria said %q, want the early stop called out", errOut)
	}
}

// A story that runs out a handful of tokens short of the count is what a model
// writing prose does, and it says nothing: the rate was measured over every run
// and over almost every token of them, so a note would only be one more true
// thing to read.
func TestBenchSaysNothingAboutAnAnswerThatEndedJustShort(t *testing.T) {
	short := measuredSize(4096, 3800, 72.5)
	for i := range short.Runs {
		short.Runs[i].GenTokens = 250
	}
	short.Mean.GenTokens = 250

	fake := &fakeServers{
		listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
		benchSizes: []serve.BenchSize{short},
	}
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.bench(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if strings.Contains(errOut.String(), "ended its answer early") {
		t.Errorf("cria said %q about 250 of 256 tokens, want the line kept for a real shortfall", errOut)
	}
}

// A sweep that was answered out of the server's prompt cache says so: part of
// the prefill never happened, so the rate above it is not the rate of reading
// that prompt.
func TestBenchFlagsACachedPrefill(t *testing.T) {
	cached := measuredSize(4096, 3800, 72.5)
	cached.Runs[1].CachedTokens = 4000
	fake := &fakeServers{
		listing:    serve.Listing{Servers: []serve.Server{serverNamed("qwen", 4242, true)}},
		benchSizes: []serve.BenchSize{cached},
	}
	app, _, errOut := newTestApp(testTree(), fake)

	if code := app.bench(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if !strings.Contains(errOut.String(), "out of its prompt cache") {
		t.Errorf("cria said %q, want the cache hit called out", errOut)
	}
}
