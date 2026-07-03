package session

import (
	"strings"
	"testing"

	"github.com/dsswift/ion/engine/internal/backend"
)

// ---------------------------------------------------------------------------
// buildToolAliasDirective unit tests
// ---------------------------------------------------------------------------

// TestBuildToolAliasDirective_EmptyInput verifies that an empty bare-name
// slice produces an empty string (callers skip the append).
func TestBuildToolAliasDirective_EmptyInput(t *testing.T) {
	got := buildToolAliasDirective(nil, backend.McpServerName)
	if got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
	got = buildToolAliasDirective([]string{}, backend.McpServerName)
	if got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}

// TestBuildToolAliasDirective_ContainsBareNames verifies each bare name
// appears in the directive.
func TestBuildToolAliasDirective_ContainsBareNames(t *testing.T) {
	names := []string{"dispatch_agent", "recall_agent", "convene_meeting"}
	got := buildToolAliasDirective(names, backend.McpServerName)
	for _, name := range names {
		if !strings.Contains(got, name) {
			t.Errorf("directive missing bare name %q:\n%s", name, got)
		}
	}
}

// TestBuildToolAliasDirective_ContainsPrefixedNames verifies each
// mcp__<server>__<name> form appears, sourced from backend.McpServerName so
// that a constant rename breaks this test rather than silently drifting.
func TestBuildToolAliasDirective_ContainsPrefixedNames(t *testing.T) {
	names := []string{"dispatch_agent", "recall_agent", "convene_meeting"}
	got := buildToolAliasDirective(names, backend.McpServerName)
	for _, name := range names {
		want := "mcp__" + backend.McpServerName + "__" + name
		if !strings.Contains(got, want) {
			t.Errorf("directive missing prefixed name %q:\n%s", want, got)
		}
	}
}

// TestBuildToolAliasDirective_ContainsInstruction verifies the directive
// contains the "treat bare as prefixed" instruction sentence.
func TestBuildToolAliasDirective_ContainsInstruction(t *testing.T) {
	got := buildToolAliasDirective([]string{"some_tool"}, backend.McpServerName)
	// The instruction must tell the model to use the prefixed name.
	if !strings.Contains(got, "prefixed name") {
		t.Errorf("directive missing 'prefixed name' instruction:\n%s", got)
	}
	if !strings.Contains(got, "bare tool name") {
		t.Errorf("directive missing 'bare tool name' instruction:\n%s", got)
	}
}

// TestBuildToolAliasDirective_SingleTool verifies single-tool output.
func TestBuildToolAliasDirective_SingleTool(t *testing.T) {
	got := buildToolAliasDirective([]string{"ion_agent"}, backend.McpServerName)
	if !strings.Contains(got, "ion_agent") {
		t.Errorf("directive missing bare name 'ion_agent':\n%s", got)
	}
	want := "mcp__" + backend.McpServerName + "__ion_agent"
	if !strings.Contains(got, want) {
		t.Errorf("directive missing prefixed name %q:\n%s", want, got)
	}
}

// ---------------------------------------------------------------------------
// IonDev tool surface regression lock
// ---------------------------------------------------------------------------

// TestBuildToolAliasDirective_IonDevSurface locks the six ion-dev extension
// tools: dispatch_agent, dispatch_specialist, request_consultant,
// convene_meeting, recall_agent, check_agent_status.  Every bare name and its
// mcp__<server>__<name> form must appear.  If backend.McpServerName drifts
// from "ion-extensions", the prefixed assertions fail here — that's
// intentional (the constant is the source of truth, not a hardcoded string).
func TestBuildToolAliasDirective_IonDevSurface(t *testing.T) {
	ionDevTools := []string{
		"dispatch_agent",
		"dispatch_specialist",
		"request_consultant",
		"convene_meeting",
		"recall_agent",
		"check_agent_status",
	}
	got := buildToolAliasDirective(ionDevTools, backend.McpServerName)
	for _, name := range ionDevTools {
		if !strings.Contains(got, name) {
			t.Errorf("ion-dev regression: bare name %q missing from directive:\n%s", name, got)
		}
		prefixed := "mcp__" + backend.McpServerName + "__" + name
		if !strings.Contains(got, prefixed) {
			t.Errorf("ion-dev regression: prefixed name %q missing from directive:\n%s", prefixed, got)
		}
	}
}
