// Package integration runs against a real PostgreSQL. It needs the env var
// TEST_DATABASE_URL (defaults to the dev database on localhost:5433).
package integration

import (
	"bytes"
	"context"
	"encoding/csv"
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

// A rollback that walks a record back to the pre-submission phase must KEEP
// the submission and first-response facts (方案 §4.2: 回退不删除真实历史).
// 「已投递 → 准备材料」really happens (HR asks for extra documents); the
// timeline says so, and the 待投递 statistic is computed from the absence of a
// submission fact rather than from the status alone.
func TestRollbackToPreSubmissionKeepsTimestamps(t *testing.T) {
	_, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "Reopen", "前端工程师")

	submitted := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	responded := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: &submitted, FirstResponseAt: &responded,
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusWithdrawn, Version: 2, Reason: "放弃",
	}); err != nil {
		t.Fatalf("withdrawn: %v", err)
	}
	// Reopen into a non-terminal stage, then roll back to 待投递 — an ordinary
	// rollback, not a correction.
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusPreparing, Version: 3, Reason: "重新考虑", ChangeType: domain.ChangeReopen,
	}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusSaved, Version: 4, ChangeType: domain.ChangeRollback,
	}); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// Re-read from the database — the transition response is built from the
	// in-memory row and would pass even while the UPDATE dropped the values.
	got, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.StatusSaved {
		t.Fatalf("status = %s, want saved", got.Status)
	}
	if got.SubmittedAt == nil || !got.SubmittedAt.Equal(submitted) {
		t.Errorf("submitted_at must survive a rollback, got %v", got.SubmittedAt)
	}
	if got.FirstResponseAt == nil || !got.FirstResponseAt.Equal(responded) {
		t.Errorf("first_response_at must survive a rollback, got %v", got.FirstResponseAt)
	}

	// The audit trail records it as a rollback, not as an ordinary advance.
	evs, err := svc.Events(ctx, owner, app.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	last := evs[len(evs)-1]
	if last.ChangeType != domain.ChangeRollback {
		t.Errorf("change_type = %q, want rollback", last.ChangeType)
	}
	var rollbacks int
	for _, ev := range evs {
		if ev.ChangeType == domain.ChangeRollback {
			rollbacks++
		}
	}
	if rollbacks != 1 {
		t.Errorf("rollback events = %d, want exactly 1", rollbacks)
	}
}

