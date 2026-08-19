package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"cria/internal/format"
	"cria/internal/serve"
)

// benchUsage is the one line every refusal of this command ends with.
const benchUsage = "usage: cria bench [<id>] [--sizes 16,4096,16384] [--runs N] [--gen N] [--json]"

// benchMissing is what a table shows for a size that was not measured. The numbers
// live in the same columns whether or not a size answered, so the row is read
// down with the others and the reason follows under the table.
const benchMissing = "—"

// bench runs `cria bench [<id>] [flags]`: how fast a server that is already
// running actually serves (docs/specs/SERVE.md).
//
// With no id it measures the only running server, and with several running the
// id is required — stop's convention, for the same reason: guessing which
// server a script meant is the one mistake this cannot take back, and a sweep
// is minutes of that server's time.
func (a *app) bench(args []string) int {
	options, err := parseBench(args)
	if err != nil {
		return a.usage("bench: %v; %s", err, benchUsage)
	}

	manager, err := a.servers()
	if err != nil {
		return a.fail("bench: %v", err)
	}
	listing, err := manager.List()
	if err != nil {
		return a.fail("bench: %v", err)
	}
	record, refusal := benchTarget(listing, options.id)
	if refusal != "" {
		return a.fail("bench: %s", refusal)
	}

	for _, note := range options.notes {
		a.note("%s", note)
	}

	// Everything about the wait goes to stderr: stdout is the table, and a
	// script reading it should meet the answer alone (cli.go).
	a.waiting("benchmarking %s (%s) on %s: %s",
		record.EntryID, format.HubReference(record.Repo, record.Quant), address(record), describe(options.spec))
	result := manager.Bench(record, options.spec, a.benchProgress)

	if options.asJSON {
		document, err := json.MarshalIndent(benchDocumentOf(result), "", "  ")
		if err != nil {
			return a.fail("bench: cannot encode the benchmark document: %v", err)
		}
		a.printf("%s\n", document)
	} else {
		a.reportBench(result)
	}

	// Partial results are still printed, and a sweep that could not measure
	// every size is still not the answer that was asked for (docs/specs/CLI.md).
	if result.Failed() {
		return exitFailure
	}
	return exitOK
}

// benchOptions is one invocation's command line, parsed: which server, what to
// measure, how to print it, and what the parse had to say about the values it
// was given.
type benchOptions struct {
	id     string
	spec   serve.BenchSpec
	asJSON bool
	notes  []string
}

// The flags `cria bench` takes. They are the only flags in the surface that
// carry a value, so they accept both spellings a caller reaches for —
// `--runs 5` and `--runs=5` are the same invocation (docs/specs/CLI.md).
const (
	sizesFlag = "--sizes"
	runsFlag  = "--runs"
	genFlag   = "--gen"
)

// parseBench reads the command line. Every refusal names the flag and what it
// takes; nothing here decides anything about a server.
func parseBench(args []string) (benchOptions, error) {
	options := benchOptions{spec: serve.DefaultBenchSpec()}
	var ids []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			ids = append(ids, arg)
			continue
		}

		name, value, attached := strings.Cut(arg, "=")
		if name == jsonFlag {
			if attached {
				return options, fmt.Errorf("%s takes no value", jsonFlag)
			}
			options.asJSON = true
			continue
		}
		if name != sizesFlag && name != runsFlag && name != genFlag {
			return options, fmt.Errorf("unknown flag %s", name)
		}
		if !attached {
			i++
			if i >= len(args) {
				return options, fmt.Errorf("%s takes a value", name)
			}
			value = args[i]
		}

		var err error
		switch name {
		case sizesFlag:
			options.spec.Sizes, options.notes, err = parseSizes(value)
		case runsFlag:
			options.spec.Runs, err = parseCount(runsFlag, value, 1)
		case genFlag:
			// Below two tokens there is no decode window at all: the first token
			// ends the prefill, and the rate is measured over the ones after it
			// (internal/serve/bench.go).
			options.spec.GenTokens, err = parseCount(genFlag, value, 2)
		}
		if err != nil {
			return options, err
		}
	}

	if len(ids) > 1 {
		return options, fmt.Errorf("one server at a time (got %s)", strings.Join(ids, ", "))
	}
	if len(ids) == 1 {
		options.id = ids[0]
	}
	return options, nil
}

