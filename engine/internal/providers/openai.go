package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dsswift/ion/engine/internal/network"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

type openaiProvider struct {
	id         string
	apiKey     string
	baseURL    string
	authHeader string // "bearer" (default) or "x-api-key"
	client     *http.Client
}

// NewOpenAIProvider creates an OpenAI provider that uses raw HTTP SSE and
// translates OpenAI streaming events to Anthropic canonical format.
func NewOpenAIProvider(opts *ProviderOptions) LlmProvider {
	apiKey := ""
	baseURL := "https://api.openai.com"
	id := "openai"
	if opts != nil {
		if opts.APIKey != "" {
			apiKey = opts.APIKey
		}
		if opts.BaseURL != "" {
			baseURL = opts.BaseURL
		}
		if opts.ID != "" {
			id = opts.ID
		}
	}
	if apiKey == "" && id == "openai" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	authHeader := "bearer"
	if opts != nil && opts.AuthHeader != "" {
		authHeader = opts.AuthHeader
	}

	result := &openaiProvider{
		id:         id,
		apiKey:     apiKey,
		baseURL:    baseURL,
		authHeader: authHeader,
		client:     &http.Client{Transport: network.GetHTTPTransport()},
	}
	utils.Log("OpenAI", fmt.Sprintf("NewOpenAIProvider: id=%s baseURL=%s apiKeyLen=%d authHeader=%s", id, baseURL, len(apiKey), authHeader))
	return result
}

func (p *openaiProvider) ID() string { return p.id }

func (p *openaiProvider) Stream(ctx context.Context, opts types.LlmStreamOptions) (<-chan types.LlmStreamEvent, <-chan error) {
	events := make(chan types.LlmStreamEvent, 32)
	errc := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errc)

		if err := p.doStream(ctx, opts, events); err != nil {
			errc <- err
		}
	}()

	return events, errc
}

