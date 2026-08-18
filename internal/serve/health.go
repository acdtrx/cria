package serve

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"cria/internal/config"
)

// probeTimeout bounds one health probe. The probe sits on the TUI's refresh tick
// and on `cria status`, and it goes to a port on this very machine: a server
// that has not answered within a second is not answering, and waiting longer
// would only hold the display.
const probeTimeout = time.Second

// The documented endpoints cria asks each backend about (docs/specs/SERVE.md).
// Both are part of their server's published API, which is the whole reason the
// phase can be read from them rather than mined out of a log (docs/cria.md,
// principle 6).
const (
	llamaHealthPath = "/health"    // llama-server's own: 200 once the model is loaded, 503 while it still is
	mlxHealthPath   = "/v1/models" // mlx_lm.server publishes no health endpoint; its model listing is the documented proof of life
)

// Health is what one probe came back with: where cria asked, whether the answer
// was the server serving, and the answer itself for display. It is one probe's
// result, not a verdict — turning it into a phase is derivePhase's job, because
// the same red answer means "still starting" or "unhealthy" depending on what
// this server has answered before.
type Health struct {
	URL    string `json:"url"`              // the endpoint probed, exactly as it was asked
	Green  bool   `json:"green"`            // the server answered 2xx: it is serving
	Status int    `json:"status,omitempty"` // the HTTP status it answered with; zero when nothing answered
	Detail string `json:"detail"`           // the status line, or why there was no answer
}

// prober asks one URL whether a server is answering there. It is the seam the
// component tests replace — the same shape as the process table and the
// spawner, so the whole phase logic runs with no server and no port.
type prober func(url string) Health

// probeURL is the endpoint cria asks about one record's server: the address it
// can be reached at, and the documented path for its backend.
func probeURL(record Record) string { return serverURL(record, healthPath(record.Backend)) }

// serverURL is where cria reaches one record's server at a given path. Every
// request cria makes to a managed server goes through here, so the probe and the
// warm can never disagree about which address a bind means.
func serverURL(record Record, path string) string {
	address := net.JoinHostPort(probeTarget(record.Host), strconv.Itoa(record.Port))
	return "http://" + address + path
}

// probeTarget is the address to probe for a server bound to host. A wildcard
// bind answers on every address the host has, and the one of those cria can
// always reach is loopback; any other bind names the single address the server
// listens on, and that is where the probe goes (docs/specs/CONFIG.md).
func probeTarget(host string) string {
	switch host {
	case "0.0.0.0":
		return "127.0.0.1"
	case "::", "[::]":
		return "::1"
	}
	return host
}

// healthPath is the endpoint a backend answers on. A record carries one of the
// two backends — nothing else survives its validation — and llama's is the one
// the spec names first, so it is also the fallback.
func healthPath(backend config.Backend) string {
	if backend == config.BackendMLX {
		return mlxHealthPath
	}
	return llamaHealthPath
}

// newHTTPProbe builds the real prober: one GET per observation, no retry. A
// probe is an observation, never control flow (CODING-RULES §6) — a server that
// is not answering yet is a phase to display, not a failure to retry around.
func newHTTPProbe() prober {
	client := &http.Client{Timeout: probeTimeout}
	return func(url string) Health {
		response, err := client.Get(url)
		if err != nil {
			return Health{URL: url, Detail: requestFailure(err, probeTimeout)}
		}
		// The body is never read: the status is the whole answer, and nothing
		// cria displays comes out of a server's payload.
		defer response.Body.Close()
		return Health{
			URL:    url,
			Green:  response.StatusCode >= 200 && response.StatusCode < 300,
			Status: response.StatusCode,
			Detail: response.Status,
		}
	}
}

// requestFailure phrases a request to a server that got no answer at all — a
// probe, or the warm that follows a start (warm.go) — bounded by the budget that
// one was given. net/http wraps its errors in the whole request ("Get
// \"http://…\": dial tcp …: connect: connection refused"), which is a paragraph
// to read a port off; the innermost error is the two words that matter, and the
// URL is carried alongside anyway.
func requestFailure(err error, within time.Duration) string {
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return fmt.Sprintf("no answer within %s", within)
	}
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err.Error()
		}
		err = inner
	}
}
