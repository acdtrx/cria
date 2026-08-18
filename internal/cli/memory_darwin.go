package cli

import (
	"encoding/binary"
	"fmt"
	"syscall"
)

// physicalMemoryMB is this Mac's memory, from the same sysctl tree the wired
// limit lives in — read via the kernel interface, no program run.
func physicalMemoryMB() (int, error) {
	raw, err := syscall.Sysctl("hw.memsize")
	if err != nil {
		return 0, fmt.Errorf("cannot read this machine's memory size (sysctl hw.memsize): %w", err)
	}

	// Sysctl hands binary values back as a string and trims trailing NULs, so a
	// value whose high bytes are zero comes back short: copy into eight bytes
	// and let the missing ones stay zero (darwin is little-endian on both
	// supported architectures).
	var buf [8]byte
	copy(buf[:], raw)
	return int(binary.LittleEndian.Uint64(buf[:]) >> 20), nil
}
