package extension

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCompactionInfo_JSONTags pins the lowercase JSON keys on CompactionInfo
// and CompactionFact. The TS SDK mirror (engine/extensions/sdk/ion-sdk/types.ts)
// and any non-Go harness consumer reading hook payloads off the JSON-RPC wire
// depends on this exact shape. If a Go field is renamed without updating its
// `json:` tag — or if the tag is dropped entirely so Go field-name casing leaks
// onto the wire — this test fails and the contract drift is caught at PR time.
func TestCompactionInfo_JSONTags(t *testing.T) {
	info := CompactionInfo{
		Strategy:       "auto",
		MessagesBefore: 30,
		MessagesAfter:  10,
		Facts: []CompactionFact{
			{Type: "decision", Content: "x"},
		},
	}
	out, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)

	// Required substrings — order is not load-bearing (encoding/json emits
	// fields in struct-declaration order, but we want this test to survive a
	// future field reorder), so check each key/value pair independently.
	mustContain := []string{
		`"strategy":"auto"`,
		`"messagesBefore":30`,
		`"messagesAfter":10`,
		`"facts":[`,
		`"type":"decision"`,
		`"content":"x"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("CompactionInfo JSON missing %q\nfull payload: %s", want, got)
		}
	}

	// Forbidden substrings — capitalized Go field names must not leak to the
	// wire. If the json: tag is dropped, encoding/json falls back to the Go
	// name, which would silently break TS/Swift mirrors.
	mustNotContain := []string{
		`"Strategy"`,
		`"MessagesBefore"`,
		`"MessagesAfter"`,
		`"Facts"`,
		`"Type"`,
		`"Content"`,
	}
	for _, bad := range mustNotContain {
		if strings.Contains(got, bad) {
			t.Errorf("CompactionInfo JSON leaks Go field name %q\nfull payload: %s", bad, got)
		}
	}
}

// TestCompactionInfo_FactsOmitEmpty verifies the `facts` key is omitted from
// the wire payload when the slice is empty or nil — older extension consumers
// that don't know about the new field will not see an unexpected key. This is
// the additive-contract invariant in test form.
func TestCompactionInfo_FactsOmitEmpty(t *testing.T) {
	info := CompactionInfo{Strategy: "auto", MessagesBefore: 5, MessagesAfter: 3}
	out, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)
	if strings.Contains(got, `"facts"`) {
		t.Errorf("expected facts key omitted when slice is nil; got: %s", got)
	}
}
