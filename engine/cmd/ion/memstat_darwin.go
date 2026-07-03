//go:build darwin

package main

import "golang.org/x/sys/unix"

// physicalMemoryBytes returns the host's total physical RAM in bytes, or 0 when
// it cannot be determined. Callers treat 0 as "unknown" and fall back to a fixed
// conservative default (see resolveMemoryLimit in memlimit.go).
//
// Platform strategy:
//   - darwin: sysctl hw.memsize (total physical RAM, in bytes).
//   - linux (and other unix): MemTotal from /proc/meminfo (kB), converted to bytes.
//     /proc/meminfo is the portable, cgo-free source and reflects the value the
//     kernel reports to the OOM killer's accounting.
func physicalMemoryBytes() uint64 {
	if v, err := unix.SysctlUint64("hw.memsize"); err == nil && v > 0 {
		return v
	}
	return 0
}
