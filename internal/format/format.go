// Package format spells machine facts the way a person reads them. It carries
// presentation only — no subsystem's data and no judgement about what a number
// means, just how it is written down — so every renderer cria has quotes one
// size the same way instead of keeping its own copy (CODING-RULES §2).
package format

import "fmt"

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
