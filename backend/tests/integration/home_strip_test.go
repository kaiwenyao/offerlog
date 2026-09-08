// Week-strip regression (review round 4, P1): with week_start=0 (Sunday), the
// strip chips must be placed relative to the Sunday start — day index 0 = the
// week's Sunday — matching the week window [Sunday, next Sunday). A Wednesday
// event sits at index 3 and a Sunday event at index 0. Previously the backend
// always emitted Mon=0..Sun=6 indexes while the frontend anchored columns at
// the (Sunday) week.start, shifting every chip by one day. repo.Get takes an
// injectable `now`, so the test is fully deterministic.
package integration

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/home"
)

func TestHomeStripFollowsWeekStartSunday(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)

	// Fixed "now": Wednesday 2026-08-26 12:00 UTC = 13:00 Europe/Dublin
	// (summer, UTC+1). Week with Sunday start = 2026-08-23 .. 2026-08-30.
	// (Week chosen in the past so seeding "submitted" rows never trips the
	// future-submission guard, whatever the real wall clock is.)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	// Sunday submission → the week-start day itself.
	sunday := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC) // 11:00 Dublin Sunday
	// Wednesday submission → ws + 3d.
	wednesday := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC) // 10:00 Dublin Wednesday

	mustCreateWithSubmit(t, svc, owner, "SunCo", "R", sunday)
	mustCreateWithSubmit(t, svc, owner, "WedCo", "R", wednesday)

	repo := home.New(db)
	s, err := repo.Get(ctx, owner, "Europe/Dublin", time.Sunday, now, 5)
	if err != nil {
		t.Fatal(err)
	}
	if s.WeekStart != 0 {
		t.Fatalf("WeekStart=%d, want 0", s.WeekStart)
	}
	if s.SubmittedWeek != 2 {
		t.Fatalf("SubmittedWeek=%d, want 2 (window must be Sunday-based)", s.SubmittedWeek)
	}

	found := map[string]int{} // who -> day index
	for _, it := range s.WeekItems {
		if it.Kind == "投递" {
			found[it.Who] = it.Day
		}
	}
	if d, ok := found["SunCo"]; !ok || d != 0 {
		t.Fatalf("Sunday submission chip day=%d (found=%v), want 0 (the week-start day)", d, ok)
	}
	if d, ok := found["WedCo"]; !ok || d != 3 {
		t.Fatalf("Wednesday submission chip day=%d (found=%v), want 3 (3 days after the Sunday start)", d, ok)
	}
}

// Sanity for the Monday default: day indexes stay Mon=0..Sun=6 under the same
// fixed clock (regression net for the previous behavior).
func TestHomeStripMondayDefaultDayIndexes(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)

	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) // Wednesday 2026-08-26
	// The Monday of that week is 2026-08-24 (week start under the default).
	monday := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	// A Friday event in the same week.
	friday := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)

	mustCreateWithSubmit(t, svc, owner, "MonCo", "R", monday)
	mustCreateWithSubmit(t, svc, owner, "FriCo", "R", friday)

	repo := home.New(db)
	s, err := repo.Get(ctx, owner, "Europe/Dublin", time.Monday, now, 5)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	for _, it := range s.WeekItems {
		if it.Kind == "投递" {
			found[it.Who] = it.Day
		}
	}
	if d, ok := found["MonCo"]; !ok || d != 0 {
		t.Fatalf("Monday submission chip day=%d (found=%v), want 0", d, ok)
	}
	if d, ok := found["FriCo"]; !ok || d != 4 {
		t.Fatalf("Friday submission chip day=%d (found=%v), want 4", d, ok)
	}
}
