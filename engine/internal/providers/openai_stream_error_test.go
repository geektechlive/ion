package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dsswift/ion/engine/internal/types"
)

func TestOpenAIStreamErrorHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		code any
		want int
	}{
		{"float64 (json number)", float64(429), 429},
		{"int", 500, 500},
		{"numeric string", "502", 502},
		{"non-numeric string", "error", 0},
		{"nil code", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := &openaiStreamError{Code: c.code}
			if got := e.httpStatus(); got != c.want {
				t.Errorf("code %v: want %d, got %d", c.code, c.want, got)
			}
		})
	}
	var nilErr *openaiStreamError
	if nilErr.httpStatus() != 0 {
		t.Error("nil receiver should return 0")
	}
}

func TestClassifyOpenAIStreamError(t *testing.T) {
	// nil error object -> generic transient (retryable) so WithRetry gets a chance.
	if pe := classifyOpenAIStreamError(nil); !pe.Retryable {
		t.Errorf("nil error should be retryable, got %+v", pe)
	}
	// No code -> generic transient (retryable).
	if pe := classifyOpenAIStreamError(&openaiStreamError{Message: "boom"}); !pe.Retryable {
		t.Errorf("missing code should be retryable, got %+v", pe)
	}
	// 429 -> rate_limit, retryable.
	if pe := classifyOpenAIStreamError(&openaiStreamError{Message: "rl", Code: float64(429)}); !pe.Retryable || pe.Code != ErrRateLimit {
		t.Errorf("429 should be retryable rate_limit, got %+v", pe)
	}
	// 5xx -> overloaded, retryable.
	if pe := classifyOpenAIStreamError(&openaiStreamError{Message: "up", Code: float64(502)}); !pe.Retryable {
		t.Errorf("502 should be retryable, got %+v", pe)
	}
	// 401 -> auth, NOT retryable (terminal).
	if pe := classifyOpenAIStreamError(&openaiStreamError{Message: "bad key", Code: float64(401)}); pe.Retryable || pe.Code != ErrAuth {
		t.Errorf("401 should be non-retryable auth, got %+v", pe)
	}
	// Message is preserved.
	if pe := classifyOpenAIStreamError(&openaiStreamError{Message: "upstream timeout", Code: float64(502)}); pe.Message != "upstream timeout" {
		t.Errorf("message not preserved, got %q", pe.Message)
	}
}

// streamErrServer serves an HTTP 200 SSE body verbatim, simulating an
// OpenAI-compatible provider that signals an upstream failure mid-stream.
func streamErrServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func drainAndGetErr(t *testing.T, p LlmProvider) error {
	t.Helper()
	evCh, errCh := p.Stream(context.Background(), types.LlmStreamOptions{Model: "google/gemini-2.5-flash-lite"})
	for range evCh { // discard buffered events
	}
	return <-errCh
}

// An HTTP 200 stream that delivers a top-level {"error":{...}} chunk (the
// common OpenRouter delivery, often with empty choices) must surface as a
// retryable *ProviderError instead of completing the stream as success.
func TestOpenAIInStreamErrorObjectSurfacesRetryable(t *testing.T) {
	body := "data: {\"id\":\"x\",\"choices\":[],\"error\":{\"message\":\"upstream timeout\",\"code\":502}}\n\ndata: [DONE]\n\n"
	srv := streamErrServer(t, body)
	p := NewOpenAIProvider(&ProviderOptions{ID: "openrouter", APIKey: "k", BaseURL: srv.URL})

	err := drainAndGetErr(t, p)
	if err == nil {
		t.Fatal("expected an error, got nil (silent success is the bug)")
	}
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if !pe.Retryable {
		t.Errorf("502 in-stream error should be retryable, got %+v", pe)
	}
}

// An HTTP 200 stream whose choice carries finish_reason:"error" (no separate
// error object) must also surface as a retryable *ProviderError.
func TestOpenAIFinishReasonErrorSurfacesRetryable(t *testing.T) {
	body := "data: {\"id\":\"x\",\"choices\":[{\"delta\":{},\"finish_reason\":\"error\"}]}\n\ndata: [DONE]\n\n"
	srv := streamErrServer(t, body)
	p := NewOpenAIProvider(&ProviderOptions{ID: "openrouter", APIKey: "k", BaseURL: srv.URL})

	err := drainAndGetErr(t, p)
	if err == nil {
		t.Fatal("expected an error, got nil (silent success is the bug)")
	}
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if !pe.Retryable {
		t.Errorf("finish_reason=error should be retryable by default, got %+v", pe)
	}
}

// A terminal in-stream error (auth) must surface as a NON-retryable error so
// WithRetry forwards it immediately instead of looping.
func TestOpenAIInStreamAuthErrorNotRetryable(t *testing.T) {
	body := "data: {\"id\":\"x\",\"choices\":[],\"error\":{\"message\":\"invalid api key\",\"code\":401}}\n\ndata: [DONE]\n\n"
	srv := streamErrServer(t, body)
	p := NewOpenAIProvider(&ProviderOptions{ID: "openrouter", APIKey: "k", BaseURL: srv.URL})

	err := drainAndGetErr(t, p)
	pe, ok := err.(*ProviderError)
	if !ok {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if pe.Retryable {
		t.Errorf("401 in-stream error should NOT be retryable, got %+v", pe)
	}
}
