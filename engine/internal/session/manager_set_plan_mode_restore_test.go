package session

import (
	"os"
	"path/filepath"
	"testing"
)

// Restore-on-toggle tests for SetPlanMode's planFilePath argument.
//
// Regression coverage for the plan-file continuity bug: when an engine
// session is replaced (rebound from the binding store) it is born with
// planFilePath="". A plan-mode toggle (set_plan_mode, no prompt) must be
// able to RESTORE the conversation's existing plan path so the next prompt
// reuses it instead of allocating a fresh slug. The restore only applies
// when enabling plan mode, the session has no path yet, and the supplied
// path exists on disk — mirroring SendPrompt's restore guard.

// TestSetPlanMode_RestoresPathWhenEmpty: enabling plan mode with a
// client-supplied path that exists on disk restores it onto the session.
// Pre-fix (SetPlanMode ignored the path) this leaves planFilePath empty.
func TestSetPlanMode_RestoresPathWhenEmpty(t *testing.T) {
	planFile := filepath.Join(t.TempDir(), "simple-sailing-pine.md")
	if err := os.WriteFile(planFile, []byte("# real plan"), 0644); err != nil {
		t.Fatal(err)
	}
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("restore-empty", defaultConfig())

	// Session is born with planFilePath="". A toggle ON carrying the
	// persisted path should adopt it.
	mgr.SetPlanMode("restore-empty", true, []string{"Read"}, "ui_dropdown", planFile)

	enabled, path := mgr.GetPlanModeState("restore-empty")
	if !enabled {
		t.Error("expected planMode=true after enable")
	}
	if path != planFile {
		t.Errorf("expected restored planFilePath=%q, got %q", planFile, path)
	}
}

// TestSetPlanMode_DoesNotOverwriteExistingPath: when the session already
// has a plan path, a toggle carrying a different path must NOT replace it.
func TestSetPlanMode_DoesNotOverwriteExistingPath(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing-plan.md")
	other := filepath.Join(dir, "other-plan.md")
	for _, p := range []string{existing, other} {
		if err := os.WriteFile(p, []byte("# plan"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("no-overwrite", defaultConfig())

	// Seed the session with an existing path directly.
	mgr.mu.Lock()
	mgr.sessions["no-overwrite"].planFilePath = existing
	mgr.mu.Unlock()

	// Toggle ON carrying a DIFFERENT path — must be ignored.
	mgr.SetPlanMode("no-overwrite", true, []string{"Read"}, "ui_dropdown", other)

	_, path := mgr.GetPlanModeState("no-overwrite")
	if path != existing {
		t.Errorf("expected existing planFilePath %q preserved, got %q", existing, path)
	}
}

// TestSetPlanMode_IgnoresPathNotOnDisk: a supplied path that does not exist
// on disk must NOT be adopted — the session stays empty so the next prompt
// allocates a fresh slug (matching SendPrompt's on-disk guard).
func TestSetPlanMode_IgnoresPathNotOnDisk(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-written.md")
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("missing-path", defaultConfig())

	mgr.SetPlanMode("missing-path", true, []string{"Read"}, "ui_dropdown", missing)

	_, path := mgr.GetPlanModeState("missing-path")
	if path != "" {
		t.Errorf("expected planFilePath to stay empty for non-existent file, got %q", path)
	}
}

// TestSetPlanMode_EmptyPathIsNoOp: the common case — client supplies no
// path. Behaviour is unchanged: planFilePath stays empty on enable.
func TestSetPlanMode_EmptyPathIsNoOp(t *testing.T) {
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("empty-noop", defaultConfig())

	mgr.SetPlanMode("empty-noop", true, []string{"Read"}, "ui_dropdown", "")

	_, path := mgr.GetPlanModeState("empty-noop")
	if path != "" {
		t.Errorf("expected planFilePath empty when no path supplied, got %q", path)
	}
}

// TestSetPlanMode_DisableIgnoresPath: a disable toggle never restores a
// path — restore only applies on enable.
func TestSetPlanMode_DisableIgnoresPath(t *testing.T) {
	planFile := filepath.Join(t.TempDir(), "disable-plan.md")
	if err := os.WriteFile(planFile, []byte("# plan"), 0644); err != nil {
		t.Fatal(err)
	}
	mb := newMockBackend()
	mgr := NewManager(mb)
	_, _ = mgr.StartSession("disable-path", defaultConfig())

	mgr.SetPlanMode("disable-path", false, nil, "ui_dropdown", planFile)

	_, path := mgr.GetPlanModeState("disable-path")
	if path != "" {
		t.Errorf("expected disable not to restore planFilePath, got %q", path)
	}
}
