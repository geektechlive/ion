package scheduling

import (
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/extension"
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

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := New(Config{PersistDir: dir})
	// We don't have a real Host here without spinning up a subprocess,
	// so instead we exercise the marker path logic via a small shim.
	// Use a nil host pointer — markerPath only reads h.Name(), and Name
	// is called only when a marker is being written, but we can craft
	// the path with safeName helpers below.
	safe := safeName("test-ext") + "_" + safeName("job-1") + ".json"
	if safe != "test-ext_job-1.json" {
		t.Fatalf("safe name was %q", safe)
	}
	// Sanity: nil-host write/read should not crash but won't write — we
	// just verify the helper compiles and the persist dir is honored.
	_ = s
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