func (p *openaiProvider) doStream(ctx context.Context, opts types.LlmStreamOptions, events chan<- types.LlmStreamEvent) error {
	utils.Log("OpenAI", fmt.Sprintf("doStream: id=%s model=%s baseURL=%s", p.id, opts.Model, p.baseURL))
	body := p.buildRequestBody(opts)

	raw, err := json.Marshal(body)
	if err != nil {
		utils.Error("OpenAI", fmt.Sprintf("doStream: marshal error: %v", err))
		return FromOpenAIError(fmt.Errorf("marshal request: %w", err), 0, "")
	}

	// Build URL: append /chat/completions, or /v1/chat/completions if baseURL doesn't include /v1
	endpoint := p.baseURL + "/v1/chat/completions"
	if strings.HasSuffix(p.baseURL, "/v1") || strings.Contains(p.baseURL, "/v1/") {
		endpoint = strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	}
	utils.Log("OpenAI", fmt.Sprintf("doStream: endpoint=%s", endpoint))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		utils.Error("OpenAI", fmt.Sprintf("doStream: create request error: %v", err))
		return FromOpenAIError(fmt.Errorf("create request: %w", err), 0, "")
	}

	req.Header.Set("Content-Type", "application/json")
	apiKey := p.apiKey
	keySource := "constructor"
	if apiKey == "" {
		apiKey = GetProviderKey(p.id)
		keySource = "registry:" + p.id
	}
	utils.Log("OpenAI", fmt.Sprintf("doStream: auth id=%s keySource=%s keyLen=%d authStyle=%s", p.id, keySource, len(apiKey), p.authHeader))
	setAuthHeader(req, p.authHeader, apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(req)
	if err != nil {
		return FromOpenAIError(err, 0, "")
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Log("OpenAI", fmt.Sprintf("doStream: response body close failed: %v", err))
		}
	}()
	utils.Debug("OpenAI", fmt.Sprintf("doStream: HTTP response status=%d", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		utils.Error("OpenAI", fmt.Sprintf("doStream: HTTP %d error for %s: %s", resp.StatusCode, endpoint, string(respBody)))
		return FromOpenAIError(
			fmt.Errorf("openai API error: %s", string(respBody)),
			resp.StatusCode,
			string(respBody),
		)
	}

	// Emit message_start
	msgStart := types.LlmStreamEvent{
		Type: "message_start",
		MessageInfo: &types.LlmStreamMessageInfo{
			ID:    fmt.Sprintf("msg_openai_%d", time.Now().UnixMilli()),
			Model: opts.Model,
			Usage: types.LlmUsage{},
		},
	}
	select {
	case events <- msgStart:
	case <-ctx.Done():
		return ctx.Err()
	}

	var (
		contentIndex    int
		inTextBlock     bool
		currentToolID   string
		totalInputToks  int
		totalOutputToks int
		toolChunksSeen  int
	)

	sseCh, sseErr := ParseSSEStream(resp.Body)
	for sse := range sseCh {
		if sse.Data == "" {
			continue
		}

		// Observability for tool-call assembly: log the first few raw chunks
		// that carry a tool_calls delta. Provider tool-call streaming shapes
		// diverge (single-chunk args vs incremental, id/index placement), and a
		// mis-parse silently drops arguments -- log the ground truth so that
		// class of bug is diagnosable from ~/.ion/engine.log alone.
		if toolChunksSeen < 3 && strings.Contains(sse.Data, "tool_calls") {
			toolChunksSeen++
			raw := sse.Data
			if len(raw) > 400 {
				raw = raw[:400] + "...(truncated)"
			}
			utils.Debug("OpenAI", fmt.Sprintf("doStream: tool_calls chunk #%d id=%s model=%s: %s", toolChunksSeen, p.id, opts.Model, raw))
		}

		var chunk openaiChunk
		if err := json.Unmarshal([]byte(sse.Data), &chunk); err != nil {
			continue
		}

		// In-stream provider error: an OpenAI-compatible provider can return
		// HTTP 200 and then signal an upstream failure via a top-level error
		// object (frequently with empty choices). Surface it as a ProviderError
		// so WithRetry can retry a transient failure and the run never exits 0
		// with empty output.
		if chunk.Error != nil {
			pe := classifyOpenAIStreamError(chunk.Error)
			utils.Error("OpenAI", fmt.Sprintf("doStream: in-stream error id=%s model=%s code=%s retryable=%v msg=%s", p.id, opts.Model, pe.Code, pe.Retryable, pe.Message))
			return pe
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		delta := choice.Delta

		// Usage
		if chunk.Usage != nil {
			totalInputToks = chunk.Usage.PromptTokens
			totalOutputToks = chunk.Usage.CompletionTokens
		}

		// Text content
		if delta.Content != "" {
			if !inTextBlock {
				// Close any open tool block first
				if currentToolID != "" {
					if err := sendEvent(ctx, events, types.LlmStreamEvent{Type: "content_block_stop", BlockIndex: contentIndex}); err != nil {
						return err
					}
					contentIndex++
					currentToolID = ""
				}
				if err := sendEvent(ctx, events, types.LlmStreamEvent{
					Type:       "content_block_start",
					BlockIndex: contentIndex,
					ContentBlock: &types.LlmStreamContentBlock{
						Type: "text",
						Text: "",
					},
				}); err != nil {
					return err
				}
				inTextBlock = true
			}
			if err := sendEvent(ctx, events, types.LlmStreamEvent{
				Type:       "content_block_delta",
				BlockIndex: contentIndex,
				Delta: &types.LlmStreamDelta{
					Type: "text_delta",
					Text: delta.Content,
				},
			}); err != nil {
				return err
			}
		}

		// Tool calls
		for _, tc := range delta.ToolCalls {
			if tc.ID != "" {
				// Close previous block
				if inTextBlock || currentToolID != "" {
					if err := sendEvent(ctx, events, types.LlmStreamEvent{Type: "content_block_stop", BlockIndex: contentIndex}); err != nil {
						return err
					}
					contentIndex++
					inTextBlock = false
				}
				currentToolID = tc.ID
				name := ""
				if tc.Function != nil {
					name = tc.Function.Name
				}
				if err := sendEvent(ctx, events, types.LlmStreamEvent{
					Type:       "content_block_start",
					BlockIndex: contentIndex,
					ContentBlock: &types.LlmStreamContentBlock{
						Type: "tool_use",
						ID:   tc.ID,
						Name: name,
					},
				}); err != nil {
					return err
				}
			}
			if tc.Function != nil && tc.Function.Arguments != "" {
				if err := sendEvent(ctx, events, types.LlmStreamEvent{
					Type:       "content_block_delta",
					BlockIndex: contentIndex,
					Delta: &types.LlmStreamDelta{
						Type:        "input_json_delta",
						PartialJSON: tc.Function.Arguments,
					},
				}); err != nil {
					return err
				}
			}
		}

		// Finish reason
		if choice.FinishReason != "" {
			// A finish_reason of "error" is an upstream failure delivered
			// without a transport error. Convert it to a ProviderError (chunk
			// error detail when present, else a generic transient one) instead
			// of forwarding "error" as a benign stop reason.
			if choice.FinishReason == "error" {
				pe := classifyOpenAIStreamError(chunk.Error)
				utils.Error("OpenAI", fmt.Sprintf("doStream: finish_reason=error id=%s model=%s code=%s retryable=%v msg=%s", p.id, opts.Model, pe.Code, pe.Retryable, pe.Message))
				return pe
			}
			// Close any open block exactly once. Reset the block state after
			// emitting content_block_stop so a trailing chunk that also carries
			// a finish_reason (OpenRouter sends one after the tool-call turn)
			// does not emit a second content_block_stop for the same block --
			// a duplicate stop downstream re-parses an already-reset buffer and
			// clobbers the tool input back to empty.
			if inTextBlock || currentToolID != "" {
				if err := sendEvent(ctx, events, types.LlmStreamEvent{Type: "content_block_stop", BlockIndex: contentIndex}); err != nil {
					return err
				}
				inTextBlock = false
				currentToolID = ""
			}
			stopReason := translateFinishReason(choice.FinishReason)
			if err := sendEvent(ctx, events, types.LlmStreamEvent{
				Type: "message_delta",
				Delta: &types.LlmStreamDelta{
					Type:       "message_delta",
					StopReason: &stopReason,
				},
				DeltaUsage: &types.LlmUsage{
					InputTokens:  totalInputToks,
					OutputTokens: totalOutputToks,
				},
			}); err != nil {
				return err
			}
		}
	}

	// Surface mid-stream read failures as real errors instead of silently
	// closing the turn with a synthetic message_stop on a partial response.
	if err := sseErr(); err != nil {
		if pe := ClassifyTransportError(err); pe != nil {
			return pe
		}
		return FromOpenAIError(fmt.Errorf("sse read: %w", err), 0, "")
	}

	// message_stop
	return sendEvent(ctx, events, types.LlmStreamEvent{Type: "message_stop"})
}

func (p *openaiProvider) buildRequestBody(opts types.LlmStreamOptions) map[string]any {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 16384
	}

	body := map[string]any{
		"model":                 opts.Model,
		"max_completion_tokens": maxTokens,
		"stream":                true,
		"messages":              formatOpenAIMessages(opts.System, opts.Messages),
	}

	if len(opts.Tools) > 0 {
		tools := make([]map[string]any, len(opts.Tools))
		for i, t := range opts.Tools {
			tools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.InputSchema,
				},
			}
		}
		body["tools"] = tools
	}

	if opts.Thinking != nil && opts.Thinking.Enabled {
		body["reasoning_effort"] = "high"
	}

	return body
}

