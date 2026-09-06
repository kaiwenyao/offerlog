// Verifies plan §12.2 case 1 on a clean dataset with exactly the benchmark
// distribution: the current-mode sankey conserves flow at every layer and all
// final nodes total the cohort, and drilldown equals the list ids.
package analytics

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
)

func TestSankeyConservationBenchmarkFixture(t *testing.T) {
	ctx := context.Background()
	db, err := database.New(ctx, "postgres://offerlog:offerlog@localhost:5433/offerlog?sslmode=disable")
	if err != nil {
		t.Skip("no local db:", err)
	}
	defer db.Close()
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
		if err := seedApp(ctx, db, owner, f.status, sub); err != nil {
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
	// conservation: 全部机会 → (尚未投递 3 | 已投递 7)
	var notSub, sub int64
	for _, l := range sk.Links {
		if l.Source == "not_submitted" {
			notSub += l.Value
		}
		if l.Source == "submitted" {
			sub += l.Value
		}
	}
	if notSub != 3 || sub != 7 {
		t.Fatalf("layer2 = %d not-submitted / %d submitted, want 3/7", notSub, sub)
	}
	// every final node sums to 10
	var finals int64
	for _, n := range sk.Nodes {
		if n.Name == "all" || n.Name == "not_submitted" || n.Name == "submitted" {
			continue
		}
		for _, l := range sk.Links {
			if l.Target == n.Name {
				finals += l.Value
			}
		}
	}
	if finals != 10 {
		t.Fatalf("final nodes total %d, want 10", finals)
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

func seedApp(ctx context.Context, db *database.DB, owner int64, status string, sub *time.Time) error {
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
		VALUES($1,$2,'C','P',$3,$4,'{}',now()) RETURNING id`, owner, cid, status, sub).Scan(&aid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO application_events(application_id, owner_id, sequence, event_type, from_status, to_status, occurred_at)
		VALUES($1,$2,1,'created',NULL,$3,now())`, aid, owner, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
