package cli

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"cria/internal/config"
	"cria/internal/procs"
	"cria/internal/serve"
)

// runningStatus is one server as a snapshot sees it, serving.
func runningStatus() serve.Status {
	return serve.Status{
		Record: testRecord(),
		Phase:  serve.PhaseRunning,
		Uptime: 93 * time.Second,
		Stats:  procs.Stats{RSSBytes: 2 << 30, CPUPercent: 12.5},
		Health: serve.Health{URL: "http://127.0.0.1:8080/health", Green: true, Status: 200, Detail: "200 OK"},
	}
}

// exitedStatus is a crash report: the record of a server whose process is gone.
func exitedStatus(id string) serve.Status {
	record := testRecord()
	record.EntryID = id
	return serve.Status{Record: record, Phase: serve.PhaseExited}
}

// The human report carries the facts the TUI's status box shows
// (docs/specs/SERVE.md), and a live server makes the exit code zero.
func TestStatusReportsALiveServer(t *testing.T) {
	fake := &fakeServers{snapshots: serve.StatusListing{Servers: []serve.Status{runningStatus()}}}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.status(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	for _, want := range []string{
		"qwen  running  llama  unsloth/Qwen3-30B-A3B-GGUF:UD-Q4_K_XL",
		"pid 4242 on 0.0.0.0:8080, up 1m33s",
		"memory 2.0 GiB, cpu 12.5%",
		"health http://127.0.0.1:8080/health: 200 OK",
		"command /opt/homebrew/bin/llama-server -hf",
		"log /home/u/.local/state/cria/logs/qwen-20260818-145730.log",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", out, want)
		}
	}
}

// Exit code zero means "at least one server is live" (docs/specs/CLI.md): a
// state directory holding only crash reports answers non-zero, and still reports
// them.
func TestStatusExitsNonZeroWithoutALiveServer(t *testing.T) {
	fake := &fakeServers{snapshots: serve.StatusListing{Servers: []serve.Status{exitedStatus("qwen")}}}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.status(nil); code != exitFailure {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
	}
	if !strings.Contains(out.String(), "qwen  exited") || !strings.Contains(out.String(), "is gone; launched") {
		t.Errorf("cria printed %q, want the crash report it holds", out)
	}
}

// An empty state directory is an answer, not a failure to explain: it says so,
// and the exit code carries "nothing is live".
func TestStatusReportsAnEmptyStateDirectory(t *testing.T) {
	app, out, errOut := newTestApp(testTree(), &fakeServers{})

	if code := app.status(nil); code != exitFailure {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
	}
	if !strings.Contains(out.String(), "no servers") {
		t.Errorf("cria printed %q, want the empty answer spelled out", out)
	}
}

// A record file cria refused is reported rather than dropped: it names a pid
// cria started (docs/specs/SERVE.md).
func TestStatusReportsBrokenRecords(t *testing.T) {
	fake := &fakeServers{snapshots: serve.StatusListing{Broken: []serve.BrokenRecord{{
		EntryID: "gemma",
		Path:    "/home/u/.local/state/cria/servers/gemma.json",
		Err:     errors.New("entry_id is missing"),
	}}}}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.status(nil); code != exitFailure {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitFailure, errOut)
	}
	for _, want := range []string{"gemma  unreadable record", "servers/gemma.json", "entry_id is missing", "delete that file"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("cria printed %q, want it to contain %q", out, want)
		}
	}
}

