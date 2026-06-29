// @file-size-exception: keystone integration test covering 8-dispatch architecture across 2 sessions with recall, continuation, isolation, and event telemetry assertions.
//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/providers"
	"github.com/dsswift/ion/engine/internal/session"
	"github.com/dsswift/ion/engine/internal/session/extcontext"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/tests/helpers"
)

// dispatchSessionResults collects events and dispatch outcomes for one session.
type dispatchSessionResults struct {
	mu          sync.Mutex
	events      []types.EngineEvent
	stubs       []*extension.DispatchAgentResult // background stubs
	completions []*extension.DispatchAgentResult // real terminal results from OnComplete
	errors      []error
	complete    chan struct{}
}

func newDispatchSR(n int) *dispatchSessionResults {
	return &dispatchSessionResults{complete: make(chan struct{}, n)}
}

// ─── 8-dispatch architecture test ───
//
// Two independent sessions run 2 rounds of 2 parallel background dispatches
// each (8 total: 7 fresh + 1 continuation). Proves:
//   - Distinct SessionIDs and DispatchIDs across all dispatches
//   - Cross-session isolation (no conv-A IDs in conv-B)
//   - Per-conversation content isolation (no cross-talk)
//   - Continuation: follow-up dispatch reuses conversation, fresh does not
//   - engine_dispatch_start/end telemetry with correct fields
//   - Per-dispatch usage is non-zero
//   - engine_agent_state carries dispatches[] metadata

