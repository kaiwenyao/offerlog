// 未来时间的时间线事件是「计划」，不是「已经发生」。
//
// 原始 bug：推导规则只看排序（业务时间升序、时间未定的排最后），所以记一个
// 「12/25 一面」之后，今天再记「被拒」也不会改变状态——岗位永远停在面试中，终态
// 的备注也进不了「原因」卡片，看板列、统计漏斗、「已结束」视图跟着一起错。
//
// 现在的规则是「最后一个**已经发生**的事件」，时间到了之后由 worker 的每日
// RecomputeDueSince 扫描补算（见本文件最后一个用例与 cmd/worker）。
package integration

import (
	"context"
	"testing"
	"time"

	apprepo "offerlog/backend/internal/applications/repository"
)

func TestFutureMilestoneDoesNotOutrankWhatActuallyHappened(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "FutureCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	now := time.Now()

	milestoneAt(t, srv.URL, base, "apply", now.Add(-10*24*time.Hour))
	// 提前记下已经排好的面试。
	milestoneAt(t, srv.URL, base, "interview", now.Add(90*24*time.Hour))

	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "applied" {
		t.Fatalf("status = %s, want applied（三个月后的面试还没发生）", row.Status)
	}

	// 今天被拒了。
	milestoneAt(t, srv.URL, base, "reject", now.Add(-time.Hour))

	row, err = svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "rejected" {
		t.Fatalf("status = %s, want rejected（真实发生的事必须能推进状态）", row.Status)
	}
}

// 工序条与「最近一次进入当前进度的日期」跟状态用同一把尺。
func TestFutureMilestoneStaysOutOfStageHistory(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	repo := apprepo.New(db)
	app := mustCreate(t, svc, owner, "RailCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	now := time.Now()

	milestoneAt(t, srv.URL, base, "apply", now.Add(-10*24*time.Hour))
	milestoneAt(t, srv.URL, base, "interview", now.Add(90*24*time.Hour))

	sum, err := repo.TimelineSummaryFor(ctx, owner, []int64{app.ID}, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	if day := sum.StageHistory[app.ID]["interviewing"]; day != "" {
		t.Errorf("stage history 记了未来的到达日 %q——工序条会把还没发生的面试画成「曾经历」", day)
	}
	if sum.StageHistory[app.ID]["applied"] == "" {
		t.Error("已经发生的投递必须留在工序条上")
	}
}

// 时间到了之后，用户不必再动一次时间线：每日扫描把它补算进来。
func TestRecomputeDueSinceAdvancesAPointThatJustBecamePast(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	repo := apprepo.New(db)
	app := mustCreate(t, svc, owner, "CatchUpCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	now := time.Now()

	milestoneAt(t, srv.URL, base, "apply", now.Add(-10*24*time.Hour))
	mid := milestoneAt(t, srv.URL, base, "interview", now.Add(2*time.Hour))

	row, _ := svc.Get(ctx, owner, app.ID, false)
	if row.Status != "applied" {
		t.Fatalf("status = %s, want applied（面试还有两小时才开始）", row.Status)
	}

	// 时间过去了（直接改库，等价于「两小时之后」）。
	if _, err := db.Pool().Exec(ctx, `UPDATE application_milestones SET occurred_at=$1 WHERE id=$2`,
		now.Add(-time.Minute), mid); err != nil {
		t.Fatal(err)
	}
	// 快照仍是旧的：没有任何写入路径碰过这条记录。
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Status != "applied" {
		t.Fatalf("status = %s, want the stale applied snapshot before the catch-up", row.Status)
	}

	// 一条状态没变的邻居：扫描不能顺手 bump 它的 version，否则任何开着编辑表单
	// 的人第二天都会撞上 409（PATCH /applications/:id 走乐观锁）。
	quiet := mustCreate(t, svc, owner, "QuietCo", "后端工程师")
	quietBase := "/api/v1/applications/" + itoa(quiet.ID)
	milestoneAt(t, srv.URL, quietBase, "apply", now.Add(-time.Hour))
	before, _ := svc.Get(ctx, owner, quiet.ID, false)

	n, err := repo.RecomputeDueSince(ctx, 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("catch-up recomputed nothing")
	}
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Status != "interviewing" {
		t.Fatalf("status = %s, want interviewing after the catch-up pass", row.Status)
	}
	after, _ := svc.Get(ctx, owner, quiet.ID, false)
	if after.Version != before.Version {
		t.Errorf("version of an unchanged record moved %d → %d", before.Version, after.Version)
	}
}
