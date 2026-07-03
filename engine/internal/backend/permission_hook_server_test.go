package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/types"
)

// postPermission issues a PreToolUse hook request to the server and returns the
// decoded decision (or "" if the handler wrote no body) plus the elapsed time.
// ctx lets the test cancel the request mid-flight.
func postPermission(t *testing.T, ctx context.Context, s *PermissionHookServer, token string) (string, time.Duration) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "rm -rf x"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL(token), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return "__error__", elapsed
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	_ = json.Unmarshal(raw, &decoded)
	return decoded.HookSpecificOutput.PermissionDecision, elapsed
}

// newTestPermissionServer starts a server with an onAsk that hands the test a
// channel it can resolve, plus the channel itself.
func newTestPermissionServer(t *testing.T) (*PermissionHookServer, string, chan string) {
	t.Helper()
	s, err := NewPermissionHookServer(nil)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	t.Cleanup(s.Close)
	token := fmt.Sprintf("tok-%d", time.Now().UnixNano())
	s.RegisterToken(token)
	askCh := make(chan string, 1)
	s.SetOnAsk(func(_, _, _, _ string, _ map[string]any, _ []types.PermissionOpt) chan string {
		return askCh
	})
	return s, token, askCh
}

// TestPermission_IndefiniteByDefault_BlocksUntilResolved pins that with no
// timeout config the permission dialog waits indefinitely and resolves to the
// user's decision (here "allow"). Revert-check: the old hardcoded
// time.After(5*time.Minute) deny would still pass this (it resolves first), so
// this test is paired with the timing assertion in the finite test below.
func TestPermission_IndefiniteByDefault_BlocksUntilResolved(t *testing.T) {
	s, token, askCh := newTestPermissionServer(t)
	// no SetTimeouts → indefinite

	type res struct {
		decision string
		elapsed  time.Duration
	}
	resCh := make(chan res, 1)
	go func() {
		d, e := postPermission(t, context.Background(), s, token)
		resCh <- res{d, e}
	}()

	// Resolve after a delay; the handler must still be blocked, then return allow.
	time.Sleep(150 * time.Millisecond)
	askCh <- "allow"

	select {
	case r := <-resCh:
		if r.decision != "allow" {
			t.Errorf("decision = %q, want allow", r.decision)
		}
		if r.elapsed < 100*time.Millisecond {
			t.Errorf("resolved in %s — handler did not actually block on the channel", r.elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("permission handler never returned")
	}
}

// TestPermission_RequestCancellationUnblocks pins that an indefinite wait is
// released when the request context is cancelled (the subprocess went away),
// and the handler writes NO decision in that case.
func TestPermission_RequestCancellationUnblocks(t *testing.T) {
	s, token, _ := newTestPermissionServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	resCh := make(chan string, 1)
	go func() {
		d, _ := postPermission(t, ctx, s, token)
		resCh <- d
	}()

	time.Sleep(100 * time.Millisecond)
	cancel() // subprocess/connection gone

	select {
	case d := <-resCh:
		// Client-side sees a transport error (we cancelled), surfaced as
		// our sentinel. The point is the handler unblocked rather than
		// hanging for the full (indefinite) wait.
		if d != "__error__" && d != "" {
			t.Errorf("unexpected decision after cancellation: %q", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not unblock on request cancellation")
	}
}

// TestPermission_FiniteTimeout_AppliesFailAction pins the headless behavior:
// with a finite human-wait and no answer, the dialog resolves to the configured
// fail-action. Default = deny; "allow" override = allow. Revert-check: removing
// the timerCh arm makes these hang and time out the test.
func TestPermission_FiniteTimeout_AppliesFailAction(t *testing.T) {
	t.Run("default deny", func(t *testing.T) {
		s, token, _ := newTestPermissionServer(t)
		s.SetTimeouts(&types.TimeoutsConfig{ElicitationMs: 80}) // 80ms finite, no decision override
		d, elapsed := postPermission(t, context.Background(), s, token)
		if d != "deny" {
			t.Errorf("decision = %q, want deny (fail-closed default)", d)
		}
		if elapsed > 2*time.Second {
			t.Errorf("finite wait took %s, expected ~80ms", elapsed)
		}
	})

	t.Run("allow override", func(t *testing.T) {
		s, token, _ := newTestPermissionServer(t)
		s.SetTimeouts(&types.TimeoutsConfig{ElicitationMs: 80, PermissionTimeoutDecision: "allow"})
		d, _ := postPermission(t, context.Background(), s, token)
		if d != "allow" {
			t.Errorf("decision = %q, want allow (configured fail-action)", d)
		}
	})
}
