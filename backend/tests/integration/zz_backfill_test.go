package integration

import (
	"context"
	"testing"
)

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
	// Run the same INSERT...WHERE NOT EXISTS once.
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
}

const backfillSQL = `
INSERT INTO actions (application_id, owner_id, title, due_date, due_ts, done_at, remind_me, priority, source, created_at, updated_at)
SELECT ap.id, ap.owner_id,
       left(btrim(ap.next_action), 500),
       ap.next_action_due_at, ap.next_action_due_ts, NULL, FALSE, 'medium', 'next_action', now(), now()
FROM applications ap
WHERE ap.deleted_at IS NULL
  AND btrim(ap.next_action) <> ''
  AND NOT EXISTS (
      SELECT 1 FROM actions a
      WHERE a.application_id = ap.id AND a.owner_id = ap.owner_id
        AND a.source = 'next_action'
  );`
