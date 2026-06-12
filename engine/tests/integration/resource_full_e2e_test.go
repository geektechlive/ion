//go:build integration

// Full end-to-end tests for the Ion resource subsystem, scheduler
// concurrency coordination, and cross-session messaging. Exercises the
// TypeScript SDK → subprocess JSON-RPC → Go host → broker pipeline for
// every major subsystem interaction.
//
// Tests in this file:
//
//   TestFullE2E_SchedulerConcurrency_SingleMode
//   TestFullE2E_SchedulerConcurrency_AllMode
//   TestFullE2E_CrossSessionMessage
//   TestFullE2E_NotifyWithTargetSession
//   TestFullE2E_SessionDiscovery

package integration

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/asyncreg"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/scheduling"
	"github.com/dsswift/ion/engine/internal/types"
)

// ─── Test A: Scheduler concurrency — single mode (default) ───
//
// Two hosts share the same extension name and the same interval job with
// default concurrency ("" → single). After the scheduler ticks past the
// interval, only ONE host's session resolver should be called.

func TestFullE2E_SchedulerConcurrency_SingleMode(t *testing.T) {
	job := extension.ScheduleJob{
		JobID:      "e2e-single-tick",
		Kind:       extension.ScheduleInterval,
		IntervalMs: 1000,
		// Concurrency defaults to "" which means single
	}

	h1 := extension.NewHost()
	h1.SetNameForTest("async-canary")
	if err := h1.AsyncRegistry().Register(asyncreg.KindSchedule, job, asyncreg.OriginInit, nil); err != nil {
		t.Fatalf("register job h1: %v", err)
	}
	t.Cleanup(func() { h1.Dispose() })

	h2 := extension.NewHost()
	h2.SetNameForTest("async-canary")
	if err := h2.AsyncRegistry().Register(asyncreg.KindSchedule, job, asyncreg.OriginInit, nil); err != nil {
		t.Fatalf("register job h2: %v", err)
	}
	t.Cleanup(func() { h2.Dispose() })

	// Track which hosts the resolver is called for.
	var mu sync.Mutex
	var resolved []string
	resolver := func(h *extension.Host) (*extension.Context, error) {
		mu.Lock()
		resolved = append(resolved, h.Name())
		mu.Unlock()
		return &extension.Context{SessionKey: "test-" + h.Name()}, nil
	}

	bus := &eventBus{}
	sch := scheduling.New(scheduling.Config{
		FireTimeout: 5 * time.Second,
		PersistDir:  t.TempDir(),
	})
	sch.SetEmit(bus.emit)
	sch.SetSessionResolver(resolver)
	sch.AddHost(h1)
	sch.AddHost(h2)
	sch.Start()
	t.Cleanup(sch.Stop)

	// Wait for the first resolver call (scheduler bootstraps nextRun on
	// the first tick, then fires on the second tick after ~1s).
	expire := time.Now().Add(5 * time.Second)
	for time.Now().Before(expire) {
		mu.Lock()
		n := len(resolved)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Record the count at the point of the first fire.
	mu.Lock()
	firstBatch := len(resolved)
	mu.Unlock()

	if firstBatch == 0 {
		t.Fatal("single mode: resolver was never called within 5s")
	}

	// Wait a short time (less than the 1s tick interval) to ensure no
	// second host fires on the same tick.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := len(resolved)
	mu.Unlock()

	// In single mode, each tick fires exactly 1 host. The first batch
	// should be 1, and after 300ms (before the next tick) no additional
	// fire should have landed.
	if firstBatch != 1 {
		mu.Lock()
		t.Fatalf("single mode: expected 1 resolver call in first batch, got %d: %v", firstBatch, resolved)
		mu.Unlock()
	}
	if count != 1 {
		mu.Lock()
		t.Fatalf("single mode: expected still 1 resolver call after 300ms, got %d: %v", count, resolved)
		mu.Unlock()
	}
}

// ─── Test B: Scheduler concurrency — all mode ───
//
// Two hosts share the same extension name and the same interval job with
// concurrency="all". After the scheduler ticks past the interval, BOTH
// hosts' session resolvers should be called.

func TestFullE2E_SchedulerConcurrency_AllMode(t *testing.T) {
	job := extension.ScheduleJob{
		JobID:       "e2e-all-tick",
		Kind:        extension.ScheduleInterval,
		IntervalMs:  1000,
		Concurrency: "all",
	}

	h1 := extension.NewHost()
	h1.SetNameForTest("async-canary")
	if err := h1.AsyncRegistry().Register(asyncreg.KindSchedule, job, asyncreg.OriginInit, nil); err != nil {
		t.Fatalf("register job h1: %v", err)
	}
	t.Cleanup(func() { h1.Dispose() })

	h2 := extension.NewHost()
	h2.SetNameForTest("async-canary")
	if err := h2.AsyncRegistry().Register(asyncreg.KindSchedule, job, asyncreg.OriginInit, nil); err != nil {
		t.Fatalf("register job h2: %v", err)
	}
	t.Cleanup(func() { h2.Dispose() })

	var mu sync.Mutex
	var resolved []string
	resolver := func(h *extension.Host) (*extension.Context, error) {
		mu.Lock()
		resolved = append(resolved, h.Name())
		mu.Unlock()
		return &extension.Context{SessionKey: "test-" + h.Name()}, nil
	}

	bus := &eventBus{}
	sch := scheduling.New(scheduling.Config{
		FireTimeout: 5 * time.Second,
		PersistDir:  t.TempDir(),
	})
	sch.SetEmit(bus.emit)
	sch.SetSessionResolver(resolver)
	sch.AddHost(h1)
	sch.AddHost(h2)
	sch.Start()
	t.Cleanup(sch.Stop)

	// Wait up to 5s for at least 2 resolver calls (both hosts fire).
	expire := time.Now().Add(5 * time.Second)
	for time.Now().Before(expire) {
		mu.Lock()
		n := len(resolved)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Wait a bit more for goroutines to settle.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := len(resolved)
	names := append([]string(nil), resolved...)
	mu.Unlock()

	if count < 2 {
		t.Fatalf("all mode: expected >= 2 resolver calls, got %d: %v", count, names)
	}
}

// ─── Test D: Cross-session messaging ───
//
// Loads two resource-canary hosts (simulating two sessions). On host A,
// invokes canary_send_to_session targeting host B's session key. The
// ctx.SendToSession closure is wired to fire the session_message hook
// on host B's SDK. Then invokes canary_get_received_messages on host B
// to verify the message arrived.

func TestFullE2E_CrossSessionMessage(t *testing.T) {
	hostA := loadResourceCanary(t)
	hostB := loadResourceCanary(t)

	ctxB := &extension.Context{
		SessionKey: "session-b",
		Cwd:        t.TempDir(),
	}

	// Wire host A's ctx.SendToSession to deliver the message to host B.
	ctxA := &extension.Context{
		SessionKey: "session-a",
		Cwd:        t.TempDir(),
		SendToSession: func(targetKey string, kind string, payload map[string]interface{}) error {
			if targetKey != "session-b" {
				t.Errorf("unexpected target key: %q", targetKey)
			}
			info := extension.SessionMessageInfo{
				SenderSessionKey: "session-a",
				Kind:             kind,
				Payload:          payload,
			}
			return hostB.SDK().FireSessionMessage(ctxB, info)
		},
	}

	// Verify the tools are registered.
	findTool(t, hostA, "canary_send_to_session")
	findTool(t, hostB, "canary_get_received_messages")

	// Step 1: send a cross-session message from A to B.
	sendTool := findTool(t, hostA, "canary_send_to_session")
	result, err := sendTool.Execute(map[string]any{
		"targetKey": "session-b",
		"kind":      "test-ping",
		"message":   "hello from session-a",
	}, ctxA)
	if err != nil {
		t.Fatalf("canary_send_to_session: %v", err)
	}
	if result.IsError {
		t.Fatalf("canary_send_to_session returned error: %s", result.Content)
	}
	inner := unwrapToolContent(t, result.Content)
	var sendResult map[string]interface{}
	if err := json.Unmarshal([]byte(inner), &sendResult); err != nil {
		t.Fatalf("parse send result: %v (inner=%q)", err, inner)
	}
	if sendResult["sent"] != true {
		t.Errorf("expected sent=true, got %v", sendResult["sent"])
	}

	// Step 2: verify host B received the message. Give the hook a
	// moment to fire (it's synchronous in-process, but tool execution
	// is async via subprocess).
	time.Sleep(200 * time.Millisecond)

	getMsgsTool := findTool(t, hostB, "canary_get_received_messages")
	result2, err := getMsgsTool.Execute(map[string]any{}, ctxB)
	if err != nil {
		t.Fatalf("canary_get_received_messages: %v", err)
	}
	if result2.IsError {
		t.Fatalf("canary_get_received_messages returned error: %s", result2.Content)
	}

	inner2 := unwrapToolContent(t, result2.Content)
	var msgs []struct {
		SenderSessionKey string                 `json:"senderSessionKey"`
		Kind             string                 `json:"kind"`
		Payload          map[string]interface{} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(inner2), &msgs); err != nil {
		t.Fatalf("parse received messages: %v (inner=%q)", err, inner2)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 received message, got 0")
	}

	msg := msgs[0]
	if msg.SenderSessionKey != "session-a" {
		t.Errorf("sender session key: want session-a, got %q", msg.SenderSessionKey)
	}
	if msg.Kind != "test-ping" {
		t.Errorf("kind: want test-ping, got %q", msg.Kind)
	}
	if msg.Payload["message"] != "hello from session-a" {
		t.Errorf("payload.message: want 'hello from session-a', got %v", msg.Payload["message"])
	}
}

// ─── Test E: Notify with target session ───
//
// Loads resource-canary, wires ctx.Notify with a capture closure,
// invokes canary_notify_target with a target session key, and verifies
// the captured NotifyOpts has TargetSessionKey set correctly.

func TestFullE2E_NotifyWithTargetSession(t *testing.T) {
	host := loadResourceCanary(t)

	var mu sync.Mutex
	var captured []types.NotifyOpts

	ctx := &extension.Context{
		SessionKey: "notify-e2e",
		Cwd:        t.TempDir(),
		Notify: func(opts types.NotifyOpts) error {
			mu.Lock()
			captured = append(captured, opts)
			mu.Unlock()
			return nil
		},
	}

	tool := findTool(t, host, "canary_notify_target")
	result, err := tool.Execute(map[string]any{
		"targetKey": "other-session-key",
		"title":     "Targeted Alert",
		"body":      "This goes to a specific session.",
	}, ctx)
	if err != nil {
		t.Fatalf("canary_notify_target: %v", err)
	}
	if result.IsError {
		t.Fatalf("canary_notify_target returned error: %s", result.Content)
	}

	inner := unwrapToolContent(t, result.Content)
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(inner), &got); err != nil {
		t.Fatalf("parse notify result: %v (inner=%q)", err, inner)
	}
	if got["notified"] != true {
		t.Errorf("expected notified=true, got %v", got["notified"])
	}

	mu.Lock()
	n := len(captured)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("expected 1 notification captured, got %d", n)
	}

	mu.Lock()
	notif := captured[0]
	mu.Unlock()

	if notif.Kind != "briefing" {
		t.Errorf("notify kind: want briefing, got %q", notif.Kind)
	}
	if notif.Title != "Targeted Alert" {
		t.Errorf("notify title: want 'Targeted Alert', got %q", notif.Title)
	}
	if notif.Body != "This goes to a specific session." {
		t.Errorf("notify body: want 'This goes to a specific session.', got %q", notif.Body)
	}
	if notif.TargetSessionKey != "other-session-key" {
		t.Errorf("notify targetSessionKey: want 'other-session-key', got %q", notif.TargetSessionKey)
	}
}

