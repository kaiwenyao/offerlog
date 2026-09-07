package integration

import (
	"context"
	"testing"

	actrepo "offerlog/backend/internal/activities/repository"
)

// Regression: GET /applications/:id/actions previously referenced the join
// alias without a join and 500'd (missing FROM-clause entry for "ap").
func TestAppScopedActionListing(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ListCo", "Role")
	// Create two actions, one open + one done.
	ar := actrepo.New(db)
	a1 := &actrepo.Action{ApplicationID: &app.ID, OwnerID: owner, Title: "open", Priority: "high"}
	if err := ar.CreateAction(ctx, ar.Pool(), a1); err != nil {
		t.Fatal(err)
	}
	a2 := &actrepo.Action{ApplicationID: &app.ID, OwnerID: owner, Title: "done", Priority: "medium"}
	if err := ar.CreateAction(ctx, ar.Pool(), a2); err != nil {
		t.Fatal(err)
	}
	if err := ar.MarkActionDone(ctx, ar.Pool(), owner, a2.ID, true); err != nil {
		t.Fatal(err)
	}
	// app-scoped list (both) must not error and include enrichment columns.
	both, err := ar.ListActions(ctx, &app.ID, owner, false)
	if err != nil {
		t.Fatalf("app-scoped ListActions errored (regression): %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("got %d actions, want 2", len(both))
	}
	for _, a := range both {
		if a.CompanyName != "ListCo" || a.Priority == "" {
			t.Fatalf("enrichment missing: %+v", a)
		}
	}
	open, err := ar.ListActions(ctx, &app.ID, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Title != "open" {
		t.Fatalf("open list wrong: %+v", open)
	}
	// unscoped (dashboard) list still works.
	all, err := ar.ListActions(ctx, nil, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("unscoped list wrong: %d", len(all))
	}
}
