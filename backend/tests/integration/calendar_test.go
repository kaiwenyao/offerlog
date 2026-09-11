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
	loc, _ := time.LoadLocation("Europe/Dublin")
	now := time.Now().In(loc)
	// Seed events on explicit FUTURE Dublin calendar days (SQL CURRENT_DATE is
	// the UTC-pinned session date and can equal the current Dublin day near UTC
	// midnight, which would land the all-day event before the [now, …) window
	// and make the test flaky). Day-of-month arithmetic on the DATE column is
	// timezone-independent once the value is stored.
	tomorrow := time.Now().In(loc).AddDate(0, 0, 1).Format("2006-01-02")
	afterTomorrow := time.Now().In(loc).AddDate(0, 0, 2).Format("2006-01-02")
	app := mustCreate(t, svc, owner, "ArchCal", "Role")
	// deadline + action + interview all within the window
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET deadline = $2::date WHERE id=$1`, app.ID, afterTomorrow); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, priority, source)
		VALUES($1,$2,'待办', $3::date, 'high','manual')`, app.ID, owner, tomorrow); err != nil {
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

// OA 轮次进日历（PR #23 review P1 #4）：计划时间是一个 timed 事件；截止时间只在
// 轮次仍开放时出现 —— 已完成的轮次不再带着它的截止日（方案 §8 停止截止提醒）。
func TestCalendarIncludesAssessmentPlannedAndOpenDue(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	repo := calendar.New(db)
	now := time.Now()
	app := mustCreate(t, svc, owner, "OACal", "Role")
	if _, err := db.Pool().Exec(ctx, `INSERT INTO assessment_rounds(application_id, owner_id, kind, name, progress, planned_at, due_at)
		VALUES($1,$2,'online_test','笔试','preparing',$3,$4)`, app.ID, owner, now.AddDate(0, 0, 1), now.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}
	// completed round: planned event stays (greyed), due event disappears.
	if _, err := db.Pool().Exec(ctx, `INSERT INTO assessment_rounds(application_id, owner_id, kind, name, progress, result, planned_at, due_at, completed_at)
		VALUES($1,$2,'take_home','作业','completed','passed',$3,$4,now())`, app.ID, owner, now.AddDate(0, 0, 1), now.AddDate(0, 0, 3)); err != nil {
		t.Fatal(err)
	}
	items, err := repo.Range(ctx, owner, "Europe/Dublin", now, now.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	planned, due := 0, 0
	for _, e := range items {
		switch e.Kind {
		case "assessment":
			planned++
			if e.Done && e.RoundName == "笔试" {
				t.Fatalf("an open round must not render as done: %+v", e)
			}
		case "assessment_due":
			due++
			if e.RoundName == "作业" {
				t.Fatalf("a completed round must not surface its deadline: %+v", e)
			}
		}
	}
	if planned != 2 {
		t.Fatalf("assessment planned events = %d, want 2", planned)
	}
	if due != 1 {
		t.Fatalf("assessment due events = %d, want 1 (open round only)", due)
	}
}
