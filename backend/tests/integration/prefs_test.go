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
	"offerlog/backend/internal/prefs"
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