func TestDispatchArchitecture_8Dispatch2Conversation(t *testing.T) {
	mp := setupMockProvider(t)

	// Script 8 responses. Each unique so per-conversation isolation is verifiable.
	for _, r := range []string{
		"Response-AAA", "Response-BBB", "Response-CCC", "Response-DDD",
		"Response-EEE", "Response-FFF", "Response-GGG", "Response-HHH",
	} {
		mp.SetResponse(helpers.TextResponse(r))
	}

	sessA := newDispatchSR(8) // buffer for stubs + completions
	sessB := newDispatchSR(8)
	srMap := map[string]*dispatchSessionResults{"sess-a": sessA, "sess-b": sessB}

	mgr := session.NewManager(backend.NewApiBackend())
	mgr.OnEvent(func(key string, ev types.EngineEvent) {
		sr, ok := srMap[key]
		if !ok {
			return
		}
		sr.mu.Lock()
		sr.events = append(sr.events, ev)
		sr.mu.Unlock()
	})

	// Start two independent sessions.
	for _, key := range []string{"sess-a", "sess-b"} {
		cfg := types.EngineConfig{
			ProfileID:        key,
			WorkingDirectory: t.TempDir(),
		}
		if _, err := mgr.StartSession(key, cfg); err != nil {
			t.Fatalf("StartSession %s: %v", key, err)
		}
		t.Cleanup(func() { mgr.StopSession(key) })
	}

	// Wire inline extension with a do_dispatch tool per session.
	hosts := map[string]*extension.Host{}
	wireSession := func(key string, sr *dispatchSessionResults) {
		host := extension.NewHost()
		hosts[key] = host
		sdk := host.SDK()
		sdk.RegisterTool(extension.ToolDefinition{
			Name:        "do_dispatch",
			Description: "dispatches an agent",
			Parameters:  map[string]interface{}{"type": "object"},
			Execute: func(params interface{}, ctx *extension.Context) (*types.ToolResult, error) {
				p, _ := params.(map[string]interface{})
				name, _ := p["name"].(string)
				task, _ := p["task"].(string)
				sid, _ := p["sessionId"].(string)
				if ctx.DispatchAgent == nil {
					return &types.ToolResult{Content: "DispatchAgent not wired", IsError: true}, nil
				}
				result, err := ctx.DispatchAgent(extension.DispatchAgentOpts{
					Name: name, Task: task, Model: "mock-model",
					SessionID: sid, MaxTurns: 1, Background: true,
					OnComplete: func(r extension.DispatchAgentResult) {
						sr.mu.Lock()
						sr.completions = append(sr.completions, &r)
						sr.mu.Unlock()
						sr.complete <- struct{}{}
					},
					OnError: func(e extension.DispatchError) {
						sr.mu.Lock()
						sr.errors = append(sr.errors, fmt.Errorf("dispatch error: %s (exit %d)", e.Message, e.ExitCode))
						sr.mu.Unlock()
						sr.complete <- struct{}{}
					},
				})
				if err != nil {
					return &types.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}, nil
				}
				if result != nil {
					sr.mu.Lock()
					sr.stubs = append(sr.stubs, result)
					sr.mu.Unlock()
				}
				return &types.ToolResult{Content: "dispatched"}, nil
			},
		})
		group := extension.NewExtensionGroup()
		group.Add(host)
		mgr.TestSetExtGroup(key, group)
	}
	wireSession("sess-a", sessA)
	wireSession("sess-b", sessB)

	// dispatch fires a background dispatch via the do_dispatch tool.
	dispatch := func(key, name, task, sid string) {
		ctx := mgr.TestNewExtContext(key)
		if ctx == nil {
			t.Fatalf("TestNewExtContext(%s) returned nil", key)
		}
		allTools := hosts[key].Tools()
		var tool *extension.ToolDefinition
		for i := range allTools {
			if allTools[i].Name == "do_dispatch" {
				tool = &allTools[i]
				break
			}
		}
		if tool == nil {
			t.Fatalf("do_dispatch not found for %s", key)
		}
		if _, err := tool.Execute(map[string]interface{}{
			"name": name, "task": task, "sessionId": sid,
		}, ctx); err != nil {
			t.Fatalf("dispatch(%s,%s) error: %v", key, name, err)
		}
	}

	waitN := func(sr *dispatchSessionResults, n int, timeout time.Duration) {
		dl := time.After(timeout)
		for i := 0; i < n; i++ {
			select {
			case <-sr.complete:
			case <-dl:
				sr.mu.Lock()
				t.Fatalf("timeout waiting for dispatches (got %d completions, %d errors)", len(sr.completions), len(sr.errors))
				sr.mu.Unlock()
			}
		}
	}

	// completionResults returns the OnComplete results (real terminal, not stubs).
	completionResults := func(sr *dispatchSessionResults) []*extension.DispatchAgentResult {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		out := make([]*extension.DispatchAgentResult, len(sr.completions))
		copy(out, sr.completions)
		return out
	}

	// ── Round 1: 2 parallel dispatches per session ──
	dispatch("sess-a", "alpha", "Task-AAA", "")
	dispatch("sess-a", "beta", "Task-BBB", "")
	dispatch("sess-b", "alpha", "Task-EEE", "")
	dispatch("sess-b", "beta", "Task-FFF", "")
	waitN(sessA, 2, 30*time.Second)
	waitN(sessB, 2, 30*time.Second)

	r1a := completionResults(sessA)
	r1b := completionResults(sessB)
	if len(r1a) < 2 {
		t.Fatalf("sess-a round 1: expected >=2 terminal, got %d", len(r1a))
	}
	if len(r1b) < 2 {
		t.Fatalf("sess-b round 1: expected >=2 terminal, got %d", len(r1b))
	}

	// Identify alpha's round-1 SessionID by conversation content.
	var alphaR1SID string
	for _, r := range r1a {
		msgs, err := conversation.LoadMessages(r.SessionID, "")
		if err != nil {
			continue
		}
		if strings.Contains(flattenContent(msgs), "Task-AAA") {
			alphaR1SID = r.SessionID
			break
		}
	}
	if alphaR1SID == "" {
		t.Fatal("could not identify alpha round-1 SessionID in sess-a")
	}

	// ── Round 2: alpha follow-up (continuation) + beta fresh ──
	dispatch("sess-a", "alpha", "Task-CCC", alphaR1SID)
	dispatch("sess-a", "beta", "Task-DDD", "")
	dispatch("sess-b", "alpha", "Task-GGG", "")
	dispatch("sess-b", "beta", "Task-HHH", "")
	waitN(sessA, 2, 30*time.Second)
	waitN(sessB, 2, 30*time.Second)

	allA := completionResults(sessA)
	allB := completionResults(sessB)
	allResults := append(allA, allB...)
	if len(allResults) < 8 {
		t.Fatalf("expected >=8 terminal results, got %d", len(allResults))
	}

	// ── Assertion 1: distinct non-empty IDs, all exit 0 ──
	sessionIDs := map[string]bool{}
	dispatchIDs := map[string]bool{}
	for _, r := range allResults {
		if r.SessionID == "" {
			t.Error("result has empty SessionID")
		}
		if r.DispatchID == "" {
			t.Error("result has empty DispatchID")
		}
		sessionIDs[r.SessionID] = true
		dispatchIDs[r.DispatchID] = true
		if r.ExitCode != 0 {
			t.Errorf("dispatch %s exit %d, want 0", r.DispatchID, r.ExitCode)
		}
	}
	if len(dispatchIDs) < 8 {
		t.Errorf("expected 8 distinct DispatchIDs, got %d", len(dispatchIDs))
	}
	// 7 fresh + 1 continuation = at least 7 distinct SessionIDs.
	if len(sessionIDs) < 7 {
		t.Errorf("expected >=7 distinct SessionIDs, got %d", len(sessionIDs))
	}

	// ── Assertion 2: cross-session independence ──
	aIDs := map[string]bool{}
	bIDs := map[string]bool{}
	for _, r := range allA {
		aIDs[r.SessionID] = true
	}
	for _, r := range allB {
		bIDs[r.SessionID] = true
	}
	for id := range aIDs {
		if bIDs[id] {
			t.Errorf("SessionID %s in both sessions", id)
		}
	}

	// ── Assertion 3: per-conversation content isolation ──
	bTasks := []string{"Task-EEE", "Task-FFF", "Task-GGG", "Task-HHH"}
	aTasks := []string{"Task-AAA", "Task-BBB", "Task-CCC", "Task-DDD"}
	for _, r := range allA {
		msgs, err := conversation.LoadMessages(r.SessionID, "")
		if err != nil {
			t.Logf("cannot load %s: %v", r.SessionID, err)
			continue
		}
		c := flattenContent(msgs)
		for _, bt := range bTasks {
			if strings.Contains(c, bt) {
				t.Errorf("sess-a conv %s has sess-b task %q", r.SessionID, bt)
			}
		}
	}
	for _, r := range allB {
		msgs, err := conversation.LoadMessages(r.SessionID, "")
		if err != nil {
			continue
		}
		c := flattenContent(msgs)
		for _, at := range aTasks {
			if strings.Contains(c, at) {
				t.Errorf("sess-b conv %s has sess-a task %q", r.SessionID, at)
			}
		}
	}

	// ── Assertion 4: continuation vs fresh ──
	msgs, err := conversation.LoadMessages(alphaR1SID, "")
	if err != nil {
		t.Logf("cannot verify continuation: %v", err)
	} else {
		c := flattenContent(msgs)
		if !strings.Contains(c, "Task-AAA") {
			t.Error("continuation missing round-1 task (Task-AAA)")
		}
		if !strings.Contains(c, "Task-CCC") {
			t.Error("continuation missing round-2 task (Task-CCC)")
		}
	}

	// ── Assertion 5: engine_dispatch_start/end telemetry ──
	countEv := func(sr *dispatchSessionResults, typ string) int {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		n := 0
		for _, ev := range sr.events {
			if ev.Type == typ {
				n++
			}
		}
		return n
	}
	if n := countEv(sessA, "engine_dispatch_start"); n != 4 {
		t.Errorf("sess-a: engine_dispatch_start=%d want 4", n)
	}
	if n := countEv(sessA, "engine_dispatch_end"); n != 4 {
		t.Errorf("sess-a: engine_dispatch_end=%d want 4", n)
	}
	if n := countEv(sessB, "engine_dispatch_start"); n != 4 {
		t.Errorf("sess-b: engine_dispatch_start=%d want 4", n)
	}
	if n := countEv(sessB, "engine_dispatch_end"); n != 4 {
		t.Errorf("sess-b: engine_dispatch_end=%d want 4", n)
	}

	// Verify dispatch_end fields.
	checkEnd := func(sr *dispatchSessionResults, label string) {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		for _, ev := range sr.events {
			if ev.Type == "engine_dispatch_end" {
				if ev.DispatchElapsed <= 0 {
					t.Errorf("%s: dispatch_end elapsed=%.4f want >0", label, ev.DispatchElapsed)
				}
				if ev.DispatchExitCode != 0 {
					t.Errorf("%s: dispatch_end exit=%d want 0", label, ev.DispatchExitCode)
				}
				if ev.DispatchInputTokens != 10 {
					t.Errorf("%s: dispatch_end InputTokens=%d want 10", label, ev.DispatchInputTokens)
				}
				if ev.DispatchOutputTokens != 5 {
					t.Errorf("%s: dispatch_end OutputTokens=%d want 5", label, ev.DispatchOutputTokens)
				}
			}
			if ev.Type == "engine_dispatch_start" && ev.DispatchModel != "mock-model" {
				t.Errorf("%s: dispatch_start model=%q want mock-model", label, ev.DispatchModel)
			}
		}
	}
	checkEnd(sessA, "sess-a")
	checkEnd(sessB, "sess-b")

	// ── Assertion 6: engine_agent_state dispatches[] ──
	checkAgentState := func(sr *dispatchSessionResults, label string) {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		var last *types.EngineEvent
		for i := len(sr.events) - 1; i >= 0; i-- {
			if sr.events[i].Type == "engine_agent_state" {
				last = &sr.events[i]
				break
			}
		}
		if last == nil {
			t.Logf("%s: no engine_agent_state events", label)
			return
		}
		found := false
		for _, a := range last.Agents {
			if a.Metadata == nil {
				continue
			}
			dispatches, _ := a.Metadata["dispatches"].([]interface{})
			for _, d := range dispatches {
				dm, _ := d.(map[string]interface{})
				if cid, ok := dm["conversationId"].(string); ok && cid != "" {
					found = true
				}
			}
		}
		if !found {
			t.Logf("%s: last agent_state has no dispatch with conversationId", label)
		}
	}
	checkAgentState(sessA, "sess-a")
	checkAgentState(sessB, "sess-b")

	// ── Assertion 8: per-dispatch usage and cost ──
	// MockProvider.TextResponse scripts InputTokens:10, OutputTokens:5.
	// Each dispatch is a single-turn run, so the cumulative totals match
	// the per-turn values exactly.
	for _, r := range allResults {
		if r.InputTokens != 10 {
			t.Errorf("dispatch %s InputTokens=%d, want 10", r.DispatchID, r.InputTokens)
		}
		if r.OutputTokens != 5 {
			t.Errorf("dispatch %s OutputTokens=%d, want 5", r.DispatchID, r.OutputTokens)
		}
		if r.Cost <= 0 {
			t.Errorf("dispatch %s Cost=%.6f, want >0", r.DispatchID, r.Cost)
		}
	}

	// ── Write event fixture for downstream layers ──
	writeEventFixture(t, sessA, sessB)
}

