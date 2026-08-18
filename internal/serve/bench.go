package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// Benchmarking is asking a server that is already running how fast it serves:
// how many tokens of prompt it reads per second, and how many tokens of answer
// it writes per second. Both are measured from outside, over the OpenAI-shaped
// endpoint every backend publishes — the same documented interface the health
// probe and the warm use (docs/specs/SERVE.md).
//
// Nothing is read from a backend's own instrumentation. llama-server volunteers
// a `timings` object on every completion and mlx_lm.server volunteers nothing,
// so a bench built on it would compare one backend against a blank — the
// comparison is the whole point, and it only holds when both numbers were taken
// the same way. cria times the bytes arriving on its own socket, which is also
// the number a client of the server actually gets.
//
// What the server is asked for, and what it hands back, is documented API:
// stream=true, and stream_options.include_usage so the token counts arrive with
// the stream. The server is the tokenizer — cria never counts a prompt itself,
// it reads usage.prompt_tokens back — so a size in this file is what cria aimed
// for and the measurement carries what the server actually read.

const (
	// benchPath is where a measurement is sent: the same documented completion
	// endpoint the warm uses (warm.go). It takes a raw prompt, so no chat
	// template stands between the request and the model, and a prompt of an
	// exact size stays a prompt of that size.
	benchPath = warmPath

	// BenchMinSize is the smallest prompt a sweep measures. A prefill of a
	// handful of tokens is dominated by request overhead rather than by the
	// model, and this is the rung where that is still readable as a number;
	// below it there is nothing left to measure. It is also what keeps the
	// never-empty-prompt rule trivially true — an empty prompt wedges
	// mlx_lm.server (warm.go).
	BenchMinSize = 16

	// benchRuns is how many times each size is measured, and benchGenTokens how
	// much answer each run asks for. Three runs is enough for the spread to show
	// whether a number is stable without turning a sweep into a wait; 256 tokens
	// is long enough that the decode rate is the model's rather than the first
	// token's.
	benchRuns      = 3
	benchGenTokens = 256

	// benchWarmupTokens is what the one unmeasured request before a sweep asks
	// for. It exists so the first measured run is not the one paying for a cold
	// path — a sampler allocating, a cache warming — and a few tokens is enough
	// to walk that path.
	benchWarmupTokens = 8

	// benchCharsPerToken is how much text a token is assumed to be before this
	// server has said otherwise: the rule of thumb for ordinary English. Every
	// sweep replaces it with the ratio its own warmup measured (benchCalibrate),
	// because the ratio is the tokenizer's and the tokenizers differ — the same
	// filler runs at 4.6 chars per token on one model and elsewhere on another,
	// and a sweep that aims with one number measures two different prompts on
	// the two backends it exists to compare.
	benchCharsPerToken = 4.0

	// benchCalibrationSize is the prompt the warmup is built at. It is large
	// enough that the ratio it measures is the filler's rather than the nonce
	// header's, and small enough to cost nothing on any server.
	benchCalibrationSize = 512

	// The ratio is clamped to what a tokenizer can plausibly be. A server that
	// answers something outside this is not describing text, and a prompt built
	// from it would be absurdly long or absurdly short.
	benchLeastChars = 2.0
	benchMostChars  = 12.0

	// benchCacheShare is the fraction of a prompt that has to come out of the
	// server's cache before the prefill rate is worth doubting: one part in
	// this many.
	benchCacheShare = 20

	// nonceChars is how much of a prompt the uniqueness header takes: "nonce ",
	// sixteen hex digits and the newline. No prompt is ever cut into it — the
	// header is the whole reason a run measures a prefill rather than a cache.
	nonceChars = len("nonce 0123456789abcdef\n")

	// benchBudget bounds one measured request. A 16k-token prefill on a large
	// model is minutes of work before a single token comes back, so the budget
	// is generous for the same reason the warm's is: it is here to end a request
	// that will never finish, not to judge a slow one.
	benchBudget = 15 * time.Minute
)

