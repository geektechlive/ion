package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/dsswift/ion/engine/internal/tools"
	"github.com/dsswift/ion/engine/internal/types"
)

// TestChildQuestion_RoutesToDispatcher pins the "AskUserQuestion
// symmetrization" runloop behavior: when a dispatched child run has a
// ChildElicitFn set, an AskUserQuestion tool call is routed to the
// dispatcher (the callback) instead of terminating the run. The dispatcher's
// answer is injected as the tool result and the run CONTINUES — no
// PermissionDenial is recorded and exitPlanMode stays false.
//
// Revert-check: removing the ChildElicitFn branch in runloop_tools.go makes
// AskUserQuestion fall through to the terminate path; the run would record a
// PermissionDenial and the answer would never appear in the tool result.
func TestChildQuestion_RoutesToDispatcher(t *testing.T) {
	b := NewApiBackend()
	b.OnNormalized(func(_ string, _ types.NormalizedEvent) {})

	const wantAnswer = "use the blue widget"
	var gotQuestion string

	run := &activeRun{
		requestID: "test-child-question",
		cfg: &RunConfig{
			ChildElicitFn: func(question string) (string, bool, error) {
				gotQuestion = question
				return wantAnswer, false, nil
			},
		},
	}

	blocks := []types.LlmContentBlock{{
		Name:  tools.AskUserQuestionName,
		ID:    "tc-1",
		Input: map[string]interface{}{"question": "which widget?"},
	}}
	results, err := b.executeTools(context.Background(), run, blocks, t.TempDir())
	if err != nil {
		t.Fatalf("executeTools error: %v", err)
	}

	if gotQuestion != "which widget?" {
		t.Errorf("dispatcher received question %q, want %q", gotQuestion, "which widget?")
	}
	if results[0].IsError {
		t.Errorf("tool result IsError=true, want false: %q", results[0].Content)
	}
	if results[0].Content != wantAnswer {
		t.Errorf("tool result content = %q, want the dispatcher's answer %q", results[0].Content, wantAnswer)
	}

	// Run must CONTINUE: no PermissionDenial, exitPlanMode stays false.
	run.mu.Lock()
	denials := len(run.permissionDenials)
	exited := run.exitPlanMode
	run.mu.Unlock()
	if denials != 0 {
		t.Errorf("expected 0 PermissionDenials (run continues), got %d", denials)
	}
	if exited {
		t.Error("expected exitPlanMode=false (run continues), got true")
	}
}

// TestChildQuestion_EmptyAnswerInjectsPlaceholder pins that an empty answer
// from the dispatcher (answer="", cancelled=false) still continues the run,
// injecting a best-judgment placeholder rather than terminating.
func TestChildQuestion_EmptyAnswerInjectsPlaceholder(t *testing.T) {
	b := NewApiBackend()
	b.OnNormalized(func(_ string, _ types.NormalizedEvent) {})

	run := &activeRun{
		requestID: "test-child-question-empty",
		cfg: &RunConfig{
			ChildElicitFn: func(_ string) (string, bool, error) {
				return "", false, nil
			},
		},
	}

	blocks := []types.LlmContentBlock{{
		Name:  tools.AskUserQuestionName,
		ID:    "tc-1",
		Input: map[string]interface{}{"question": "anything?"},
	}}
	results, err := b.executeTools(context.Background(), run, blocks, t.TempDir())
	if err != nil {
		t.Fatalf("executeTools error: %v", err)
	}
	if results[0].IsError {
		t.Errorf("tool result IsError=true, want false")
	}
	if !strings.Contains(results[0].Content, "best judgment") {
		t.Errorf("expected placeholder content for empty answer, got %q", results[0].Content)
	}
	run.mu.Lock()
	denials := len(run.permissionDenials)
	run.mu.Unlock()
	if denials != 0 {
		t.Errorf("expected 0 PermissionDenials, got %d", denials)
	}
}

