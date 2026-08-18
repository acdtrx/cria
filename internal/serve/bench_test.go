package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
)

// benchScript is a server's side of one measurement, written down: how long it
// takes before the first token, how fast it streams after that, and in which of
// the two shapes the backends actually use its usage arrives.
//
// Both shapes are real. llama-server hangs usage on the same chunk that carries
// the final (empty) choice; mlx_lm.server sends a chunk of its own with no
// choices at all, after a run of `: keepalive` comments while it prefills
// (verified live, 2026-08-19). A measurement has to read either.
type benchScript struct {
	prefill   time.Duration // before the first token: what a TTFT measures
	interval  time.Duration // between tokens: what a decode rate measures
	tokens    int           // content chunks streamed
	prompt    int           // usage.prompt_tokens
	tokenizer int           // chars per token: when set, usage.prompt_tokens is counted off the prompt instead
	cached    int           // usage.prompt_tokens_details.cached_tokens
	keepalive int           // comment lines sent before the first token
	separate  bool          // usage arrives in a chunk of its own
	noUsage   bool          // no usage at all: the mlx shape when nobody asks for it
	status    int           // the status to answer with instead of a stream
	refusal   string        // the body that comes with it
}

// benchListener is an httptest server speaking one script, holding on to every
// request it was sent.
type benchListener struct {
	*httptest.Server
	sent []benchRequest
}

// newBenchListener stands up that server. The requests arrive one at a time —
// a sweep never has two in flight — so nothing here is guarded.
func newBenchListener(t *testing.T, script benchScript) *benchListener {
	t.Helper()
	listener := &benchListener{}
	listener.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent benchRequest
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("the measurement sent a body the endpoint cannot read: %v", err)
		}
		listener.sent = append(listener.sent, sent)

		if script.status != 0 {
			w.WriteHeader(script.status)
			io.WriteString(w, script.refusal)
			return
		}
		answer := script
		if script.tokenizer > 0 {
			answer.prompt = len(sent.Prompt) / script.tokenizer
		}
		answer.stream(w)
	}))
	t.Cleanup(listener.Close)
	return listener
}

// stream writes the script out as server-sent events, flushing each one so the
// client meets them when the script says it should.
func (s benchScript) stream(w http.ResponseWriter) {
	flush := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	for i := range s.keepalive {
		fmt.Fprintf(w, ": keepalive %d/6\n\n", i+1)
		flush.Flush()
	}
	time.Sleep(s.prefill)

	for i := range s.tokens {
		if i > 0 {
			time.Sleep(s.interval)
		}
		io.WriteString(w, `data: {"choices":[{"text":" token","index":0,"finish_reason":null}]}`+"\n\n")
		flush.Flush()
	}

	usage := fmt.Sprintf(`"usage":{"prompt_tokens":%d,"completion_tokens":%d,"prompt_tokens_details":{"cached_tokens":%d}}`,
		s.prompt, s.tokens, s.cached)
	switch {
	case s.noUsage:
		io.WriteString(w, `data: {"choices":[{"text":"","index":0,"finish_reason":"length"}]}`+"\n\n")
	case s.separate:
		io.WriteString(w, `data: {"choices":[{"text":"","index":0,"finish_reason":"length"}]}`+"\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[],%s}\n\n", usage)
	default:
		fmt.Fprintf(w, "data: {\"choices\":[{\"text\":\"\",\"index\":0,\"finish_reason\":\"length\"}],%s}\n\n", usage)
	}
	flush.Flush()
	io.WriteString(w, "data: [DONE]\n\n")
	flush.Flush()
}

// benchManager is a manager wired to the real bencher, pointed at a listener.
func benchManager(t *testing.T, listener *benchListener, entry config.Entry) (*Manager, Record) {
	t.Helper()
	manager := newManager(t, &fakeHost{})
	manager.bench = newHTTPBench()
	manager.benchWithin = 10 * time.Second
	return manager, recordAt(t, entry, listener.URL)
}

