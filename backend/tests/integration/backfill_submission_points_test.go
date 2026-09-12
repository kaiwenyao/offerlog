// 迁移 00007 的存量修复：导入代码修好之前建下的记录，时间线上也要补出投递落点，
// 否则它们的 submitted_at 仍然会在下一次回放时被顶掉或清空。
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/applications/domain"
	appservice "offerlog/backend/internal/applications/service"
	"offerlog/backend/internal/platform/database"
)

const backfillMigrationPath = "../../internal/platform/migrate/migrations/00007_backfill_submission_stage_points.sql"

// oldStyleImport recreates exactly what the pre-fix importer wrote: the real
// submission time on the column only, plus one created event stamped now().
func oldStyleImport(t *testing.T, db *database.DB, owner int64, company, status string, submitted, importedAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	var companyID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO companies(owner_id, name) VALUES($1,$2) RETURNING id`,
		owner, company).Scan(&companyID); err != nil {
		t.Fatal(err)
	}
	var appID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO applications(owner_id, company_id, company_name, position,
		status, submitted_at, saved_at) VALUES($1,$2,$3,'Role',$4,$5,$6) RETURNING id`,
		owner, companyID, company, status, submitted, importedAt).Scan(&appID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO application_events(application_id, owner_id, sequence,
		event_type, from_status, to_status, note, occurred_at)
		VALUES($1,$2,1,'created',NULL,$3,'导入创建',$4)`, appID, owner, status, importedAt); err != nil {
		t.Fatal(err)
	}
	return appID
}

func runBackfillMigration(t *testing.T, db *database.DB) {
	t.Helper()
	sql, err := os.ReadFile(backfillMigrationPath)
	if err != nil {
		t.Fatal(err)
	}
	// One transaction, like the migration runner: the temp table is ON COMMIT DROP.
	if err := db.RunInTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, string(sql))
		return err
	}); err != nil {
		t.Fatalf("backfill migration: %v", err)
	}
}

func TestBackfillRepairsOldImportedSubmissions(t *testing.T) {
	// Arrange: 两条旧式导入记录——一条停在「已投递」，一条已经走到面试。
	db, svc, repo, owner := setup(t)
	ctx := context.Background()
	importedAt := time.Now().Add(-time.Hour)
	submitted := importedAt.AddDate(0, 0, -4)
	appliedID := oldStyleImport(t, db, owner, "OldApplied", domain.StatusApplied, submitted, importedAt)
	laterID := oldStyleImport(t, db, owner, "OldInterviewing", domain.StatusInterviewing, submitted, importedAt)

	// Act: 迁移 + 一次回放（任何节点写入都会触发）。
	runBackfillMigration(t, db)
	for _, id := range []int64{appliedID, laterID} {
		if err := repo.RecomputeStatus(ctx, db.Pool(), id, owner); err != nil {
			t.Fatal(err)
		}
	}

	// Assert
	want := submitted.UTC().Format("2006-01-02")
	for _, tc := range []struct {
		id     int64
		status string
	}{{appliedID, domain.StatusApplied}, {laterID, domain.StatusInterviewing}} {
		row, err := svc.Get(ctx, owner, tc.id, false)
		if err != nil {
			t.Fatal(err)
		}
		if got := dayOf(row.SubmittedAt); got != want {
			t.Errorf("%s: submitted_at after replay = %s, want the imported %s", tc.status, got, want)
		}
		if row.Status != tc.status {
			t.Errorf("status after replay = %s, want %s (修复不能改变阶段)", row.Status, tc.status)
		}
	}
}

// 被用户动过的记录一概不碰，迁移重复执行也不能再写一遍。
func TestBackfillIsIdempotentAndLeavesEditedRowsAlone(t *testing.T) {
	// Arrange
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	importedAt := time.Now().Add(-time.Hour)
	submitted := importedAt.AddDate(0, 0, -4)
	appID := oldStyleImport(t, db, owner, "OldIdem", domain.StatusApplied, submitted, importedAt)

	// 另一条「已经被编辑过」的记录：正常建档 + 一个用户节点。
	edited, err := svc.Create(ctx, owner, &appservice.CreateInput{
		CompanyName: "EditedCo", Position: "Role",
		Status: domain.StatusApplied, SubmittedAt: &submitted,
	})
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore := countEvents(t, db, edited.ID)

	// Act
	runBackfillMigration(t, db)
	runBackfillMigration(t, db)

	// Assert
	if got, want := countEvents(t, db, appID), 2; got != want {
		t.Errorf("repaired row has %d events, want %d (重复执行不能再补一遍)", got, want)
	}
	if got := countEvents(t, db, edited.ID); got != eventsBefore {
		t.Errorf("已经正常建档的记录事件数 %d → %d，不该被修改", eventsBefore, got)
	}
}

func countEvents(t *testing.T, db *database.DB, appID int64) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM application_events WHERE application_id=$1`, appID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
