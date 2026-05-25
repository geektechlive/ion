//go:build integration

package integration

// Integration test for ion-meta's deterministic git-gate.
//
// The git-gate is wired in engine/extensions/ion-meta/index.ts via the
// `tool_call` hook. It calls gateWriteToolCall from git-gate.ts and
// returns { block: true, reason } when the target path is outside any
// git working tree; otherwise it returns undefined (allow).
//
// This test exercises the full wire-level integration: load ion-meta
// into a Host, fire `tool_call` with a Write/file_path payload, and
// assert the hook returns the right block decision. Two cases:
//   1. Target NOT in a git repo → blocked with reason mentioning the path.
//   2. Target IS in a git repo → allowed (FireToolCall returns nil/no-block).
//
// Why this is here vs in a TS unit test: the engine extensions directory
// has no TS test runner configured. The Go integration harness is the
// existing pattern for exercising ion-meta end-to-end, and it validates
// not just the helper function but the actual hook wiring (the
// `tool_call` listener must be registered, the return shape must round-
// trip through the TS forwarder, the engine must interpret it as a
// block decision).
//
// File-size note: a separate test file keeps ion_meta_v2_test.go focused
// on tool / persona / agent-state semantics.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dsswift/ion/engine/internal/extension"
)

// ─── Outside a repo: tool_call blocks Write ───

func TestIonMetaGitGate_BlocksWriteOutsideRepo(t *testing.T) {
	requireEsbuild(t)
	metaDir := ionMetaDir(t)
	entry := ionMetaEntry(t)

	// Build a tmpProject with NO .git anywhere. We also redirect HOME
	// so the gate's "stop at ~" rule doesn't accidentally find a real
	// .git inside the developer's actual home dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	tmpProject := filepath.Join(tmpHome, "no-repo-here")
	if err := os.MkdirAll(tmpProject, 0o755); err != nil {
		t.Fatalf("mkdir tmpProject: %v", err)
	}
	targetFile := filepath.Join(tmpProject, "scratch.ts")

	host := extension.NewHost()
	t.Cleanup(func() { host.Dispose() })

	if err := host.Load(entry, &extension.ExtensionConfig{
		ExtensionDir:     metaDir,
		WorkingDirectory: tmpProject,
	}); err != nil {
		t.Fatalf("Load ion-meta: %v", err)
	}

	ctx := &extension.Context{
		SessionKey: "ion-meta-gate-blocks",
		Cwd:        tmpProject,
	}
	result, err := host.FireToolCall(ctx, extension.ToolCallInfo{
		ToolName: "Write",
		ToolID:   "test-tool-1",
		Input: map[string]interface{}{
			"file_path": targetFile,
			"content":   "ignored",
		},
	})
	if err != nil {
		t.Fatalf("FireToolCall: %v", err)
	}
	if result == nil {
		t.Fatalf("expected block decision, got nil (call would be allowed)")
	}
	if !result.Block {
		t.Errorf("expected Block=true, got false (reason: %q)", result.Reason)
	}
	// The reason must include the target path AND the policy rationale
	// so the LLM can recover gracefully (surface to user with the three
	// remediation options).
	if !strings.Contains(result.Reason, targetFile) {
		t.Errorf("block reason should include target path %q; got: %q", targetFile, result.Reason)
	}
	if !strings.Contains(strings.ToLower(result.Reason), "git working tree") {
		t.Errorf("block reason should mention 'git working tree'; got: %q", result.Reason)
	}
	for _, want := range []string{"git init", "teach", "explain"} {
		if !strings.Contains(strings.ToLower(result.Reason), want) {
			t.Errorf("block reason should mention remediation %q; got: %q", want, result.Reason)
		}
	}
}

// ─── Inside a repo: tool_call allows Write ───

func TestIonMetaGitGate_AllowsWriteInsideRepo(t *testing.T) {
	requireEsbuild(t)
	metaDir := ionMetaDir(t)
	entry := ionMetaEntry(t)

	// Build a tmpProject WITH a .git directory at its root. We also
	// redirect HOME so the gate's "stop at ~" rule still applies and
	// the only `.git` reachable is the one we just created.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	tmpProject := filepath.Join(tmpHome, "has-repo-here")
	if err := os.MkdirAll(filepath.Join(tmpProject, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	targetFile := filepath.Join(tmpProject, "scratch.ts")

	host := extension.NewHost()
	t.Cleanup(func() { host.Dispose() })

	if err := host.Load(entry, &extension.ExtensionConfig{
		ExtensionDir:     metaDir,
		WorkingDirectory: tmpProject,
	}); err != nil {
		t.Fatalf("Load ion-meta: %v", err)
	}

	ctx := &extension.Context{
		SessionKey: "ion-meta-gate-allows",
		Cwd:        tmpProject,
	}
	result, err := host.FireToolCall(ctx, extension.ToolCallInfo{
		ToolName: "Write",
		ToolID:   "test-tool-2",
		Input: map[string]interface{}{
			"file_path": targetFile,
			"content":   "ignored",
		},
	})
	if err != nil {
		t.Fatalf("FireToolCall: %v", err)
	}
	// The hook returned undefined (allow) OR a non-block result. Both
	// are acceptable; the engine treats them the same. Failure mode is
	// result != nil && result.Block == true.
	if result != nil && result.Block {
		t.Errorf("expected allow (Block=false or no decision); got Block=true reason=%q",
			result.Reason)
	}
}

// ─── Read-class tools are never gated ───

func TestIonMetaGitGate_AllowsReadOutsideRepo(t *testing.T) {
	requireEsbuild(t)
	metaDir := ionMetaDir(t)
	entry := ionMetaEntry(t)

	// Same setup as the block case: no .git anywhere reachable.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	tmpProject := filepath.Join(tmpHome, "no-repo-here-2")
	if err := os.MkdirAll(tmpProject, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	host := extension.NewHost()
	t.Cleanup(func() { host.Dispose() })

	if err := host.Load(entry, &extension.ExtensionConfig{
		ExtensionDir:     metaDir,
		WorkingDirectory: tmpProject,
	}); err != nil {
		t.Fatalf("Load ion-meta: %v", err)
	}

	ctx := &extension.Context{
		SessionKey: "ion-meta-gate-read",
		Cwd:        tmpProject,
	}
	// Read is not in GATED_TOOLS — should always pass regardless of
	// git status. This protects the gate from being over-broad and
	// blocking legitimate read-only work outside repos (e.g. the user
	// asks "look at this Python file in /tmp/").
	result, err := host.FireToolCall(ctx, extension.ToolCallInfo{
		ToolName: "Read",
		ToolID:   "test-tool-3",
		Input: map[string]interface{}{
			"file_path": filepath.Join(tmpProject, "anything.txt"),
		},
	})
	if err != nil {
		t.Fatalf("FireToolCall: %v", err)
	}
	if result != nil && result.Block {
		t.Errorf("Read should not be gated; got Block=true reason=%q", result.Reason)
	}
}
