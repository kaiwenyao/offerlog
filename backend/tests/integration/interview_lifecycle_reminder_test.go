// Interview lifecycle reminder cleanup (review round 4, P1): cancel was wired
// to ClearInterviewReminders but updateInterview (reschedule → new day) and
// deleteInterview were not. A rescheduled interview must drop the old day's
// "明天有面试" notification (its idempotency key interview:<id>:<oldday> would
// otherwise pin it forever and mute the fresh one), and a deleted interview
// must not leave its generated reminder behind.
package integration

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/prefs"
	"offerlog/backend/internal/reminders"
)

func TestRescheduleInterviewClearsOldDayReminder(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	_ = prefs.New(db).Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	})
	app := mustCreate(t, svc, owner, "ReschedCo", "Role")

	// Interview scheduled TOMORROW.
	tomorrow := time.Now().Add(24 * time.Hour)
	var iid int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video',$3,'Europe/Dublin') RETURNING id`, app.ID, owner, tomorrow).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	// The reminder pass fires for "tomorrow" today.
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='interview' AND idempotency_key LIKE $2`,
			owner, fmt.Sprintf("interview:%d:%%", iid)).Scan(&n)
		return n
	}
	if count() == 0 {
		t.Fatal("expected an interview reminder before reschedule")
	}

	// Reschedule to 3 days out via the transport.
	srv := newFullActivityServer(t, db, owner)
	defer srv.Close()
	body := fmt.Sprintf(`{"round_name":"一面","format":"video","scheduled_at":%q}`,
		time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339))
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/v1/applications/"+itoa(app.ID)+"/interviews/"+itoa(iid), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reschedule status = %d, want 200", res.StatusCode)
	}
	if count() != 0 {
		t.Fatalf("old-day interview reminder survived the reschedule (%d rows)", count())
	}
}

func TestDeleteInterviewClearsReminder(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	_ = prefs.New(db).Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	})
	app := mustCreate(t, svc, owner, "DelIvCo", "Role")
	var iid int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video', now() + interval '1 day','Europe/Dublin') RETURNING id`, app.ID, owner).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var before int
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='interview' AND idempotency_key LIKE $2`,
		owner, fmt.Sprintf("interview:%d:%%", iid)).Scan(&before)
	if before == 0 {
		t.Fatal("expected an interview reminder before delete")
	}

	srv := newFullActivityServer(t, db, owner)
	defer srv.Close()
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/v1/applications/"+itoa(app.ID)+"/interviews/"+itoa(iid), nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", res.StatusCode)
	}
	var after int
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='interview' AND idempotency_key LIKE $2`,
		owner, fmt.Sprintf("interview:%d:%%", iid)).Scan(&after)
	if after != 0 {
		t.Fatalf("interview reminders after delete = %d, want 0", after)
	}
	_ = notifications.New(db)
}

// 方案 §3.3：把一轮面试标为「已完成」后，就不该再收到「明天有面试」。完成事实独立
// 于排期 —— 时间还挂在明天，但面试其实已经发生了（提前面完 / 改期补录）。
func TestCompletedInterviewStopsDayReminder(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	_ = prefs.New(db).Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Europe/Dublin", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	})
	app := mustCreate(t, svc, owner, "DoneCo", "Role")
	tomorrow := time.Now().Add(24 * time.Hour)

	count := func() int {
		var n int
		_ = db.Pool().QueryRow(ctx,
			`SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='interview'`, owner).Scan(&n)
		return n
	}

	var pendingID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone, progress)
		VALUES($1,$2,'一面','video',$3,'Europe/Dublin','preparing') RETURNING id`, app.ID, owner, tomorrow).Scan(&pendingID); err != nil {
		t.Fatal(err)
	}
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if count() == 0 {
		t.Fatal("expected a reminder for a still-pending round")
	}

	// 把它标成已完成，并清掉已生成的提醒（模拟用户在轮次卡片上的操作）。
	if _, err := db.Pool().Exec(ctx, `UPDATE interviews SET progress='completed' WHERE id=$1`, pendingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `DELETE FROM notifications WHERE owner_id=$1 AND kind='interview'`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if count() != 0 {
		t.Fatalf("a completed round must not remind again (%d rows)", count())
	}
}
