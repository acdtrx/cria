package cli

import (
	"strings"
	"testing"

	"cria/internal/config"
)

// The flags `cria new` takes for the backends it scaffolds, spelled the way the
// surface documents them rather than read from production: a test that composed
// a flag the way new.go does would agree with a renamed flag instead of catching
// it (docs/specs/CLI.md).
const (
	llamaFlag  = "--llama"
	mlxFlag    = "--mlx"
	routerFlag = "--router"
)

// Every backend an entry may declare is one `cria new` can scaffold, and one the
// help page offers. The flags are the backend ids, while the page is written by
// hand — this is where the two meet, so a backend that reaches only one of them
// is a red suite rather than a flag nothing documents.
func TestEveryBackendHasAScaffoldFlagOnTheHelpPage(t *testing.T) {
	for _, backend := range config.Backends() {
		flag := backendFlag(backend)

		if !strings.Contains(helpPage, flag) {
			t.Errorf("the help page does not offer %s, the flag that scaffolds a %q entry", flag, backend)
		}
		if !strings.Contains(newUsage, flag) {
			t.Errorf("`cria new`'s usage line does not offer %s: %s", flag, newUsage)
		}

		id, scaffolded, refusal := parseNew([]string{"qwen", flag})
		if refusal != "" {
			t.Errorf("`cria new qwen %s` was refused: %s", flag, refusal)
			continue
		}
		if id != "qwen" || scaffolded != backend {
			t.Errorf("`cria new qwen %s` scaffolds entry %q on backend %q, want %q on %q", flag, id, scaffolded, "qwen", backend)
		}
	}
}

// An engine no entry declares has no entry file to scaffold, and asking for one
// is answered with why rather than with "unknown flag": the models the router
// serves are ordinary entries, and what makes them the router's lives outside
// the tree (docs/specs/CONFIG.md).
func TestScaffoldingAnEngineThatHasNoEntryFileSaysWhy(t *testing.T) {
	_, _, refusal := parseNew([]string{"qwen", routerFlag})
	if refusal == "" {
		t.Fatalf("`cria new qwen %s` scaffolded something; the router declares no entries", routerFlag)
	}
	for _, want := range []string{`"router"`, "engines/router.toml", llamaFlag} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal reads %q, want it to name %q", refusal, want)
		}
	}
	if strings.Contains(refusal, "unknown flag") {
		t.Errorf("the refusal reads %q, want the reason rather than a typo's answer", refusal)
	}
}
