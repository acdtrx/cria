package cli

import (
	"strings"
	"testing"

	"cria/internal/engine"
)

// The flags `cria new` takes for the backends it scaffolds, spelled the way the
// surface documents them rather than read from production: a test that composed
// a flag the way new.go does would agree with a renamed flag instead of catching
// it (docs/specs/CLI.md).
const (
	llamaFlag = "--llama"
	mlxFlag   = "--mlx"
)

// Every engine cria has is one `cria new` can scaffold, and one the help page
// offers. The flags are the engine ids, while the page is written by hand — this
// is where the two meet, so an engine that reaches only one of them is a red
// suite rather than a flag nothing documents.
func TestEveryEngineHasAScaffoldFlagOnTheHelpPage(t *testing.T) {
	for _, served := range engine.All() {
		flag := backendFlag(served.ID())

		if !strings.Contains(helpPage, flag) {
			t.Errorf("the help page does not offer %s, the flag that scaffolds a %q entry", flag, served.ID())
		}
		if !strings.Contains(newUsage, flag) {
			t.Errorf("`cria new`'s usage line does not offer %s: %s", flag, newUsage)
		}

		id, backend, refusal := parseNew([]string{"qwen", flag})
		if refusal != "" {
			t.Errorf("`cria new qwen %s` was refused: %s", flag, refusal)
			continue
		}
		if id != "qwen" || backend != served.ID() {
			t.Errorf("`cria new qwen %s` scaffolds entry %q on backend %q, want %q on %q", flag, id, backend, "qwen", served.ID())
		}
	}
}