// oneSize is a sweep of a single size, so a test about one measurement waits
// for one measurement.
func oneSize(size, runs int) BenchSpec {
	return BenchSpec{Sizes: []int{size}, Runs: runs, GenTokens: 32}
}

// Where a measurement is sent: the same address rule the probe and the warm
// follow — loopback for a wildcard bind, the bound address otherwise
// (docs/specs/CONFIG.md) — at the documented completion endpoint.
func TestBenchURL(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		want   string
	}{
		{
			name:   "a wildcard bind is measured on loopback",
			record: Record{Backend: config.BackendLlama, Host: "0.0.0.0", Port: 8080},
			want:   "http://127.0.0.1:8080/v1/completions",
		},
		{
			name:   "a bind on the LAN is measured where it listens",
			record: Record{Backend: config.BackendMLX, Host: "192.168.1.10", Port: 9090},
			want:   "http://192.168.1.10:9090/v1/completions",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := benchURL(test.record); got != test.want {
				t.Errorf("the measurement goes to %q, want %q", got, test.want)
			}
		})
	}
}

// One measurement against a server whose timing is known: the prefill rate is
// what the server said it read over the wait for the first token, and the decode
// rate is the tokens after that first one over the window they arrived in.
func TestBenchMeasuresPrefillAndDecodeSeparately(t *testing.T) {
	// 600 prompt tokens read in 100ms is 6000 t/s; 6 tokens streamed 20ms apart
	// puts 5 of them in a 100ms window, which is 50 t/s.
	listener := newBenchListener(t, benchScript{
		prefill:  100 * time.Millisecond,
		interval: 20 * time.Millisecond,
		tokens:   6,
		prompt:   600,
	})
	manager, record := benchManager(t, listener, llamaEntry())

	result := manager.Bench(record, oneSize(512, 1), nil)
	if result.Failed() {
		t.Fatalf("the sweep reported %v, want a measurement", result.Sizes[0].Err)
	}
	run := result.Sizes[0].Runs[0]

	if run.PromptTokens != 600 {
		t.Errorf("the run counted %d prompt tokens, want the 600 the server said it read", run.PromptTokens)
	}
	if run.GenTokens != 6 {
		t.Errorf("the run counted %d generated tokens, want the 6 that were streamed", run.GenTokens)
	}
	if run.TTFT < 100*time.Millisecond || run.TTFT > 400*time.Millisecond {
		t.Errorf("the run measured a TTFT of %s, want roughly the 100ms the server took", run.TTFT)
	}
	if run.PrefillRate < 1500 || run.PrefillRate > 6000 {
		t.Errorf("the prefill rate is %.0f t/s, want roughly 600 tokens over %s", run.PrefillRate, run.TTFT)
	}
	if run.DecodeRate < 25 || run.DecodeRate > 60 {
		t.Errorf("the decode rate is %.1f t/s, want roughly 5 tokens over the ~100ms window", run.DecodeRate)
	}

	// The two rates are ratios of the numbers beside them, so the row a table
	// draws can never disagree with the run behind it.
	if want := float64(run.PromptTokens) / run.TTFT.Seconds(); run.PrefillRate != want {
		t.Errorf("the prefill rate is %.3f, want prompt tokens over TTFT (%.3f)", run.PrefillRate, want)
	}
	if want := float64(run.GenTokens-1) / run.Decode.Seconds(); run.DecodeRate != want {
		t.Errorf("the decode rate is %.3f, want the tokens after the first over their window (%.3f)", run.DecodeRate, want)
	}
}

