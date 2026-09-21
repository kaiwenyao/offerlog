// Verifies plan §12.2 case 1 on a clean dataset with exactly the benchmark
// distribution: the current-mode sankey conserves flow (each application is
// a leaf exactly once) and drilldown equals the list ids.
//
// Also pins the v2 classification: 未投递 must reuse the 待投递 metric
// definition (repo.Counts), so an in-progress row without submitted_at
// (内推 / 猎头) and a preparing row that still carries a submission fact both
// stay on the submitted branch instead of being counted 未投递.
package analytics

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/migrate"
)

func sankeyTestDB(t *testing.T) *database.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://offerlog:offerlog@localhost:5433/offerlog?sslmode=disable"
	}
	ctx := context.Background()
	db, err := database.New(ctx, url)
	if err != nil {
		t.Skip("no local db:", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate.Up(ctx, db.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestSankeyConservationBenchmarkFixture(t *testing.T) {
	ctx := context.Background()
	db := sankeyTestDB(t)
	owner := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner)

	repo := New(db)
	// 10-row fixture (§5.3): 待投递2 准备材料1 已投递等待2 面试中2 Offer1 拒绝1 投递后撤回1
	fixture := []struct {
		status string
		sub    bool
	}{
		{domain.StatusSaved, false}, {domain.StatusSaved, false},
		{domain.StatusPreparing, false},
		{domain.StatusApplied, true}, {domain.StatusApplied, true},
		{domain.StatusInterviewing, true}, {domain.StatusInterviewing, true},
		{domain.StatusOffer, true},
		{domain.StatusRejected, true},
		{domain.StatusWithdrawn, true},
	}
	now := time.Now()
	for i, f := range fixture {
		var sub *time.Time
		if f.sub {
			s := now.AddDate(0, 0, -(i + 1))
			sub = &s
		}
		if err := seedApp(ctx, db, owner, f.status, sub, nil); err != nil {
			t.Fatal(err)
		}
	}
	req := &SnapshotRequest{OwnerID: owner, Now: now}
	sk, err := repo.SankeyA(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if sk.CohortCount != 10 {
		t.Fatalf("cohort = %d, want 10", sk.CohortCount)
	}
	// conservation: 全部机会 → (未投递 3 | 已投递 7)
	var allToNS, allToSub, subSplit int64
	for _, l := range sk.Links {
		if l.Source == "all" && l.Target == "not_submitted" {
			allToNS = l.Value
		}
		if l.Source == "all" && l.Target == "submitted" {
			allToSub = l.Value
		}
		if l.Source == "submitted" {
			subSplit += l.Value
		}
	}
	if allToNS != 3 || allToSub != 7 {
		t.Fatalf("layer1 = %d not-submitted / %d submitted, want 3/7", allToNS, allToSub)
	}
	// 未投递 is a leaf: the submitted branch must carry all further detail.
	if subSplit != 7 {
		t.Fatalf("submitted subdivision total = %d, want 7", subSplit)
	}
	for _, n := range sk.Nodes {
		if strings.HasPrefix(n.Name, "ns_") {
			t.Fatalf("not-submitted branch should not be subdivided, found node %q", n.Name)
		}
	}
	// each application is a leaf exactly once: leaf inflow totals the cohort
	if got := leafInflow(sk); got != 10 {
		t.Fatalf("leaf inflow total %d, want cohort 10", got)
	}
	if sk.DefinitionVersion != DefinitionVersion {
		t.Fatalf("definition_version = %d, want %d", sk.DefinitionVersion, DefinitionVersion)
	}
}

// Regression for the v2 misclassification: rows past the preparing phase
// count as submitted even without submitted_at (内推 / 猎头直接约面), and a
// preparing row that still carries a submission fact (投递后退回补材料) is
// not 未投递 — mirroring the 待投递 metric card on the same page.
func TestSankeyAUnsubmittedMatchesToApplyMetric(t *testing.T) {
	ctx := context.Background()
	db := sankeyTestDB(t)
	owner := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner)

	now := time.Now()
	fixture := []struct {
		status string
		sub    bool
	}{
		{domain.StatusApplied, true},
		{domain.StatusAssessment, false}, // 内推免正式投递：无 submitted_at
		{domain.StatusPreparing, true},   // 投递后退回准备材料：仍带 submitted_at
		{domain.StatusSaved, false},
	}
	for i, f := range fixture {
		var sub *time.Time
		if f.sub {
			s := now.AddDate(0, 0, -(i + 1))
			sub = &s
		}
		if err := seedApp(ctx, db, owner, f.status, sub, nil); err != nil {
			t.Fatal(err)
		}
	}

	repo := New(db)
	sk, err := repo.SankeyA(ctx, &SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if sk.CohortCount != 4 {
		t.Fatalf("cohort = %d, want 4", sk.CohortCount)
	}
	var allToNS, allToSub int64
	split := map[string]int64{}
	for _, l := range sk.Links {
		if l.Source == "all" && l.Target == "not_submitted" {
			allToNS = l.Value
		}
		if l.Source == "all" && l.Target == "submitted" {
			allToSub = l.Value
		}
		if l.Source == "submitted" {
			split[l.Target] = l.Value
		}
	}
	if allToNS != 1 || allToSub != 3 {
		t.Fatalf("layer1 = %d not-submitted / %d submitted, want 1/3", allToNS, allToSub)
	}
	for _, want := range []string{"s_applied", "s_assessment", "s_preparing"} {
		if split[want] != 1 {
			t.Fatalf("submitted branch %s = %d, want 1", want, split[want])
		}
	}
}

// The metrics panel must read the same cohort as the sankey middle layer:
// 已投递 / 样本 count pipeline entries (NOT 未投递), so the 内推 OA row and
// the rolled-back preparing row stay in every rate denominator instead of
// vanishing behind a missing submitted_at.
func TestCountsSubmittedCohortMatchesPipeline(t *testing.T) {
	ctx := context.Background()
	db := sankeyTestDB(t)
	owner := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner)

	now := time.Now()
	fixture := []struct {
		status string
		sub    bool
	}{
		{domain.StatusApplied, true},
		{domain.StatusAssessment, false}, // 内推免正式投递：无 submitted_at
		{domain.StatusPreparing, true},   // 投递后退回准备材料：仍带 submitted_at
		{domain.StatusSaved, false},
	}
	for i, f := range fixture {
		var sub *time.Time
		if f.sub {
			s := now.AddDate(0, 0, -(i + 1))
			sub = &s
		}
		if err := seedApp(ctx, db, owner, f.status, sub, nil); err != nil {
			t.Fatal(err)
		}
	}

	repo := New(db)
	m, err := repo.Counts(ctx, &SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if m.ToApply != 1 {
		t.Fatalf("to_apply = %d, want 1", m.ToApply)
	}
	if m.SubmittedCount != 3 || m.Denominator != 3 {
		t.Fatalf("submitted_count = %d / denominator = %d, want 3/3", m.SubmittedCount, m.Denominator)
	}
	if m.Responded != 0 || m.PendingResponse != 3 {
		t.Fatalf("responded = %d / pending = %d, want 0/3", m.Responded, m.PendingResponse)
	}
	if m.ResponseRate == nil || *m.ResponseRate != 0 {
		t.Fatalf("response_rate = %v, want 0 (denominator > 0, numerator 0)", m.ResponseRate)
	}
	ch, err := repo.ByChannel(ctx, &SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	// every seeded row has channel='' → excluded from the channel panel
	if len(ch) != 0 {
		t.Fatalf("by_channel rows = %d, want 0", len(ch))
	}
}

// 口径 v2 regression (PR #38 review P1): a reply on a no-formal-submission
// row (内推) counts as responded but has no submission anchor. With ONLY such
// replies the median query matches zero rows and percentile_cont returns SQL
// NULL — it must scan as "no median" instead of failing /summary.
func TestCountsMedianIgnoresRepliesWithoutSubmissionAnchor(t *testing.T) {
	ctx := context.Background()
	db := sankeyTestDB(t)
	owner := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner)

	now := time.Now()
	// applied with anchors: submitted 96h ago, replied 48h ago → median 48h
	sub := now.Add(-96 * time.Hour)
	resp48 := now.Add(-48 * time.Hour)
	if err := seedApp(ctx, db, owner, domain.StatusApplied, &sub, &resp48); err != nil {
		t.Fatal(err)
	}
	// 内推 OA replied with no submitted_at: responded, but no elapsed anchor
	if err := seedApp(ctx, db, owner, domain.StatusAssessment, nil, &resp48); err != nil {
		t.Fatal(err)
	}

	repo := New(db)
	m, err := repo.Counts(ctx, &SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if m.Responded != 2 || m.Denominator != 2 {
		t.Fatalf("responded = %d / denominator = %d, want 2/2", m.Responded, m.Denominator)
	}
	if m.MedianSample != 1 {
		t.Fatalf("median_sample = %d, want 1", m.MedianSample)
	}
	if m.ResponseMedianH != 48 {
		t.Fatalf("response_median_hours = %v, want 48", m.ResponseMedianH)
	}

	// now the pure-referral cohort: a replied row with no anchor at all must
	// leave the median unset (and not error) — the exact P1 shape.
	owner2 := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner2)
	if err := seedApp(ctx, db, owner2, domain.StatusAssessment, nil, &resp48); err != nil {
		t.Fatal(err)
	}
	m2, err := repo.Counts(ctx, &SnapshotRequest{OwnerID: owner2, Now: now})
	if err != nil {
		t.Fatalf("counts on anchor-less replied cohort: %v", err)
	}
	if m2.Responded != 1 || m2.MedianSample != 0 || m2.ResponseMedianH != 0 {
		t.Fatalf("responded/median_sample/median = %d/%d/%v, want 1/0/0", m2.Responded, m2.MedianSample, m2.ResponseMedianH)
	}
}

func TestSankeyARejectAfterAssessmentFlowsThroughAssessment(t *testing.T) {
	ctx := context.Background()
	db := sankeyTestDB(t)
	owner := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner)

	now := time.Now()
	sub := now.AddDate(0, 0, -3)
	if err := seedAppWithStages(ctx, db, owner, domain.StatusRejected, &sub,
		[]string{domain.StatusApplied, domain.StatusAssessment, domain.StatusRejected}); err != nil {
		t.Fatal(err)
	}

	sk, err := New(db).SankeyA(ctx, &SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	links := linkIndex(sk)
	if links[[2]string{"s_applied", "s_assessment"}] != 1 {
		t.Fatalf("applied → assessment = %d, want 1", links[[2]string{"s_applied", "s_assessment"}])
	}
	if links[[2]string{"s_assessment", "s_rejected"}] != 1 {
		t.Fatalf("assessment → rejected = %d, want 1", links[[2]string{"s_assessment", "s_rejected"}])
	}
	if links[[2]string{"submitted", "s_rejected"}] != 0 {
		t.Fatalf("submitted → rejected = %d, want 0 (must walk through 笔试)", links[[2]string{"submitted", "s_rejected"}])
	}
	if leafInflow(sk) != 1 {
		t.Fatalf("leaf inflow = %d, want 1", leafInflow(sk))
	}
}

func TestSankeyARejectWithoutAssessmentStaysDirect(t *testing.T) {
	ctx := context.Background()
	db := sankeyTestDB(t)
	owner := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner)

	now := time.Now()
	sub := now.AddDate(0, 0, -2)
	if err := seedAppWithStages(ctx, db, owner, domain.StatusRejected, &sub,
		[]string{domain.StatusApplied, domain.StatusRejected}); err != nil {
		t.Fatal(err)
	}

	sk, err := New(db).SankeyA(ctx, &SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	links := linkIndex(sk)
	if links[[2]string{"s_applied", "s_rejected"}] != 1 {
		t.Fatalf("applied → rejected = %d, want 1", links[[2]string{"s_applied", "s_rejected"}])
	}
	for _, n := range sk.Nodes {
		if n.Name == "s_assessment" {
			t.Fatal("skipped 笔试 must not invent an assessment node")
		}
	}
}

func TestSankeyACurrentAssessmentIsLeaf(t *testing.T) {
	ctx := context.Background()
	db := sankeyTestDB(t)
	owner := createBenchUser(t, db)
	defer cleanupBench(ctx, db, owner)

	now := time.Now()
	sub := now.AddDate(0, 0, -1)
	if err := seedAppWithStages(ctx, db, owner, domain.StatusAssessment, &sub,
		[]string{domain.StatusApplied, domain.StatusAssessment}); err != nil {
		t.Fatal(err)
	}

	sk, err := New(db).SankeyA(ctx, &SnapshotRequest{OwnerID: owner, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	links := linkIndex(sk)
	if links[[2]string{"s_applied", "s_assessment"}] != 1 {
		t.Fatalf("applied → assessment = %d, want 1", links[[2]string{"s_applied", "s_assessment"}])
	}
	for k, v := range links {
		if k[0] == "s_assessment" && v > 0 {
			t.Fatalf("still-in-OA row must stop at assessment, found %s → %s = %d", k[0], k[1], v)
		}
	}
}

func createBenchUser(t *testing.T, db *database.DB) int64 {
	t.Helper()
	var id int64
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO users(email, password_hash, display_name, timezone) VALUES($1,'x','Sankey',$2) RETURNING id`,
		"sk_"+time.Now().Format("150405.000000000")+"@test.local", "Europe/Dublin").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func cleanupBench(ctx context.Context, db *database.DB, owner int64) {
	_, _ = db.Pool().Exec(ctx, `DELETE FROM applications WHERE owner_id=$1`, owner)
	_, _ = db.Pool().Exec(ctx, `DELETE FROM companies WHERE owner_id=$1`, owner)
	_, _ = db.Pool().Exec(ctx, `DELETE FROM users WHERE id=$1`, owner)
}

func seedApp(ctx context.Context, db *database.DB, owner int64, status string, sub, resp *time.Time) error {
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var cid int64
	name := "Co" + time.Now().Format("150405.000000000")
	if err := tx.QueryRow(ctx, `INSERT INTO companies(owner_id, name) VALUES($1,$2) RETURNING id`,
		owner, name).Scan(&cid); err != nil {
		return err
	}
	var aid int64
	if err := tx.QueryRow(ctx, `INSERT INTO applications(owner_id, company_id, company_name, position, status, submitted_at, first_response_at, custom_values, saved_at)
		VALUES($1,$2,'C','P',$3,$4,$5,'{}',now()) RETURNING id`, owner, cid, status, sub, resp).Scan(&aid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO application_events(application_id, owner_id, sequence, event_type, from_status, to_status, occurred_at)
		VALUES($1,$2,1,'created',NULL,$3,now())`, aid, owner, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// seedAppWithStages writes a real timeline so Sankey A can reconstruct the
// process stages (seedApp's single created→current event has no intermediates).
func seedAppWithStages(ctx context.Context, db *database.DB, owner int64, current string, sub *time.Time, stages []string) error {
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var cid int64
	name := "Co" + time.Now().Format("150405.000000000")
	if err := tx.QueryRow(ctx, `INSERT INTO companies(owner_id, name) VALUES($1,$2) RETURNING id`,
		owner, name).Scan(&cid); err != nil {
		return err
	}
	var aid int64
	if err := tx.QueryRow(ctx, `INSERT INTO applications(owner_id, company_id, company_name, position, status, submitted_at, custom_values, saved_at)
		VALUES($1,$2,'C','P',$3,$4,'{}',now()) RETURNING id`, owner, cid, current, sub).Scan(&aid); err != nil {
		return err
	}
	base := time.Now().Add(-time.Hour)
	for i, st := range stages {
		eventType := "created"
		var from any
		if i > 0 {
			eventType = "status_change"
			from = stages[i-1]
		}
		if _, err := tx.Exec(ctx, `INSERT INTO application_events(application_id, owner_id, sequence, event_type, from_status, to_status, occurred_at)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, aid, owner, i+1, eventType, from, st, base.Add(time.Duration(i)*time.Minute)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func linkIndex(sk *Sankey) map[[2]string]int64 {
	m := map[[2]string]int64{}
	for _, l := range sk.Links {
		m[[2]string{l.Source, l.Target}] = l.Value
	}
	return m
}

func leafInflow(sk *Sankey) int64 {
	outflow := map[string]int64{}
	inflow := map[string]int64{}
	for _, l := range sk.Links {
		outflow[l.Source] += l.Value
		inflow[l.Target] += l.Value
	}
	var sum int64
	for _, n := range sk.Nodes {
		if outflow[n.Name] == 0 {
			sum += inflow[n.Name]
		}
	}
	return sum
}
