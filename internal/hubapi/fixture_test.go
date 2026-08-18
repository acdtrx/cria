package hubapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// hubEntry is one row of a fake repo's file listing, in the shape the models
// tree API returns it.
type hubEntry struct {
	path string
	size int64
	dir  bool
}

// fakeHub serves the models tree API the way huggingface.co serves it: one
// repo, listed a page at a time, each page pointing at the next through a Link
// header. It records what every request carried, which is how the credential
// rules are tested.
type fakeHub struct {
	*httptest.Server

	repo     string
	entries  []hubEntry
	pageSize int           // entries per page; the whole listing by default
	delay    time.Duration // how long a page takes to come back

	mu    sync.Mutex
	seen  []string // the URI of every request, in order
	auths []string // the Authorization header of every request, in order
}

// newHub starts a fake Hub holding one repo.
func newHub(t *testing.T, repo string, entries ...hubEntry) *fakeHub {
	t.Helper()
	hub := &fakeHub{repo: repo, entries: entries, pageSize: len(entries)}
	hub.Server = httptest.NewServer(http.HandlerFunc(hub.serve))
	t.Cleanup(hub.Close)
	return hub
}

func (h *fakeHub) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.seen = append(h.seen, r.URL.RequestURI())
	h.auths = append(h.auths, r.Header.Get("Authorization"))
	h.mu.Unlock()

	// Not a race workaround: a Hub that answers slowly is what the timeout
	// exists for, so the test has to make one.
	if h.delay > 0 {
		time.Sleep(h.delay)
	}

	if r.URL.Path != "/api/models/"+h.repo+"/tree/main" {
		// What the real Hub answers for a repo the caller cannot see, whether it
		// is private or was never there.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"Invalid username or password."}`)
		return
	}
	if r.URL.Query().Get("recursive") != "true" {
		// Without it the Hub lists the top level only, and repos publish quants
		// in per-quant directories.
		http.Error(w, "the listing must be recursive", http.StatusBadRequest)
		return
	}

	from := 0
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		from, _ = strconv.Atoi(cursor)
	}
	to := min(from+h.pageSize, len(h.entries))
	if to < len(h.entries) {
		next := *r.URL
		next.Scheme, next.Host = "http", r.Host
		query := next.Query()
		query.Set("cursor", strconv.Itoa(to))
		next.RawQuery = query.Encode()
		w.Header().Set("Link", "<"+next.String()+`>; rel="next"`)
	}

	rows := make([]map[string]any, 0, to-from)
	for _, entry := range h.entries[from:to] {
		kind := "file"
		if entry.dir {
			kind = "directory"
		}
		rows = append(rows, map[string]any{"type": kind, "path": entry.path, "size": entry.size, "oid": "0"})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

// requests lists the URIs the hub was asked for.
func (h *fakeHub) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.seen...)
}

// credentials lists the Authorization header of every request the hub saw.
func (h *fakeHub) credentials() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.auths...)
}