// The mlx shape reads the same: keepalive comments while the server prefills are
// not tokens, and a usage chunk carrying no choices is still the usage.
func TestBenchReadsTheMLXStreamShape(t *testing.T) {
	listener := newBenchListener(t, benchScript{
		prefill:   80 * time.Millisecond,
		interval:  10 * time.Millisecond,
		tokens:    4,
		prompt:    128,
		keepalive: 3,
		separate:  true,
	})
	manager, record := benchManager(t, listener, mlxEntry())

	result := manager.Bench(record, oneSize(128, 1), nil)
	if result.Failed() {
		t.Fatalf("the sweep reported %v, want a measurement", result.Sizes[0].Err)
	}
	run := result.Sizes[0].Runs[0]

	if run.GenTokens != 4 {
		t.Errorf("the run counted %d generated tokens; the keepalive comments are not tokens", run.GenTokens)
	}
	if run.PromptTokens != 128 {
		t.Errorf("the run counted %d prompt tokens, want the usage chunk that carried no choices", run.PromptTokens)
	}
	if run.TTFT < 80*time.Millisecond {
		t.Errorf("the run measured a TTFT of %s, want the wait to the first real token rather than to a keepalive", run.TTFT)
	}
}

// A server that streams no usage cannot be measured: the prompt count is the
// server's to give, and cria refuses to invent one.
func TestASizeWithoutUsageIsReportedRatherThanGuessed(t *testing.T) {
	listener := newBenchListener(t, benchScript{tokens: 3, prompt: 100, noUsage: true})
	manager, record := benchManager(t, listener, mlxEntry())

	result := manager.Bench(record, oneSize(64, 1), nil)
	if !result.Failed() {
		t.Fatalf("a stream with no usage was reported as a measurement: %+v", result.Sizes[0])
	}
	if !strings.Contains(result.Sizes[0].Err.Error(), "streamed no usage") {
		t.Errorf("the failure reads %q, want it to name the usage that never arrived", result.Sizes[0].Err)
	}
}

// Every run sends a prompt of its own. Both servers keep the prefix of the last
// prompt they read, so a repeated prompt would skip the prefill the run exists
// to measure — and the smallest rung is still a real prompt, never an empty one.
func TestEveryRunSendsItsOwnPrompt(t *testing.T) {
	listener := newBenchListener(t, benchScript{tokens: 3, prompt: 42, interval: time.Millisecond})
	manager, record := benchManager(t, listener, llamaEntry())

	manager.Bench(record, BenchSpec{Sizes: []int{BenchMinSize, 256}, Runs: 3, GenTokens: 8}, nil)

	seen := map[string]bool{}
	for i, sent := range listener.sent {
		if strings.TrimSpace(sent.Prompt) == "" {
			t.Fatalf("request %d sent an empty prompt; an empty prompt wedges mlx_lm.server", i)
		}
		if seen[sent.Prompt] {
			t.Errorf("request %d repeated a prompt; the server would answer it out of its prefix cache", i)
		}
		seen[sent.Prompt] = true
	}
	if len(seen) != len(listener.sent) {
		t.Errorf("%d requests sent %d distinct prompts", len(listener.sent), len(seen))
	}

	// A prompt is aimed at its size: the count that is reported is the server's,
	// but what is sent has to be of the right order or the sweep measures the
	// wrong thing.
	small, large := listener.sent[1].Prompt, listener.sent[len(listener.sent)-1].Prompt
	if len(small) > 4*len(large) {
		t.Errorf("the %d-token prompt is %d chars and the 256-token one %d; the sizes are not being aimed at",
			BenchMinSize, len(small), len(large))
	}
}

// The request is the documented one, and it asks for the two things that make
// the measurement possible on both backends: the stream, and the usage with it.
func TestBenchAsksForTheStreamAndItsUsage(t *testing.T) {
	listener := newBenchListener(t, benchScript{tokens: 2, prompt: 20})
	manager, record := benchManager(t, listener, llamaEntry())

	manager.Bench(record, oneSize(64, 1), nil)
	if len(listener.sent) == 0 {
		t.Fatal("the sweep sent nothing")
	}
	sent := listener.sent[len(listener.sent)-1]

	if sent.Model != record.Repo {
		t.Errorf("the measurement asked for model %q, want the model the record launched (%q)", sent.Model, record.Repo)
	}
	if !sent.Stream {
		t.Error("the measurement did not ask for a stream; without one there is no first-token time")
	}
	if !sent.StreamOptions.IncludeUsage {
		t.Error("the measurement did not ask for usage with the stream; mlx_lm.server sends none unless it is asked")
	}
	if sent.MaxTokens != 32 {
		t.Errorf("the measurement asked for %d tokens, want the spec's %d", sent.MaxTokens, 32)
	}
}

