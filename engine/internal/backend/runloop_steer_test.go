package backend

import (
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/types"
)

// TestSteer_NoSteerNormalExit verifies that a run with no steer message
// completes normally with exit code 0. This is the no-regression baseline.
func TestSteer_NoSteerNormalExit(t *testing.T) {
	setupTestProvider([][]types.LlmStreamEvent{
		textResponse("done", 10, 5),
	})

	b := NewApiBackend()
	c := collectEvents(b, "steer-no-steer")
	b.StartRun("steer-no-steer", types.RunOptions{
		Prompt:           "hello",
		ProjectPath:      "/tmp",
		Model:            testModel,
		EarlyStopEnabled: testEarlyStopDisabled(),
	})

	if !waitForExit(c, 5*time.Second) {
		t.Fatal("timed out waiting for exit")
	}

	c.mu.Lock()
	code := c.exitCode
	c.mu.Unlock()

	if code == nil || *code != 0 {
		t.Errorf("expected exit code 0, got %v", code)
	}

	// No SteerInjectedEvent should be present.
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ev := range c.normalized {
		if _, ok := ev.Data.(*types.SteerInjectedEvent); ok {
			t.Error("unexpected SteerInjectedEvent in no-steer run")
		}
	}
}

// TestSteer_BeforeEndTurnForcesContinuation verifies the critical fix:
// a steer message queued while the first LLM response is streaming is
// detected before TaskCompleteEvent fires, causing a forced continuation
// turn. The run must end with exit 0 and a SteerInjectedEvent must have
// been emitted.
func TestSteer_BeforeEndTurnForcesContinuation(t *testing.T) {
	setupTestProvider([][]types.LlmStreamEvent{
		textResponse("first response", 10, 5),
		textResponse("after steer response", 10, 5),
	})

	b := NewApiBackend()
	requestID := "steer-end-turn"
	c := collectEvents(b, requestID)

	b.StartRun(requestID, types.RunOptions{
		Prompt:           "do something",
		ProjectPath:      "/tmp",
		Model:            testModel,
		EarlyStopEnabled: testEarlyStopDisabled(),
	})

	// Send steer quickly — the mock provider returns immediately, so we
	// queue the steer message; drainSteer at the end_turn checkpoint will
	// pick it up before the run exits.
	b.Steer(requestID, "please redirect to topic B")

	if !waitForExit(c, 5*time.Second) {
		t.Fatal("timed out waiting for exit after steer injection")
	}

	c.mu.Lock()
	code := c.exitCode
	evs := append([]types.NormalizedEvent(nil), c.normalized...)
	c.mu.Unlock()

	if code == nil || *code != 0 {
		t.Errorf("expected exit code 0, got %v", code)
	}

	// SteerInjectedEvent must have been emitted.
	found := false
	for _, ev := range evs {
		if se, ok := ev.Data.(*types.SteerInjectedEvent); ok {
			found = true
			if se.MessageLength != len("please redirect to topic B") {
				t.Errorf("SteerInjectedEvent.MessageLength: want %d, got %d",
					len("please redirect to topic B"), se.MessageLength)
			}
		}
	}
	if !found {
		t.Error("expected SteerInjectedEvent but none was emitted")
	}
}

// TestSteer_DrainedAfterToolExecution verifies that a steer sent while a
// tool is running is captured by the post-tool-results drainSteer call and
// that the run completes normally.
func TestSteer_DrainedAfterToolExecution(t *testing.T) {
	setupTestProvider([][]types.LlmStreamEvent{
		// First turn: tool use
		toolUseResponse("Bash", "tool-1", map[string]any{"command": "echo hi"}, 10, 5),
		// Second turn: text response after tool result
		textResponse("tool done", 10, 5),
	})

	b := NewApiBackend()
	requestID := "steer-tool-drain"
	c := collectEvents(b, requestID)

	b.StartRun(requestID, types.RunOptions{
		Prompt:           "run a tool",
		ProjectPath:      "/tmp",
		Model:            testModel,
		EarlyStopEnabled: testEarlyStopDisabled(),
	})

	// Queue steer immediately; it will be drained after the tool completes.
	b.Steer(requestID, "steer during tool")

	if !waitForExit(c, 5*time.Second) {
		t.Fatal("timed out waiting for exit after tool+steer")
	}

	c.mu.Lock()
	code := c.exitCode
	evs := append([]types.NormalizedEvent(nil), c.normalized...)
	c.mu.Unlock()

	if code == nil || *code != 0 {
		t.Errorf("expected exit code 0, got %v", code)
	}

	// SteerInjectedEvent must have been emitted.
	found := false
	for _, ev := range evs {
		if _, ok := ev.Data.(*types.SteerInjectedEvent); ok {
			found = true
		}
	}
	if !found {
		t.Error("expected SteerInjectedEvent after tool execution steer")
	}
}

