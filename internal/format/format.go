// Package format spells machine facts the way a person reads them. It carries
// presentation only — no subsystem's data and no judgement about what a number
// means, just how it is written down — so every renderer cria has quotes one
// size the same way instead of keeping its own copy (CODING-RULES §2).
package format

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// byteUnits are the binary units model sizes are quoted in, the same ones the
// Hub and `du -h` use.
var byteUnits = []string{"KiB", "MiB", "GiB", "TiB"}

// Bytes spells a size the way a person reads one. Bytes below a kibibyte are
// printed as they are — a size that small is a number, not a scale.
func Bytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	value, unit := float64(bytes)/1024, byteUnits[0]
	for _, next := range byteUnits[1:] {
		if value < 1024 {
			break
		}
		value, unit = value/1024, next
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

// HubReference spells the model a config entry or a state record names, the way
// the entry named it: the repo, qualified by its quantization when there is one
// (docs/cria.md, principle 2). An mlx model has no quantization to qualify — the
// repo is the quant — so it is the repo alone.
func HubReference(repo, quant string) string {
	if quant == "" {
		return repo
	}
	return repo + ":" + quant
}

// Picks spells the combination a launch was composed from the way
// `cria start <id> choice=option` takes it — `quant=q6 layout=coding` — so what
// a status names is what reproduces it: one vocabulary across the CLI's block,
// its JSON document and the TUI's status box (docs/specs/CLI.md).
//
// The order is sorted, and deliberately not the entry's. A record is
// self-contained (docs/specs/SERVE.md): a status is read without the config
// tree, so that editing or deleting an entry never confuses its running server,
// and the file order its axes were written in is not something the record
// carries. Sorted is what a JSON document's keys come out in anyway, so every
// face of one status agrees.
func Picks(selection map[string]string) string {
	pairs := make([]string, 0, len(selection))
	for _, choice := range slices.Sorted(maps.Keys(selection)) {
		pairs = append(pairs, choice+"="+selection[choice])
	}
	return strings.Join(pairs, " ")
}
