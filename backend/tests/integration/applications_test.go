// Package integration runs against a real PostgreSQL. It needs the env var
// TEST_DATABASE_URL (defaults to the dev database on localhost:5433).
package integration

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"offerlog/backend/internal/applications/domain"
	apprepo "offerlog/backend/internal/applications/repository"
	appservice "offerlog/backend/internal/applications/service"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/migrate"
	"offerlog/backend/internal/transfers"
	"strings"
)

func dbURL(t *testing.T) string {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://offerlog:offerlog@localhost:5433/offerlog?sslmode=disable"
	}
	return url
}

func setup(t *testing.T) (*database.DB, *appservice.Service, *apprepo.Repo, int64) {
	t.Helper()
	ctx := context.Background()
	db, err := database.New(ctx, dbURL(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	// CI points TEST_DATABASE_URL at an empty service database; apply the
	// embedded migrations here (idempotent via schema_migrations).
	if err := migrate.Up(ctx, db.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := apprepo.New(db)
	svc := appservice.New(db, repo)
	// create a throwaway owner (single-account product but we still scope)
	uid := createOwner(t, db)
	// clean slate for this owner
	_, _ = db.Pool().Exec(ctx, `DELETE FROM applications WHERE owner_id=$1`, uid)
	_, _ = db.Pool().Exec(ctx, `DELETE FROM companies WHERE owner_id=$1`, uid)
	return db, svc, repo, uid
}

func createOwner(t *testing.T, db *database.DB) int64 {
	var id int64
	email := "it_" + time.Now().Format("150405.000000000") + "@test.local"
	err := db.Pool().QueryRow(context.Background(), `INSERT INTO users(email, password_hash, display_name, timezone)
		VALUES($1,'x','Test',$2) RETURNING id`, email, "Europe/Dublin").Scan(&id)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return id
}

func mustCreate(t *testing.T, svc *appservice.Service, owner int64, company, position string) *apprepo.Row {
	t.Helper()
	row, err := svc.Create(context.Background(), owner, &appservice.CreateInput{CompanyName: company, Position: position})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return row
}

func TestFullPipelineToAccepted(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "Northstar", "后端工程师")

	step := func(to string, v int, extra ...func(*appservice.TransitionInput)) *apprepo.Row {
		t.Helper()
		in := &appservice.TransitionInput{ToStatus: to, Version: v}
		for _, f := range extra {
			f(in)
		}
		row, err := svc.Transition(ctx, owner, app.ID, in)
		if err != nil {
			t.Fatalf("transition to %s: %v", to, err)
		}
		return row
	}
	submitted := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	r := step("applied", 1, func(in *appservice.TransitionInput) { in.SubmittedAt = &submitted; in.IdempotencyKey = "k-pipeline" })
	if r.Status != domain.StatusApplied || r.SubmittedAt == nil {
		t.Fatalf("applied wrong: %+v", r)
	}
	// duplicate idempotency: no new event
	before, _ := svc.Events(ctx, owner, app.ID)
	_, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{ToStatus: "applied", Version: 1, SubmittedAt: &submitted, IdempotencyKey: "k-pipeline"})
	if err != nil {
		t.Fatalf("idempotent retry should succeed: %v", err)
	}
	after, _ := svc.Events(ctx, owner, app.ID)
	if len(before) != len(after) {
		t.Fatalf("idempotent retry created events: %d -> %d", len(before), len(after))
	}

	step("screening", 2)
	step("assessment", 3)
	step("interviewing", 4)
	step("offer", 5)
	acc := step("accepted", 6, func(in *appservice.TransitionInput) { in.Reason = "接受 offer" })
	if acc.Status != domain.StatusAccepted || acc.AcceptedAt == nil {
		t.Fatalf("accepted wrong: %+v", acc)
	}
	// events include created + 6 transitions (+1 synthesized offer on accept? no: had offer)
	evs, _ := svc.Events(ctx, owner, app.ID)
	// created,saved->applied,applied->screening,screening->assessment,assessment->interviewing,interviewing->offer,offer->accepted
	want := 7
	if len(evs) != want {
		t.Fatalf("want %d events, got %d", want, len(evs))
	}
	// version conflict: old version returns ErrVersionConflict
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{ToStatus: "rejected", Version: 1}); !errors.Is(err, appservice.ErrVersionConflict) {
		t.Fatalf("want version conflict, got %v", err)
	}
	_ = db
}