// benchSizes is the sweep cria measures unless it is told otherwise: the small
// rung where overhead still shows, a mid-length prompt, and a long one. Three
// points are what make a rate a curve rather than a number — prefill is not
// linear in prompt length, and a single size hides that.
var benchSizes = []int{BenchMinSize, 4096, 16384}

// BenchSpec is what one sweep asks for: which prompt sizes, how many runs of
// each, and how much answer every run asks the server to generate.
type BenchSpec struct {
	Sizes     []int // prompt sizes in tokens, measured in the order given
	Runs      int   // measured runs per size
	GenTokens int   // max_tokens each run asks for
}

// DefaultBenchSpec is the sweep cria runs when nobody names one — what the TUI
// always runs, and what `cria bench` runs with no flags.
func DefaultBenchSpec() BenchSpec {
	return BenchSpec{Sizes: append([]int(nil), benchSizes...), Runs: benchRuns, GenTokens: benchGenTokens}
}

// normalized fills in what a caller left out and clamps what it cannot honour.
// A size below the smallest rung is raised to it rather than sent: a degenerate
// prompt measures the request rather than the model, and an empty one wedges an
// mlx server outright (warm.go). It is a floor rather than a policy — a caller
// that has someone to explain it to applies the same floor while parsing, so
// that what it says and what it asks for agree, and this is what makes the rule
// true whoever calls.
func (s BenchSpec) normalized() BenchSpec {
	if len(s.Sizes) == 0 {
		s.Sizes = benchSizes
	}
	sizes := make([]int, 0, len(s.Sizes))
	for _, size := range s.Sizes {
		sizes = append(sizes, max(size, BenchMinSize))
	}
	s.Sizes = sizes

	if s.Runs < 1 {
		s.Runs = benchRuns
	}
	if s.GenTokens < 1 {
		s.GenTokens = benchGenTokens
	}
	return s
}

// BenchResult is one sweep: the server it was taken against, when it started,
// the spec as it actually ran, and one entry per size.
//
// It carries the record for the same reason a Status does — a result is read
// long after the sweep, and the entry, backend and model it was taken against
// are part of the measurement rather than context the reader has to keep.
type BenchResult struct {
	Record
	StartedAt time.Time
	Spec      BenchSpec
	Sizes     []BenchSize
}

// Failed reports whether any size of the sweep went unmeasured. It is the
// question a caller asks to decide an exit code: a size with no measurement at
// all is not the answer that was asked for, however much of the sweep around it
// answered.
//
// A size that lost one of its runs is not failed. It has a rate, taken fewer
// times than asked for, and its reason travels with it (Err) so that thinness
// is visible rather than inferred.
func (r BenchResult) Failed() bool {
	for _, size := range r.Sizes {
		if !size.Measured() {
			return true
		}
	}
	return false
}

// BenchSize is one prompt size measured: every run of it, their mean, and — for
// a size that did not get all of its runs — why. A size that fails does not end
// the sweep: a prompt longer than the server's context is a fact about that
// size, and the shorter ones are still worth measuring (docs/specs/SERVE.md).
type BenchSize struct {
	Tokens int // the size cria aimed for; what the server counted is in each run
	Runs   []BenchRun
	Mean   BenchMean
	Err    error // why this size has fewer runs than were asked for; nil when it has them all
}

// Measured reports whether this size has any measurement behind it. A size the
// server refused outright has none and is a hole in the sweep; one that lost a
// run still answers, from the runs it kept.
func (s BenchSize) Measured() bool { return len(s.Runs) > 0 }

// BenchRun is one measured request, as it was timed on cria's own socket.
//
// The two rates are the whole point of the measurement, and each is a ratio of
// something the server said to something cria timed. Prefill is the tokens the
// server reports having read over the wait for the first one it wrote. Decode
// is the tokens after that first one over the window they arrived in — the
// first token is not in the window, it is what ended the prefill.
type BenchRun struct {
	PromptTokens int           // usage.prompt_tokens: the server is the tokenizer
	CachedTokens int           // usage.prompt_tokens_details.cached_tokens: prefix the server did not have to read
	GenTokens    int           // content chunks streamed back
	TTFT         time.Duration // request sent → first content chunk
	Decode       time.Duration // first content chunk → last
	PrefillRate  float64       // prompt tokens per second
	DecodeRate   float64       // generated tokens per second
}

