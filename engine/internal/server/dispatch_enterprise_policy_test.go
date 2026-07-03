package server

// dispatch_enterprise_policy_test.go — end-to-end tests for the
// get_enterprise_policy command handler. Each test drives the full
// JSON-decode → dispatch → ServerResult path against actual wire input,
// pinning the handler's two contractual behaviors:
//
//   1. When the engine has an enterprise NewConversationDefaults policy, the RPC
//      returns it under data.newConversationDefaults with baseDirectory /
//      engineProfileId / locked intact.
//   2. When no enterprise config (or no NewConversationDefaults section) is present,
//      data.newConversationDefaults is null — NOT absent, NOT an error.
//
// This is the contract desktop and iOS rely on to decide whether the
// new-conversation flow is enterprise-locked.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/types"
)

// enterprisePolicyResult decodes the ServerResult.Data of a
// get_enterprise_policy response into its { newConversationDefaults } shape.
// hasField reports whether the "newConversationDefaults" key was present at all
// (so we can distinguish a null value from an omitted one).
func enterprisePolicyResult(t *testing.T, lines []string) (policy *types.NewConversationDefaultsPolicy, hasField bool, ok bool) {
	t.Helper()
	result := findResult(t, lines)
	if result == nil {
		t.Fatalf("no result received; lines=%v", lines)
	}
	// Re-marshal Data to inspect the newConversationDefaults key explicitly.
	raw, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("marshal result.Data: %v", err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal result.Data into map: %v", err)
	}
	rawPolicy, present := probe["newConversationDefaults"]
	if !present {
		return nil, false, result.OK
	}
	if string(rawPolicy) == "null" {
		return nil, true, result.OK
	}
	var p types.NewConversationDefaultsPolicy
	if err := json.Unmarshal(rawPolicy, &p); err != nil {
		t.Fatalf("unmarshal newConversationDefaults: %v", err)
	}
	return &p, true, result.OK
}

// TestGetEnterprisePolicy_Present verifies that a configured NewConversationDefaults
// policy is returned verbatim under data.newConversationDefaults.
func TestGetEnterprisePolicy_Present(t *testing.T) {
	mb := newMockBackend()
	srv := newShortPathTestServer(t, mb)

	// Inject an enterprise config carrying a locked new-conversation policy.
	srv.SetConfig(&types.EngineRuntimeConfig{
		Enterprise: &types.EnterpriseConfig{
			NewConversationDefaults: &types.NewConversationDefaultsPolicy{
				BaseDirectory:   "/corp/projects",
				EngineProfileId: "profile-corp",
				Locked:          true,
			},
		},
	})

	conn := dialServer(t, srv)
	t.Cleanup(func() { conn.Close() })

	sendJSON(t, conn, map[string]interface{}{
		"cmd":       "get_enterprise_policy",
		"requestId": "req-ent-present",
	})

	lines := readLines(t, conn, 3, 2*time.Second)
	policy, hasField, ok := enterprisePolicyResult(t, lines)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if !hasField {
		t.Fatalf("response data must contain newConversationDefaults key")
	}
	if policy == nil {
		t.Fatalf("newConversationDefaults must be the configured policy, got null")
	}
	if policy.BaseDirectory != "/corp/projects" {
		t.Errorf("BaseDirectory: got %q, want %q", policy.BaseDirectory, "/corp/projects")
	}
	if policy.EngineProfileId != "profile-corp" {
		t.Errorf("EngineProfileId: got %q, want %q", policy.EngineProfileId, "profile-corp")
	}
	if !policy.Locked {
		t.Errorf("Locked: got false, want true")
	}
}

// TestGetEnterprisePolicy_NoConfig verifies that when the server has no
// engine config at all, data.newConversationDefaults is present and null (the
// "no enterprise policy" signal), and the result is still ok=true.
func TestGetEnterprisePolicy_NoConfig(t *testing.T) {
	mb := newMockBackend()
	srv := newShortPathTestServer(t, mb)
	// Deliberately do NOT call SetConfig: s.config stays nil.

	conn := dialServer(t, srv)
	t.Cleanup(func() { conn.Close() })

	sendJSON(t, conn, map[string]interface{}{
		"cmd":       "get_enterprise_policy",
		"requestId": "req-ent-none",
	})

	lines := readLines(t, conn, 3, 2*time.Second)
	policy, hasField, ok := enterprisePolicyResult(t, lines)
	if !ok {
		t.Fatalf("expected ok=true even with no config, got ok=false")
	}
	if !hasField {
		t.Fatalf("response data must contain the newConversationDefaults key (as null), not omit it")
	}
	if policy != nil {
		t.Errorf("newConversationDefaults must be null when no config is loaded, got %+v", policy)
	}
}

// TestGetEnterprisePolicy_ConfigWithoutSection verifies that an enterprise
// config that has no NewConversationDefaults section still yields a null policy
// (not an error, not a zero-value struct).
func TestGetEnterprisePolicy_ConfigWithoutSection(t *testing.T) {
	mb := newMockBackend()
	srv := newShortPathTestServer(t, mb)

	// Enterprise config present, but NewConversationDefaults intentionally nil.
	srv.SetConfig(&types.EngineRuntimeConfig{
		Enterprise: &types.EnterpriseConfig{},
	})

	conn := dialServer(t, srv)
	t.Cleanup(func() { conn.Close() })

	sendJSON(t, conn, map[string]interface{}{
		"cmd":       "get_enterprise_policy",
		"requestId": "req-ent-empty",
	})

	lines := readLines(t, conn, 3, 2*time.Second)
	policy, hasField, ok := enterprisePolicyResult(t, lines)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if !hasField {
		t.Fatalf("response data must contain the newConversationDefaults key (as null)")
	}
	if policy != nil {
		t.Errorf("newConversationDefaults must be null when the section is absent, got %+v", policy)
	}
}
