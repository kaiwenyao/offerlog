// Regression (review P2): cancel/uncancel must first check the interview
// belongs to the caller (404), never upsert a schedule_link against another
// owner's interview or mask a generic DB error as 404.
package integration

import (
	"context"
	"testing"

	actrepo "offerlog/backend/internal/activities/repository"
)

func TestCancelOwnershipCheckBeforeUpsert(t *testing.T) {
	ctx := context.Background()
	db, svc, _, ownerA := setup(t)
	// owner B is a different user.
	ownerB := createOwner(t, db)
	repo := actrepo.New(db)

	// A's interview.
	appA := mustCreate(t, svc, ownerA, "OwnerA Co", "Role")
	var iid int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
		VALUES($1,$2,'一面','video', now() + interval '1 day','Europe/Dublin') RETURNING id`, appA.ID, ownerA).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	// The handler's ownership check is repo.GetInterview(appID, ownerID, id):
	// querying B's context for A's interview must fail (→ transport 404). The
	// exact sentinel is irrelevant — the gate runs before any schedule upsert.
	if _, err := repo.GetInterview(ctx, appA.ID, ownerB, iid); err == nil {
		t.Fatal("B must not read A's interview (expected an error → 404)")
	}
	// Cross-owner GetScheduleLink also yields nil → the handler would create a
	// new link; with the ownership gate in front it never reaches that point.
	sch, err := repo.GetScheduleLink(ctx, iid, ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if sch != nil {
		t.Fatal("B should not see A's schedule link")
	}
	// The correct owner can cancel: ownership check passes, upsert lands.
	schA, err := repo.GetScheduleLink(ctx, iid, ownerA)
	if err != nil || schA != nil {
		t.Fatalf("A link before cancel: %v %v", schA, err)
	}
	link := &actrepo.ScheduleLink{InterviewID: iid, OwnerID: ownerA, Cancelled: true, CancelledReason: "改期"}
	if err := repo.UpsertScheduleLink(ctx, repo.Pool(), link); err != nil {
		t.Fatalf("owner A upsert: %v", err)
	}
	// B still cannot create a link for A's interview (unique interview_id would
	// conflict even if the gate were bypassed) — the error is not "not found".
	linkB := &actrepo.ScheduleLink{InterviewID: iid, OwnerID: ownerB, Cancelled: true}
	if err := repo.UpsertScheduleLink(ctx, repo.Pool(), linkB); err == nil {
		t.Fatal("B upserting A's interview link should fail")
	}
}
