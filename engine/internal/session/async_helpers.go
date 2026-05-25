// Tiny helpers used by async_lifecycle.go that have nowhere natural
// to live. Kept in a separate file so async_lifecycle.go focuses on
// the wiring logic.

package session

import (
	"os"
	"path/filepath"
	"time"
)

// millisToDuration converts a millisecond count to a time.Duration.
// Trivial but pulled out so the calling translation funcs don't have
// to import time directly.
func millisToDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

// defaultSchedulerPersistDir returns ~/.ion/scheduler. When the home
// directory is unresolvable, returns "" — persistence becomes a no-op
// and the scheduler degrades to in-process catch-up only.
func defaultSchedulerPersistDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ion", "scheduler")
}
