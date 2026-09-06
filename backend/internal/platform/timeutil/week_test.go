package timeutil

import (
	"testing"
	"time"
)

func TestWeekBoundsMonday(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Dublin")
	// 2026-09-02 is a Wednesday.
	wed := time.Date(2026, 9, 2, 14, 30, 0, 0, loc)
	start, end := WeekBounds(wed, loc, time.Monday)
	wantStart := time.Date(2026, 8, 31, 0, 0, 0, 0, loc) // Monday
	if !start.Equal(wantStart) {
		t.Fatalf("start = %v, want %v", start, wantStart)
	}
	wantEnd := time.Date(2026, 9, 7, 0, 0, 0, 0, loc)
	if !end.Equal(wantEnd) {
		t.Fatalf("end = %v, want %v", end, wantEnd)
	}
}

func TestWeekBoundsCrossYear(t *testing.T) {
	loc := time.UTC
	// 2027-01-01 is a Friday; week start Monday => start 2026-12-28.
	fri := time.Date(2027, 1, 1, 10, 0, 0, 0, loc)
	start, end := WeekBounds(fri, loc, time.Monday)
	if start.Year() != 2026 || start.Month() != 12 || start.Day() != 28 {
		t.Fatalf("cross-year start = %v", start)
	}
	if end.Day() != 4 || end.Month() != 1 {
		t.Fatalf("cross-year end = %v", end)
	}
}

func TestWeekStartIsMondayEvenOnMonday(t *testing.T) {
	loc := time.UTC
	mon := time.Date(2026, 8, 31, 0, 1, 0, 0, loc)
	start, _ := WeekBounds(mon, loc, time.Monday)
	if start.Day() != 31 {
		t.Fatalf("start of Monday week = %v, want the same day", start)
	}
}

func TestDSTChangeDoesNotShiftWindow(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Dublin")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-03-29 is the EU spring-forward (IST starts at 01:00 UTC → 02:00).
	// A time on that Sunday belongs to the Mon-Sun week ending 03-30.
	sun := time.Date(2026, 3, 29, 12, 0, 0, 0, loc)
	start, end := WeekBounds(sun, loc, time.Monday)
	// Week start: Monday 2026-03-23.
	wantStart := time.Date(2026, 3, 23, 0, 0, 0, 0, loc)
	if !start.Equal(wantStart) {
		t.Fatalf("DST week start = %v want %v", start, wantStart)
	}
	// End must be the next Monday 00:00 local — exactly 7 local days later,
	// which is 168h but the UTC offset changed; local wall clock still lands
	// on 03-30 00:00 IST.
	wantEnd := time.Date(2026, 3, 30, 0, 0, 0, 0, loc)
	if !end.Equal(wantEnd) {
		t.Fatalf("DST week end = %v want %v", end, wantEnd)
	}
}

func TestInHalfOpen(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	end := time.Date(2026, 1, 8, 0, 0, 0, 0, loc)
	if !In(start, start, end) {
		t.Fatal("start inclusive")
	}
	if In(end, start, end) {
		t.Fatal("end exclusive")
	}
	mid := time.Date(2026, 1, 3, 0, 0, 0, 0, loc)
	if !In(mid, start, end) {
		t.Fatal("mid should be in")
	}
}

func TestDaysSince(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, loc)
	submitted := time.Date(2026, 8, 24, 12, 0, 0, 0, loc)
	if d := DaysSince(submitted, now, loc); d != 14 {
		t.Fatalf("DaysSince = %d, want 14", d)
	}
}