// parseSizes reads the sweep. A size too small to measure is raised to the
// smallest rung rather than refused — the caller asked for a sweep, and a rung
// that cannot be measured is a value to correct, not a command to reject — and
// the correction is said out loud.
func parseSizes(value string) ([]int, []string, error) {
	var (
		sizes []int
		notes []string
	)
	for _, field := range strings.Split(value, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, nil, fmt.Errorf("%s takes prompt sizes in tokens, comma-separated", sizesFlag)
		}
		size, err := strconv.Atoi(field)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %q is not a number of tokens", sizesFlag, field)
		}
		if size < serve.BenchMinSize {
			notes = append(notes, fmt.Sprintf(
				"a %d-token prompt measures the request rather than the model; measuring the smallest rung (%d tokens) instead",
				size, serve.BenchMinSize))
			size = serve.BenchMinSize
		}
		sizes = append(sizes, size)
	}
	return sizes, notes, nil
}

// parseCount reads one of the two counting flags, refusing a value that would
// measure nothing.
func parseCount(flag, value string, least int) (int, error) {
	count, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", flag, value)
	}
	if count < least {
		return 0, fmt.Errorf("%s: %d is less than %d, which measures nothing", flag, count, least)
	}
	return count, nil
}

// benchTarget is the server a sweep will measure, or why there is none.
//
// A named entry answers for itself, running or not. With nothing named, one
// running server is the whole answer and several are the caller's to choose
// between (docs/specs/SERVE.md).
func benchTarget(listing serve.Listing, id string) (serve.Record, string) {
	if id != "" {
		for _, server := range listing.Servers {
			if server.EntryID != id {
				continue
			}
			if !server.Live {
				return serve.Record{}, fmt.Sprintf(
					"%s is not running (pid %d is no longer the process cria launched); start it first",
					id, server.PID)
			}
			return server.Record, ""
		}
		for _, broken := range listing.Broken {
			if broken.EntryID == id {
				return serve.Record{}, fmt.Sprintf(
					"%s: its state record %s cannot be read: %v; delete that file once the pid it names is gone",
					id, broken.Path, broken.Err)
			}
		}
		return serve.Record{}, fmt.Sprintf("cria has no server record for %s; nothing to measure%s", id, whatIsRecorded(listing))
	}

	var live []serve.Server
	for _, server := range listing.Servers {
		if server.Live {
			live = append(live, server)
		}
	}
	switch len(live) {
	case 0:
		return serve.Record{}, fmt.Sprintf("nothing is running%s", whatElseIsRecorded(listing))
	case 1:
		return live[0].Record, ""
	default:
		ids := make([]string, 0, len(live))
		for _, server := range live {
			ids = append(ids, server.EntryID)
		}
		return serve.Record{}, fmt.Sprintf("%d servers are running (%s); name the one to measure: cria bench <id>",
			len(live), strings.Join(ids, ", "))
	}
}

// describe is the sweep in one phrase, for the line that says what the caller
// is now waiting on.
func describe(spec serve.BenchSpec) string {
	sizes := make([]string, 0, len(spec.Sizes))
	for _, size := range spec.Sizes {
		sizes = append(sizes, strconv.Itoa(size))
	}
	return fmt.Sprintf("%s tokens of prompt, %d runs each, %d tokens generated per run",
		strings.Join(sizes, "/"), spec.Runs, spec.GenTokens)
}

// benchProgress is where a sweep has got to, one line per request. It is not an
// aside about the answer but the reason the answer is taking this long, so it
// carries no "note:" — and it goes to stderr for the same reason a note does
// (cli.go).
func (a *app) benchProgress(step serve.BenchStep) {
	if step.Warmup {
		a.waiting("  warming up (unmeasured)…")
		return
	}
	a.waiting("  %d tokens (size %d/%d), run %d/%d…", step.Size, step.Nth, step.Sizes, step.Run, step.Runs)
}

