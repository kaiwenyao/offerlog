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

	noPlan := mk("没安排下一步")            // 命中：缺 next_action
	tooLate := mk("逾期")                 // 命中：有安排但已逾期
	onTrack := mk("还早")                 // 不命中：安排在未来
	setNext(noPlan, "", nil)
	setNext(tooLate, "回访 HR", &overdue)
	setNext(onTrack, "回访 HR", &future)

	// 非进行中的岗位（待投递）也有「没下一步」，但不该出现在待跟进里。
	draft := mustCreate(t, svc, owner, "待投递草稿", "R")

	// 归档过的进行中岗位同样不该出现在待跟进里。
	archived := mk("已归档进行中")
	setNext(archived, "", nil)
	if err := svc.Archive(ctx, owner, archived, true); err != nil {
		t.Fatal(err)
	}

	ids := queryAppIDs(t, db, owner, followUpFilter(todayKey))
	if !containsID(ids, noPlan) || !containsID(ids, tooLate) {
		t.Fatalf("待跟进应包含「没安排下一步」和「已逾期」的进行中岗位, got %v", ids)
	}
	if containsID(ids, onTrack) {
		t.Fatalf("待跟进不应包含下一步还在未来的岗位, got %v", ids)
	}
	if containsID(ids, draft.ID) {
		t.Fatalf("待跟进不应包含未投递的草稿, got %v", ids)
	}
	if containsID(ids, archived) {
		t.Fatalf("待跟进不应包含已归档的岗位, got %v", ids)
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
				{Field: "next_action", Op: "is_empty"},
				{Field: "next_action_due_at", Op: "lte", Value: todayKey},
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