// The unmeasured warmup runs once before the sweep and appears in nothing it
// reports: it exists so the first measured run is not the one paying for a cold
// path.
func TestTheWarmupIsSentButNotCounted(t *testing.T) {
	listener := newBenchListener(t, benchScript{tokens: 2, prompt: 30})
	manager, record := benchManager(t, listener, llamaEntry())

	spec := BenchSpec{Sizes: []int{64, 128}, Runs: 2, GenTokens: 16}
	result := manager.Bench(record, spec, nil)

	if want := 1 + len(spec.Sizes)*spec.Runs; len(listener.sent) != want {
		t.Fatalf("the sweep sent %d requests, want %d — one warmup and %d measured runs",
			len(listener.sent), want, want-1)
	}
	if listener.sent[0].MaxTokens != benchWarmupTokens {
		t.Errorf("the first request asked for %d tokens, want the warmup's %d", listener.sent[0].MaxTokens, benchWarmupTokens)
	}
	measured := 0
	for _, size := range result.Sizes {
		measured += len(size.Runs)
	}
	if measured != len(spec.Sizes)*spec.Runs {
		t.Errorf("the result holds %d runs, want the %d measured ones", measured, len(spec.Sizes)*spec.Runs)
	}
}

// Progress is reported before every request, warmup included: a sweep is
// minutes of waiting, and this is the only thing that makes the wait legible.
func TestASweepReportsWhereItHasGot(t *testing.T) {
	listener := newBenchListener(t, benchScript{tokens: 2, prompt: 30})
	manager, record := benchManager(t, listener, llamaEntry())

	var steps []BenchStep
	manager.Bench(record, BenchSpec{Sizes: []int{64, 128}, Runs: 2, GenTokens: 16},
		func(step BenchStep) { steps = append(steps, step) })

	if len(steps) != 5 {
		t.Fatalf("the sweep reported %d steps, want the warmup and four runs: %+v", len(steps), steps)
	}
	if !steps[0].Warmup {
		t.Errorf("the first step is %+v, want the warmup", steps[0])
	}
	for i, want := range []BenchStep{
		{Size: 64, Nth: 1, Sizes: 2, Run: 1, Runs: 2},
		{Size: 64, Nth: 1, Sizes: 2, Run: 2, Runs: 2},
		{Size: 128, Nth: 2, Sizes: 2, Run: 1, Runs: 2},
		{Size: 128, Nth: 2, Sizes: 2, Run: 2, Runs: 2},
	} {
		if steps[i+1] != want {
			t.Errorf("step %d is %+v, want %+v", i+1, steps[i+1], want)
		}
	}
}

