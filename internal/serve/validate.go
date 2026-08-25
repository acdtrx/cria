package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cria/internal/config"
)

// Validating an entry means starting it where something else may already be
// serving: the server holding the target's port is stopped, the target is
// proved, and the displaced server is put back from what cria wrote down about
// it. This file answers the two questions that come before any of that — which
// server would be displaced, and whether it may be displaced right now.
//
// The port is the whole scope of a displacement. The one server that may be
// stopped is the live record holding the target's port, and the target entry
// carries that knowledge on its own: a caller needs to know nothing about what
// else runs on the host, and a server on another port cannot be touched at all.

const (
	// slotsPath is llama-server's documented per-slot endpoint, and the only
	// place either backend publishes what a server is doing right now.
	slotsPath = "/slots"

	// slotsTimeout bounds the one look the gate takes. A server worth asking is
	// busy answering somebody else, so it is given more patience than a display
	// probe (health.go) — and still only one look, because a gate that waits is
	// a refusal that arrives late.
	slotsTimeout = 2 * time.Second
)

// Displacement is what stands between a target entry and the port it launches
// on: the one server a validation may stop, or the processes it has to refuse
// over.
//
// The two answers are the two a port attribution gives (port.go), read in the
// vocabulary of a swap. A live record is a server cria started, so it can be
// stopped and started again from what it recorded. A process cria did not start
// has no record to restore from, which is what makes it a refusal rather than a
// displacement.
type Displacement struct {
	Port    int      // the port the target launches on
	Holder  *Record  // the live record serving there; nil when no server of cria's is
	Foreign []Holder // the processes serving there that cria did not start; empty when none are
}

// Free reports whether the target's port is nobody's: there is nothing to
// displace, and nothing to put back afterwards.
func (d Displacement) Free() bool { return d.Holder == nil && len(d.Foreign) == 0 }

// Displaced answers what starting one entry would displace.
//
// The port is the entry's own resolved port — the value a start composes with
// (docs/specs/CONFIG.md) — and no pick can move it: an option replaces a
// quantization, a repo or flags, never the address a server binds. So the entry
// alone says which server a validation displaces, whatever combination of it is
// being validated.
//
// An entry that holds its own port is answered like any other: the record found
// there is the one to stop and the one to restore, whether or not it belongs to
// the target.
func (m *Manager) Displaced(entry config.Entry) (Displacement, error) {
	use, err := m.PortUse(entry.Port)
	if err != nil {
		return Displacement{}, err
	}

	displaced := Displacement{Port: entry.Port, Foreign: use.Holders}
	if use.Managed != nil {
		displaced.Holder = &use.Managed.Record
	}
	return displaced, nil
}

// Busy is what cria could tell about a server's work at one moment.
type Busy string

const (
	BusyIdle         Busy = "idle"         // every slot the server published is free
	BusyGenerating   Busy = "generating"   // at least one slot is answering a request
	BusyUnverifiable Busy = "unverifiable" // there was no signal to read, which is not the same as idle
)

// Generation is one look at whether a server is answering somebody right now,
// and what stood in the way when it could not be told.
//
// Unverifiable is a third answer rather than a cautious idle. Whoever is about
// to stop a server has to know the difference between "nobody is waiting on it"
// and "cria cannot see who is": only one of those is a fact about the server,
// and only the caller knows what the other one is worth.
type Generation struct {
	Busy   Busy
	Detail string // why there was nothing to read; empty when there was
}

// slotsReader asks one server which of its slots are working. It is the seam
// the component tests replace — the same shape as the probe, the warm and the
// bench — so the gate's rules run with no server and no port.
type slotsReader func(url string) Generation

// Generating reports whether the server one record names is mid-generation —
// the question that decides whether stopping it would cut somebody off.
//
// The signal is per-slot is_processing, never the number of open connections: a
// caller about to displace a server is holding an idle keep-alive socket to it,
// and counting connections would answer with the caller's own ghost.
//
// A backend that publishes no such signal is not asked at all. mlx_lm.server
// documents no equivalent, and deriving one from something adjacent would hand
// back a guess spelled like a measurement.
func (m *Manager) Generating(record Record) Generation {
	if !publishesSlots(record.Backend) {
		return unverifiable("mlx_lm.server publishes no per-slot signal, so cria cannot tell whether it is generating")
	}
	return m.slots(slotsURL(record))
}

// publishesSlots reports whether a backend says what its slots are doing.
// llama-server publishes it at /slots; mlx_lm.server documents nothing of the
// kind. A record carries one of the two backends and nothing else survives its
// validation (record.go), so llama is the whole of the yes.
func publishesSlots(backend config.Backend) bool { return backend == config.BackendLlama }

// slotsURL is where the gate asks: the same address rule the probe and the warm
// follow — loopback for a wildcard bind, the bound address otherwise
// (docs/specs/CONFIG.md) — at the slot endpoint.
func slotsURL(record Record) string { return serverURL(record, slotsPath) }

// slot is the one thing cria reads of a llama-server slot. The endpoint
// publishes far more per slot — context sizes, token counts, sampler settings —
// and it is the server's payload to grow: unknown fields are ignored rather
// than refused, because this reads a single flag out of somebody else's API
// (CODING-RULES §4).
type slot struct {
	IsProcessing bool `json:"is_processing"`
}

// newHTTPSlots builds the real reader: one GET, no retry. Everything that can
// stand between it and the flag — a build that does not publish the endpoint, a
// server that is not answering, a payload cria cannot read — comes back as the
// same answer, and that answer is never idle.
func newHTTPSlots() slotsReader {
	client := &http.Client{Timeout: slotsTimeout}
	return func(url string) Generation {
		response, err := client.Get(url)
		if err != nil {
			return unverifiable(fmt.Sprintf("%s: %s", url, requestFailure(err, slotsTimeout)))
		}
		defer response.Body.Close()

		// A server whose slot endpoint is not enabled refuses this in the shape
		// it refuses anything, and what it says there is the reason the signal is
		// missing — quoted as the server gave it (warm.go, refusal).
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return unverifiable(fmt.Sprintf("%s answered %s%s", url, response.Status, refusal(response.Body)))
		}

		var slots []slot
		if err := json.NewDecoder(response.Body).Decode(&slots); err != nil {
			return unverifiable(fmt.Sprintf("%s answered something cria cannot read as a slot listing: %v", url, err))
		}
		// A listing with nothing in it carries no flag either way. Reading it as
		// idle would be an answer with no evidence under it, which is the one
		// shape this gate must not take.
		if len(slots) == 0 {
			return unverifiable(fmt.Sprintf("%s listed no slots at all", url))
		}

		for _, published := range slots {
			if published.IsProcessing {
				return Generation{Busy: BusyGenerating}
			}
		}
		return Generation{Busy: BusyIdle}
	}
}

// unverifiable is the answer to everything that stands between the gate and the
// signal, carrying what happened so a caller can name it.
func unverifiable(detail string) Generation {
	return Generation{Busy: BusyUnverifiable, Detail: detail}
}
