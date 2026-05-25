//go:build integration

// End-to-end tests for the scheduler tick loop + extension subprocess
// pipeline. These tests use a fast 1-2s interval against the real
// canary, verify the handler ran (via a counter tool the canary
// exposes), and check the observability events fire in order.

package integration

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/asyncreg"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/scheduling"
	"github.com/dsswift/ion/engine/internal/types"
)

// schedulerE2E loads the async-canary, commits its declarations,
// starts a scheduling.Scheduler wired to the host, and returns a
// teardown closure. The canary's static "async-canary-tick" interval
// job fires every second; tests poll for the resulting events.
func setupSchedulerE2E(t *testing.T) (*extension.Host, *scheduling.Scheduler, *eventBus) {
	t.Helper()
	requireEsbuild(t)
	t.Setenv("ASYNC_CANARY_TOKEN", "irrelevant-here")

	host := extension.NewHost()
	t.Cleanup(func() { host.Dispose() })

	entry := asyncCanaryEntry(t)
	if err := host.Load(entry, &extension.ExtensionConfig{
		ExtensionDir:     filepath.Dir(entry),
		WorkingDirectory: t.TempDir(),
	}); err != nil {
		t.Fatalf("load async-canary: %v", err)
	}
	if errs := host.CommitPendingAsyncDecls(); len(errs) != 0 {
		t.Fatalf("commit errors: %v", errs)
	}

	bus := &eventBus{}
	persistDir := t.TempDir()
	sch := scheduling.New(scheduling.Config{
		FireTimeout: 5 * time.Second,
		PersistDir:  persistDir,
	})
	sch.SetEmit(bus.emit)
	sch.SetSessionResolver(func(h *extension.Host) (*extension.Context, error) {
		return &extension.Context{SessionKey: "sch-e2e"}, nil
	})
	sch.AddHost(host)
	sch.Start()
	t.Cleanup(sch.Stop)
	return host, sch, bus
}

