//go:build !darwin

package cli

import (
	"errors"
	"runtime"
)

// physicalMemoryMB refuses off macOS: iogpu.wired_limit_mb is an Apple-silicon
// knob, and a plist for a machine that has no such sysctl would be a file that
// lies.
func physicalMemoryMB() (int, error) {
	return 0, errors.New("iogpu.wired_limit_mb is an Apple-silicon knob; this host runs " + runtime.GOOS)
}