func TestAcceptedWithSynthesizedOfferEvent(t *testing.T) {
	_, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "Direct", "OfferOnly")
	sub := time.Now().Add(-24 * time.Hour)
	// applying with submit
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{ToStatus: "applied", Version: 1, SubmittedAt: &sub}); err != nil {
		t.Fatal(err)
	}
	// screening → offer via events? no; go straight: applied→offer allowed
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{ToStatus: "offer", Version: 2}); err != nil {
		t.Fatalf("offer: %v", err)
	}
	// Now accept — hadOffer true
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{ToStatus: "accepted", Version: 3, Reason: "签了"}); err != nil {
		t.Fatalf("accept after offer: %v", err)
	}
}

func TestCorrectionAudit(t *testing.T) {
	_, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "Corp", "Role")
	sub := time.Now().Add(-48 * time.Hour)
	_, _ = svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{ToStatus: "applied", Version: 1, SubmittedAt: &sub})
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{ToStatus: "rejected", Version: 2, Reason: "职位取消"}); err != nil {
		t.Fatal(err)
	}
	// list events, correct the rejected event back to interviewing
	evs, _ := svc.Events(ctx, owner, app.ID)
	var rejectedEvent int64
	for _, e := range evs {
		if e.ToStatus != nil && *e.ToStatus == domain.StatusRejected {
			rejectedEvent = e.ID
		}
	}
	if err := svc.Correct(ctx, owner, app.ID, &appservice.CorrectionInput{EventID: rejectedEvent, NewStatus: domain.StatusInterviewing, Reason: "误操作撤销"}); err != nil {
		t.Fatalf("correct: %v", err)
	}
	// status must be resynced to interviewing; correction event recorded
	row, _ := svc.Get(ctx, owner, app.ID, false)
	if row.Status != domain.StatusInterviewing {
		t.Fatalf("after correction status = %s, want interviewing", row.Status)
	}
	evs2, _ := svc.Events(ctx, owner, app.ID)
	foundCorrection := false
	for _, e := range evs2 {
		if e.EventType == "correction" && e.CorrectsEventID != nil && *e.CorrectsEventID == rejectedEvent {
			foundCorrection = true
		}
	}
	if !foundCorrection {
		t.Fatal("correction event with corrects_event_id not recorded")
	}
}

func TestArchiveSoftDeleteRestore(t *testing.T) {
	_, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ARC", "X")
	if err := svc.Archive(ctx, owner, app.ID, true); err != nil {
		t.Fatal(err)
	}
	row, _ := svc.Get(ctx, owner, app.ID, false)
	if row.ArchivedAt == nil {
		t.Fatal("archived flag missing")
	}
	if err := svc.SoftDelete(ctx, owner, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, owner, app.ID, false); !errors.Is(err, appservice.ErrNotFound) {
		t.Fatal("soft-deleted row must be invisible in normal reads")
	}
	if err := svc.Restore(ctx, owner, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, owner, app.ID, false); err != nil {
		t.Fatal("restored row should be visible")
	}
}

func TestCSVImportPreviewAndCommit(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()
	tr := transfers.New(db)
	csvData := "公司,岗位,状态,渠道,备注\nAcme,后端,已投递,LinkedIn,急招\nAcme,前端,待投递,,\nBigCo,数据,面试中,Referral,内推\n,空行,待投递,,\n"
	pv, err := tr.ParseCSV(ctx, owner, "x.csv", stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if pv.TotalRows != 4 || pv.ValidRows != 3 {
		t.Fatalf("preview counts wrong: %+v", pv)
	}
	n, err := tr.CommitImport(ctx, owner, pv.BatchID, stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 inserted, got %d", n)
	}
	// idempotent second commit
	n2, err := tr.CommitImport(ctx, owner, pv.BatchID, stringsNewReader(csvData))
	if err != nil || n2 != 3 {
		t.Fatalf("idempotent commit: %d %v", n2, err)
	}
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM applications WHERE owner_id=$1`, owner).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 apps, got %d", count)
	}
}

func stringsNewReader(s string) *strings.Reader { return strings.NewReader(s) }
