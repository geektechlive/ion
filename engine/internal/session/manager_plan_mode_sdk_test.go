package session

import (
	"testing"

	"github.com/dsswift/ion/engine/internal/extension"
)

// ---------------------------------------------------------------------------
// GetPlanModeState tests (Gap 2)
// ---------------------------------------------------------------------------

func TestGetPlanModeState_InitialState(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("gpm-init", defaultConfig())

	enabled, path := mgr.GetPlanModeState("gpm-init")
	if enabled {
		t.Error("expected planMode=false on fresh session")
	}
	if path != "" {
		t.Errorf("expected empty planFilePath on fresh session, got %q", path)
	}
}

func TestGetPlanModeState_AfterEnable(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("gpm-enable", defaultConfig())

	mgr.SetPlanMode("gpm-enable", true, []string{"Read"}, "test", "")
	_ = mgr.SendPrompt("gpm-enable", "plan it", nil)

	enabled, path := mgr.GetPlanModeState("gpm-enable")
	if !enabled {
		t.Error("expected planMode=true after enable")
	}
	if path == "" {
		t.Error("expected non-empty planFilePath after SendPrompt in plan mode")
	}
}

func TestGetPlanModeState_AfterToggle(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("gpm-toggle", defaultConfig())

	mgr.SetPlanMode("gpm-toggle", true, []string{"Read"}, "test", "")
	_ = mgr.SendPrompt("gpm-toggle", "plan it", nil)

	mgr.mu.RLock()
	firstPath := mgr.sessions["gpm-toggle"].planFilePath
	mgr.mu.RUnlock()

	// Toggle off — path is preserved (Part 1 behaviour).
	mgr.SetPlanMode("gpm-toggle", false, nil, "ui_dropdown", "")

	enabled, path := mgr.GetPlanModeState("gpm-toggle")
	if enabled {
		t.Error("expected planMode=false after toggle off")
	}
	// Path preserved across manual toggle.
	if path != firstPath {
		t.Errorf("expected planFilePath=%q preserved after toggle, got %q", firstPath, path)
	}
}

func TestGetPlanModeState_UnknownSession(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)

	enabled, path := mgr.GetPlanModeState("no-such-session")
	if enabled {
		t.Error("expected enabled=false for unknown session")
	}
	if path != "" {
		t.Error("expected empty path for unknown session")
	}
}

// ---------------------------------------------------------------------------
// sessionAccessor.SetPlanMode and GetPlanModeState delegation tests (Gap 1+2)
// ---------------------------------------------------------------------------

// TestSessionAccessor_SetPlanMode verifies that calling ctx.SetPlanMode from
// an extension hook correctly flips session state via the sessionAccessor.
func TestSessionAccessor_SetPlanMode(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("sa-set", defaultConfig())

	// Attach an in-process extension that calls ctx.SetPlanMode(true) from
	// session_start — simulating a harness that starts sessions in plan mode.
	mgr.mu.Lock()
	s := mgr.sessions["sa-set"]
	if s.extGroup == nil {
		s.extGroup = extension.NewExtensionGroup()
	}
	host := extension.NewHost()
	s.extGroup.Add(host)
	host.SDK().On(extension.HookSessionStart, func(ctx *extension.Context, _ interface{}) (interface{}, error) {
		if ctx.SetPlanMode != nil {
			ctx.SetPlanMode(true, "session_start_hook")
		}
		return nil, nil
	})
	mgr.mu.Unlock()

	// Fire session_start by re-running loadAndWireExtensions via a shim.
	// In real usage this fires automatically during StartSession. Here we
	// trigger it manually via the manager's ext context.
	mgr.mu.RLock()
	extGroup := mgr.sessions["sa-set"].extGroup
	mgr.mu.RUnlock()
	ctx := mgr.newExtContextForKey("sa-set")
	_ = extGroup.FireSessionStart(ctx)

	// The session should now be in plan mode (SetPlanMode was called from hook).
	enabled, _ := mgr.GetPlanModeState("sa-set")
	if !enabled {
		t.Error("expected session to be in plan mode after extension called ctx.SetPlanMode(true)")
	}
}

// TestSessionAccessor_GetPlanMode verifies that ctx.GetPlanMode returns the
// correct state through the sessionAccessor.
func TestSessionAccessor_GetPlanMode(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("sa-get", defaultConfig())

	mgr.SetPlanMode("sa-get", true, []string{"Read"}, "test", "")
	_ = mgr.SendPrompt("sa-get", "plan it", nil)

	mgr.mu.RLock()
	allocatedPath := mgr.sessions["sa-get"].planFilePath
	mgr.mu.RUnlock()

	// Simulate what an extension hook would call via ctx.GetPlanMode.
	ctx := mgr.newExtContextForKey("sa-get")
	if ctx.GetPlanMode == nil {
		t.Fatal("expected ctx.GetPlanMode to be wired")
	}
	enabled, path := ctx.GetPlanMode()
	if !enabled {
		t.Error("expected GetPlanMode to return enabled=true")
	}
	if path != allocatedPath {
		t.Errorf("expected GetPlanMode planFilePath=%q, got=%q", allocatedPath, path)
	}
}