// A size the server refuses is that size's answer, carrying what the server
// said — a prompt longer than the context is the expected one — and the sweep
// goes on to the sizes that fit.
func TestASizeTheServerRefusesDoesNotEndTheSweep(t *testing.T) {
	const tooLong = 65536
	listener := &benchListener{}
	listener.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent benchRequest
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("the measurement sent a body the endpoint cannot read: %v", err)
		}
		listener.sent = append(listener.sent, sent)

		// The long prompt is the one this server has no room for; everything
		// shorter is served.
		if float64(len(sent.Prompt)) > float64(tooLong)*benchCharsPerToken {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"code":400,"message":"request (65536 tokens) exceeds the available context size (32768 tokens), try increasing it"}}`)
			return
		}
		benchScript{tokens: 3, prompt: 100}.stream(w)
	}))
	t.Cleanup(listener.Close)

	manager, record := benchManager(t, listener, llamaEntry())
	result := manager.Bench(record, BenchSpec{Sizes: []int{256, tooLong, 512}, Runs: 2, GenTokens: 16}, nil)

	if len(result.Sizes) != 3 {
		t.Fatalf("the sweep reported %d sizes, want all three", len(result.Sizes))
	}
	if !result.Failed() {
		t.Error("a sweep with a refused size reported itself as complete")
	}

	refused := result.Sizes[1]
	if refused.Err == nil {
		t.Fatalf("the %d-token size came back measured: %+v", tooLong, refused)
	}
	for _, want := range []string{"65536 tokens", "400 Bad Request", "exceeds the available context size"} {
		if !strings.Contains(refused.Err.Error(), want) {
			t.Errorf("the refusal reads %q, want it to carry %q", refused.Err, want)
		}
	}
	// The refused size stops at its first failure rather than asking again.
	if len(refused.Runs) != 0 {
		t.Errorf("the refused size holds %d runs, want none", len(refused.Runs))
	}

	for _, measured := range []BenchSize{result.Sizes[0], result.Sizes[2]} {
		if measured.Err != nil {
			t.Errorf("the %d-token size failed with %v, want it measured either side of the refusal", measured.Tokens, measured.Err)
		}
		if len(measured.Runs) != 2 {
			t.Errorf("the %d-token size holds %d runs, want both", measured.Tokens, len(measured.Runs))
		}
	}
}

// A size's mean is the mean of its runs, and a run that was served out of the
// server's prefix cache is flagged rather than averaged in silence: a cache hit
// means part of the prefill never happened.
func TestASizeMeansItsRunsAndFlagsACacheHit(t *testing.T) {
	listener := newBenchListener(t, benchScript{
		prefill:  40 * time.Millisecond,
		interval: 5 * time.Millisecond,
		tokens:   4,
		prompt:   200,
		cached:   64, // a third of the prompt: a prefill that did not happen
	})
	manager, record := benchManager(t, listener, llamaEntry())

	result := manager.Bench(record, oneSize(200, 3), nil)
	size := result.Sizes[0]
	if len(size.Runs) != 3 {
		t.Fatalf("the size holds %d runs, want 3", len(size.Runs))
	}

	var prefill, decode, ttft float64
	for _, run := range size.Runs {
		prefill += run.PrefillRate / 3
		decode += run.DecodeRate / 3
		ttft += float64(run.TTFT) / 3
	}
	if diff := size.Mean.PrefillRate - prefill; diff > 0.001 || diff < -0.001 {
		t.Errorf("the mean prefill rate is %.3f, want the mean of the runs (%.3f)", size.Mean.PrefillRate, prefill)
	}
	if diff := size.Mean.DecodeRate - decode; diff > 0.001 || diff < -0.001 {
		t.Errorf("the mean decode rate is %.3f, want the mean of the runs (%.3f)", size.Mean.DecodeRate, decode)
	}
	if size.Mean.PromptTokens != 200 {
		t.Errorf("the mean prompt is %.1f tokens, want the 200 every run read", size.Mean.PromptTokens)
	}
	if got := time.Duration(ttft); size.Mean.TTFT != got {
		t.Errorf("the mean TTFT is %s, want the mean of the runs (%s)", size.Mean.TTFT, got)
	}
	if !size.Cached() {
		t.Error("a size whose runs were served out of the prefix cache does not say so")
	}

	// The opening word every prompt shares is not a prefill anyone skipped: a
	// couple of tokens of a long prompt leaves the rate exactly where it was.
	shared := BenchSize{Runs: []BenchRun{{PromptTokens: 4166, CachedTokens: 2}}}
	if shared.Cached() {
		t.Error("two cached tokens of a 4166-token prompt were reported as a cache hit")
	}
}

// A sweep aims its prompts with the ratio its own warmup measured, not with a
// rule of thumb: the tokenizers differ, and a sweep that measured 14k tokens
// where it asked for 16k is not the sweep the caller asked for — nor the same
// prompt the other backend was given.
func TestASweepAimsWithTheTokenizerItMeasured(t *testing.T) {
	// A server whose tokenizer runs at six characters a token.
	listener := newBenchListener(t, benchScript{tokens: 3, tokenizer: 6, interval: time.Millisecond})
	manager, record := benchManager(t, listener, llamaEntry())

	const target = 2000
	result := manager.Bench(record, oneSize(target, 1), nil)
	if result.Failed() {
		t.Fatalf("the sweep reported %v, want a measurement", result.Sizes[0].Err)
	}

	// The warmup is what measured the ratio, so it is still built on the rule of
	// thumb; everything after it is aimed with what the server counted.
	if warmup := listener.sent[0]; len(warmup.Prompt) > 2*benchCalibrationSize*benchCharsPerToken {
		t.Errorf("the warmup sent %d chars, want roughly the calibration size", len(warmup.Prompt))
	}
	if got := result.Sizes[0].Runs[0].PromptTokens; got < target*9/10 || got > target*11/10 {
		t.Errorf("the %d-token size measured %d tokens; the sweep is aiming with the wrong ratio", target, got)
	}
}

// A size that lost a run still answers from the runs it kept: the reason
// travels with it, so the thinness is visible, but a rate measured twice
// instead of three times is still a rate.
func TestASizeThatLostARunStillAnswers(t *testing.T) {
	answered := 0
	listener := &benchListener{}
	listener.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent benchRequest
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("the measurement sent a body the endpoint cannot read: %v", err)
		}
		listener.sent = append(listener.sent, sent)

		// The warmup and the first run answer; the second is refused.
		answered++
		if answered == 3 {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":"the sampler blew up"}`)
			return
		}
		benchScript{tokens: 4, prompt: 300, interval: 5 * time.Millisecond}.stream(w)
	}))
	t.Cleanup(listener.Close)

	manager, record := benchManager(t, listener, llamaEntry())
	result := manager.Bench(record, oneSize(300, 3), nil)

	size := result.Sizes[0]
	if !size.Measured() || len(size.Runs) != 1 {
		t.Fatalf("the size holds %d runs, want the one that answered", len(size.Runs))
	}
	if size.Err == nil || !strings.Contains(size.Err.Error(), "the sampler blew up") {
		t.Errorf("the size reports %v, want the reason it has fewer runs than were asked for", size.Err)
	}
	if result.Failed() {
		t.Error("a sweep that measured every size reported itself as having a hole in it")
	}
	if size.Mean.DecodeRate != size.Runs[0].DecodeRate {
		t.Errorf("the mean decode rate is %.3f, want the run behind it (%.3f)", size.Mean.DecodeRate, size.Runs[0].DecodeRate)
	}
}

