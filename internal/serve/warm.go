package serve

import (
	"bytes"
	"encoding/json"
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
// It is a request to the backend's own documented endpoint, like the health
// probe (docs/specs/SERVE.md) — nothing here reads a log or a payload beyond the
// status line and, when a server refuses, the reason it gives.

const (
	// warmPath is the documented OpenAI-shaped endpoint both backends publish.
	// The completion endpoint rather than the chat one: it takes a raw prompt,
	// so it needs no chat template — one fewer thing about a model that can
	// refuse a request whose only job is to make the weights load.
	warmPath = "/v1/completions"

	// warmPrompt is the prompt that load costs. One short word: the request has
	// to reach the model, and nothing more is asked of it. Never empty —
	// mlx_lm.server takes an empty prompt and never answers, wedging the server
	// it was meant to warm (verified against mlx_lm 0.32.0, 2026-08-18).
	warmPrompt = "hi"

	// warmTokens is how much of an answer cria waits for: the single token that
	// proves the model generated.
	warmTokens = 1

	// warmBudget bounds one warm. Loading weights is minutes of work for a large
	// model on a cold cache — an order of magnitude more than any probe cria
	// takes — so the budget is generous: it is here to end a wait that will never
	// finish, not to judge a slow load.
	warmBudget = 15 * time.Minute

	// warmReason is how much of a refused warm's body is quoted back. The
	// servers answer errors as one short JSON object; this is enough of it to
	// carry the reason without pasting a payload into a terminal.
	warmReason = 512
)

// warmRequest is the cheapest completion the endpoint takes: the model the
// record launched, one word of prompt, one token of answer.
//
// The model is named rather than left out. Both servers accept a request with no
// model and serve whatever they were launched with, but the record already holds
// the exact reference cria passed on the command line, and saying it is what
// makes this request about *this* server's model rather than about its default.
type warmRequest struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	MaxTokens int    `json:"max_tokens"`
}

// warmer sends one warm to a server and reports whether it answered. It is the
// seam the component tests replace, the same shape as the probe and the spawner,
// so every rule around a warm runs with no server and no port.
type warmer func(url, model string, within time.Duration) error

// LoadsLazily reports whether a backend's server goes green before it has loaded
// any weights. mlx_lm.server does — its model listing answers immediately and
// the load happens on the first completion — while llama-server answers 503 at
// /health until its model is in memory, so a green llama server has nothing left
// to load (docs/specs/SERVE.md).
func LoadsLazily(backend config.Backend) bool { return backend == config.BackendMLX }

// Warm makes a server load its weights now, so the first request a caller sends
// meets a model that is ready. A backend that loads at startup has nothing to
// warm and Warm does nothing for it — the rule lives here rather than in each
// caller.
//
// The error is what happened, never a verdict on the server: a warm that fails
// says the completion did not come back, and the server may well still be
// serving. Its caller decides what that is worth.
func (m *Manager) Warm(record Record) error {
	if !LoadsLazily(record.Backend) {
		return nil
	}
	if err := m.warm(warmURL(record), record.Repo, m.warmWithin); err != nil {
		return fmt.Errorf("%s did not load its weights: %w", record.EntryID, err)
	}
	return nil
}

// warmURL is where a warm is sent: the same address a probe goes to — loopback
// for a wildcard bind, the bound address otherwise (docs/specs/CONFIG.md) — at
// the completion endpoint.
func warmURL(record Record) string { return serverURL(record, warmPath) }

// newHTTPWarm builds the real warmer: one POST, no retry. A server that will not
// answer this request is a fact to report, not a call to make again.
func newHTTPWarm() warmer {
	return func(url, model string, within time.Duration) error {
		body, err := json.Marshal(warmRequest{Model: model, Prompt: warmPrompt, MaxTokens: warmTokens})
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

// refusal is what a server said when it refused the warm, as a tail for the
// error naming the status. Both servers answer a short JSON object here, and it
// carries the one thing the status code does not: which model, or which field,
// it could not take. A body that could not be read costs the error nothing —
// the status is already in it.
func refusal(body io.Reader) string {
	said, err := io.ReadAll(io.LimitReader(body, warmReason))
	if err != nil || len(bytes.TrimSpace(said)) == 0 {
		return ""
	}
	return ": " + strings.Join(strings.Fields(string(said)), " ")
}
