package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cria/internal/config"
)

// A server that answers is not always a server that can serve. mlx_lm.server
// lists its models the moment its HTTP listener is up and loads the weights on
// the first completion instead, so a green mlx server still owes the caller
// every second of that load. Warming is cria paying it: one minimal completion,
// sent by cria, so the first real request meets a loaded model.
//
// The same request is what proves a server serves at all, whatever its backend
// (validate.go): a model that answers one completion is a model that fits. So
// the request lives here and both purposes send it — the rule about which
// backends have anything to warm is Warm's alone.
//
// It is a request to the backend's own documented endpoint, like the health
// probe (docs/specs/SERVE.md) — nothing here reads a log or a payload beyond the
// status line and, when a server refuses, the reason it gives.

const (
	// completionPath is the documented OpenAI-shaped endpoint both backends
	// publish. The completion endpoint rather than the chat one: it takes a raw
	// prompt, so it needs no chat template — one fewer thing about a model that
	// can refuse a request whose only job is to make the weights load.
	completionPath = "/v1/completions"

	// completionPrompt is the prompt that load costs. One short word: the
	// request has to reach the model, and nothing more is asked of it. Never
	// empty — mlx_lm.server takes an empty prompt and never answers, wedging the
	// server it was meant to warm (verified against mlx_lm 0.32.0, 2026-08-18).
	completionPrompt = "hi"

	// completionTokens is how much of an answer cria waits for: the single token
	// that proves the model generated.
	completionTokens = 1

	// completionBudget bounds each wait one of these requests is made of: for
	// the server to answer at all, and then for the completion itself. Loading
	// weights is minutes of work for a large model on a cold cache — an order of
	// magnitude more than any probe cria takes — so the budget is generous: it
	// is here to end a wait that will never finish, not to judge a slow load.
	// One budget each rather than one shared between them, so whichever wait a
	// warm ends in says what it was given.
	completionBudget = 15 * time.Minute

	// warmPoll is how often the wait asks whether the server is answering yet.
	// A fresh mlx server binds its port seconds after the spawn, so this is
	// short enough that the completion follows the listener closely and long
	// enough that a minutes-long load is not thousands of probes.
	warmPoll = 250 * time.Millisecond

	// refusalQuote is how much of a refused request's body is quoted back. The
	// servers answer errors as one short JSON object; this is enough of it to
	// carry the reason without pasting a payload into a terminal.
	refusalQuote = 512
)

// completionRequest is the cheapest completion the endpoint takes: the model the
// record launched, one word of prompt, one token of answer.
//
// The model is named rather than left out. Both servers accept a request with no
// model and serve whatever they were launched with, but the record already holds
// the exact reference cria passed on the command line, and saying it is what
// makes this request about *this* server's model rather than about its default.
type completionRequest struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	MaxTokens int    `json:"max_tokens"`
}

// completer sends one of those completions and reports whether the server
// answered it. It is the seam the component tests replace, the same shape as the
// probe and the spawner, so every rule around a warm and around a proof runs
// with no server and no port.
type completer func(url, model string, within time.Duration) error

// LoadsLazily reports whether a backend's server goes green before it has loaded
// any weights. mlx_lm.server does — its model listing answers immediately and
// the load happens on the first completion — while llama-server answers 503 at
// /health until its model is in memory, so a green llama server has nothing left
// to load (docs/specs/SERVE.md).
func LoadsLazily(backend config.Backend) bool { return backend == config.BackendMLX }

// ErrServerGone is a warm that had nothing left to warm: the process died while
// cria was waiting for its port to answer. It is named so a caller can tell it
// apart from a server that is up and would not answer — the state records
// already say a dead server is dead, and a caller with a display of them has
// nothing to add (docs/specs/TUI.md).
var ErrServerGone = errors.New("it exited before it answered")

// Warm makes a server load its weights now, so the first request a caller sends
// meets a model that is ready. A backend that loads at startup has nothing to
// warm and Warm does nothing for it — the rule lives here rather than in each
// caller.
//
// It waits for the server to answer before asking it for anything. A start
// returns as soon as the record is written (start.go) and the port comes up
// afterwards, so the two callers are on opposite sides of that gap: `--wait`
// warms a server it has already watched go green, while the TUI fires the warm
// straight off the start. The wait is what makes them the same call.
//
// The error is what happened, never a verdict on the server: a warm that fails
// says the completion did not come back, and the server may well still be
// serving. Its caller decides what that is worth.
func (m *Manager) Warm(record Record) error {
	if !LoadsLazily(record.Backend) {
		return nil
	}
	if err := m.awaitAnswer(record); err != nil {
		return fmt.Errorf("%s did not load its weights: %w", record.EntryID, err)
	}
	if err := m.complete(completionURL(record), record.Repo, m.completionWithin); err != nil {
		return fmt.Errorf("%s did not load its weights: %w", record.EntryID, err)
	}
	return nil
}

// awaitAnswer holds the warm until the server is answering.
//
// A completion sent the moment a server was spawned is answered by nothing:
// mlx_lm.server binds its port seconds after the spawn, and the request lands
// in that gap as "connection refused" — a load that is going perfectly well,
// reported as a server that did not load. The signal waited on is the one a
// phase is read from (health.go), which for a lazily-loading backend goes green
// before a single weight is read: exactly the moment there is something to warm.
//
// The wait ends on the pid as well as on the clock. A server that has died has
// nothing left to answer, and sitting out a budget measured in minutes to say so
// would delay the truth rather than establish it.
func (m *Manager) awaitAnswer(record Record) error {
	deadline := time.Now().Add(m.completionWithin)
	url := probeURL(record)
	for {
		if health := m.probe(url); health.Green {
			return nil
		}
		live, err := m.Live(record)
		if err != nil {
			return err
		}
		if !live {
			return ErrServerGone
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%s did not answer within %s", url, m.completionWithin)
		}
		// Observation on an interval: whether a port has come up lives in another
		// process, and there is no event to wait on (CODING-RULES §6).
		time.Sleep(m.warmPoll)
	}
}

// completionURL is where the request is sent: the same address a probe goes to
// — loopback for a wildcard bind, the bound address otherwise
// (docs/specs/CONFIG.md) — at the completion endpoint.
func completionURL(record Record) string { return serverURL(record, completionPath) }

// newHTTPCompletion builds the real sender: one POST, no retry. A server that
// will not answer this request is a fact to report, not a call to make again.
func newHTTPCompletion() completer {
	return func(url, model string, within time.Duration) error {
		body, err := json.Marshal(completionRequest{Model: model, Prompt: completionPrompt, MaxTokens: completionTokens})
		if err != nil {
			return fmt.Errorf("cannot compose the completion %s takes: %w", url, err)
		}

		client := &http.Client{Timeout: within}
		response, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("%s: %s", url, requestFailure(err, within))
		}
		defer response.Body.Close()

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("%s answered %s%s", url, response.Status, refusal(response.Body))
		}
		return nil
	}
}

// refusal is what a server said when it refused a request, as a tail for the
// error naming the status. Both servers answer a short JSON object here, and it
// carries the one thing the status code does not: which model, or which field,
// it could not take. A body that could not be read costs the error nothing —
// the status is already in it.
func refusal(body io.Reader) string {
	said, err := io.ReadAll(io.LimitReader(body, refusalQuote))
	if err != nil || len(bytes.TrimSpace(said)) == 0 {
		return ""
	}
	return ": " + strings.Join(strings.Fields(string(said)), " ")
}
