//go:build !windows && !darwin

package main

import (
	"os"
	"strconv"
	"strings"
)

// physicalMemoryBytes returns the host's total physical RAM in bytes, or 0 when
// it cannot be determined. Callers treat 0 as "unknown" and fall back to a fixed
// conservative default (see resolveMemoryLimit in memlimit.go).
//
// Platform strategy:
//   - darwin: sysctl hw.memsize (total physical RAM, in bytes) — see memstat_darwin.go.
//   - linux (and other unix): MemTotal from /proc/meminfo (kB), converted to bytes.
//     /proc/meminfo is the portable, cgo-free source and reflects the value the
//     kernel reports to the OOM killer's accounting.
func physicalMemoryBytes() uint64 {
	return memTotalFromProcMeminfo("/proc/meminfo")
}

// memTotalFromProcMeminfo parses the MemTotal line from a /proc/meminfo-formatted
// file and returns it in bytes. Returns 0 on any read/parse failure. Split out so
// it is unit-testable with a fixture path.
func memTotalFromProcMeminfo(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		// Format: "MemTotal:       16384000 kB"
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