// reportBench writes the human answer: what was measured, the table, and the
// reason under any size that has none.
func (a *app) reportBench(result serve.BenchResult) {
	a.printf("%s  %s  %s  pid %d on %s\n", result.EntryID, result.Backend,
		format.HubReference(result.Repo, result.Quant), result.PID, address(result.Record))
	a.printf("%s\n\n", describe(result.Spec))

	rows := [][]string{{"size", "tokens", "prefill t/s", "ttft", "decode t/s"}}
	for _, size := range result.Sizes {
		rows = append(rows, benchRow(size))
	}
	for _, line := range aligned(rows) {
		a.printf("%s\n", line)
	}

	for _, size := range result.Sizes {
		switch {
		case size.Err == nil:
		case !size.Measured():
			a.printf("\nnot measured — %v\n", size.Err)
		default:
			a.printf("\nmeasured %d of %d runs — %v\n", len(size.Runs), result.Spec.Runs, size.Err)
		}
	}
	for _, size := range result.Sizes {
		// A prefix the server already held is a prefill that did not happen, so
		// the rate above it is not the rate of reading that prompt. Every prompt
		// cria sends starts with a nonce to prevent exactly this, and a hit
		// anyway is worth saying out loud rather than leaving in the numbers.
		if size.Cached() {
			a.note("the server answered the %d-token size partly out of its prompt cache; its prefill rate is optimistic", size.Tokens)
		}
		// A model that ends its answer early is not a slow model, but a size
		// whose answers were cut materially short had its decode rate measured
		// over a fraction of them — which is the difference between a number and
		// a stable one. Which shortfalls are material is serve's (EndedEarly):
		// the ordinary story that stops a few tokens short is not worth a line.
		if size.EndedEarly(result.Spec.GenTokens) {
			a.note("the model ended its answer early on the %d-token size (%.0f of %d tokens on average); its decode rate is measured over what it wrote",
				size.Tokens, size.Mean.GenTokens, result.Spec.GenTokens)
		}
	}
}

// benchRow is one size's line: what was asked for, what the server counted, and
// the two rates with the spread of the runs behind each.
func benchRow(size serve.BenchSize) []string {
	if !size.Measured() {
		return []string{strconv.Itoa(size.Tokens), benchMissing, benchMissing, benchMissing, benchMissing}
	}
	return []string{
		strconv.Itoa(size.Tokens),
		fmt.Sprintf("%.0f", size.Mean.PromptTokens),
		spread(size.Runs, 0, func(run serve.BenchRun) float64 { return run.PrefillRate }),
		size.Mean.TTFT.Round(time.Millisecond).String(),
		// The decode column is the runs that measured a decode: a run the model
		// ended after one token has no rate to put in it (internal/serve).
		spread(decoded(size.Runs), 1, func(run serve.BenchRun) float64 { return run.DecodeRate }),
	}
}

// decoded is the runs of a size that measured a decode rate at all.
func decoded(runs []serve.BenchRun) []serve.BenchRun {
	measured := make([]serve.BenchRun, 0, len(runs))
	for _, run := range runs {
		if run.Decoded() {
			measured = append(measured, run)
		}
	}
	return measured
}

// spread is a mean with the range its runs actually landed in. A single run has
// no spread to show, and several with nothing between them say so by their
// numbers rather than by a claim about them.
func spread(runs []serve.BenchRun, decimals int, of func(serve.BenchRun) float64) string {
	if len(runs) == 0 {
		return benchMissing
	}
	number := "%." + strconv.Itoa(decimals) + "f"

	low, high, total := of(runs[0]), of(runs[0]), 0.0
	for _, run := range runs {
		value := of(run)
		low, high, total = min(low, value), max(high, value), total+value
	}
	mean := fmt.Sprintf(number, total/float64(len(runs)))
	if len(runs) == 1 {
		return mean
	}
	return mean + fmt.Sprintf(" ("+number+"–"+number+")", low, high)
}

