package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// runOnceRegistry unit tests
// ---------------------------------------------------------------------------

func TestRunOnceRegistry_FirstCallExecutes(t *testing.T) {
	r := newRunOnceRegistry()
	result := r.check("ext/a", "init", 60000)
	if !result.Execute {
		t.Fatalf("first check should return execute=true, got reason=%q", result.Reason)
	}
}

func TestRunOnceRegistry_SecondCallBlockedWhileRunning(t *testing.T) {
	r := newRunOnceRegistry()
	// First call wins.
	r.check("ext/a", "init", 60000)

	// Second call before complete returns in_progress.
	result := r.check("ext/a", "init", 60000)
	if result.Execute {
		t.Fatal("second check while running should return execute=false")
	}
	if result.Reason != "in_progress" {
		t.Errorf("expected reason=in_progress, got %q", result.Reason)
	}
}

func TestRunOnceRegistry_AfterCompleteDebounced(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 60000)
	r.complete("ext/a", "init", false)

	// Immediately after: still within debounce window.
	result := r.check("ext/a", "init", 60000)
	if result.Execute {
		t.Fatal("check within debounce window should return execute=false")
	}
	if result.Reason != "debounced" {
		t.Errorf("expected reason=debounced, got %q", result.Reason)
	}
}

func TestRunOnceRegistry_DebounceExpiry(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 50) // 50ms debounce
	r.complete("ext/a", "init", false)

	// Wait for debounce to expire.
	time.Sleep(60 * time.Millisecond)

	result := r.check("ext/a", "init", 50)
	if !result.Execute {
		t.Fatalf("check after debounce expiry should return execute=true, got reason=%q", result.Reason)
	}
}

func TestRunOnceRegistry_FailureReleasesLock(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 60000)
	r.complete("ext/a", "init", true) // failure

	// Next call should be allowed to execute (lastRun was not set).
	result := r.check("ext/a", "init", 60000)
	if !result.Execute {
		t.Fatalf("check after failure should return execute=true, got reason=%q", result.Reason)
	}
}

func TestRunOnceRegistry_ZeroDebounceRunOncePerLifecycle(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "once", 0)
	r.complete("ext/a", "once", false)

	result := r.check("ext/a", "once", 0)
	if result.Execute {
		t.Fatal("debounceMs=0: second check should return execute=false")
	}
	if result.Reason != "already_ran" {
		t.Errorf("expected reason=already_ran, got %q", result.Reason)
	}
}

func TestRunOnceRegistry_PurgeExtension(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 60000)
	r.complete("ext/a", "init", false)

	// After purge, a new check for the same op on the same ext executes.
	r.purgeExtension("ext/a")

	result := r.check("ext/a", "init", 60000)
	if !result.Execute {
		t.Fatalf("check after purge should return execute=true, got reason=%q", result.Reason)
	}
}

func TestRunOnceRegistry_DifferentOpsAreIndependent(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 60000)
	r.complete("ext/a", "init", false)

	// A different op on the same ext should execute.
	result := r.check("ext/a", "migrate", 60000)
	if !result.Execute {
		t.Fatalf("different op should execute independently, got reason=%q", result.Reason)
	}
}

func TestRunOnceRegistry_DifferentExtensionsAreIndependent(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 60000)
	r.complete("ext/a", "init", false)

	// Same op on a different extension should execute.
	result := r.check("ext/b", "init", 60000)
	if !result.Execute {
		t.Fatalf("different extension should execute independently, got reason=%q", result.Reason)
	}
}

func TestRunOnceRegistry_ConcurrentCheckOnlyOneWins(t *testing.T) {
	r := newRunOnceRegistry()
	const concurrency = 10

	var wins atomic.Int32
	var wg sync.WaitGroup
	wg.Add(concurrency)

	start := make(chan struct{})
	for range concurrency {
		go func() {
			defer wg.Done()
			<-start
			result := r.check("ext/a", "init", 60000)
			if result.Execute {
				wins.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if wins.Load() != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", wins.Load())
	}
}

func TestRunOnceRegistry_ReleaseRunning(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 60000)

	// Simulate host death: release running flag without completing.
	r.releaseRunning("ext/a", "init")

	// Next call should be able to execute.
	result := r.check("ext/a", "init", 60000)
	if !result.Execute {
		t.Fatalf("check after releaseRunning should return execute=true, got reason=%q", result.Reason)
	}
}

func TestRunOnceRegistry_RunningIDsReturnsActive(t *testing.T) {
	r := newRunOnceRegistry()
	r.check("ext/a", "init", 60000)
	r.check("ext/a", "migrate", 60000)
	r.complete("ext/a", "migrate", false) // only "init" still running

	ids := r.runningIDs("ext/a")
	if len(ids) != 1 || ids[0] != "init" {
		t.Errorf("expected [init], got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// Manager.RunOnceCheck / RunOnceComplete (via Manager methods directly)
// ---------------------------------------------------------------------------

func TestManager_RunOnceCheck_NoSession(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	defer mgr.Shutdown()

	result := mgr.RunOnceCheck("nonexistent-session", "init", 60000)
	// Session not found: graceful fallback.
	if result.Execute {
		t.Fatal("RunOnceCheck on nonexistent session should return execute=false")
	}
}

func TestManager_RunOnceCheck_SessionWithoutExtension(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	defer mgr.Shutdown()

	_, err := mgr.StartSession("s1", defaultConfig())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Session exists but has no extension loaded: RunOnceCheck degrades
	// gracefully by returning Execute=true (dedup not applicable).
	result := mgr.RunOnceCheck("s1", "init", 60000)
	if !result.Execute {
		t.Fatalf("session without extension: expected execute=true, got reason=%q", result.Reason)
	}
}

func TestManager_RunOnceComplete_NoOp_WhenNotChecked(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	defer mgr.Shutdown()

	_, err := mgr.StartSession("s1", defaultConfig())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Should not panic even when no prior check exists.
	mgr.RunOnceComplete("s1", "init", false)
}
