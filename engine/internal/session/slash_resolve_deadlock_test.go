package session

import (
	"testing"
	"time"
)

// Regression test for the deadlock caused by emitUnknownCommand being called
// inside resolveSlashIntoOpts while m.mu (write lock) was held. emit acquires
// m.mu.RLock; Go's sync.RWMutex deadlocks when a goroutine tries RLock while
// holding a write lock on the same mutex. The fix moves the emit to after the
// lock is released. This test verifies that sending an unknown slash command
// does not deadlock the manager — StartSession must still work afterward.

func TestSendPrompt_UnknownSlash_DoesNotDeadlock(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)

	_, err := mgr.StartSession("s1", defaultConfig())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- mgr.SendPrompt("s1", "/nonexistent-command", &PromptOverrides{
			ResolveSlash: true,
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SendPrompt returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SendPrompt deadlocked — m.mu never released after unknown slash command")
	}

	// Verify the manager mutex is free: StartSession on a new key must succeed.
	result := make(chan error, 1)
	go func() {
		_, err := mgr.StartSession("s2", defaultConfig())
		result <- err
	}()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("StartSession after unknown slash: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartSession deadlocked — m.mu still held after unknown slash command")
	}
}
