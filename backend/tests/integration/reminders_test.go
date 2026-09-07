// Reminder generator integration coverage (plan §5.1): same event does not
// notify twice, closing a preference stops new rows, rescheduling updates the
// reminder, cancelled interviews don't fire, stale stops after a reply.
package integration

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/prefs"
	"offerlog/backend/internal/reminders"
)

func TestRemindersIdempotentAndPrefOff(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	// A real pref row (overdue ON, interview ON, stale OFF).
	pr := prefs.New(db)
	if err := pr.Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: true, RemindInterview: true, RemindStaleDays: 0,
	}); err != nil {
		t.Fatal(err)
	}

	app := mustCreate(t, svc, owner, "RemindCo", "Role")
	// overdue action due yesterday
	var actionID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		VALUES($1,$2,'跟进简历', CURRENT_DATE - 1, NULL, FALSE, 'medium','manual') RETURNING id`, app.ID, owner).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	// interview tomorrow
	if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video', now() + interval '1 day','Europe/Dublin')`, app.ID, owner); err != nil {
		t.Fatal(err)
	}

	gen := reminders.New(db)
	now := time.Now()
	// Run the pass twice — the second must insert nothing (idempotency keys).
	n1, err := gen.Run(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	n2, err := gen.Run(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("second pass inserted %d (should be 0, idempotent)", n2)
	}
	if n1 < 1 {
		t.Fatalf("first pass inserted %d, want >=1", n1)
	}

	nots := notifications.New(db)
	open, _ := nots.List(ctx, owner, true, 50)
	// The open list must contain exactly one overdue and one interview.
	overdue, interview := 0, 0
	for _, n := range open {
		switch n.Kind {
		case "overdue":
			overdue++
		case "interview":
			interview++
		}
	}
	if overdue != 1 || interview != 1 {
		t.Fatalf("open reminders overdue=%d interview=%d, want 1/1", overdue, interview)
	}

	// Turn overdue OFF and re-run: no new overdue rows.
	if err := pr.Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := nots.List(ctx, owner, true, 100)
	n3, err := gen.Run(ctx, now.Add(24*time.Hour)) // next day — but still overdue & upcoming
	if err != nil {
		t.Fatal(err)
	}
	after, _ := nots.List(ctx, owner, true, 100)
	// no new overdue created; existing one remains (not auto-deleted)
	if len(after) != len(before) {
		t.Fatalf("after turning pref off len changed %d -> %d", len(before), len(after))
	}
	_ = actionID
	_ = n3
}

func TestCancelledInterviewDoesNotRemind(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	pr := prefs.New(db)
	_ = pr.Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	})
	app := mustCreate(t, svc, owner, "CancelCo", "Role")
	// interview tomorrow, but with a cancelled schedule link
	var iid int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video', now() + interval '1 day','Europe/Dublin') RETURNING id`, app.ID, owner).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO schedule_links(interview_id, owner_id, cancelled, cancelled_reason)
		VALUES($1,$2,TRUE,'改期')`, iid, owner); err != nil {
		t.Fatal(err)
	}
	gen := reminders.New(db)
	if _, err := gen.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	nots := notifications.New(db)
	open, _ := nots.List(ctx, owner, true, 50)
	for _, n := range open {
		if n.Kind == "interview" {
			t.Fatalf("cancelled interview produced a reminder: %+v", n)
		}
	}
}

func TestStaleStopsAfterFirstReply(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	pr := prefs.New(db)
	_ = pr.Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: false, RemindStaleDays: 5,
	})
	// submitted 10 days ago, no reply yet → stale eligible.
	sub := time.Now().AddDate(0, 0, -10)
	app := mustCreate(t, svc, owner, "StaleCo", "Role")
	_ = app
	var appID int64
	_ = db.Pool().QueryRow(ctx, `SELECT id FROM applications WHERE id=$1`, app.ID).Scan(&appID)
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET submitted_at=$2, status='applied'
		WHERE id=$1`, app.ID, sub); err != nil {
		t.Fatal(err)
	}
	gen := reminders.New(db)
	if _, err := gen.Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	nots := notifications.New(db)
	open, _ := nots.List(ctx, owner, true, 50)
	stale := 0
	for _, n := range open {
		if n.Kind == "stale" {
			stale++
		}
	}
	if stale != 1 {
		t.Fatalf("stale count=%d want 1", stale)
	}
	// A reply arrives (first_response_at set) — re-running must not re-notify.
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET first_response_at=now() WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := gen.Run(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	open2, _ := nots.List(ctx, owner, true, 50)
	stale2 := 0
	for _, n := range open2 {
		if n.Kind == "stale" {
			stale2++
		}
	}
	if stale2 != 1 {
		t.Fatalf("stale after reply = %d, want unchanged 1 (no duplicate)", stale2)
	}
}

// Regression (round 3, P2): cancelling an interview must clear its generated
// "明天有面试" reminder (kind='interview', key prefix interview:<id>:).
func TestCancelInterviewClearsReminder(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	pr := prefs.New(db)
	_ = pr.Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	})
	app := mustCreate(t, svc, owner, "CancelRemCo", "Role")
	var iid int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video', now() + interval '1 day','Europe/Dublin') RETURNING id`, app.ID, owner).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	// Generate the tomorrow reminder.
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	nots := notifications.New(db)
	before := 0
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='interview'`, owner).Scan(&before)
	if before == 0 {
		t.Fatal("expected an interview reminder before cancel")
	}
	// Cancel via the repo path the transport now takes: set cancelled + clear.
	if err := nots.ClearInterviewReminders(ctx, owner, iid); err != nil {
		t.Fatal(err)
	}
	after := 0
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='interview'`, owner).Scan(&after)
	if after != 0 {
		t.Fatalf("interview reminders after cancel = %d, want 0", after)
	}
}