// BenchMean is one size's runs averaged — the row a table shows, with the runs
// behind it for the spread.
type BenchMean struct {
	PromptTokens float64
	GenTokens    float64
	TTFT         time.Duration
	PrefillRate  float64
	DecodeRate   float64
}

// Decoded reports whether this run measured a decode rate at all. A model is
// free to end its answer whenever it likes — a long prompt of filler is exactly
// the kind that invites it — and a run it ended after a single token has no
// window between two tokens to have measured anything in. That is a fact about
// what the model wrote, never a speed of zero.
func (r BenchRun) Decoded() bool { return r.GenTokens > 1 && r.Decode > 0 }

// Cached reports whether any run of this size had enough of its prompt served
// out of the server's own cache to make the prefill rate above it optimistic.
// It is the measurement's own honesty check: a reused prefix is a prefill that
// never happened, and every prompt cria sends starts with a nonce exactly so
// there is nothing to reuse.
//
// It is a share rather than any hit at all. mlx_lm.server counts a prefix from
// the first token, and every prompt does share its opening word — two or three
// tokens of a sixteen-thousand-token prompt, measured 2026-08-19 — which is not
// a prefill anyone skipped. What matters is a prefix large enough to move the
// number.
func (s BenchSize) Cached() bool {
	for _, run := range s.Runs {
		if run.CachedTokens*benchCacheShare >= run.PromptTokens {
			return true
		}
	}
	return false
}

// BenchStep is where a sweep has got to, handed to the caller as each request
// is about to be sent. A sweep is minutes of waiting, and this is what lets the
// waiting be legible — a progress line on stderr, a line in a pane — without
// anything outside this file knowing how a sweep is ordered.
type BenchStep struct {
	Warmup bool // the unmeasured request before the sweep
	Size   int  // the prompt size this step measures
	Nth    int  // its place among the sweep's sizes, counted from one
	Sizes  int  // how many sizes the sweep has
	Run    int  // the run within this size, counted from one
	Runs   int  // how many runs each size gets
}

// bencher sends one completion to a server and reports what came back and how
// long each part of it took. It is the seam the component tests replace — the
// same shape as the probe, the spawner and the warm — so the sweep's own rules
// run with no server and no port.
type bencher func(url, model, prompt string, tokens int, within time.Duration) (BenchRun, error)

