package integration

import (
	"context"
	"os"
	"regexp"
	"testing"
)

// loadBackfillSQL reads the live migration 00002 and extracts the action
// backfill INSERT statement, so this test always guards the ACTUAL migration
// text (a copy that diverges from the migration would fail the test, not pass
// it). Migration files are embedded under internal/platform/migrate/migrations
// and mirrored at db/migrations — read the embedded copy.
func loadBackfillSQL(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../internal/platform/migrate/migrations/00002_preferences_upcoming_interviews.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	re := regexp.MustCompile(`(?s)INSERT INTO actions .*?;`)
	m := re.FindString(string(data))
	if m == "" {
		t.Fatal("could not locate the actions backfill INSERT in migration 00002")
	}
	return m
}

// TestLegacyNextActionBackfillIdempotent verifies migration 00002's backfill
// semantics: an app with a legacy next_action and no standalone action gets
// exactly one open action (source='next_action'); re-applying the same SQL
// creates no additional rows.
func TestLegacyNextActionBackfillIdempotent(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "LegacyCo", "Role")
	_ = app
	// Simulate a legacy next_action row (pre-migration data shape).
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET next_action='Follow up in 3 days',
		next_action_due_at = CURRENT_DATE + 3 WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	// Run the live migration's own backfill INSERT once.
	backfillSQL := loadBackfillSQL(t)
	if _, err := db.Pool().Exec(ctx, backfillSQL); err != nil {
		t.Fatal(err)
	}
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM actions WHERE application_id=$1 AND source='next_action'`, app.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("after first backfill count=%d want 1", count)
	}
	// Run again: still exactly one.
	if _, err := db.Pool().Exec(ctx, backfillSQL); err != nil {
		t.Fatal(err)
	}
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM actions WHERE application_id=$1 AND source='next_action'`, app.ID).Scan(&count)
	if count != 1 {
		t.Fatalf("after second backfill count=%d want 1 (idempotent)", count)
	}
	// The guard must be “no action of any kind” (matches migration text): an
	// application with a MANUAL action must not gain a migrated duplicate.
	app2 := mustCreate(t, svc, owner, "LegacyManualCo", "Role")
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET next_action='既有手动待办外还有旧文本', next_action_due_at=CURRENT_DATE+2 WHERE id=$1`, app2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		VALUES($1,$2,'手动待办', CURRENT_DATE+1, NULL, FALSE, 'medium','manual')`, app2.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, backfillSQL); err != nil {
		t.Fatal(err)
	}
	var manualOnly int
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM actions WHERE application_id=$1`, app2.ID).Scan(&manualOnly)
	if manualOnly != 1 {
		t.Fatalf("app with a manual action gained %d migrated rows, want 1 total", manualOnly)
	}
}
