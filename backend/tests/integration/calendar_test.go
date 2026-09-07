// Calendar range endpoint integration coverage (plan §5.5): cross-application
// interviews in a window, cancelled rounds excluded, date-only action/deadline
// events surfaced as all-day rows, and the original timezone preserved.
package integration

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/calendar"
)

func TestCalendarRangeIncludesCrossAppInterviews(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := calendar.New(db)
	loc, _ := time.LoadLocation("Europe/Dublin")
	now := time.Now().In(loc)
	// two applications, two interviews on distinct future days
	app1 := mustCreate(t, svc, owner, "CalCo1", "Role")
	app2 := mustCreate(t, svc, owner, "CalCo2", "Role")
	if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video',$3,'Europe/Dublin')`, app1.ID, owner, now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'终面','onsite',$3,'Europe/Dublin')`, app2.ID, owner, now.AddDate(0, 0, 3)); err != nil {
		t.Fatal(err)
	}
	from := now
	to := now.AddDate(0, 0, 7)
	items, err := repo.Range(ctx, owner, "Europe/Dublin", from, to)
	if err != nil {
		t.Fatal(err)
	}
	interviews := 0
	for _, e := range items {
		if e.Kind == "interview" {
			interviews++
		}
	}
	if interviews != 2 {
		t.Fatalf("interviews in window = %d, want 2", interviews)
	}
}

func TestCalendarExcludesCancelledAndKeepsTimezone(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := calendar.New(db)
	now := time.Now()
	app := mustCreate(t, svc, owner, "CancelCal", "Role")
	var iid int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video',$3,'Asia/Shanghai') RETURNING id`, app.ID, owner, now.AddDate(0, 0, 2)).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	// cancelled → excluded from range events (kept in DB for history)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO schedule_links(interview_id, owner_id, cancelled, cancelled_reason)
		VALUES($1,$2,TRUE,'改期')`, iid, owner); err != nil {
		t.Fatal(err)
	}
	items, err := repo.Range(ctx, owner, "Europe/Dublin", now, now.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range items {
		if e.Kind == "interview" && e.ID == iid {
			t.Fatalf("cancelled interview leaked into calendar: %+v", e)
		}
	}
	// Un-cancel → appears with the original timezone label.
	if _, err := db.Pool().Exec(ctx, `UPDATE schedule_links SET cancelled=FALSE, cancelled_reason='' WHERE interview_id=$1`, iid); err != nil {
		t.Fatal(err)
	}
	items2, err := repo.Range(ctx, owner, "Europe/Dublin", now, now.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range items2 {
		if e.Kind == "interview" && e.ID == iid {
			found = true
			if e.Timezone != "Asia/Shanghai" {
				t.Fatalf("timezone = %q, want original Asia/Shanghai", e.Timezone)
			}
		}
	}
	if !found {
		t.Fatal("uncancelled interview missing from range")
	}
}

func TestCalendarAllDayActionAndDeadline(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := calendar.New(db)
	now := time.Now()
	app := mustCreate(t, svc, owner, "DeadlineCo", "Role")
	// action due in 3 days (date only)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, priority, source)
		VALUES($1,$2,'Follow up', CURRENT_DATE + 3, 'high','manual')`, app.ID, owner); err != nil {
		t.Fatal(err)
	}
	// application deadline in 5 days (date only)
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET deadline = CURRENT_DATE + 5 WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	from := now
	to := now.AddDate(0, 0, 14)
	items, err := repo.Range(ctx, owner, "Europe/Dublin", from, to)
	if err != nil {
		t.Fatal(err)
	}
	actionSeen, deadlineSeen := false, false
	for _, e := range items {
		if e.Kind == "action" {
			actionSeen = true
			if !e.AllDay {
				t.Fatalf("date-only action must be all_day: %+v", e)
			}
		}
		if e.Kind == "deadline" {
			deadlineSeen = true
			if !e.AllDay {
				t.Fatalf("date-only deadline must be all_day: %+v", e)
			}
		}
	}
	if !actionSeen || !deadlineSeen {
		t.Fatalf("action=%v deadline=%v want both", actionSeen, deadlineSeen)
	}
}

// Regression (round 3, P2): /calendar must consistently exclude deleted and
// archived applications across all three event kinds (interview / action /
// deadline) — an archived app's events must not keep appearing.
func TestCalendarExcludesArchivedAppsAcrossKinds(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := calendar.New(db)
	now := time.Now()
	app := mustCreate(t, svc, owner, "ArchCal", "Role")
	// deadline + action + interview all within the window
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET deadline = CURRENT_DATE + 2 WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, priority, source)
		VALUES($1,$2,'待办', CURRENT_DATE + 1, 'high','manual')`, app.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video', now() + interval '1 day','Europe/Dublin')`, app.ID, owner); err != nil {
		t.Fatal(err)
	}
	items, err := repo.Range(ctx, owner, "Europe/Dublin", now, now.AddDate(0, 0, 14))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range items {
		seen[e.Kind] = true
	}
	if !seen["interview"] || !seen["action"] || !seen["deadline"] {
		t.Fatalf("before archive expected all three kinds, got %v", seen)
	}
	// Archive → all three disappear.
	if err := svc.Archive(ctx, owner, app.ID, true); err != nil {
		t.Fatal(err)
	}
	items2, err := repo.Range(ctx, owner, "Europe/Dublin", now, now.AddDate(0, 0, 14))
	if err != nil {
		t.Fatal(err)
	}
	if len(items2) != 0 {
		t.Fatalf("after archive calendar still returns %d events, want 0 (archived apps excluded across kinds)", len(items2))
	}
}