// A downloading server shows where the fetch has got to, in the human report as
// well as in the JSON one.
func TestStatusReportsADownload(t *testing.T) {
	status := runningStatus()
	status.Phase = serve.PhaseDownloading
	status.Health = serve.Health{URL: "http://127.0.0.1:8080/health", Detail: "connection refused"}
	status.Progress = serve.Progress{Bytes: 512 << 20, Total: 2 << 30, Known: true}
	fake := &fakeServers{snapshots: serve.StatusListing{Servers: []serve.Status{status}}}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.status(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if !strings.Contains(out.String(), "downloading 512.0 MiB of 2.0 GiB (25%)") {
		t.Errorf("cria printed %q, want the download's progress", out)
	}
}

// The JSON document is the machine contract (docs/specs/CLI.md): one document,
// stable field names, every field present — and nothing of cria's own
// bookkeeping in it.
func TestStatusJSONDocument(t *testing.T) {
	fake := &fakeServers{snapshots: serve.StatusListing{
		Servers: []serve.Status{runningStatus()},
		Broken: []serve.BrokenRecord{{
			EntryID: "gemma",
			Path:    "/home/u/.local/state/cria/servers/gemma.json",
			Err:     errors.New("entry_id is missing"),
		}},
	}}
	app, out, errOut := newTestApp(testTree(), fake)

	if code := app.status([]string{"--json"}); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}

	var document map[string]any
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, out)
	}

	servers, ok := document["servers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("servers is %#v, want the one server the listing holds", document["servers"])
	}
	server, ok := servers[0].(map[string]any)
	if !ok {
		t.Fatalf("the server is %#v, want an object", servers[0])
	}

	// The scalar facts, by the names a script reads them under.
	wantFields := map[string]any{
		"entry":          "qwen",
		"backend":        "llama",
		"repo":           "unsloth/Qwen3-30B-A3B-GGUF",
		"quant":          "UD-Q4_K_XL",
		"host":           "0.0.0.0",
		"port":           float64(8080),
		"pid":            float64(4242),
		"phase":          "running",
		"uptime_seconds": float64(93),
		"rss_bytes":      float64(2 << 30),
		"cpu_percent":    12.5,
		"log":            "/home/u/.local/state/cria/logs/qwen-20260818-145730.log",
		"launched_at":    "2026-08-18T14:57:30Z",
	}
	for field, want := range wantFields {
		if got := server[field]; got != want {
			t.Errorf("%s is %#v, want %#v", field, got, want)
		}
	}

	command, ok := server["command"].([]any)
	if !ok || len(command) == 0 || command[0] != "/opt/homebrew/bin/llama-server" {
		t.Errorf("command is %#v, want the composed argv, program first", server["command"])
	}

	health, ok := server["health"].(map[string]any)
	if !ok {
		t.Fatalf("health is %#v, want an object", server["health"])
	}
	for field, want := range map[string]any{
		"url":    "http://127.0.0.1:8080/health",
		"green":  true,
		"status": float64(200),
		"detail": "200 OK",
	} {
		if got := health[field]; got != want {
			t.Errorf("health.%s is %#v, want %#v", field, got, want)
		}
	}

	// Progress is present even when there is no download: a script tests values,
	// never the presence of a key.
	progress, ok := server["progress"].(map[string]any)
	if !ok {
		t.Fatalf("progress is %#v, want an object", server["progress"])
	}
	for _, field := range []string{"bytes", "total", "known", "reason"} {
		if _, present := progress[field]; !present {
			t.Errorf("progress has no %q; the document's fields are always present", field)
		}
	}

	// cria's own bookkeeping stays out of the machine contract.
	if _, leaked := server["identity"]; leaked {
		t.Errorf("the document carries the process identity: %#v", server["identity"])
	}

	broken, ok := document["broken"].([]any)
	if !ok || len(broken) != 1 {
		t.Fatalf("broken is %#v, want the one record cria refused", document["broken"])
	}
	refused, ok := broken[0].(map[string]any)
	if !ok {
		t.Fatalf("the broken record is %#v, want an object", broken[0])
	}
	for field, want := range map[string]any{
		"entry": "gemma",
		"path":  "/home/u/.local/state/cria/servers/gemma.json",
		"error": "entry_id is missing",
	} {
		if got := refused[field]; got != want {
			t.Errorf("broken[0].%s is %#v, want %#v", field, got, want)
		}
	}
}

// A server composed from picks is reported as the combination it is, in both
// faces and in the vocabulary `cria start` takes — sorted, since the record is
// read without the config tree that holds the axes' file order
// (docs/specs/SERVE.md).
func TestStatusNamesTheRunningPicks(t *testing.T) {
	status := runningStatus()
	status.Selection = config.Selection{"quant": "q6", "layout": "coding"}
	fake := &fakeServers{snapshots: serve.StatusListing{Servers: []serve.Status{status}}}

	app, out, errOut := newTestApp(testTree(), fake)
	if code := app.status(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if !strings.Contains(out.String(), "\n  picks layout=coding quant=q6\n") {
		t.Errorf("cria printed %q, want the combination the server was composed from", out)
	}

	app, out, errOut = newTestApp(testTree(), fake)
	if code := app.status([]string{"--json"}); code != exitOK {
		t.Fatalf("exit code %d with --json, want %d (stderr: %s)", code, exitOK, errOut)
	}
	var document struct {
		Servers []struct {
			Picks map[string]string `json:"picks"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, out)
	}
	if len(document.Servers) != 1 {
		t.Fatalf("the document holds %d servers, want the one the listing holds", len(document.Servers))
	}
	if !maps.Equal(document.Servers[0].Picks, map[string]string{"quant": "q6", "layout": "coding"}) {
		t.Errorf("picks is %v, want the record's own selection", document.Servers[0].Picks)
	}
}

// A flat entry has no combination, so neither face invents one: no picks line,
// and no key in the document.
func TestStatusOmitsPicksForAFlatEntry(t *testing.T) {
	fake := &fakeServers{snapshots: serve.StatusListing{Servers: []serve.Status{runningStatus()}}}

	app, out, errOut := newTestApp(testTree(), fake)
	if code := app.status(nil); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", code, exitOK, errOut)
	}
	if strings.Contains(out.String(), "picks") {
		t.Errorf("cria printed %q, want a flat entry's block exactly as it always was", out)
	}

	app, out, errOut = newTestApp(testTree(), fake)
	if code := app.status([]string{"--json"}); code != exitOK {
		t.Fatalf("exit code %d with --json, want %d (stderr: %s)", code, exitOK, errOut)
	}
	var document map[string]any
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, out)
	}
	servers, ok := document["servers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("servers is %#v, want the one server the listing holds", document["servers"])
	}
	server, ok := servers[0].(map[string]any)
	if !ok {
		t.Fatalf("the server is %#v, want an object", servers[0])
	}
	if _, present := server["picks"]; present {
		t.Errorf("the document carries picks for a flat entry: %#v", server["picks"])
	}
}

// An empty state directory is still one document with both lists, so a script
// iterates them instead of testing for null.
func TestStatusJSONIsAlwaysADocument(t *testing.T) {
	app, out, _ := newTestApp(testTree(), &fakeServers{})

	if code := app.status([]string{"--json"}); code != exitFailure {
		t.Fatalf("exit code %d, want %d", code, exitFailure)
	}
	var document struct {
		Servers []any `json:"servers"`
		Broken  []any `json:"broken"`
	}
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, out)
	}
	if document.Servers == nil || document.Broken == nil {
		t.Errorf("the document is %s, want both lists present and empty", out)
	}
}