// formatOpenAIMessages translates canonical (Anthropic) messages to OpenAI format.
func formatOpenAIMessages(system string, messages []types.LlmMessage) []map[string]any {
	result := []map[string]any{
		{"role": "system", "content": system},
	}

	for _, msg := range messages {
		blocks := contentBlocks(msg)

		// Simple string content
		if s, ok := msg.Content.(string); ok {
			result = append(result, map[string]any{"role": msg.Role, "content": s})
			continue
		}

		if blocks == nil {
			continue
		}

		// Separate tool_result blocks (they become separate "tool" messages)
		var toolResults []types.LlmContentBlock
		var otherBlocks []types.LlmContentBlock
		for _, b := range blocks {
			if b.Type == "tool_result" {
				toolResults = append(toolResults, b)
			} else {
				otherBlocks = append(otherBlocks, b)
			}
		}

		// Assistant messages with tool_use blocks
		if msg.Role == "assistant" {
			var toolUses []map[string]any
			var textParts []string
			for _, b := range otherBlocks {
				switch b.Type {
				case "tool_use":
					inputJSON, _ := json.Marshal(b.Input)
					toolUses = append(toolUses, map[string]any{
						"id":   b.ID,
						"type": "function",
						"function": map[string]any{
							"name":      b.Name,
							"arguments": string(inputJSON),
						},
					})
				case "text":
					textParts = append(textParts, b.Text)
				}
			}

			if len(toolUses) > 0 {
				m := map[string]any{
					"role":       "assistant",
					"tool_calls": toolUses,
				}
				if len(textParts) > 0 {
					combined := ""
					for _, t := range textParts {
						combined += t
					}
					m["content"] = combined
				}
				result = append(result, m)
			} else if len(textParts) > 0 {
				combined := ""
				for _, t := range textParts {
					combined += t
				}
				result = append(result, map[string]any{"role": "assistant", "content": combined})
			}
		}

		// User messages
		if msg.Role == "user" {
			var parts []map[string]any
			for _, b := range otherBlocks {
				switch b.Type {
				case "text":
					parts = append(parts, map[string]any{"type": "text", "text": b.Text})
				case "image":
					if b.Source != nil {
						url := fmt.Sprintf("data:%s;base64,%s", b.Source.MediaType, b.Source.Data)
						parts = append(parts, map[string]any{
							"type":      "image_url",
							"image_url": map[string]any{"url": url},
						})
					}
				case "compact_boundary":
					// Flatten the engine-internal compaction marker to a
					// plain text part so the model still sees the
					// rendered summary in context. See the matching case
					// in anthropic.go formatAnthropicBlock for rationale.
					text := b.Summary
					if text == "" {
						text = "[Previous conversation compacted]"
					}
					parts = append(parts, map[string]any{"type": "text", "text": text})
				}
			}
			if len(parts) > 0 {
				result = append(result, map[string]any{"role": "user", "content": parts})
			}
		}

		// Tool results as separate messages
		for _, tr := range toolResults {
			result = append(result, map[string]any{
				"role":         "tool",
				"tool_call_id": tr.ToolUseID,
				"content":      tr.Content,
			})
		}
	}

	return result
}

