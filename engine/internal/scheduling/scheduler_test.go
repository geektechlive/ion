package scheduling

import (
	"sync"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/asyncreg"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
)

func TestParseHHMM(t *testing.T) {
	h, m, ok := parseHHMM("09:30")
	if !ok || h != 9 || m != 30 {
		t.Fatalf("got h=%d m=%d ok=%v", h, m, ok)
	}
	if _, _, ok := parseHHMM("9:30"); ok {
		t.Fatal("4-char string should reject")
	}
	if _, _, ok := parseHHMM("24:00"); ok {
		t.Fatal("hour >23 should reject")
	}
	if _, _, ok := parseHHMM("23:60"); ok {
		t.Fatal("minute >59 should reject")
	}
}

func TestWeekdayFromName(t *testing.T) {
	if weekdayFromName("monday") != time.Monday {
		t.Fatal("monday")
	}
	if weekdayFromName("FRIDAY") != time.Friday {
		t.Fatal("uppercase friday")
	}
}

func TestNextRunFor_Interval(t *testing.T) {
	job := extension.ScheduleJob{Kind: extension.ScheduleInterval, IntervalMs: 60_000}
	from := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	got := nextRunFor(job, from, time.UTC)
	want := from.Add(time.Minute)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNextRunFor_DailyForward(t *testing.T) {
	// 09:30 UTC daily. Asked from 08:00 same day → should fire today at 09:30.
	job := extension.ScheduleJob{Kind: extension.ScheduleDaily, Time: "09:30"}
	from := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	got := nextRunFor(job, from, time.UTC)
	want := time.Date(2026, 5, 25, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNextRunFor_DailyWraps(t *testing.T) {
	// 09:30 UTC daily. Asked from 10:00 same day → should fire tomorrow at 09:30.
	job := extension.ScheduleJob{Kind: extension.ScheduleDaily, Time: "09:30"}
	from := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	got := nextRunFor(job, from, time.UTC)
	want := time.Date(2026, 5, 26, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNextRunFor_WeeklyForward(t *testing.T) {
	// 09:30 Monday. Asked from a Wednesday → next Monday.
	job := extension.ScheduleJob{Kind: extension.ScheduleWeekly, Time: "09:30", DayOfWeek: "monday"}
	// 2026-05-25 is a Monday.
	from := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC) // Wed
	got := nextRunFor(job, from, time.UTC)
	if got.Weekday() != time.Monday {
		t.Fatalf("got weekday %v want Monday", got.Weekday())
	}
	if got.Hour() != 9 || got.Minute() != 30 {
		t.Fatalf("got time %v want 09:30", got)
	}
	// The next Monday after 2026-05-27 is 2026-06-01.
	want := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNextRunFor_WeeklyTodayAlreadyPassed(t *testing.T) {
	// Monday 09:30, asked from Monday 11:00 → next Monday.
	job := extension.ScheduleJob{Kind: extension.ScheduleWeekly, Time: "09:30", DayOfWeek: "monday"}
	from := time.Date(2026, 5, 25, 11, 0, 0, 0, time.UTC) // Mon 11:00
	got := nextRunFor(job, from, time.UTC)
	want := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC) // next Monday
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLastScheduledSlotBefore_Daily(t *testing.T) {
	job := extension.ScheduleJob{Kind: extension.ScheduleDaily, Time: "09:30"}
	// at 11:00 today, last slot is today at 09:30
	t11 := time.Date(2026, 5, 25, 11, 0, 0, 0, time.UTC)
	got := lastScheduledSlotBefore(job, t11, time.UTC)
	want := time.Date(2026, 5, 25, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// at 08:00 today, last slot is yesterday at 09:30
	t8 := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	got = lastScheduledSlotBefore(job, t8, time.UTC)
	want = time.Date(2026, 5, 24, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSchedulerStartStop(t *testing.T) {
	s := New(Config{})
	s.Start()
	// Idempotent
	s.Start()
	// Tick once manually to be sure it does not panic with no hosts.
	s.tickOnce()
	s.Stop()
	// Idempotent stop
	s.Stop()
}

// TestPersistence_FullCoverage_SeeOtherFile is a marker pointing
// readers at persistence_test.go for the on-disk round-trip,
// sanitisation, and bad-file behaviors. Kept here so a search for
// "persist" in this file finds the cross-reference.
func TestPersistence_FullCoverage_SeeOtherFile(t *testing.T) {
	t.Skip("see persistence_test.go for full coverage")
}

func TestSafeName(t *testing.T) {
	if safeName("hello/world") != "hello_world" {
		t.Fatal("slash should sanitize")
	}
	if safeName("") != "unnamed" {
		t.Fatal("empty should become unnamed")
	}
	if safeName("abc-123_def.foo") != "abc-123_def.foo" {
		t.Fatal("alnum + - _ . should pass")
	}
}

// ─── Concurrency coordination tests ───

// testHostWithSchedule creates a Host with a name and schedule jobs
// registered in its asyncreg registry. No subprocess is loaded.
func testHostWithSchedule(t *testing.T, name string, jobs ...extension.ScheduleJob) *extension.Host {
	t.Helper()
	h := extension.NewHost()
	h.SetNameForTest(name)
	for _, job := range jobs {
		err := h.AsyncRegistry().Register(asyncreg.KindSchedule, job, asyncreg.OriginInit, nil)
		if err != nil {
			t.Fatalf("register job %q: %v", job.JobID, err)
		}
	}
	return h
}

// fireTracker records which host names enter the fire path via the
// session resolver.
type fireTracker struct {
	mu    sync.Mutex
	fired []string
}

func (ft *fireTracker) resolver() SessionResolver {
	return func(host *extension.Host) (*extension.Context, error) {
		ft.mu.Lock()
		ft.fired = append(ft.fired, host.Name())
		ft.mu.Unlock()
		return &extension.Context{SessionKey: "test-" + host.Name()}, nil
	}
}

func (ft *fireTracker) count() int {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return len(ft.fired)
}

func (ft *fireTracker) countByName(name string) int {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	n := 0
	for _, f := range ft.fired {
		if f == name {
			n++
		}
	}
	return n
}

// setupConcurrencyTest creates a scheduler with a controllable clock,
// adds the given hosts, bootstraps nextRun on the first tick, advances
// time past the interval, and returns the scheduler + tracker ready
// for the second tick.
func setupConcurrencyTest(t *testing.T, hosts ...*extension.Host) (*Scheduler, *fireTracker) {
	t.Helper()
	tracker := &fireTracker{}
	s := New(Config{})
	s.SetSessionResolver(tracker.resolver())
	s.SetEmit(func(ev types.EngineEvent) {})

	baseTime := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	s.nowFn = func() time.Time { return baseTime }

	for _, h := range hosts {
		s.AddHost(h)
	}

	// First tick: bootstraps nextRun for all jobs
	s.tickOnce()

	// Advance time past the interval so jobs are due
	s.nowFn = func() time.Time { return baseTime.Add(2 * time.Second) }

	return s, tracker
}

func TestScheduler_Concurrency_SingleDefault(t *testing.T) {
	job := extension.ScheduleJob{
		JobID:      "morning-brief",
		Kind:       extension.ScheduleInterval,
		IntervalMs: 1000,
		// Concurrency defaults to "" which means single
	}

	h1 := testHostWithSchedule(t, "ion-dev", job)
	h2 := testHostWithSchedule(t, "ion-dev", job)
	h3 := testHostWithSchedule(t, "ion-dev", job)

	s, tracker := setupConcurrencyTest(t, h1, h2, h3)

	// Second tick: fires with concurrency coordination
	s.tickOnce()

	// Wait for fire goroutines (they fail quickly — no subprocess)
	time.Sleep(200 * time.Millisecond)

	if got := tracker.count(); got != 1 {
		t.Fatalf("single mode: expected 1 fire, got %d: %v", got, tracker.fired)
	}
}

func TestScheduler_Concurrency_All(t *testing.T) {
	job := extension.ScheduleJob{
		JobID:       "morning-brief",
		Kind:        extension.ScheduleInterval,
		IntervalMs:  1000,
		Concurrency: "all",
	}

	h1 := testHostWithSchedule(t, "ion-dev", job)
	h2 := testHostWithSchedule(t, "ion-dev", job)
	h3 := testHostWithSchedule(t, "ion-dev", job)

	s, tracker := setupConcurrencyTest(t, h1, h2, h3)

	s.tickOnce()
	time.Sleep(200 * time.Millisecond)

	if got := tracker.count(); got != 3 {
		t.Fatalf("all mode: expected 3 fires, got %d: %v", got, tracker.fired)
	}
}

func TestScheduler_Concurrency_CrossExtension(t *testing.T) {
	job := extension.ScheduleJob{
		JobID:      "morning-brief",
		Kind:       extension.ScheduleInterval,
		IntervalMs: 1000,
		// single mode (default)
	}

	// Two ion-dev hosts + two chief-of-staff hosts
	id1 := testHostWithSchedule(t, "ion-dev", job)
	id2 := testHostWithSchedule(t, "ion-dev", job)
	cs1 := testHostWithSchedule(t, "chief-of-staff", job)
	cs2 := testHostWithSchedule(t, "chief-of-staff", job)

	s, tracker := setupConcurrencyTest(t, id1, id2, cs1, cs2)

	s.tickOnce()
	time.Sleep(200 * time.Millisecond)

	idCount := tracker.countByName("ion-dev")
	csCount := tracker.countByName("chief-of-staff")

	if idCount != 1 {
		t.Errorf("ion-dev: expected 1 fire, got %d", idCount)
	}
	if csCount != 1 {
		t.Errorf("chief-of-staff: expected 1 fire, got %d", csCount)
	}
	if got := tracker.count(); got != 2 {
		t.Errorf("total: expected 2 fires, got %d: %v", got, tracker.fired)
	}
}

func TestScheduler_Concurrency_DeadHostSkipped(t *testing.T) {
	job := extension.ScheduleJob{
		JobID:      "morning-brief",
		Kind:       extension.ScheduleInterval,
		IntervalMs: 1000,
	}

	h1 := testHostWithSchedule(t, "ion-dev", job)
	h2 := testHostWithSchedule(t, "ion-dev", job)
	h3 := testHostWithSchedule(t, "ion-dev", job)

	// Kill the first host
	h1.MarkDeadForTest()

	s, tracker := setupConcurrencyTest(t, h1, h2, h3)

	s.tickOnce()
	time.Sleep(200 * time.Millisecond)

	if got := tracker.count(); got != 1 {
		t.Fatalf("dead-host skip: expected 1 fire, got %d: %v", got, tracker.fired)
	}
}

func TestScheduleJob_Validate_Concurrency(t *testing.T) {
	base := extension.ScheduleJob{
		JobID:      "test",
		Kind:       extension.ScheduleInterval,
		IntervalMs: 1000,
	}

	for _, c := range []string{"", "single", "all"} {
		j := base
		j.Concurrency = c
		if err := j.Validate(); err != nil {
			t.Errorf("concurrency=%q should be valid, got: %v", c, err)
		}
	}

	j := base
	j.Concurrency = "invalid"
	if err := j.Validate(); err == nil {
		t.Error("concurrency='invalid' should fail validation")
	}
}
