package backend

import (
	"fmt"

	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// handleErrorStopReason surfaces a provider "error" stop reason as a real
// failure (ErrorEvent + non-zero exit) instead of a silent code-0 success.
// It returns true when it handled the reason, in which case the caller must
// return from the run loop.
//
// Background: an OpenAI-compatible provider can return HTTP 200 and then
// signal an upstream failure mid-stream as finish_reason:"error". The
// providers layer (providers/openai.go) now converts that into a retryable
// ProviderError so WithRetry can retry a transient failure and the
// streamErr != nil path surfaces an exhausted one. This is the defense in
// depth for any provider or path that still reaches the run loop's switch
// with a bare "error" stop reason, so it can never exit 0 with empty output
// (indistinguishable from a staffer who genuinely had nothing to say).
func (b *ApiBackend) handleErrorStopReason(run *activeRun, convID, stopReason string, turn int) bool {
	if stopReason != "error" {
		return false
	}
	utils.Error("ApiBackend", fmt.Sprintf("provider error stop reason: runID=%s turn=%d", run.requestID, turn))
	b.emit(run, types.NormalizedEvent{Data: &types.ErrorEvent{
		ErrorMessage: "The model provider returned an error before completing the response.",
		IsError:      true,
		ErrorCode:    "provider_stream_error",
	}})
	b.emitExit(run.requestID, intPtr(1), nil, convID)
	return true
}