// TestChildQuestion_NilCallback_TerminatesRun pins that without a
// ChildElicitFn, AskUserQuestion terminates the run via the standard
// PermissionDenial path (exitPlanMode=true), exactly as before this feature.
func TestChildQuestion_NilCallback_TerminatesRun(t *testing.T) {
	b := NewApiBackend()
	b.OnNormalized(func(_ string, _ types.NormalizedEvent) {})

	run := &activeRun{
		requestID: "test-child-question-nil",
		cfg:       &RunConfig{}, // no ChildElicitFn
	}

	blocks := []types.LlmContentBlock{{
		Name:  tools.AskUserQuestionName,
		ID:    "tc-1",
		Input: map[string]interface{}{"question": "should I terminate?"},
	}}
	results, err := b.executeTools(context.Background(), run, blocks, t.TempDir())
	if err != nil {
		t.Fatalf("executeTools error: %v", err)
	}

	run.mu.Lock()
	denials := len(run.permissionDenials)
	exited := run.exitPlanMode
	run.mu.Unlock()
	if denials != 1 {
		t.Errorf("expected 1 PermissionDenial (standard terminate path), got %d", denials)
	}
	if !exited {
		t.Error("expected exitPlanMode=true (standard terminate path), got false")
	}
	if !strings.Contains(results[0].Content, "Awaiting response") {
		t.Errorf("expected standard 'awaiting response' content, got %q", results[0].Content)
	}
}

// TestChildQuestion_CancelledByDispatcher pins that when the dispatcher
// cancels (cancelled=true), the child run terminates via the PermissionDenial
// path — the same terminal outcome as the nil-callback case, so consumers see
// uniform behavior whether the dispatcher was absent or declined to answer.
func TestChildQuestion_CancelledByDispatcher(t *testing.T) {
	b := NewApiBackend()
	b.OnNormalized(func(_ string, _ types.NormalizedEvent) {})

	run := &activeRun{
		requestID: "test-child-question-cancelled",
		cfg: &RunConfig{
			ChildElicitFn: func(_ string) (string, bool, error) {
				return "", true, nil // cancelled
			},
		},
	}

	blocks := []types.LlmContentBlock{{
		Name:  tools.AskUserQuestionName,
		ID:    "tc-1",
		Input: map[string]interface{}{"question": "cancel me"},
	}}
	results, err := b.executeTools(context.Background(), run, blocks, t.TempDir())
	if err != nil {
		t.Fatalf("executeTools error: %v", err)
	}

	run.mu.Lock()
	denials := len(run.permissionDenials)
	exited := run.exitPlanMode
	run.mu.Unlock()
	if denials != 1 {
		t.Errorf("expected 1 PermissionDenial (cancelled terminates), got %d", denials)
	}
	if !exited {
		t.Error("expected exitPlanMode=true (cancelled terminates), got false")
	}
	if !strings.Contains(results[0].Content, "dispatcher unavailable") {
		t.Errorf("expected dispatcher-unavailable content, got %q", results[0].Content)
	}
}

// TestChildQuestion_ErrorByDispatcher pins that a callback error also
// terminates the run via the PermissionDenial path.
func TestChildQuestion_ErrorByDispatcher(t *testing.T) {
	b := NewApiBackend()
	b.OnNormalized(func(_ string, _ types.NormalizedEvent) {})

	run := &activeRun{
		requestID: "test-child-question-err",
		cfg: &RunConfig{
			ChildElicitFn: func(_ string) (string, bool, error) {
				return "", false, context.Canceled
			},
		},
	}

	blocks := []types.LlmContentBlock{{
		Name:  tools.AskUserQuestionName,
		ID:    "tc-1",
		Input: map[string]interface{}{"question": "error path"},
	}}
	if _, err := b.executeTools(context.Background(), run, blocks, t.TempDir()); err != nil {
		t.Fatalf("executeTools error: %v", err)
	}

	run.mu.Lock()
	denials := len(run.permissionDenials)
	exited := run.exitPlanMode
	run.mu.Unlock()
	if denials != 1 {
		t.Errorf("expected 1 PermissionDenial (error terminates), got %d", denials)
	}
	if !exited {
		t.Error("expected exitPlanMode=true (error terminates), got false")
	}
}