// ---------------------------------------------------------------------------
// RequestPlanModeExit / before_plan_mode_exit hook tests (Gap 3)
// ---------------------------------------------------------------------------

// TestRequestPlanModeExit_AutoAllowNoExtensions verifies default behaviour:
// exit is always allowed when no extensions are registered.
func TestRequestPlanModeExit_AutoAllowNoExtensions(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("exit-auto", defaultConfig())

	allowed, reason := mgr.RequestPlanModeExit("exit-auto", "/tmp/plan.md")
	if !allowed {
		t.Errorf("expected auto-allow with no extensions, got denied: reason=%q", reason)
	}
	if reason != "" {
		t.Errorf("expected empty reason when allowed, got %q", reason)
	}
}

// TestRequestPlanModeExit_UnknownSession verifies that an unknown session
// does NOT block the exit (fail-open to avoid locking the UI).
func TestRequestPlanModeExit_UnknownSession(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)

	allowed, _ := mgr.RequestPlanModeExit("ghost", "/tmp/plan.md")
	if !allowed {
		t.Error("expected exit allowed for unknown session (fail-open)")
	}
}

// TestRequestPlanModeExit_BlockedByHook verifies that an extension returning
// Allow=&false from before_plan_mode_exit prevents the model from exiting
// plan mode.
func TestRequestPlanModeExit_BlockedByHook(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("exit-hook-deny", defaultConfig())

	const denyReason = "plan is too short, add more detail"
	falseVal := false
	mgr.mu.Lock()
	s := mgr.sessions["exit-hook-deny"]
	if s.extGroup == nil {
		s.extGroup = extension.NewExtensionGroup()
	}
	host := extension.NewHost()
	s.extGroup.Add(host)
	host.SDK().On(extension.HookBeforePlanModeExit, func(_ *extension.Context, payload interface{}) (interface{}, error) {
		return &extension.BeforePlanModeExitResult{Allow: &falseVal, Reason: denyReason}, nil
	})
	mgr.mu.Unlock()

	allowed, reason := mgr.RequestPlanModeExit("exit-hook-deny", "/tmp/plan.md")
	if allowed {
		t.Error("expected exit denied by hook")
	}
	if reason != denyReason {
		t.Errorf("expected reason=%q, got=%q", denyReason, reason)
	}
}

// TestRequestPlanModeExit_AllowedByHook verifies that an extension explicitly
// returning Allow=&true permits the exit as expected.
func TestRequestPlanModeExit_AllowedByHook(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("exit-hook-allow", defaultConfig())

	trueVal := true
	mgr.mu.Lock()
	s := mgr.sessions["exit-hook-allow"]
	if s.extGroup == nil {
		s.extGroup = extension.NewExtensionGroup()
	}
	host := extension.NewHost()
	s.extGroup.Add(host)
	host.SDK().On(extension.HookBeforePlanModeExit, func(_ *extension.Context, _ interface{}) (interface{}, error) {
		return &extension.BeforePlanModeExitResult{Allow: &trueVal}, nil
	})
	mgr.mu.Unlock()

	allowed, reason := mgr.RequestPlanModeExit("exit-hook-allow", "/tmp/plan.md")
	if !allowed {
		t.Errorf("expected exit allowed by explicit hook, got denied: reason=%q", reason)
	}
}

// TestFireBeforePlanModeExit_DefaultAllow verifies SDK-level default allow.
func TestFireBeforePlanModeExit_DefaultAllow(t *testing.T) {
	sdk := extension.NewSDK()
	allowed, reason := sdk.FireBeforePlanModeExit(
		&extension.Context{},
		extension.BeforePlanModeExitInfo{PlanFilePath: "/tmp/plan.md", Source: "model_tool"},
	)
	if !allowed {
		t.Errorf("expected allowed=true by default, got false reason=%q", reason)
	}
}

// TestFireBeforePlanModeExit_DenyLastWins verifies last-writer-wins semantics.
func TestFireBeforePlanModeExit_DenyLastWins(t *testing.T) {
	sdk := extension.NewSDK()
	trueVal := true
	falseVal := false

	// First allows, second denies — last wins (deny).
	sdk.On(extension.HookBeforePlanModeExit, func(_ *extension.Context, _ interface{}) (interface{}, error) {
		return &extension.BeforePlanModeExitResult{Allow: &trueVal}, nil
	})
	sdk.On(extension.HookBeforePlanModeExit, func(_ *extension.Context, _ interface{}) (interface{}, error) {
		return &extension.BeforePlanModeExitResult{Allow: &falseVal, Reason: "second handler"}, nil
	})

	allowed, reason := sdk.FireBeforePlanModeExit(
		&extension.Context{},
		extension.BeforePlanModeExitInfo{PlanFilePath: "/tmp/plan.md", Source: "model_tool"},
	)
	if allowed {
		t.Error("expected denied when second handler returns Allow=false")
	}
	if reason != "second handler" {
		t.Errorf("expected reason=%q, got %q", "second handler", reason)
	}
}