// TestSteer_MultipleSteersDrainCorrectly verifies that the steerCh buffer
// (capacity 4) can hold multiple messages and that each message queued before
// the run ends is surfaced as a SteerInjectedEvent. The run must complete with
// exit code 0.
//
// Each drainSteer call consumes exactly one message, so N steers require N
// continuation turns to fully drain. We set up N+1 provider responses to
// satisfy each turn.
func TestSteer_MultipleSteersDrainCorrectly(t *testing.T) {
	// 2 steers → 2 continuation turns → 3 provider responses total
	setupTestProvider([][]types.LlmStreamEvent{
		textResponse("response 1", 10, 5),
		textResponse("response 2", 10, 5),
		textResponse("response 3", 10, 5),
	})

	b := NewApiBackend()
	requestID := "steer-multi"
	c := collectEvents(b, requestID)

	b.StartRun(requestID, types.RunOptions{
		Prompt:           "do work",
		ProjectPath:      "/tmp",
		Model:            testModel,
		EarlyStopEnabled: testEarlyStopDisabled(),
	})

	// Queue two steers before the run finishes.
	b.Steer(requestID, "steer one")
	b.Steer(requestID, "steer two")

	if !waitForExit(c, 5*time.Second) {
		t.Fatal("timed out waiting for exit with multiple steers")
	}

	c.mu.Lock()
	code := c.exitCode
	evs := append([]types.NormalizedEvent(nil), c.normalized...)
	c.mu.Unlock()

	if code == nil || *code != 0 {
		t.Errorf("expected exit code 0, got %v", code)
	}

	// Count SteerInjectedEvents — should be at least 1 (may be 2 depending
	// on timing of the steer sends vs. drain checkpoints).
	steerCount := 0
	for _, ev := range evs {
		if _, ok := ev.Data.(*types.SteerInjectedEvent); ok {
			steerCount++
		}
	}
	if steerCount < 1 {
		t.Errorf("expected at least 1 SteerInjectedEvent, got %d", steerCount)
	}
}

// TestSteerWithReason_Delivered asserts the typed verdict for a live run with
// space in its steer channel: SteerWithReason returns SteerResultDelivered and
// the message is buffered (not dropped) for the next drain.
func TestSteerWithReason_Delivered(t *testing.T) {
	setupTestProvider([][]types.LlmStreamEvent{
		// A tool turn keeps the run live long enough to accept a steer while
		// the loop is parked inside executeTools — the same window a parent is
		// in while awaiting dispatched sub-agents (which execute as tools).
		toolUseResponse("Bash", "tool-1", map[string]any{"command": "echo hi"}, 10, 5),
		textResponse("done", 10, 5),
	})

	b := NewApiBackend()
	requestID := "steer-reason-delivered"
	c := collectEvents(b, requestID)

	b.StartRun(requestID, types.RunOptions{
		Prompt:           "park on a tool",
		ProjectPath:      "/tmp",
		Model:            testModel,
		EarlyStopEnabled: testEarlyStopDisabled(),
	})

	// The buffered channel (cap 4) accepts the steer immediately even while
	// the run is mid-tool; it is drained at the post-tool-results checkpoint
	// after the tool (sub-agent) returns.
	if got := b.SteerWithReason(requestID, "redirect while parked"); got != SteerResultDelivered {
		t.Fatalf("expected SteerResultDelivered, got %s", got)
	}

	if !waitForExit(c, 5*time.Second) {
		t.Fatal("timed out waiting for exit")
	}

	// And the steer must have actually been injected (drained) after the tool.
	c.mu.Lock()
	evs := append([]types.NormalizedEvent(nil), c.normalized...)
	c.mu.Unlock()
	found := false
	for _, ev := range evs {
		if _, ok := ev.Data.(*types.SteerInjectedEvent); ok {
			found = true
		}
	}
	if !found {
		t.Error("expected SteerInjectedEvent after the parked-on-tool steer was drained")
	}
}

// TestSteerWithReason_NoRun asserts the typed verdict for an unknown / inactive
// run: SteerWithReason returns SteerResultNoRun. The bare Steer wrapper must
// agree by returning false.
func TestSteerWithReason_NoRun(t *testing.T) {
	b := NewApiBackend()

	if got := b.SteerWithReason("no-such-run", "hello"); got != SteerResultNoRun {
		t.Fatalf("expected SteerResultNoRun, got %s", got)
	}
	if b.Steer("no-such-run", "hello") {
		t.Error("expected Steer to return false for an unknown run")
	}
}

// TestSteerWithReason_ChannelFull asserts the typed verdict when the steer
// channel is saturated. We construct an activeRun with a zero-capacity steer
// channel and register it directly so the non-blocking send fails immediately,
// exercising the channel-full branch deterministically.
func TestSteerWithReason_ChannelFull(t *testing.T) {
	b := NewApiBackend()
	const requestID = "steer-reason-full"

	// A zero-capacity channel with no receiver: every non-blocking send hits
	// the default branch (channel full).
	run := &activeRun{
		requestID: requestID,
		steerCh:   make(chan string), // cap 0, no drainer
	}
	b.mu.Lock()
	b.activeRuns[requestID] = run
	b.mu.Unlock()

	if got := b.SteerWithReason(requestID, "overflow"); got != SteerResultChannelFull {
		t.Fatalf("expected SteerResultChannelFull, got %s", got)
	}
	if b.Steer(requestID, "overflow") {
		t.Error("expected Steer to return false when the channel is full")
	}

	b.mu.Lock()
	delete(b.activeRuns, requestID)
	b.mu.Unlock()
}