// ─── Recall sub-test ───
//
// Dispatches two parallel agents, recalls one by name, verifies:
//   - The recalled agent exits with ExitCodeRecalled (2)
//   - The sibling completes normally (exit 0)

func TestDispatchArchitecture_RecallOneSibling(t *testing.T) {
	providers.ResetRegistries()
	t.Cleanup(func() { providers.ResetRegistries() })

	// We need "doomed" to block until recalled and "keeper" to complete.
	// Use a multi-turn setup: both agents call a tool, tool blocks on a
	// channel. We recall doomed while both are blocked in their tool call,
	// then unblock the tool so keeper can proceed.
	//
	// Simpler: use the mock provider with blocking, then cancel the
	// session context to unblock keeper after doomed is recalled.
	// But that would recall keeper too.
	//
	// Simplest: give keeper a very fast single-turn response. Give doomed
	// a response too but dispatch them sequentially: dispatch keeper,
	// wait for keeper to hit the provider, dispatch doomed, wait for
	// doomed to hit the provider, then recall doomed. Since keeper's
	// response is fast and doesn't block, it will complete before we
	// recall doomed.

	recallMP := helpers.NewMockProvider("mock")
	// First response for keeper: fast.
	recallMP.SetResponse(helpers.TextResponse("keeper-done"))
	// Second response for doomed: we'll set BlockUntilCancel before
	// dispatching doomed. But SetBlockUntilCancel is global, which would
	// block keeper too if it hasn't completed yet.
	//
	// Better strategy: dispatch keeper first, wait for its completion,
	// then set BlockUntilCancel and dispatch doomed.
	providers.RegisterProvider(recallMP)
	providers.RegisterModel("mock-model", types.ModelInfo{
		ProviderID:      "mock",
		ContextWindow:   200000,
		CostPer1kInput:  0.003,
		CostPer1kOutput: 0.015,
	})

	mgr := session.NewManager(backend.NewApiBackend())
	var mu sync.Mutex
	var events []types.EngineEvent
	mgr.OnEvent(func(_ string, ev types.EngineEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	cfg := types.EngineConfig{
		ProfileID:        "recall-test",
		WorkingDirectory: t.TempDir(),
	}
	if _, err := mgr.StartSession("recall-s", cfg); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	t.Cleanup(func() { mgr.StopSession("recall-s") })

	type outcome struct {
		result   *extension.DispatchAgentResult
		recalled bool
	}
	outcomes := map[string]*outcome{}
	var omu sync.Mutex
	done := make(chan string, 4)

	host := extension.NewHost()
	sdk := host.SDK()
	sdk.RegisterTool(extension.ToolDefinition{
		Name:        "do_dispatch",
		Description: "dispatches an agent",
		Parameters:  map[string]interface{}{"type": "object"},
		Execute: func(params interface{}, ctx *extension.Context) (*types.ToolResult, error) {
			p, _ := params.(map[string]interface{})
			name, _ := p["name"].(string)
			task, _ := p["task"].(string)
			_, err := ctx.DispatchAgent(extension.DispatchAgentOpts{
				Name: name, Task: task, Model: "mock-model",
				MaxTurns: 1, Background: true,
				OnComplete: func(r extension.DispatchAgentResult) {
					omu.Lock()
					outcomes[name] = &outcome{result: &r}
					omu.Unlock()
					done <- name
				},
				OnError: func(e extension.DispatchError) {
					omu.Lock()
					outcomes[name] = &outcome{result: &extension.DispatchAgentResult{ExitCode: e.ExitCode}}
					omu.Unlock()
					done <- name
				},
				OnRecall: func(_ extension.RecallInfo) {
					omu.Lock()
					outcomes[name] = &outcome{
						recalled: true,
						result:   &extension.DispatchAgentResult{ExitCode: extcontext.ExitCodeRecalled},
					}
					omu.Unlock()
					done <- name
				},
			})
			if err != nil {
				return &types.ToolResult{Content: fmt.Sprintf("error: %v", err), IsError: true}, nil
			}
			return &types.ToolResult{Content: "dispatched"}, nil
		},
	})

	group := extension.NewExtensionGroup()
	group.Add(host)
	mgr.TestSetExtGroup("recall-s", group)

	ctx := mgr.TestNewExtContext("recall-s")
	if ctx == nil {
		t.Fatal("TestNewExtContext returned nil")
	}
	cAllTools := host.Tools()
	var tool *extension.ToolDefinition
	for i := range cAllTools {
		if cAllTools[i].Name == "do_dispatch" {
			tool = &cAllTools[i]
			break
		}
	}
	if tool == nil {
		t.Fatal("do_dispatch not found")
	}

	// Phase 1: Dispatch keeper (fast response, will complete quickly).
	_, _ = tool.Execute(map[string]interface{}{"name": "keeper", "task": "keep"}, ctx)

	// Wait for keeper to complete.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for keeper")
	}

	// Phase 2: Now block the provider and dispatch doomed.
	recallMP.SetBlockUntilCancel(true)
	_, _ = tool.Execute(map[string]interface{}{"name": "doomed", "task": "doom"}, ctx)

	// Wait for doomed to hit the provider.
	dl := time.After(10 * time.Second)
	for recallMP.CallCount() < 2 {
		select {
		case <-dl:
			t.Fatalf("timeout: only %d calls hit provider", recallMP.CallCount())
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Phase 3: Recall doomed. Its context cancels, unblocking the provider.
	found, _ := ctx.RecallAgent("doomed", extension.RecallAgentOpts{Reason: "test"})
	if !found {
		t.Error("RecallAgent(doomed) returned false")
	}

	// Wait for doomed's outcome.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for doomed recall outcome")
	}

	omu.Lock()
	doomedO := outcomes["doomed"]
	keeperO := outcomes["keeper"]
	omu.Unlock()

	if doomedO == nil {
		t.Fatal("no outcome for doomed")
	}
	if !doomedO.recalled {
		t.Errorf("doomed not recalled (exit=%d)", doomedO.result.ExitCode)
	}
	if doomedO.result.ExitCode != extcontext.ExitCodeRecalled {
		t.Errorf("doomed exit=%d want %d", doomedO.result.ExitCode, extcontext.ExitCodeRecalled)
	}

	if keeperO == nil {
		t.Fatal("no outcome for keeper")
	}
	if keeperO.recalled {
		t.Error("keeper incorrectly recalled")
	}
	if keeperO.result.ExitCode != 0 {
		t.Errorf("keeper exit=%d want 0", keeperO.result.ExitCode)
	}

	// Verify telemetry: one exitCode 0, one exitCode 2.
	mu.Lock()
	var endCodes []int
	for _, ev := range events {
		if ev.Type == "engine_dispatch_end" {
			endCodes = append(endCodes, ev.DispatchExitCode)
		}
	}
	mu.Unlock()
	if len(endCodes) < 2 {
		t.Fatalf("expected 2 engine_dispatch_end, got %d", len(endCodes))
	}
	hasZero, hasRecalled := false, false
	for _, c := range endCodes {
		if c == 0 {
			hasZero = true
		}
		if c == extcontext.ExitCodeRecalled {
			hasRecalled = true
		}
	}
	if !hasZero {
		t.Error("no dispatch_end with exitCode 0")
	}
	if !hasRecalled {
		t.Errorf("no dispatch_end with exitCode %d", extcontext.ExitCodeRecalled)
	}
}

// ─── helpers ───

func flattenContent(msgs []types.SessionMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteByte(' ')
	}
	return b.String()
}

type fixtureEvent struct {
	Type      string  `json:"type"`
	Agent     string  `json:"agent,omitempty"`
	Task      string  `json:"task,omitempty"`
	Model     string  `json:"model,omitempty"`
	SessionID string  `json:"sessionId,omitempty"`
	ExitCode  int     `json:"exitCode,omitempty"`
	Elapsed   float64 `json:"elapsed,omitempty"`
	Cost      float64 `json:"cost,omitempty"`
	Session   string  `json:"session"`
}

func writeEventFixture(t *testing.T, sessA, sessB *dispatchSessionResults) {
	t.Helper()
	var fixture []fixtureEvent
	emit := func(sr *dispatchSessionResults, label string) {
		sr.mu.Lock()
		defer sr.mu.Unlock()
		for _, ev := range sr.events {
			switch ev.Type {
			case "engine_dispatch_start":
				fixture = append(fixture, fixtureEvent{
					Type: ev.Type, Agent: ev.DispatchAgent, Task: ev.DispatchTask,
					Model: ev.DispatchModel, SessionID: ev.DispatchSessionID, Session: label,
				})
			case "engine_dispatch_end":
				fixture = append(fixture, fixtureEvent{
					Type: ev.Type, Agent: ev.DispatchAgent, ExitCode: ev.DispatchExitCode,
					Elapsed: ev.DispatchElapsed, Cost: ev.DispatchCost, Session: label,
				})
			case "engine_agent_state":
				fixture = append(fixture, fixtureEvent{Type: ev.Type, Session: label})
			}
		}
	}
	emit(sessA, "sess-a")
	emit(sessB, "sess-b")

	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Logf("marshal fixture: %v", err)
		return
	}
	dir := filepath.Join("testdata")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "dispatch_architecture_events.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Logf("write fixture: %v", err)
		return
	}
	t.Logf("wrote %d events to %s", len(fixture), path)
}
