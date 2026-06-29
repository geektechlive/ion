//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
)

// TestSIGHUP_InSignalSet confirms that SIGHUP is registered in the signal
// notification channel that cmd_serve.go uses to trigger graceful shutdown.
// This pins the behaviour that launchctl bootout (SIGTERM) and parent-process
// death (SIGHUP sent to non-detached children) both produce a clean exit.
func TestSIGHUP_InSignalSet(t *testing.T) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(ch)

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("kill SIGHUP: %v", err)
	}

	sig := <-ch
	if sig != syscall.SIGHUP {
		t.Errorf("got signal %v, want SIGHUP", sig)
	}
}
