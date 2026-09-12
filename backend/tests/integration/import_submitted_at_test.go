// 投递时间必须活过任何一次时间线回放。
//
// 迁移 00006 之后 applications.submitted_at 是**派生**列：RecomputeStatus 用
// 时间线上第一个「已投递」落点回填它。于是任何「只写列、不留落点」的建档路径
// 都埋了一颗地雷——用户下一次加／改／删一个时间线节点，真实投递时间就被换成
// 建档时刻，甚至直接清空；而投递队列、等待回复提醒、分析漏斗读的都是这一列。
//
// 契约：带投递时间建档 ⇒ 时间线上一定有一个业务时间等于它的「已投递」落点。
package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"offerlog/backend/internal/applications/domain"
	appservice "offerlog/backend/internal/applications/service"
	"offerlog/backend/internal/transfers"
)

func dayOf(ts *time.Time) string {
	if ts == nil {
		return "<nil>"
	}
	return ts.UTC().Format("2006-01-02")
}

// 导入 + 之后在时间线上加一个节点：投递时间不能变成导入时刻。
func TestImportedSubmittedAtSurvivesATimelineEdit(t *testing.T) {
	// Arrange
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	submitted := time.Now().AddDate(0, 0, -2)
	want := submitted.Format("2006-01-02")
	csvData := "公司,岗位,状态,投递时间\nImportApplied,后端,已投递," + want + "\n"

	tr := transfers.New(db)
	pv, err := tr.ParseCSV(ctx, owner, "x.csv", stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.CommitImport(ctx, owner, pv.BatchID, stringsNewReader(csvData)); err != nil {
		t.Fatal(err)
	}
	var appID int64
	if err := db.Pool().QueryRow(ctx, `SELECT id FROM applications WHERE owner_id=$1 AND company_name='ImportApplied'`,
		owner).Scan(&appID); err != nil {
		t.Fatal(err)
	}
	if row, _ := svc.Get(ctx, owner, appID, false); dayOf(row.SubmittedAt) != want {
		t.Fatalf("submitted_at right after import = %s, want %s", dayOf(row.SubmittedAt), want)
	}

	// Act: 用户在时间线上补一个面试节点（任何节点写入都会触发回放）。
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(appID)
	body := `{"kind":"interview","occurred_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	if code, out := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones", body); code != http.StatusCreated {
		t.Fatalf("create milestone = %d (%v)", code, out)
	}

	// Assert
	row, err := svc.Get(ctx, owner, appID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := dayOf(row.SubmittedAt); got != want {
		t.Errorf("submitted_at after the timeline edit = %s, want the imported %s", got, want)
	}
}

// 导入一条已经走到面试的记录：回放既要留住投递时间，也不能把阶段退回去。
func TestImportedLaterStageKeepsItsSubmittedAt(t *testing.T) {
	// Arrange
	db, svc, repo, owner := setup(t)
	ctx := context.Background()
	want := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	csvData := "公司,岗位,状态,投递时间\nImportLater,后端,面试中," + want + "\n"

	tr := transfers.New(db)
	pv, err := tr.ParseCSV(ctx, owner, "x.csv", stringsNewReader(csvData))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.CommitImport(ctx, owner, pv.BatchID, stringsNewReader(csvData)); err != nil {
		t.Fatal(err)
	}
	var appID int64
	if err := db.Pool().QueryRow(ctx, `SELECT id FROM applications WHERE owner_id=$1 AND company_name='ImportLater'`,
		owner).Scan(&appID); err != nil {
		t.Fatal(err)
	}

	// Act: 回放（任何节点写入、更正都会走到这里）。
	if err := repo.RecomputeStatus(ctx, db.Pool(), appID, owner); err != nil {
		t.Fatal(err)
	}

	// Assert
	row, err := svc.Get(ctx, owner, appID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := dayOf(row.SubmittedAt); got != want {
		t.Errorf("submitted_at after replay = %s, want the imported %s", got, want)
	}
	if row.Status != domain.StatusInterviewing {
		t.Errorf("status after replay = %s, want interviewing", row.Status)
	}
}

// 直接建在「面试中」的记录（API 允许）同样只有列、没有落点。
func TestCreateBeyondAppliedKeepsItsSubmittedAt(t *testing.T) {
	// Arrange
	db, svc, repo, owner := setup(t)
	ctx := context.Background()
	submitted := time.Now().AddDate(0, 0, -3)
	want := submitted.Format("2006-01-02")
	app, err := svc.Create(ctx, owner, &appservice.CreateInput{
		CompanyName: "SkipAheadCo", Position: "Role",
		Status: domain.StatusInterviewing, SubmittedAt: &submitted,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Act
	if err := repo.RecomputeStatus(ctx, db.Pool(), app.ID, owner); err != nil {
		t.Fatal(err)
	}

	// Assert
	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := dayOf(row.SubmittedAt); got != want {
		t.Errorf("submitted_at after replay = %s, want the user's %s", got, want)
	}
	if row.Status != domain.StatusInterviewing {
		t.Errorf("status after replay = %s, want interviewing", row.Status)
	}
}