// A model is free to end its answer whenever it likes, and a run it ended after
// one token measured no decode at all. That absence is not a speed of zero: it
// stays out of the mean, and what the run did write is still reported.
func TestARunTheModelEndedAtOnceIsNotAveragedAsZero(t *testing.T) {
	answered := 0
	listener := &benchListener{}
	listener.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent benchRequest
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("the measurement sent a body the endpoint cannot read: %v", err)
		}
		listener.sent = append(listener.sent, sent)

		// The warmup, then a run the model ends at once, then a full one.
		answered++
		script := benchScript{prefill: 20 * time.Millisecond, interval: 10 * time.Millisecond, tokens: 5, prompt: 300}
		if answered == 2 {
			script.tokens = 1
		}
		script.stream(w)
	}))
	t.Cleanup(listener.Close)

	manager, record := benchManager(t, listener, llamaEntry())
	result := manager.Bench(record, oneSize(300, 2), nil)
	size := result.Sizes[0]
	if len(size.Runs) != 2 {
		t.Fatalf("the size holds %d runs, want both", len(size.Runs))
	}

	stopped, whole := size.Runs[0], size.Runs[1]
	if stopped.Decoded() || stopped.DecodeRate != 0 {
		t.Errorf("a one-token run reports a decode rate of %.1f, want none at all", stopped.DecodeRate)
	}
	if stopped.GenTokens != 1 || stopped.PrefillRate <= 0 {
		t.Errorf("the one-token run reports %+v, want what it did measure: one token and a prefill", stopped)
	}
	if !whole.Decoded() {
		t.Fatalf("the full run measured no decode: %+v", whole)
	}
	if size.Mean.DecodeRate != whole.DecodeRate {
		t.Errorf("the mean decode rate is %.3f, want the one run that measured one (%.3f)", size.Mean.DecodeRate, whole.DecodeRate)
	}
	if want := (1 + float64(whole.GenTokens)) / 2; size.Mean.GenTokens != want {
		t.Errorf("the mean generated %.1f tokens, want %.1f — what the model actually wrote", size.Mean.GenTokens, want)
	}
}

