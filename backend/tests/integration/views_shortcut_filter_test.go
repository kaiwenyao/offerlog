// 侧栏快捷视图的筛选能力（/views/query 的 archived / id-in 字段与日期比较）。
//
// 原始 bug：侧栏「已归档」筛的是「已结束状态」，而归档是可见性旗标——归档一个
// 进行中的岗位后，它在「已归档」里反而找不到。修复后 archived 是白名单里的独立
// 字段（archived_at IS NOT NULL），「本周面试」用 id in (...) 过滤前端算出的岗位
// 集合。这里用真 PostgreSQL 锁住三件事：archived 的真假两向、id 集合（含空集合）、
// 以及「待跟进」依赖的 next_action_due_at 日期比较真的能在 SQL 层工作。
package integration

import (
	"context"
	"testing"
	"time"

	appservice "offerlog/backend/internal/applications/service"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/day"
	"offerlog/backend/internal/views"
	viewrepo "offerlog/backend/internal/views/repository"
	vservice "offerlog/backend/internal/views/service"
)

// queryAppIDs runs /views/query's service path and returns the matched ids.
func queryAppIDs(t *testing.T, db *database.DB, owner int64, filters []views.FilterNode) []int64 {
	t.Helper()
	vs := vservice.New(db, viewrepo.New(db))
	items, _, _, err := vs.RunQuery(context.Background(), owner, filters, nil, 1, 100)
	if err != nil {
		t.Fatalf("query %#v: %v", filters, err)
	}
	ids := make([]int64, 0, len(items))
	for _, r := range items {
		ids = append(ids, r["id"].(int64))
	}
	return ids
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// 归档是旗标而不是状态：进行中的岗位归档后必须出现在「已归档」，且不再是「进行中」。
func TestArchivedFilterUsesArchiveFlagNotEndedStatus(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()

	app := mustCreate(t, svc, owner, "归档公司", "工程师")
	// 推进到「已投递」（进行中），正是用户描述的场景。
	submitted := time.Now().Add(-72 * time.Hour)
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: "applied", Version: app.Version, SubmittedAt: &submitted, IdempotencyKey: "k-arch",
	}); err != nil {
		t.Fatalf("transition to applied: %v", err)
	}
	if err := svc.Archive(ctx, owner, app.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	archivedTrue := []views.FilterNode{{Field: "archived", Op: "eq", Value: true}}
	if ids := queryAppIDs(t, db, owner, archivedTrue); !containsID(ids, app.ID) {
		t.Fatalf("已归档 must contain the archived in-progress application, got %v", ids)
	}
	archivedFalse := []views.FilterNode{{Field: "archived", Op: "eq", Value: false}}
	if ids := queryAppIDs(t, db, owner, archivedFalse); containsID(ids, app.ID) {
		t.Fatalf("未归档 must not contain the archived row, got %v", ids)
	}

	// 关键回归：这条岗位是「进行中」而不是「已结束」。旧实现（按已结束状态筛）
	// 找不到它——所以这个断言在旧代码下必然失败。
	ended := []views.FilterNode{statusFilter("accepted", "rejected", "withdrawn", "closed")}
	if ids := queryAppIDs(t, db, owner, ended); containsID(ids, app.ID) {
		t.Fatalf("已归档 must not be derived from ended statuses, got %v", ids)
	}
	// 取消归档后回到「未归档」，且不再出现在「已归档」。
	if err := svc.Archive(ctx, owner, app.ID, false); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if ids := queryAppIDs(t, db, owner, archivedTrue); containsID(ids, app.ID) {
		t.Fatalf("取消归档后不应出现在已归档, got %v", ids)
	}
	if ids := queryAppIDs(t, db, owner, archivedFalse); !containsID(ids, app.ID) {
		t.Fatalf("取消归档后应回到未归档, got %v", ids)
	}
}

// 本周面试：id 集合过滤（前端按用户时区的周窗口从 /calendar 算出）。
func TestWeekInterviewIDFilter(t *testing.T) {
	db, svc, _, owner := setup(t)

	a := mustCreate(t, svc, owner, "本周面试A", "R")
	b := mustCreate(t, svc, owner, "本周面试B", "R")
	_ = mustCreate(t, svc, owner, "本周面试C", "R")

	inSet := []views.FilterNode{{
		Op: "and", Conditions: []views.FilterNode{
			{Field: "id", Op: "in", Value: []any{float64(a.ID), float64(b.ID)}},
			{Field: "archived", Op: "eq", Value: false},
		},
	}}
	ids := queryAppIDs(t, db, owner, inSet)
	if len(ids) != 2 || !containsID(ids, a.ID) || !containsID(ids, b.ID) {
		t.Fatalf("id in (...) 应只返回两条, got %v", ids)
	}

	// 本周没有面试时，前端传空集合：必须匹配 0 条，而不是编译出 `IN ()` 让 SQL 报错。
	empty := []views.FilterNode{{Field: "id", Op: "in", Value: []any{}}}
	if got := queryAppIDs(t, db, owner, empty); len(got) != 0 {
		t.Fatalf("空 id 集合应匹配 0 条, got %v", got)
	}
}

