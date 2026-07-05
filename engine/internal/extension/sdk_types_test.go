package extension

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDispatchAgentResult_CacheTokenFields_JSON(t *testing.T) {
	// Round-trip: populated cache fields appear in JSON and survive unmarshal.
	orig := DispatchAgentResult{
		Output:                   "done",
		ExitCode:                 0,
		Elapsed:                  1.5,
		Cost:                     0.003,
		InputTokens:              100,
		OutputTokens:             50,
		CacheReadInputTokens:     80,
		CacheCreationInputTokens: 20,
		SessionID:                "sess-1",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	raw := string(data)
	if !strings.Contains(raw, `"cacheReadInputTokens"`) {
		t.Errorf("JSON missing cacheReadInputTokens key: %s", raw)
	}
	if !strings.Contains(raw, `"cacheCreationInputTokens"`) {
		t.Errorf("JSON missing cacheCreationInputTokens key: %s", raw)
	}

	var decoded DispatchAgentResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.CacheReadInputTokens != 80 {
		t.Errorf("CacheReadInputTokens = %d, want 80", decoded.CacheReadInputTokens)
	}
	if decoded.CacheCreationInputTokens != 20 {
		t.Errorf("CacheCreationInputTokens = %d, want 20", decoded.CacheCreationInputTokens)
	}

	// Omitempty: zero-value cache fields must be absent from JSON output.
	zero := DispatchAgentResult{
		Output:       "done",
		InputTokens:  100,
		OutputTokens: 50,
	}

	zeroData, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}

	zeroRaw := string(zeroData)
	if strings.Contains(zeroRaw, "cacheReadInputTokens") {
		t.Errorf("zero-value JSON should omit cacheReadInputTokens: %s", zeroRaw)
	}
	if strings.Contains(zeroRaw, "cacheCreationInputTokens") {
		t.Errorf("zero-value JSON should omit cacheCreationInputTokens: %s", zeroRaw)
	}
}

// TestDispatchAgentResult_ThinkingTokens_JSON pins the issue #158 telemetry
// field: ThinkingTokens serializes when non-zero and is omitted when zero.
func TestDispatchAgentResult_ThinkingTokens_JSON(t *testing.T) {
	orig := DispatchAgentResult{
		Output:         "done",
		InputTokens:    100,
		OutputTokens:   50,
		ThinkingTokens: 30,
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"thinkingTokens":30`) {
		t.Errorf("JSON missing thinkingTokens=30: %s", string(data))
	}

	var decoded DispatchAgentResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ThinkingTokens != 30 {
		t.Errorf("ThinkingTokens = %d, want 30", decoded.ThinkingTokens)
	}

	// Omitempty: zero thinking tokens must be absent.
	zero := DispatchAgentResult{Output: "done"}
	zeroData, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(zeroData), "thinkingTokens") {
		t.Errorf("zero-value JSON should omit thinkingTokens: %s", string(zeroData))
	}
}

// TestDispatchID_OnNotificationPayloads pins that DispatchID serializes
// correctly on DispatchAgentResult, DispatchError, RecallInfo, and all
// lifecycle info types. The SDK notification receiver uses this field for
// parallel-safe handler routing.
//
// Revert-red: removing the DispatchID field from any of these types causes
// the corresponding JSON key assertion to fail.
func TestDispatchID_OnNotificationPayloads(t *testing.T) {
	// DispatchAgentResult (terminal: dispatch_complete)
	result := DispatchAgentResult{
		DispatchID: "dispatch-agent-123-abc",
		Output:     "done",
	}
	data, _ := json.Marshal(result)
	if !strings.Contains(string(data), `"dispatchId":"dispatch-agent-123-abc"`) {
		t.Errorf("DispatchAgentResult JSON missing dispatchId: %s", string(data))
	}

	// DispatchError (terminal: dispatch_error)
	de := DispatchError{
		Name:       "agent",
		DispatchID: "dispatch-agent-123-abc",
		Message:    "failed",
		ExitCode:   1,
	}
	data, _ = json.Marshal(de)
	if !strings.Contains(string(data), `"dispatchId":"dispatch-agent-123-abc"`) {
		t.Errorf("DispatchError JSON missing dispatchId: %s", string(data))
	}

	// RecallInfo (terminal: dispatch_recall)
	ri := RecallInfo{
		Name:       "agent",
		DispatchID: "dispatch-agent-123-abc",
		Reason:     "test",
	}
	data, _ = json.Marshal(ri)
	if !strings.Contains(string(data), `"dispatchId":"dispatch-agent-123-abc"`) {
		t.Errorf("RecallInfo JSON missing dispatchId: %s", string(data))
	}

	// Lifecycle: DispatchToolStartInfo
	ts := DispatchToolStartInfo{DispatchID: "d1", ToolName: "Read", ToolID: "t1"}
	data, _ = json.Marshal(ts)
	if !strings.Contains(string(data), `"dispatchId":"d1"`) {
		t.Errorf("DispatchToolStartInfo JSON missing dispatchId: %s", string(data))
	}

	// Lifecycle: DispatchToolEndInfo
	te := DispatchToolEndInfo{DispatchID: "d1", ToolName: "Read", ToolID: "t1"}
	data, _ = json.Marshal(te)
	if !strings.Contains(string(data), `"dispatchId":"d1"`) {
		t.Errorf("DispatchToolEndInfo JSON missing dispatchId: %s", string(data))
	}

	// Lifecycle: DispatchToolErrorInfo
	ter := DispatchToolErrorInfo{DispatchID: "d1", ToolName: "Read", ToolID: "t1"}
	data, _ = json.Marshal(ter)
	if !strings.Contains(string(data), `"dispatchId":"d1"`) {
		t.Errorf("DispatchToolErrorInfo JSON missing dispatchId: %s", string(data))
	}

	// Lifecycle: DispatchUsageInfo
	ui := DispatchUsageInfo{DispatchID: "d1", InputTokens: 10}
	data, _ = json.Marshal(ui)
	if !strings.Contains(string(data), `"dispatchId":"d1"`) {
		t.Errorf("DispatchUsageInfo JSON missing dispatchId: %s", string(data))
	}

	// Lifecycle: DispatchTextDeltaInfo
	td := DispatchTextDeltaInfo{DispatchID: "d1", Delta: "hi"}
	data, _ = json.Marshal(td)
	if !strings.Contains(string(data), `"dispatchId":"d1"`) {
		t.Errorf("DispatchTextDeltaInfo JSON missing dispatchId: %s", string(data))
	}

	// Omitempty: zero-value DispatchID must be absent.
	zero := DispatchError{Name: "agent", Message: "fail"}
	data, _ = json.Marshal(zero)
	if strings.Contains(string(data), "dispatchId") {
		t.Errorf("zero-value DispatchID should be omitted: %s", string(data))
	}
}
