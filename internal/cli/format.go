package cli

import (
	"fmt"

	"cria/internal/serve"
)

// byteUnits are the binary units model sizes are quoted in, the same ones the
// Hub and `du -h` use.
var byteUnits = []string{"KiB", "MiB", "GiB", "TiB"}

// formatBytes spells a size the way a person reads one. Bytes below a kibibyte
// are printed as they are — a size that small is a number, not a scale.
func formatBytes(bytes int64) string {
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

// downloaded phrases how far a download has got. The percentage needs a total,
// and a total needs the Hub: when it could not answer, the bytes on disk are
// still the honest half of the answer and the reason travels with them
// (docs/specs/SERVE.md).
func downloaded(progress serve.Progress) string {
	if progress.Known && progress.Total > 0 {
		return fmt.Sprintf("%s of %s (%.0f%%)", formatBytes(progress.Bytes), formatBytes(progress.Total),
			100*float64(progress.Bytes)/float64(progress.Total))
	}
	if progress.Reason == "" {
		return fmt.Sprintf("%s so far (no total)", formatBytes(progress.Bytes))
	}
	return fmt.Sprintf("%s so far (no total: %s)", formatBytes(progress.Bytes), progress.Reason)
}
