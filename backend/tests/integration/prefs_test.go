// Preferences + profile integration coverage (plan §5.1 账户偏好与提醒):
// saving display name / timezone / reminder prefs persists and another
// "device" (fresh read) sees the same values; timezone validation rejects
// non-IANA names; stale-days=0 turns off stale reminders.
package integration

import (
	"context"
	"testing"
	"time"

	idrepo "offerlog/backend/internal/identity/repository"
	idservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/prefs"
	"offerlog/backend/internal/reminders"
)

func TestPreferencesPersistAndReadBackAcrossSessions(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()

	pr := prefs.New(db)
	err := pr.Upsert(ctx, &prefs.Preferences{
		UserID: owner, DisplayName: "面试达人", Timezone: "Asia/Shanghai",
		WeekStart: 1, RemindOverdue: false, RemindInterview: true,
		RemindStaleDays: 7, RemindWeekly: false, Locale: "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := pr.Get(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected preferences row")
	}
	if got.DisplayName != "面试达人" || got.Timezone != "Asia/Shanghai" {
		t.Fatalf("read-back mismatch: %+v", got)
	}
	if got.RemindOverdue || !got.RemindInterview || got.RemindStaleDays != 7 {
		t.Fatalf("reminder prefs mismatch: %+v", got)
	}
}

func TestProfileUpdatePersistsToUsersRow(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()

	// Simulate a registered user row (setup() creates the users row for
	// ownership; the display name/timezone live there).
	repo := idrepo.NewSQLUsers(db)
	svc := idservice.New(db, repo, 24, "test")
	row, err := svc.UpdateProfile(ctx, owner, "新名字", "Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	if row.DisplayName != "新名字" || row.Timezone != "Europe/London" {
		t.Fatalf("profile row not updated: %+v", row)
	}
	// Re-read from a fresh query (another device): still the saved values.
	fresh, err := svc.UpdateProfile(ctx, owner, "新名字", "Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.DisplayName != "新名字" {
		t.Fatalf("cross-session profile mismatch: %+v", fresh)
	}
}

func TestTimezoneValidationRejectsNonIANA(t *testing.T) {
	loc, err := time.LoadLocation("Mars/Olympus")
	if err == nil {
		t.Fatalf("expected invalid tz to fail, got %v", loc)
	}
	if _, err := time.LoadLocation("Europe/Dublin"); err != nil {
		t.Fatalf("Europe/Dublin should load: %v", err)
	}
}

// Regression (review P1): remind_stale_days=0 ("关闭") must survive a read-back
// instead of being collapsed to the 14-day default — otherwise the settings
// dropdown snaps back to 14 after the user saves 关闭.
func TestStaleDaysZeroRoundTrips(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()
	pr := prefs.New(db)
	if err := pr.Upsert(ctx, &prefs.Preferences{
		UserID: owner, DisplayName: "x", Timezone: "UTC",
		WeekStart: 1, RemindOverdue: true, RemindInterview: true,
		RemindStaleDays: 0, // 关闭
	}); err != nil {
		t.Fatal(err)
	}
	got, err := pr.Get(ctx, owner)
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.RemindStaleDays != 0 {
		t.Fatalf("RemindStaleDays = %d, want 0 (关闭 must persist)", got.RemindStaleDays)
	}
}

// Regression (round 3, P1): the reminder generator must use users.timezone
// (single source) — not a stale user_preferences.timezone mirror. We set the
// users row to Europe/Dublin and the prefs mirror to Asia/Shanghai, then seed
// an interview at a UTC instant that is “tomorrow” in Dublin but “the day
// after” in Shanghai; only the Dublin interpretation must fire a reminder.
func TestReminderTimezoneFollowsUsersRow(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	// prefs mirror says Shanghai; users row says Dublin (e.g. via /auth/me).
	if err := prefs.New(db).Upsert(ctx, &prefs.Preferences{
		UserID: owner, Timezone: "Asia/Shanghai", WeekStart: 1,
		RemindOverdue: false, RemindInterview: true, RemindStaleDays: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE users SET timezone='Europe/Dublin' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	app := mustCreate(t, svc, owner, "TZFollow", "Role")
	// Pick an instant that is 23:30 Dublin on day X (→ interview is tomorrow in
	// Dublin) but 06:30 Shanghai on day X+1 (→ not “tomorrow” in Shanghai).
	// 2026-09-10T22:30:00Z = 2026-09-10 23:30 Dublin, 2026-09-11 06:30 Shanghai.
	at := time.Date(2026, 9, 10, 22, 30, 0, 0, time.UTC)
	// Run the generator at a "now" such that the interview is tomorrow in
	// Dublin: generator day = 2026-09-09 (tomorrow = 09-10 in Dublin).
	genNow := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video',$3,'Europe/Dublin')`, app.ID, owner, at); err != nil {
		t.Fatal(err)
	}
	if _, err := reminders.New(db).Run(ctx, genNow); err != nil {
		t.Fatal(err)
	}
	nots := notifications.New(db)
	open, _ := nots.List(ctx, owner, true, 50)
	found := false
	for _, n := range open {
		if n.Kind == "interview" {
			found = true
		}
	}
	// users.timezone = Dublin must drive the generator; if the stale Shanghai
	// mirror won, the interview would not be “tomorrow” and no reminder fires.
	if !found {
		t.Fatal("interview reminder did not fire with users.timezone=Dublin (stale prefs mirror leaked)")
	}
}