// ─── Test F: Session discovery ───
//
// Loads resource-canary, wires ctx.ListSessions to return a canned list,
// invokes canary_list_sessions, and verifies the sessions are returned
// correctly through the full RPC pipeline.

func TestFullE2E_SessionDiscovery(t *testing.T) {
	host := loadResourceCanary(t)

	cannedSessions := []extension.SessionListEntry{
		{
			Key:           "session-alpha",
			HasActiveRun:  true,
			ExtensionName: "resource-canary",
		},
		{
			Key:            "session-beta",
			HasActiveRun:   false,
			ExtensionName:  "resource-canary",
			ConversationID: "conv-123",
		},
	}

	ctx := &extension.Context{
		SessionKey: "discovery-e2e",
		Cwd:        t.TempDir(),
		ListSessions: func() ([]extension.SessionListEntry, error) {
			return cannedSessions, nil
		},
	}

	tool := findTool(t, host, "canary_list_sessions")
	result, err := tool.Execute(map[string]any{}, ctx)
	if err != nil {
		t.Fatalf("canary_list_sessions: %v", err)
	}
	if result.IsError {
		t.Fatalf("canary_list_sessions returned error: %s", result.Content)
	}

	inner := unwrapToolContent(t, result.Content)
	var sessions []struct {
		Key            string `json:"key"`
		HasActiveRun   bool   `json:"hasActiveRun"`
		ExtensionName  string `json:"extensionName"`
		ConversationID string `json:"conversationId"`
	}
	if err := json.Unmarshal([]byte(inner), &sessions); err != nil {
		t.Fatalf("parse sessions: %v (inner=%q)", err, inner)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %v", len(sessions), sessions)
	}

	s0 := sessions[0]
	if s0.Key != "session-alpha" {
		t.Errorf("session[0].key: want session-alpha, got %q", s0.Key)
	}
	if !s0.HasActiveRun {
		t.Errorf("session[0].hasActiveRun: want true, got false")
	}
	if s0.ExtensionName != "resource-canary" {
		t.Errorf("session[0].extensionName: want resource-canary, got %q", s0.ExtensionName)
	}

	s1 := sessions[1]
	if s1.Key != "session-beta" {
		t.Errorf("session[1].key: want session-beta, got %q", s1.Key)
	}
	if s1.HasActiveRun {
		t.Errorf("session[1].hasActiveRun: want false, got true")
	}
	if s1.ConversationID != "conv-123" {
		t.Errorf("session[1].conversationId: want conv-123, got %q", s1.ConversationID)
	}
}

// ensure imports used.
var _ = filepath.Join
var _ = asyncreg.KindSchedule
var _ = scheduling.TickInterval
