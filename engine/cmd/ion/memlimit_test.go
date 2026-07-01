package main

import (
	"runtime/debug"
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

func TestResolveMemoryLimit_EnvWins(t *testing.T) {
	// GOMEMLIMIT set ⇒ source=env, bytes=0 (runtime owns it; we must not override).
	cfg := &types.EngineRuntimeConfig{MemoryLimitMB: 2048}
	bytes, source := resolveMemoryLimit(cfg, "3GiB", 16*1024*1024*1024)
	if source != memSourceEnv {
		t.Fatalf("source: got %q want %q", source, memSourceEnv)
	}
	if bytes != 0 {
		t.Fatalf("bytes: got %d want 0 (runtime owns the env value)", bytes)
	}
}

func TestResolveMemoryLimit_ConfigWhenNoEnv(t *testing.T) {
	cfg := &types.EngineRuntimeConfig{MemoryLimitMB: 2048}
	bytes, source := resolveMemoryLimit(cfg, "", 16*1024*1024*1024)
	if source != memSourceConfig {
		t.Fatalf("source: got %q want %q", source, memSourceConfig)
	}
	want := int64(2048) * bytesPerMiB
	if bytes != want {
		t.Fatalf("bytes: got %d want %d", bytes, want)
	}
}

func TestResolveMemoryLimit_HostRAMFraction(t *testing.T) {
	// No env, no config ⇒ fraction of host RAM.
	var physRAM uint64 = 16 * 1024 * 1024 * 1024
	bytes, source := resolveMemoryLimit(&types.EngineRuntimeConfig{}, "", physRAM)
	if source != memSourceHostRAM {
		t.Fatalf("source: got %q want %q", source, memSourceHostRAM)
	}
	want := int64(float64(physRAM) * defaultMemLimitFraction)
	if bytes != want {
		t.Fatalf("bytes: got %d want %d", bytes, want)
	}
	if bytes <= 0 {
		t.Fatalf("bytes must be positive, got %d", bytes)
	}
}

func TestResolveMemoryLimit_FallbackWhenHostRAMUnknown(t *testing.T) {
	// No env, no config, physRAM=0 (Windows / sysctl failure) ⇒ fixed fallback.
	bytes, source := resolveMemoryLimit(&types.EngineRuntimeConfig{}, "", 0)
	if source != memSourceFallback {
		t.Fatalf("source: got %q want %q", source, memSourceFallback)
	}
	if bytes != fallbackMemLimitBytes {
		t.Fatalf("bytes: got %d want %d", bytes, fallbackMemLimitBytes)
	}
	if bytes <= 0 {
		t.Fatalf("bytes must be positive, got %d", bytes)
	}
}

func TestResolveMemoryLimit_NilConfigFallsThrough(t *testing.T) {
	// A nil config must not panic and must derive from host RAM / fallback.
	bytes, source := resolveMemoryLimit(nil, "", 8*1024*1024*1024)
	if source != memSourceHostRAM {
		t.Fatalf("source: got %q want %q", source, memSourceHostRAM)
	}
	if bytes <= 0 {
		t.Fatalf("bytes must be positive, got %d", bytes)
	}
}

func TestApplyMemoryLimit_SetsRuntimeLimit(t *testing.T) {
	// Ensure GOMEMLIMIT is unset so config takes effect.
	t.Setenv("GOMEMLIMIT", "")
	// Restore the runtime limit after the test so we don't leak GC pressure into
	// other tests in the package.
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })

	cfg := &types.EngineRuntimeConfig{MemoryLimitMB: 1024}
	got := applyMemoryLimit(cfg)

	want := int64(1024) * bytesPerMiB
	if got != want {
		t.Fatalf("applyMemoryLimit returned %d want %d", got, want)
	}
	// Observable via read-back: the runtime now reports the limit we set.
	if effective := debug.SetMemoryLimit(-1); effective != want {
		t.Fatalf("runtime effective limit %d want %d", effective, want)
	}
}

func TestApplyMemoryLimit_DoesNotOverrideEnv(t *testing.T) {
	// With GOMEMLIMIT set, applyMemoryLimit must return the runtime's effective
	// limit and must NOT clobber it. We can't easily set GOMEMLIMIT so the runtime
	// re-parses it mid-process, so we assert the guard: the env branch returns the
	// current effective limit unchanged.
	t.Setenv("GOMEMLIMIT", "2GiB")
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })

	// Seed a known effective limit so we can prove applyMemoryLimit returns it and
	// leaves it untouched (the "not overriding" guarantee).
	seeded := int64(5) * 1024 * bytesPerMiB
	debug.SetMemoryLimit(seeded)

	got := applyMemoryLimit(&types.EngineRuntimeConfig{MemoryLimitMB: 1024})
	if got != seeded {
		t.Fatalf("env branch returned %d want the seeded effective limit %d", got, seeded)
	}
	if effective := debug.SetMemoryLimit(-1); effective != seeded {
		t.Fatalf("env branch changed the limit to %d; want it left at %d", effective, seeded)
	}
}