// Bench measures one running server: every size of the spec, every run of every
// size, one request at a time.
//
// Sequential by construction. Two requests in flight would queue inside the
// server and each would time the other's work as its own, which is the one
// mistake that makes every number here meaningless.
//
// There is no error return: everything that can go wrong belongs to a size —
// the server refused this prompt, the stream stopped, nothing came back within
// the budget — and each one is reported on the size it happened to while the
// sweep goes on. report may be nil for a caller with nowhere to show progress.
func (m *Manager) Bench(record Record, spec BenchSpec, report func(BenchStep)) BenchResult {
	spec = spec.normalized()
	result := BenchResult{Record: record, StartedAt: time.Now(), Spec: spec}
	url := benchURL(record)

	// One unmeasured request first, so the first measured run is not the one
	// paying for whatever a server does once — and so the sizes that follow are
	// aimed with this model's own ratio rather than with a rule of thumb. Its
	// failure is not reported here: whatever refuses this refuses the first
	// measured run too, and that is where it belongs — on the size it was
	// measuring.
	benchReport(report, BenchStep{Warmup: true, Sizes: len(spec.Sizes), Runs: spec.Runs})
	warmup := benchPrompt(benchCalibrationSize, benchCharsPerToken)
	measured, _ := m.bench(url, record.Repo, warmup, benchWarmupTokens, m.benchWithin)
	chars := benchCalibrate(warmup, measured.PromptTokens)

	for i, size := range spec.Sizes {
		measured := BenchSize{Tokens: size}
		for run := 1; run <= spec.Runs; run++ {
			benchReport(report, BenchStep{Size: size, Nth: i + 1, Sizes: len(spec.Sizes), Run: run, Runs: spec.Runs})

			// A prompt of its own for every run. llama-server keeps the prefix of
			// the last prompt it read, and mlx_lm.server does the same: sending
			// one prompt twice would have the second run skip the prefill it was
			// there to measure and report a rate the model cannot do
			// (verified live, 2026-08-19 — the second identical request came back
			// with cached_tokens set).
			one, err := m.bench(url, record.Repo, benchPrompt(size, chars), spec.GenTokens, m.benchWithin)
			if err != nil {
				// The size stops here rather than asking again. What refused
				// this run refuses the next one — a prompt longer than the
				// context always will — and a run that failed by running out of
				// its budget would cost that budget again for the same answer.
				measured.Err = fmt.Errorf("%d tokens: %w", size, err)
				break
			}
			measured.Runs = append(measured.Runs, one)
		}
		measured.Mean = benchMean(measured.Runs)
		result.Sizes = append(result.Sizes, measured)
	}
	return result
}

// benchReport hands one step to a caller that wanted them.
func benchReport(report func(BenchStep), step BenchStep) {
	if report != nil {
		report(step)
	}
}

// benchURL is where a measurement is sent: the same address rule the probe and
// the warm follow — loopback for a wildcard bind, the bound address otherwise
// (docs/specs/CONFIG.md).
func benchURL(record Record) string { return serverURL(record, benchPath) }

// benchMean averages one size's runs. Rates are averaged as rates: each run is
// one measurement of the same thing, and the mean of them is what the runs
// agree on — where a rate computed from summed tokens and summed seconds would
// quietly weight the slowest run heaviest.
//
// The decode mean is over the runs that measured one. A run the model ended
// after a single token has no decode rate, and averaging its absence in as a
// zero would report a speed no server ever ran at (Decoded).
func benchMean(runs []BenchRun) BenchMean {
	if len(runs) == 0 {
		return BenchMean{}
	}
	var mean BenchMean
	decoded := 0
	for _, run := range runs {
		mean.PromptTokens += float64(run.PromptTokens)
		mean.GenTokens += float64(run.GenTokens)
		mean.TTFT += run.TTFT
		mean.PrefillRate += run.PrefillRate
		if run.Decoded() {
			mean.DecodeRate += run.DecodeRate
			decoded++
		}
	}
	count := float64(len(runs))
	mean.PromptTokens /= count
	mean.GenTokens /= count
	mean.TTFT /= time.Duration(len(runs))
	mean.PrefillRate /= count
	if decoded > 0 {
		mean.DecodeRate /= float64(decoded)
	}
	return mean
}

// benchFiller is the text a prompt is padded with: ordinary English sentences,
// because that is what the tokenizers these models ship with were fitted to —
// repeated punctuation or random characters would tokenize at a wildly
// different ratio and measure a prompt nobody sends.
//
// They are laid out as a numbered list, and that is a measurement rather than a
// style. A model asked to continue a long stretch of loose prose frequently
// ends its answer immediately, which leaves a run with no decode to time: on
// LFM2.5-2.6B at 16k tokens, prose came back with nothing at all on three of
// four attempts, while the same sentences numbered came back with the full 256
// tokens on four of four (measured 2026-08-19). A list has an obvious next
// item, and a benchmark needs the model to keep writing.
var benchFiller = []string{
	"The cache holds every model the servers have fetched so far.",
	"A record names the process it launched and the port it listens on.",
	"Loading weights costs seconds on a warm cache and minutes on a cold one.",
	"The prompt is read once, and every token after it is written one at a time.",
	"Memory bandwidth decides how fast the answer arrives on this machine.",
	"An entry declares a model, a quantization and the flags its server takes.",
	"Nothing here is read out of a log file, and nothing is guessed.",
	"The same measurement has to mean the same thing on either backend.",
}