// 待跟进：进行中 + 未归档 +（没有下一步 或 下一步已到期/逾期）。
func TestFollowUpFilterMatchesMissingOrOverdueNextAction(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()

	loc, err := time.LoadLocation("Europe/Dublin")
	if err != nil {
		t.Fatal(err)
	}
	todayKey := time.Now().In(loc).Format("2006-01-02")
	today, err := day.Parse(todayKey)
	if err != nil {
		t.Fatal(err)
	}
	overdue := today.AddDate(0, 0, -1)
	future := today.AddDate(0, 0, 3)
	overdueKey, futureKey := overdue.Format("2006-01-02"), future.Format("2006-01-02")

	mk := func(company string) int64 {
		app := mustCreate(t, svc, owner, company, "R")
		submitted := time.Now().Add(-48 * time.Hour)
		if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
			ToStatus: "applied", Version: app.Version, SubmittedAt: &submitted, IdempotencyKey: "k-" + company,
		}); err != nil {
			t.Fatalf("transition %s: %v", company, err)
		}
		return app.ID
	}
	setNext := func(id int64, action string, due *time.Time) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx,
			`UPDATE applications SET next_action=$2, next_action_due_at=$3 WHERE id=$1`, id, action, due); err != nil {
			t.Fatal(err)
		}
	}
	// 完成 / 撤销走真实端点：镜像的清空与「不复活」都是它决定的。
	srv := newActivityServer(t, db, owner)
	defer srv.Close()

	noPlan := mk("没安排下一步") // 命中：没有任何未完成待办
	tooLate := mk("逾期")    // 命中：有安排但已逾期
	onTrack := mk("还早")    // 不命中：安排在未来
	setNext(noPlan, "", nil)
	setNext(tooLate, "回访 HR", &overdue)
	setNext(onTrack, "回访 HR", &future)

	// 真实的未完成待办（actions 行，而不是镜像）：
	// - 逾期的那条必须进待跟进（只看镜像时它会漏掉：镜像可能是空的）；
	// - 未来的那条不进。
	realOverdue := mk("有逾期待办")
	insertAction(t, db, realOverdue, owner, "今天必须做", ptr(overdueKey))
	realFuture := mk("有未来待办")
	insertAction(t, db, realFuture, owner, "下周再说", ptr(futureKey))

	// PR #42 review P2 场景一：未来才到期的待办被「完成 → 撤销」后，岗位行镜像已
	// 被清空（reopen 故意不复活它），而 actions 里仍有一条未来的未完成待办。
	// 按镜像判断会把它当成「没有下一步安排」而误入待跟进。
	reopened := mk("完成又撤销")
	reopenedAction := insertAction(t, db, reopened, owner, "未来再跟进", ptr(futureKey))
	postActionDone(t, srv.URL, reopenedAction, true)
	postActionDone(t, srv.URL, reopenedAction, false)

	// PR #42 review P2 场景二：镜像还停在一条已完成的旧待办上（且已逾期），但同时
	// 还有一条未来的未完成待办。按镜像的 due 判断会误入待跟进。
	stale := mk("镜像指向已完成")
	staleDone := insertAction(t, db, stale, owner, "已完成的旧待办", ptr(overdueKey))
	insertAction(t, db, stale, owner, "未来的一步", ptr(futureKey))
	setNext(stale, "已完成的旧待办", &overdue)
	postActionDone(t, srv.URL, staleDone, true)

	// 非进行中的岗位（待投递）也有「没下一步」，但不该出现在待跟进里。
	draft := mustCreate(t, svc, owner, "待投递草稿", "R")

	// 归档过的进行中岗位同样不该出现在待跟进里（即使它有逾期待办）。
	archived := mk("已归档进行中")
	setNext(archived, "", nil)
	insertAction(t, db, archived, owner, "归档前的逾期事", ptr(overdueKey))
	if err := svc.Archive(ctx, owner, archived, true); err != nil {
		t.Fatal(err)
	}

	ids := queryAppIDs(t, db, owner, followUpFilter(todayKey))
	for _, want := range []struct {
		name string
		id   int64
	}{
		{"没安排下一步", noPlan},
		{"逾期的镜像待办", tooLate},
		{"逾期的真实待办", realOverdue},
	} {
		if !containsID(ids, want.id) {
			t.Fatalf("待跟进应包含「%s」, got %v", want.name, ids)
		}
	}
	for _, unwanted := range []struct {
		name string
		id   int64
	}{
		{"下一步还在未来（镜像）", onTrack},
		{"未来的真实待办", realFuture},
		{"完成又撤销的未来待办", reopened},
		{"镜像停在已完成旧待办但另有未来待办", stale},
		{"未投递的草稿", draft.ID},
		{"已归档的岗位", archived},
	} {
		if containsID(ids, unwanted.id) {
			t.Fatalf("待跟进不应包含「%s」, got %v", unwanted.name, ids)
		}
	}
}

// followUpFilter mirrors frontend shortcutConditions(FOLLOW_UP_VIEW, {today}).
func followUpFilter(todayKey string) []views.FilterNode {
	return []views.FilterNode{{
		Op: "and", Conditions: []views.FilterNode{
			statusFilter("applied", "screening", "assessment", "interviewing"),
			{Field: "archived", Op: "eq", Value: false},
			// 只用 lte：DATE 列上的 is_not_empty 会拼出 `<> ''`，PostgreSQL 直接报错。
			{Op: "or", Conditions: []views.FilterNode{
				{Field: "open_todo_count", Op: "eq", Value: float64(0)},
				{Field: "next_open_todo_due", Op: "lte", Value: todayKey},
			}},
		},
	}}
}

func statusFilter(statuses ...string) views.FilterNode {
	conds := make([]views.FilterNode, 0, len(statuses))
	for _, s := range statuses {
		conds = append(conds, views.FilterNode{Field: "status", Op: "eq", Value: s})
	}
	return views.FilterNode{Op: "or", Conditions: conds}
}