// The `cria bench --json` document is a projection, not a marshalled result. A
// result carries the whole state record — the process identity among it, cria's
// own bookkeeping — and a document built by marshalling it would publish that
// and would change every time the internals do.
//
// So the field names below are the machine contract (docs/specs/CLI.md): every
// one of them is always present, both lists are always lists, and a size that
// was not measured carries its reason in the same shape as one that was.
type benchDocument struct {
	Entry     string            `json:"entry"`
	Backend   string            `json:"backend"`
	Repo      string            `json:"repo"`
	Quant     string            `json:"quant"`
	Host      string            `json:"host"`
	Port      int               `json:"port"`
	PID       int               `json:"pid"`
	StartedAt time.Time         `json:"started_at"`
	Spec      specDocument      `json:"spec"`
	Sizes     []benchSizeRecord `json:"sizes"`
}

type specDocument struct {
	Sizes     []int `json:"sizes"`
	Runs      int   `json:"runs"`
	GenTokens int   `json:"gen_tokens"`
}

type benchSizeRecord struct {
	Size  int              `json:"size"`
	Error string           `json:"error"`
	Runs  []benchRunRecord `json:"runs"`
	Mean  benchRunRecord   `json:"mean"`
}

// benchRunRecord is one run, and — with its two token counts as means — one
// size's average. Both shapes carry the same keys so a script reads a row and a
// run the same way.
type benchRunRecord struct {
	PromptTokens float64 `json:"prompt_tokens"`
	CachedTokens int     `json:"cached_tokens"`
	GenTokens    float64 `json:"gen_tokens"`
	TTFTSeconds  float64 `json:"ttft_seconds"`
	PrefillRate  float64 `json:"prefill_tokens_per_second"`
	DecodeRate   float64 `json:"decode_tokens_per_second"`
}

// benchDocumentOf projects one result into the document.
func benchDocumentOf(result serve.BenchResult) benchDocument {
	document := benchDocument{
		Entry:     result.EntryID,
		Backend:   string(result.Backend),
		Repo:      result.Repo,
		Quant:     result.Quant,
		Host:      result.Host,
		Port:      result.Port,
		PID:       result.PID,
		StartedAt: result.StartedAt,
		Spec: specDocument{
			Sizes:     append(make([]int, 0, len(result.Spec.Sizes)), result.Spec.Sizes...),
			Runs:      result.Spec.Runs,
			GenTokens: result.Spec.GenTokens,
		},
		Sizes: make([]benchSizeRecord, 0, len(result.Sizes)),
	}

	for _, size := range result.Sizes {
		record := benchSizeRecord{
			Size: size.Tokens,
			Runs: make([]benchRunRecord, 0, len(size.Runs)),
			Mean: benchRunRecord{
				PromptTokens: rounded(size.Mean.PromptTokens, 1),
				GenTokens:    rounded(size.Mean.GenTokens, 1),
				TTFTSeconds:  seconds(size.Mean.TTFT),
				PrefillRate:  rounded(size.Mean.PrefillRate, 2),
				DecodeRate:   rounded(size.Mean.DecodeRate, 2),
			},
		}
		if size.Err != nil {
			record.Error = size.Err.Error()
		}
		for _, run := range size.Runs {
			record.Runs = append(record.Runs, benchRunRecord{
				PromptTokens: float64(run.PromptTokens),
				CachedTokens: run.CachedTokens,
				GenTokens:    float64(run.GenTokens),
				TTFTSeconds:  seconds(run.TTFT),
				PrefillRate:  rounded(run.PrefillRate, 2),
				DecodeRate:   rounded(run.DecodeRate, 2),
			})
		}
		document.Sizes = append(document.Sizes, record)
	}
	return document
}

// seconds is a duration as a JSON number, at the microsecond a measurement is
// meaningful to.
func seconds(d time.Duration) float64 { return rounded(d.Seconds(), 6) }

// rounded keeps a rate readable in a document a person also reads: the digits
// past these are the machine's noise, not the server's speed.
func rounded(value float64, decimals int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	scale := math.Pow(10, float64(decimals))
	return math.Round(value*scale) / scale
}
