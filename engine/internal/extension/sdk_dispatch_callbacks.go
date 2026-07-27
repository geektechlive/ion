package extension

// SDK types for the dispatch lifecycle event callbacks (OnToolStart, OnToolEnd,
// OnToolError, OnUsage, OnTextDelta, OnPlanProposal, OnChildQuestion).
//
// Extracted from sdk_types.go, which reached the 800-line cap. These are a
// naturally cohesive group: every type here is a payload the engine hands to one
// dispatch callback, they share the Name/DispatchID identifying pair, and none
// of them is referenced by the option/result types that stayed behind. Splitting
// here keeps whole doc comments intact rather than trimming them to fit, per the
// file-organization rule.

// --- Phase 2: Lifecycle event callback payloads ---

// DispatchToolStartInfo carries data for the OnToolStart callback.
type DispatchToolStartInfo struct {
	Name       string `json:"name"`
	DispatchID string `json:"dispatchId,omitempty"`
	ToolName   string `json:"toolName"`
	ToolID     string `json:"toolId"`
}

// DispatchToolEndInfo carries data for the OnToolEnd callback.
type DispatchToolEndInfo struct {
	Name       string `json:"name"`
	DispatchID string `json:"dispatchId,omitempty"`
	ToolName   string `json:"toolName"`
	ToolID     string `json:"toolId"`
	Content    string `json:"content"`
}

// DispatchToolErrorInfo carries data for the OnToolError callback.
type DispatchToolErrorInfo struct {
	Name       string `json:"name"`
	DispatchID string `json:"dispatchId,omitempty"`
	ToolName   string `json:"toolName"`
	ToolID     string `json:"toolId"`
	Content    string `json:"content"`
}

// DispatchUsageInfo carries per-turn and cumulative usage data.
type DispatchUsageInfo struct {
	Name       string `json:"name"`
	DispatchID string `json:"dispatchId,omitempty"`

	// Per-turn usage from the current UsageEvent.
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`

	// Cumulative totals across all turns in this dispatch.
	CumulativeInputTokens  int     `json:"cumulativeInputTokens"`
	CumulativeOutputTokens int     `json:"cumulativeOutputTokens"`
	CumulativeCost         float64 `json:"cumulativeCost"`
}

// DispatchTextDeltaInfo carries a text chunk and accumulated text.
type DispatchTextDeltaInfo struct {
	Name        string `json:"name"`
	DispatchID  string `json:"dispatchId,omitempty"`
	Delta       string `json:"delta"`
	Accumulated string `json:"accumulated"`
}

// DispatchPlanProposalInfo carries data for the OnPlanProposal callback.
type DispatchPlanProposalInfo struct {
	Name         string `json:"name"`
	AgentID      string `json:"agentId"`
	PlanFilePath string `json:"planFilePath"`
	PlanSlug     string `json:"planSlug"`
	// PlanRequested is true when the caller explicitly set PlanMode=true
	// on the dispatch opts. False when the child agent self-initiated
	// plan mode (called EnterPlanMode without being told to).
	PlanRequested bool `json:"planRequested"`
}

// DispatchChildQuestionInfo carries the question raised by a dispatched child
// via AskUserQuestion. Surfaced to the dispatcher via OnChildQuestion.
type DispatchChildQuestionInfo struct {
	// Name is the dispatched agent's name.
	Name string `json:"name"`
	// DispatchID is the dispatch's unique identifier.
	DispatchID string `json:"dispatchId"`
	// Question is the text from the child's AskUserQuestion call.
	Question string `json:"question"`
	// Depth is the dispatch nesting depth of the child (1 = direct child of orchestrator).
	Depth int `json:"depth"`
}