// TestSchedulerE2E_IntervalJobFires verifies the tick loop actually
// calls the registered handler. The canary's static interval is 1s;
// we wait up to 4s for the first fire and confirm at least one
// engine_schedule_fired event landed for the right job id.
func TestSchedulerE2E_IntervalJobFires(t *testing.T) {
	_, _, bus := setupSchedulerE2E(t)

	// Wait up to 4s for the first fire.
	fired := waitForEvent(t, bus, "engine_schedule_fired", 4*time.Second)
	if fired.AsyncKind != "schedule" {
		t.Errorf("AsyncKind = %q, want schedule", fired.AsyncKind)
	}
	if fired.AsyncID != "async-canary-tick" {
		t.Errorf("AsyncID = %q, want async-canary-tick", fired.AsyncID)
	}
	if fired.AsyncDurationMs < 0 {
		t.Errorf("durationMs = %d, want >= 0", fired.AsyncDurationMs)
	}

	// A second fire should land within another ~2s window.
	expire := time.Now().Add(3 * time.Second)
	for time.Now().Before(expire) {
		if len(bus.ofType("engine_schedule_fired")) >= 2 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("expected >=2 fires within 7s total; saw %d", len(bus.ofType("engine_schedule_fired")))
}

// TestSchedulerE2E_DisabledPredicateSkipsFire registers a job with
// an `enabled: () => false` predicate and verifies the scheduler
// emits engine_schedule_skipped with reason='disabled' instead of
// firing the handler.
func TestSchedulerE2E_DisabledPredicateSkipsFire(t *testing.T) {
	host, _, bus := setupSchedulerE2E(t)

	// Use the canary's register-disabled-interval tool.
	tool := findTool(t, host, "async_canary_register_disabled_interval")
	if _, err := tool.Execute(map[string]any{}, &extension.Context{SessionKey: "sch-e2e"}); err != nil {
		t.Fatalf("register disabled interval: %v", err)
	}

	// Wait for at least one skip event referencing the disabled job.
	expire := time.Now().Add(5 * time.Second)
	var found bool
	for time.Now().Before(expire) {
		for _, ev := range bus.ofType("engine_schedule_skipped") {
			if ev.AsyncID == "async-canary-disabled" && ev.AsyncReason == "disabled" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected engine_schedule_skipped for async-canary-disabled with reason=disabled; saw %v",
			summariseEvents(bus.snapshot()))
	}

	// And critically: no engine_schedule_fired for that job ever.
	for _, ev := range bus.ofType("engine_schedule_fired") {
		if ev.AsyncID == "async-canary-disabled" {
			t.Fatalf("disabled job fired anyway: %+v", ev)
		}
	}
}

// TestSchedulerE2E_DynamicScheduleRegistrationFires verifies a job
// registered at runtime (via the canary tool) is picked up by the
// running tick loop on the next iteration and fires.
func TestSchedulerE2E_DynamicScheduleRegistrationFires(t *testing.T) {
	host, _, bus := setupSchedulerE2E(t)

	// Register a 1s dynamic interval.
	tool := findTool(t, host, "async_canary_register_dynamic_schedule")
	if _, err := tool.Execute(map[string]any{}, &extension.Context{SessionKey: "sch-e2e"}); err != nil {
		t.Fatalf("dyn register: %v", err)
	}

	// Wait up to 5s for the dynamic job to fire.
	expire := time.Now().Add(5 * time.Second)
	var found bool
	for time.Now().Before(expire) {
		for _, ev := range bus.ofType("engine_schedule_fired") {
			if ev.AsyncID == "async-canary-dynamic" {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatalf("dynamic schedule never fired; saw events %v", summariseEvents(bus.snapshot()))
	}
}

// TestSchedulerE2E_DeregisterStopsFiring verifies that calling
// Host.DeregisterScheduleDecl removes the job from the scheduler's
// view: after deregister, no further fires for that id arrive.
func TestSchedulerE2E_DeregisterStopsFiring(t *testing.T) {
	host, _, bus := setupSchedulerE2E(t)

	// Wait for the static job to fire at least once first.
	waitForEvent(t, bus, "engine_schedule_fired", 4*time.Second)
	preCount := countSchedule(bus, "async-canary-tick", "engine_schedule_fired")

	// Deregister.
	if !host.DeregisterScheduleDecl("async-canary-tick") {
		t.Fatal("DeregisterScheduleDecl returned false")
	}

	// Allow the tick loop one iteration to absorb the change, then
	// confirm no further fires arrive over a 2.5s observation
	// window (long enough for at least 2 ticks past the dereg).
	time.Sleep(2500 * time.Millisecond)
	postCount := countSchedule(bus, "async-canary-tick", "engine_schedule_fired")
	if postCount > preCount {
		t.Fatalf("post-deregister fires: pre=%d post=%d (expected no growth)", preCount, postCount)
	}
}

func countSchedule(bus *eventBus, id, evType string) int {
	n := 0
	for _, ev := range bus.ofType(evType) {
		if ev.AsyncID == id {
			n++
		}
	}
	return n
}

// TestSchedulerE2E_HandlerErrorEmitsFailedEvent verifies that a
// handler that throws bubbles up as engine_schedule_failed (the
// canary exposes a tool to register a throwing job).
func TestSchedulerE2E_HandlerErrorEmitsFailedEvent(t *testing.T) {
	host, _, bus := setupSchedulerE2E(t)

	tool := findTool(t, host, "async_canary_register_failing_interval")
	if _, err := tool.Execute(map[string]any{}, &extension.Context{SessionKey: "sch-e2e"}); err != nil {
		t.Fatalf("register failing interval: %v", err)
	}

	// Wait up to 5s for the failed event.
	expire := time.Now().Add(5 * time.Second)
	var failed *types.EngineEvent
	for time.Now().Before(expire) {
		for _, ev := range bus.ofType("engine_schedule_failed") {
			ev := ev
			if ev.AsyncID == "async-canary-failing" {
				failed = &ev
				break
			}
		}
		if failed != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if failed == nil {
		t.Fatalf("no engine_schedule_failed for async-canary-failing; events: %v",
			summariseEvents(bus.snapshot()))
	}
	if failed.AsyncReason == "" {
		t.Error("AsyncReason on failed event should not be empty")
	}
}

// Note: persistence and catch-up coverage live in
// engine/internal/scheduling/persistence_test.go and
// engine/internal/scheduling/catchup_test.go. Those exercise the
// on-disk round-trip and the catch-up decision tree with full
// determinism via SetNowFn; spinning up a real subprocess buys no
// extra coverage there.

// ensure imports used.
var _ = sync.Mutex{}
var _ = asyncreg.KindSchedule