// benchCalibrate reads this model's chars-per-token off the warmup: cria knows
// exactly how much text it sent, and the server has just said how many tokens
// that came to. It is the same usage field every measurement reads, so the
// calibration is as uniform across backends as the measurement is.
//
// A warmup that failed says nothing about the tokenizer, and the rule of thumb
// stands — the sizes are aims either way, and every run reports the count the
// server actually read.
func benchCalibrate(prompt string, tokens int) float64 {
	if tokens <= 0 {
		return benchCharsPerToken
	}
	return min(max(float64(len(prompt))/float64(tokens), benchLeastChars), benchMostChars)
}

// benchPrompt builds one run's prompt: a nonce, then filler shuffled behind it,
// long enough to aim at tokens at the given chars-per-token.
//
// The nonce comes first and it is what makes the prompt unique. Both servers
// cache the longest prefix they have already read, so a prompt that begins the
// same way as the last one skips exactly the work a prefill measurement is
// about; a prompt that differs in its first line shares nothing.
func benchPrompt(tokens int, charsPerToken float64) string {
	var prompt strings.Builder
	fmt.Fprintf(&prompt, "nonce %016x\n", rand.Uint64())

	budget := int(float64(tokens) * charsPerToken)
	order := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	for item := 1; prompt.Len() < budget; {
		for _, i := range order.Perm(len(benchFiller)) {
			if prompt.Len() >= budget {
				break
			}
			fmt.Fprintf(&prompt, "%d. %s\n", item, benchFiller[i])
			item++
		}
	}

	// Cut to the budget rather than to the last whole sentence. Two reasons, and
	// both matter: the size lands where it was aimed, and the prompt ends
	// mid-word — a completed sentence is an invitation to answer with nothing,
	// and a model that ends its answer at once leaves no decode to measure
	// (Decoded). The filler and the nonce are ASCII, so the cut is a cut.
	text := prompt.String()
	if len(text) > budget && budget > nonceChars {
		text = text[:budget]
	}
	return text
}

// benchRequest is one measured completion as the endpoint takes it.
//
// stream is what makes the measurement possible at all: the first token's
// arrival is the end of the prefill, and only a stream says when that was.
// stream_options.include_usage is what makes it uniform — mlx_lm.server sends
// no usage at all with a stream unless it is asked, while llama-server sends it
// either way (verified live, 2026-08-19), so cria asks and both answer.
type benchRequest struct {
	Model         string       `json:"model"`
	Prompt        string       `json:"prompt"`
	MaxTokens     int          `json:"max_tokens"`
	Stream        bool         `json:"stream"`
	StreamOptions streamOption `json:"stream_options"`
}

type streamOption struct {
	IncludeUsage bool `json:"include_usage"`
}

