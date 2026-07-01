//go:build windows

package main

// physicalMemoryBytes returns 0 on Windows: host-RAM detection is not implemented
// there because Windows is not a target for the OS memory-pressure killer this
// mechanism guards against (macOS jetsam / Linux OOM). Callers treat 0 as
// "unknown" and fall back to a fixed conservative default (see resolveMemoryLimit
// in memlimit.go), so the engine still gets a sane soft heap ceiling on Windows.
func physicalMemoryBytes() uint64 {
	return 0
}
