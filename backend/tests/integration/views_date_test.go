// Views-query date wire regression (review P0): the rich query endpoint that
// powers the database table must emit DATE columns (deadline,
// next_action_due_at) as YYYY-MM-DD — the same wire format as the plain
// applications list/detail — so the frontend never sees a timestamp that shifts
// across timezones.
package integration

import (
	"context"
	"testing"

	"offerlog/backend/internal/platform/day"
	"offerlog/backend/internal/views"
	viewrepo "offerlog/backend/internal/views/repository"
	vservice "offerlog/backend/internal/views/service"
)

func TestViewsQueryEmitsDateOnlyStrings(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)

	app := mustCreate(t, svc, owner, "ViewDateCo", "Role")
	// Store fixed calendar days via the transport's own parse helper.
	d, err := day.Parse("2026-10-05")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET deadline=$2, next_action='回访', next_action_due_at=$3 WHERE id=$1`,
		app.ID, d, d); err != nil {
		t.Fatal(err)
	}

	vs := vservice.New(db, viewrepo.New(db))
	items, total, _, err := vs.RunQuery(ctx, owner, nil, nil, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("query returned total=%d items=%d, want 1/1", total, len(items))
	}
	row := items[0]
	deadline, ok := row["deadline"].(*string)
	if !ok || deadline == nil || *deadline != "2026-10-05" {
		t.Fatalf("deadline wire = %#v (%T), want *string '2026-10-05'", row["deadline"], row["deadline"])
	}
	na, ok := row["next_action_due_at"].(*string)
	if !ok || na == nil || *na != "2026-10-05" {
		t.Fatalf("next_action_due_at wire = %#v (%T), want *string '2026-10-05'", row["next_action_due_at"], row["next_action_due_at"])
	}
}

var _ = views.FilterNode{}