// A sweep nobody specified is the default one, and a size nobody can measure is
// raised to the smallest rung rather than sent as a degenerate prompt.
func TestBenchSpecDefaultsAndClamps(t *testing.T) {
	spec := BenchSpec{}.normalized()
	if want := DefaultBenchSpec(); spec.Runs != want.Runs || spec.GenTokens != want.GenTokens {
		t.Errorf("an empty spec runs %d×%d tokens, want the default %d×%d",
			spec.Runs, spec.GenTokens, want.Runs, want.GenTokens)
	}
	if len(spec.Sizes) != 3 || spec.Sizes[0] != BenchMinSize {
		t.Errorf("an empty spec sweeps %v, want the default sizes starting at %d", spec.Sizes, BenchMinSize)
	}

	clamped := BenchSpec{Sizes: []int{0, -1, 4096}, Runs: 0, GenTokens: 0}.normalized()
	if clamped.Sizes[0] != BenchMinSize || clamped.Sizes[1] != BenchMinSize {
		t.Errorf("the sizes clamped to %v, want everything under %d raised to it", clamped.Sizes, BenchMinSize)
	}
	if clamped.Sizes[2] != 4096 {
		t.Errorf("the sizes clamped to %v, want a real size left alone", clamped.Sizes)
	}

	// The defaults are a copy: a caller that edits what it was handed does not
	// edit what the next one gets.
	first := DefaultBenchSpec()
	first.Sizes[0] = 99
	if DefaultBenchSpec().Sizes[0] != BenchMinSize {
		t.Error("editing one default spec's sizes changed the next one's")
	}
}

// A server that never answers ends the run at its budget, saying so.
func TestAMeasurementThatIsNeverAnsweredEndsAtItsBudget(t *testing.T) {
	held := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-held
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(held)

	manager := newManager(t, &fakeHost{})
	manager.bench = newHTTPBench()
	manager.benchWithin = 50 * time.Millisecond
	record := recordAt(t, llamaEntry(), server.URL)

	result := manager.Bench(record, oneSize(64, 1), nil)
	if !result.Failed() {
		t.Fatal("a measurement that never came back was reported as a rate")
	}
	if !strings.Contains(result.Sizes[0].Err.Error(), "no answer within 50ms") {
		t.Errorf("the failure reads %q, want the budget it was bound by", result.Sizes[0].Err)
	}
}

// The result carries the server it was taken against: a bench is read long
// after it ran, and which entry and model it measured is part of the answer.
func TestABenchResultNamesWhatItMeasured(t *testing.T) {
	listener := newBenchListener(t, benchScript{tokens: 2, prompt: 30})
	manager, record := benchManager(t, listener, llamaEntry())

	began := time.Now()
	result := manager.Bench(record, oneSize(64, 1), nil)

	if result.EntryID != record.EntryID || result.Repo != record.Repo || result.Quant != record.Quant {
		t.Errorf("the result names %s %s:%s, want the record's %s %s:%s",
			result.EntryID, result.Repo, result.Quant, record.EntryID, record.Repo, record.Quant)
	}
	if result.StartedAt.Before(began) {
		t.Errorf("the result started at %s, before the sweep did (%s)", result.StartedAt, began)
	}
	if len(result.Spec.Sizes) != 1 || result.Spec.Sizes[0] != 64 {
		t.Errorf("the result carries the spec %+v, want the one it actually ran", result.Spec)
	}
}
