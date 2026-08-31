package picks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cria/internal/config"
)

// The router's store is read back exactly as it was written: which entries it
// holds, and the combination each is held under. Inclusion is the key, so an
// entry held under no picks at all survives a round trip as an entry the router
// holds.
func TestTheRouterStoreRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "engines", "router")

	held := Router{}
	held.Include("qwen", config.Selection{"quant": "q6", "context": "long"})
	held.Include("gemma", config.Selection{})

	if err := SaveRouter(dir, held); err != nil {
		t.Fatalf("saving the router's models: %v", err)
	}
	read, err := LoadRouter(dir)
	if err != nil {
		t.Fatalf("reading the router's models back: %v", err)
	}

	if got := strings.Join(read.IDs(), ", "); got != "gemma, qwen" {
		t.Errorf("the router holds %q, want both entries sorted by id", got)
	}
	if got := read.Picks("qwen"); got["quant"] != "q6" || got["context"] != "long" {
		t.Errorf("qwen is held under %v, want the combination it was included with", got)
	}
	if !read.Holds("gemma") {
		t.Error("an entry held under no picks came back as one the router does not hold")
	}
	if picked := read.Picks("gemma"); len(picked) != 0 {
		t.Errorf("gemma is held under %v, want nothing picked", picked)
	}

	// The file is the shape a person reads while wondering what the router serves.
	want := "{\n  \"models\": {\n    \"gemma\": {},\n    \"qwen\": {\n      \"context\": \"long\",\n      \"quant\": \"q6\"\n    }\n  }\n}\n"
	if got := readStore(t, dir); got != want {
		t.Errorf("the store reads\n%s\nwant\n%s", got, want)
	}
}

// A host whose router has never held anything has no file, and that is a fresh
// router rather than a problem to report.
func TestAnAbsentRouterStoreHoldsNothing(t *testing.T) {
	held, err := LoadRouter(filepath.Join(t.TempDir(), "engines", "router"))
	if err != nil {
		t.Fatalf("reading a store that is not there: %v", err)
	}
	if len(held.IDs()) != 0 {
		t.Errorf("an absent store holds %v, want nothing", held.IDs())
	}

	// And it is usable as it stands: including into it needs no map of its own.
	held.Include("qwen", nil)
	if !held.Holds("qwen") {
		t.Error("an entry included into a store that was never read is not held")
	}
}

// Include replaces the combination an entry is held under; Exclude says whether
// it dropped anything, which is what makes a second exclude an answer rather
// than a silent success.
func TestIncludingAndExcludingOneEntry(t *testing.T) {
	held := Router{}
	held.Include("qwen", config.Selection{"quant": "q4"})
	held.Include("qwen", config.Selection{"quant": "q6"})

	if picked := held.Picks("qwen"); picked["quant"] != "q6" {
		t.Errorf("qwen is held under %v, want the combination it was last included with", picked)
	}
	if !held.Exclude("qwen") {
		t.Error("excluding a held entry reported that nothing changed")
	}
	if held.Exclude("qwen") {
		t.Error("excluding an entry the router does not hold reported a change")
	}
	if held.Holds("qwen") {
		t.Error("an excluded entry is still held")
	}
}

// Nothing the store hands out is the store itself: what a caller does with a
// combination it was given, or with the one it included, must not rewrite the
// file.
func TestTheRouterStoreLeavesItsInputsAlone(t *testing.T) {
	included := config.Selection{"quant": "q4"}
	held := Router{}
	held.Include("qwen", included)

	included["quant"] = "q8"
	handed := held.Picks("qwen")
	handed["context"] = "long"

	if picked := held.Picks("qwen"); picked["quant"] != "q4" || len(picked) != 1 {
		t.Errorf("qwen is held under %v, want the combination as it was included", picked)
	}
}

// The store is cria's own format, so it is validated loudly on read: an unknown
// key, a wrong shape or a name that can never match anything is a file to fix,
// never a silent default (CLAUDE.md, feature-building mode).
//
// It refuses rather than falling back to an empty router, which is where it
// parts from the picks beside it: an empty answer would start a router serving
// nothing and look like success.
func TestARouterStoreThatCannotBeReadIsRefused(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{name: "an unknown key", file: `{"models": {}, "engine": "router"}`, want: "engine"},
		{name: "no models object", file: `{}`, want: "no models object"},
		{name: "a null instead of a models object", file: `{"models": null}`, want: "no models object"},
		{name: "an entry held with no picks object", file: `{"models": {"qwen": null}}`, want: "no picks object"},
		{name: "an empty entry id", file: `{"models": {"": {}}}`, want: "empty entry id"},
		{name: "an unnamed choice", file: `{"models": {"qwen": {"": "q4"}}}`, want: "unnamed choice"},
		{name: "a choice picking nothing", file: `{"models": {"qwen": {"quant": ""}}}`, want: `picks nothing for choice "quant"`},
		{name: "the wrong type entirely", file: `{"models": {"qwen": "q4"}}`, want: "cannot unmarshal"},
		{name: "two documents", file: `{"models": {}} {"models": {}}`, want: "more than one JSON document"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(routerModelsPath(dir), []byte(test.file), 0o644); err != nil {
				t.Fatalf("writing the store: %v", err)
			}

			held, err := LoadRouter(dir)
			if err == nil {
				t.Fatalf("%s was read as %v", test.name, held)
			}
			for _, want := range []string{test.want, routerModelsPath(dir)} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal reads %v, want it to name %q", err, want)
				}
			}
		})
	}
}

// readStore is the store file's whole text, for the comparison the file's shape
// is held to.
func readStore(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(routerModelsPath(dir))
	if err != nil {
		t.Fatalf("reading the store: %v", err)
	}
	return string(data)
}
