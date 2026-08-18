package cli

import (
	"fmt"

	"cria/internal/format"
	"cria/internal/serve"
)

// downloaded phrases how far a download has got. The percentage needs a total,
// and a total needs the Hub: when it could not answer, the bytes on disk are
// still the honest half of the answer and the reason travels with them
// (docs/specs/SERVE.md).
func downloaded(progress serve.Progress) string {
	if progress.Known && progress.Total > 0 {
		return fmt.Sprintf("%s of %s (%.0f%%)", format.Bytes(progress.Bytes), format.Bytes(progress.Total),
			100*float64(progress.Bytes)/float64(progress.Total))
	}
	if progress.Reason == "" {
		return fmt.Sprintf("%s so far (no total)", format.Bytes(progress.Bytes))
	}
	return fmt.Sprintf("%s so far (no total: %s)", format.Bytes(progress.Bytes), progress.Reason)
}
