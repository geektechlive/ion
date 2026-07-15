package providers

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dsswift/ion/engine/internal/utils"
)

// sse_idle.go — the provider stream-idle deadline and heartbeat.
//
// Why this exists. The shared HTTP transport (internal/network) caps the wait
// for the FIRST response byte via ResponseHeaderTimeout (effective because the
// transport is pinned to HTTP/1.1 — see internal/network/network.go). That does
// not protect against a stream that returns headers and then stops emitting SSE
// bytes while the upstream keeps the connection alive at the protocol level: the
// provider read loop (`for sse := range sseCh`) blocks forever with no output
// and no error. That was the originating failure in the
// 1782088921498-960b064fe896 incident — ~7 minutes of total silence before an
// external watchdog intervened.
//
// streamWithIdle wraps the raw SSE channel from ParseSSEStream with a
// per-event idle timer. Every received event resets the timer; if the gap
// between events exceeds the configured idle deadline, the wrapper stops and
// reports a RETRYABLE stream error (ErrStreamTruncated, tagged stream_idle) so
// the existing WithRetry machinery re-streams transparently. It also emits a
// periodic heartbeat log and invokes an optional progress callback so a
// healthy-but-slow stream is observable in engine.log and keeps the run's
// progress clock fresh (so the run-progress watchdog does not mistake a slow
// stream for a stall).
//
// The mechanism is engine-owned and generic; the threshold is the opinion,
// configured via TimeoutsConfig.StreamIdle() and installed once through
// SetStreamIdleTimeout (mirroring the backend's runProgressWatchdogTickNanos
// pattern — a package-level atomic default with a setter, so the hot streaming
// path reads a plain int64 rather than threading config through every
// provider's Stream signature, which would be a contract change).

// streamIdleNanos is the configured per-event idle deadline in nanoseconds.
// 0 means "use the compiled default"; a negative value disables the deadline.
// Stored atomically because SetStreamIdleTimeout (called from session/backend
// setup) races the long-lived provider stream goroutines that read it.
var streamIdleNanos atomic.Int64

// streamHeartbeatNanos is the heartbeat log cadence. Fixed; not user-tunable —
// it is pure observability and has no behavioral effect.
const streamHeartbeatInterval = 15 * time.Second

// defaultStreamIdle mirrors TimeoutsConfig.StreamIdle()'s compiled default
// (90s). Defined here so the providers package has a self-contained default
// when no setter has run (e.g. unit tests that call streamWithIdle directly).
const defaultStreamIdle = 90 * time.Second

// SetStreamIdleTimeout installs the per-event SSE idle deadline used by all
// provider streams. Call once at engine/session startup from
// TimeoutsConfig.StreamIdle(). A non-positive duration disables the deadline
// (the wrapper relies solely on the transport + the run-progress watchdog).
func SetStreamIdleTimeout(d time.Duration) {
	streamIdleNanos.Store(int64(d))
	utils.Log("providers", fmt.Sprintf("SetStreamIdleTimeout: streamIdle=%s", d))
}

// resolvedStreamIdle reads the configured deadline, falling back to the
// compiled default when unset (0). A negative stored value disables it.
func resolvedStreamIdle() (time.Duration, bool) {
	v := streamIdleNanos.Load()
	if v < 0 {
		return 0, false
	}
	if v == 0 {
		return defaultStreamIdle, true
	}
	return time.Duration(v), true
}

// streamProgress is an optional callback invoked on every received SSE event
// and on every heartbeat tick, so the caller (the run loop) can keep its
// progress clock fresh while a slow-but-alive stream is in flight. Nil-safe.
type streamProgress func()