// 「已投递」 asserts a submission happened, so the no-formal-submission escape
// hatch must not apply to it — otherwise the row reads as submitted in the UI
// while every submitted_at IS NOT NULL query treats it as not submitted.
func TestNoFormalSubmissionRejectedForApplied(t *testing.T) {
	_, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "SkipGuard", "数据工程师")

	_, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, NoFormalSubmission: true,
	})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Code != "missing_submitted_at" {
		t.Fatalf("want missing_submitted_at, got %v", err)
	}

	// The same assertion IS valid for a later stage.
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: 1, NoFormalSubmission: true,
	})
	if err != nil {
		t.Fatalf("interviewing with no-formal-submission: %v", err)
	}
	if row.Status != domain.StatusInterviewing || row.SubmittedAt != nil {
		t.Fatalf("want interviewing with null submitted_at, got %+v", row)
	}
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
	n, skipped, err := tr.CommitImport(ctx, owner, pv.BatchID, stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 inserted, got %d", n)
	}
	// The 4th row (empty 公司) is the one the preview rejected — the commit must
	// own up to skipping it rather than quietly counting it as a success.
	if skipped != 1 {
		t.Fatalf("expected 1 skipped row, got %d", skipped)
	}
	// idempotent second commit
	n2, skipped2, err := tr.CommitImport(ctx, owner, pv.BatchID, stringsNewReader(csvData))
	if err != nil || n2 != 3 || skipped2 != 1 {
		t.Fatalf("idempotent commit: inserted=%d skipped=%d %v", n2, skipped2, err)
	}
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM applications WHERE owner_id=$1`, owner).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 apps, got %d", count)
	}
}

// A backup you cannot restore is not a backup. This walks the REAL round trip —
// ExportHeader + ExportRows serialized exactly as the HTTP handler writes them,
// then fed straight back through ParseCSV/CommitImport.
//
// The previous version of this test hand-wrote a 3-column CSV, so it never
// exercised the exported 投递时间 format and happily passed while every restored
// row silently lost its submission time (parseDate did not accept the very
// layout ExportRows emits).
func TestCSVExportReimportsCleanly(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	submitted := time.Date(2026, 3, 9, 14, 30, 0, 0, time.UTC)
	if _, err := svc.Create(ctx, owner, &appservice.CreateInput{
		CompanyName: "RoundTripCo", Position: "后端", Status: domain.StatusApplied, SubmittedAt: &submitted,
	}); err != nil {
		t.Fatal(err)
	}
	archived := mustCreate(t, svc, owner, "ArchiveCo", "已归档岗")
	if err := svc.Archive(ctx, owner, archived.ID, true); err != nil {
		t.Fatal(err)
	}

	csvData := exportCSV(t, db, owner)

	other := createOwner(t, db)
	tr := transfers.New(db)
	pv, err := tr.ParseCSV(ctx, other, "applications.csv", stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	// The app's own export must survive its own preview without a single error.
	if len(pv.Errors) != 0 {
		t.Fatalf("own export failed preview: %+v", pv.Errors)
	}
	if pv.TotalRows != 2 || pv.ValidRows != 2 {
		t.Fatalf("preview total=%d valid=%d, want 2/2", pv.TotalRows, pv.ValidRows)
	}
	n, skipped, err := tr.CommitImport(ctx, other, pv.BatchID, stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || skipped != 0 {
		t.Fatalf("import inserted=%d skipped=%d, want 2/0", n, skipped)
	}

	// 投递时间 survives to the minute the CSV renders.
	var got *time.Time
	if err := db.Pool().QueryRow(ctx,
		`SELECT submitted_at FROM applications WHERE owner_id=$1 AND company_name='RoundTripCo'`,
		other).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("submitted_at lost on re-import (was 2026-03-09 14:30Z)")
	}
	if want := submitted.Truncate(time.Minute); !got.UTC().Equal(want) {
		t.Fatalf("submitted_at = %s, want %s", got.UTC(), want)
	}
	// 归档 survives too: a restore must not resurrect archived jobs as live ones.
	var nArchived, nLive int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FILTER (WHERE archived_at IS NOT NULL),
		count(*) FILTER (WHERE archived_at IS NULL) FROM applications WHERE owner_id=$1`,
		other).Scan(&nArchived, &nLive); err != nil {
		t.Fatal(err)
	}
	if nArchived != 1 || nLive != 1 {
		t.Fatalf("imported archived=%d live=%d, want 1/1", nArchived, nLive)
	}
}

// The commit must apply the same gate the preview showed, so「有效 N 行」and
// 「写入 N 条」can never contradict each other on screen.
func TestCSVCommitSkipsRowsThePreviewRejected(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()
	tr := transfers.New(db)
	csvData := "公司,岗位,状态,截止日期,投递时间\n" +
		"GoodCo,后端,已投递,2026-01-02,2026-01-02 09:30\n" +
		"BadDate,后端,已投递,,not-a-date\n" +
		"BadStatus,后端,火星状态,,\n" +
		"BadDeadline,后端,,乱七八糟,\n" +
		",没有公司,,,\n"
	pv, err := tr.ParseCSV(ctx, owner, "x.csv", stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if pv.TotalRows != 5 || pv.ValidRows != 1 || len(pv.Errors) != 4 {
		t.Fatalf("preview total=%d valid=%d errors=%d, want 5/1/4", pv.TotalRows, pv.ValidRows, len(pv.Errors))
	}
	n, skipped, err := tr.CommitImport(ctx, owner, pv.BatchID, stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if int(n) != pv.ValidRows {
		t.Fatalf("inserted=%d but preview promised valid=%d", n, pv.ValidRows)
	}
	if int(skipped) != len(pv.Errors) {
		t.Fatalf("skipped=%d but preview reported %d errors", skipped, len(pv.Errors))
	}
	var count int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM applications WHERE owner_id=$1`, owner).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rows in db = %d, want only the 1 valid row", count)
	}
}

// exportCSV renders the export exactly as the HTTP handler does: the shared
// header plus ExportRows, through encoding/csv.
func exportCSV(t *testing.T, db *database.DB, owner int64) string {
	t.Helper()
	rows, err := transfers.New(db).ExportRows(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(transfers.ExportHeader()); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if len(r) != len(transfers.ExportHeader()) {
			t.Fatalf("export row has %d cells, header has %d", len(r), len(transfers.ExportHeader()))
		}
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	return buf.String()
}

func stringsNewReader(s string) *strings.Reader { return strings.NewReader(s) }