// benchChunk is the part of one streamed chunk cria reads. Unknown fields are
// left alone rather than refused: these are other people's payloads, they carry
// far more than this, and llama-server's own timings object is among the things
// deliberately not read here.
type benchChunk struct {
	Choices []struct {
		Text string `json:"text"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens int `json:"prompt_tokens"`
		Details      struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// The shape of the stream both servers speak: server-sent events, one JSON
// chunk per `data:` line, ended by a `[DONE]` payload. A line that is not a
// data line is not an event — mlx_lm.server sends `: keepalive` comments while
// it is still reading the prompt, and reading one of those as the first token
// would report a prefill that never finished.
const (
	sseData = "data: "
	sseDone = "[DONE]"

	// sseBuffer is the longest single chunk cria will read. The chunks are a few
	// hundred bytes; the room is there so a server that ever writes a large final
	// event fails no measurement.
	sseBuffer = 1 << 20
)

// newHTTPBench builds the real bencher: one client for the whole sweep, so the
// runs share a connection and no measurement pays for a handshake.
func newHTTPBench() bencher {
	// No client timeout: each request carries its own deadline, and a client
	// timeout would also cap the stream a measurement is read from.
	client := &http.Client{}

	return func(url, model, prompt string, tokens int, within time.Duration) (BenchRun, error) {
		body, err := json.Marshal(benchRequest{
			Model:         model,
			Prompt:        prompt,
			MaxTokens:     tokens,
			Stream:        true,
			StreamOptions: streamOption{IncludeUsage: true},
		})
		if err != nil {
			return BenchRun{}, fmt.Errorf("cannot compose the completion %s takes: %w", url, err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), within)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return BenchRun{}, fmt.Errorf("cannot compose the request to %s: %w", url, err)
		}
		request.Header.Set("Content-Type", "application/json")

		sent := time.Now()
		response, err := client.Do(request)
		if err != nil {
			return BenchRun{}, fmt.Errorf("%s: %s", url, requestFailure(err, within))
		}
		defer response.Body.Close()

		// A refused measurement is the server's answer, quoted as it came: a
		// prompt longer than the context is the expected refusal here, and what
		// the server says about it names the context it has (docs/specs/SERVE.md).
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return BenchRun{}, fmt.Errorf("%s answered %s%s", url, response.Status, refusal(response.Body))
		}

		run, err := readBenchStream(response.Body, sent)
		if err != nil {
			return BenchRun{}, fmt.Errorf("%s: %s", url, requestFailure(err, within))
		}
		return run, nil
	}
}

// readBenchStream times one stream: when the first token arrived, when the last
// one did, and what the server said it read.
//
// Every timestamp is taken the moment the line is off the socket, before the
// chunk is parsed — the parse is cria's cost, not the server's.
func readBenchStream(body io.Reader, sent time.Time) (BenchRun, error) {
	var (
		run          BenchRun
		first, last  time.Time
		usageArrived bool
	)

	stream := bufio.NewScanner(body)
	stream.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), sseBuffer)
	for stream.Scan() {
		at := time.Now()

		// Blank lines separate events, and lines beginning with a colon are
		// comments: mlx_lm.server sends keepalives there while it prefills.
		payload, isEvent := strings.CutPrefix(stream.Text(), sseData)
		if !isEvent {
			continue
		}
		if strings.TrimSpace(payload) == sseDone {
			break
		}

		var chunk benchChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return BenchRun{}, fmt.Errorf("streamed a chunk cria cannot read: %w", err)
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Text != "" {
			if run.GenTokens == 0 {
				first = at
			}
			last = at
			run.GenTokens++
		}
		if chunk.Usage != nil {
			run.PromptTokens = chunk.Usage.PromptTokens
			run.CachedTokens = chunk.Usage.Details.CachedTokens
			usageArrived = true
		}
	}
	if err := stream.Err(); err != nil {
		return BenchRun{}, err
	}

	if run.GenTokens == 0 {
		return BenchRun{}, errors.New("streamed no tokens at all")
	}
	if !usageArrived {
		return BenchRun{}, errors.New("streamed no usage, so cria cannot tell how many tokens it read")
	}

	run.TTFT = first.Sub(sent)
	run.Decode = last.Sub(first)
	return benchRates(run), nil
}

// benchRates is where a timed run becomes two numbers, and the only place
// either is computed.
//
// The decode window holds every token after the first: the first one arrived at
// the end of the prefill, and counting it against the window it did not spend
// would report a rate the model never ran at. A single token has no decode rate
// for the same reason — there is no window yet.
func benchRates(run BenchRun) BenchRun {
	if run.TTFT > 0 {
		run.PrefillRate = float64(run.PromptTokens) / run.TTFT.Seconds()
	}
	if run.GenTokens > 1 && run.Decode > 0 {
		run.DecodeRate = float64(run.GenTokens-1) / run.Decode.Seconds()
	}
	return run
}