// streamWithIdle consumes the raw SSE channel and re-emits its events on the
// returned channel, enforcing the per-event idle deadline and emitting
// heartbeat logs. The returned errFn (call after draining) reports:
//   - the idle-deadline error (retryable stream_truncated, tagged stream_idle)
//     when the gap between events exceeded the deadline,
//   - otherwise whatever the underlying srcErr() reports (clean EOF → nil,
//     transport error → that error).
//
// tag/model/requestID are logging context only. onProgress may be nil.
func streamWithIdle(
	src <-chan SSEEvent,
	srcErr func() error,
	tag, model, requestID string,
	onProgress streamProgress,
) (<-chan SSEEvent, func() error) {
	out := make(chan SSEEvent, 16)

	idle, idleEnabled := resolvedStreamIdle()

	var (
		idleErr  *ProviderError
		doneCh   = make(chan struct{})
	)

	utils.Debug(tag, fmt.Sprintf(
		"stream start: model=%s requestID=%s idleDeadline=%s idleEnabled=%t",
		model, requestID, idle, idleEnabled,
	))

	go func() {
		defer close(out)
		defer close(doneCh)

		// Idle timer. When disabled, idleC stays nil so the select arm never
		// fires (a nil channel blocks forever).
		var idleTimer *time.Timer
		var idleC <-chan time.Time
		if idleEnabled {
			idleTimer = time.NewTimer(idle)
			idleC = idleTimer.C
			defer idleTimer.Stop()
		}

		heartbeat := time.NewTicker(streamHeartbeatInterval)
		defer heartbeat.Stop()

		start := time.Now()
		lastEventAt := start
		eventCount := 0

		for {
			select {
			case sse, ok := <-src:
				if !ok {
					// Source drained (EOF or read error). errFn defers to
					// srcErr below. Log the clean end for observability.
					utils.Debug(tag, fmt.Sprintf(
						"stream end: model=%s requestID=%s events=%d elapsed=%s",
						model, requestID, eventCount, time.Since(start).Round(time.Millisecond),
					))
					return
				}
				eventCount++
				lastEventAt = time.Now()
				if onProgress != nil {
					onProgress()
				}
				// Reset the idle timer for the next event.
				if idleEnabled {
					if !idleTimer.Stop() {
						// Drain a possibly-fired timer so Reset is clean.
						select {
						case <-idleTimer.C:
						default:
						}
					}
					idleTimer.Reset(idle)
				}
				// Forward downstream. The consumer reads with its own ctx
				// select; here we just block on out, which the consumer
				// drains promptly.
				out <- sse

			case <-idleC:
				// No event for longer than the idle deadline. The upstream is
				// holding the stream open but sending nothing. Surface a
				// retryable error so WithRetry re-streams.
				gap := time.Since(lastEventAt).Round(time.Millisecond)
				utils.Error(tag, fmt.Sprintf(
					"stream idle deadline exceeded: model=%s requestID=%s noEventFor=%s events=%d — cancelling read for retry",
					model, requestID, gap, eventCount,
				))
				idleErr = &ProviderError{
					Code: ErrStreamTruncated,
					Message: fmt.Sprintf(
						"stream_idle: no SSE event for %s (idle deadline %s) — upstream stalled mid-stream",
						gap, idle,
					),
					Retryable: true,
				}
				return

			case <-heartbeat.C:
				// Pure observability + progress bump for a slow-but-alive
				// stream. Logged at DEBUG so it never spams INFO.
				if onProgress != nil {
					onProgress()
				}
				utils.Debug(tag, fmt.Sprintf(
					"stream alive: model=%s requestID=%s events=%d sinceLastEvent=%s totalElapsed=%s",
					model, requestID, eventCount,
					time.Since(lastEventAt).Round(time.Second),
					time.Since(start).Round(time.Second),
				))
			}
		}
	}()

	errFn := func() error {
		<-doneCh
		if idleErr != nil {
			return idleErr
		}
		// No idle timeout — defer to the underlying reader's error (clean EOF
		// → nil, transport error → that error). srcErr blocks until the source
		// goroutine finishes, which has already happened once src closed.
		return srcErr()
	}

	return out, errFn
}