func translateFinishReason(reason string) string {
	switch reason {
	case "tool_calls":
		return "tool_use"
	case "stop":
		return "end_turn"
	default:
		return reason
	}
}

func sendEvent(ctx context.Context, ch chan<- types.LlmStreamEvent, ev types.LlmStreamEvent) error {
	select {
	case ch <- ev:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- OpenAI SSE JSON structures ---

type openaiChunk struct {
	ID      string             `json:"id"`
	Choices []openaiChoice     `json:"choices"`
	Usage   *openaiUsage       `json:"usage,omitempty"`
	Error   *openaiStreamError `json:"error,omitempty"`
}

// openaiStreamError captures an error object delivered inside the SSE body.
// OpenAI-compatible providers (notably OpenRouter) can return HTTP 200 and
// then signal an upstream failure mid-stream, either as a top-level
// {"error":{...}} chunk (often with empty choices) or as a choice with
// finish_reason:"error". The Code field is `any` because OpenRouter sends it
// as a number (HTTP-status-like) while OpenAI uses a string.
type openaiStreamError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

// httpStatus returns the numeric HTTP-status-like value embedded in the error
// code (OpenRouter convention), or 0 when the code is non-numeric/absent.
func (e *openaiStreamError) httpStatus() int {
	if e == nil {
		return 0
	}
	switch c := e.Code.(type) {
	case float64:
		return int(c)
	case int:
		return c
	case string:
		if n, err := strconv.Atoi(c); err == nil {
			return n
		}
	}
	return 0
}

// classifyOpenAIStreamError converts an in-stream provider error (delivered
// inside an HTTP 200 SSE body) into a *ProviderError. Mid-stream errors are
// overwhelmingly transient upstream hiccups, so the default is retryable;
// when the provider supplies an HTTP-status-like code, the existing
// FromOpenAIError classifier maps known-terminal statuses (auth, bad request)
// to non-retryable. The point is to never let an in-stream error fall through
// as a benign stop reason that exits the run 0 with empty output.
func classifyOpenAIStreamError(e *openaiStreamError) *ProviderError {
	msg := "provider returned an in-stream error"
	if e != nil && e.Message != "" {
		msg = e.Message
	}
	if status := e.httpStatus(); status != 0 {
		return FromOpenAIError(errors.New(msg), status, msg)
	}
	// No usable status code: treat as a transient upstream failure so
	// WithRetry gets a chance, rather than swallowing it as success.
	return &ProviderError{Code: ErrOverloaded, Message: msg, Retryable: true}
}

type openaiChoice struct {
	Delta        openaiDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type openaiDelta struct {
	Content   string           `json:"content"`
	ToolCalls []openaiToolCall `json:"tool_calls"`
}

type openaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function *openaiFunction `json:"function,omitempty"`
}

type openaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}
