// 两个「改不了 / 改了没反应」的回归：
//
//  1. 回收站里的记录不能被批量操作命中。前端在回收站模式下隐藏了勾选列，但旧
//     页面 / 脚本仍可能带着已删除的 id 发请求 —— 后端按 id 取行（GetForUpdate），
//     于是「批量打标签」会静默成功，而列表上什么变化都看不到。Bulk 现在跳过
//     deleted_at 非空的行，同时也跳过它们不计入 updated。
//
//  2. 清空 OA 截止时间是一个明确的用户意图（截止日填错了）。PATCH 的合并语义把
//     「没传」和「传 null」都当成「保留原值」（见
//     TestPatchAssessmentPreservesUnmentionedTimes），所以清除必须有专门的手段：
//     显式的 clear_due_at 旗标。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	actrepo "offerlog/backend/internal/activities/repository"
	"offerlog/backend/internal/applications/domain"
	appservice "offerlog/backend/internal/applications/service"
)

// Bulk 必须放过回收站（软删）里的行：勾选列已隐藏，但那批 id 仍可能被发上来，
// 而已删除记录被改「看起来就像点了没反应」。
func TestBulkSkipsSoftDeletedApplications(t *testing.T) {
	_, svc, repo, owner := setup(t)
	ctx := context.Background()

	alive := mustCreate(t, svc, owner, "AliveCo", "Backend")
	trashed := mustCreate(t, svc, owner, "TrashCo", "Backend")
	if err := svc.SoftDelete(ctx, owner, trashed.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	high := "high"
	n, err := svc.Bulk(ctx, owner, []int64{alive.ID, trashed.ID}, []string{"批量"}, &high, nil)
	if err != nil {
		t.Fatalf("bulk: %v", err)
	}
	if n != 1 {
		t.Fatalf("bulk updated %d rows, want 1 (the trashed row must be skipped)", n)
	}

	// 在库的那条真的改了。
	got, err := repo.GetByID(ctx, owner, alive.ID, false)
	if err != nil {
		t.Fatalf("reload alive: %v", err)
	}
	if got.Priority != "high" {
		t.Errorf("alive priority = %q, want high", got.Priority)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "批量" {
		t.Errorf("alive tags = %v, want [批量]", got.Tags)
	}

	// 回收站里的那条一个字都没动（标签 / 优先级都保持原样）。
	trashedRow, err := repo.GetByID(ctx, owner, trashed.ID, true)
	if err != nil {
		t.Fatalf("reload trashed: %v", err)
	}
	if len(trashedRow.Tags) != 0 {
		t.Errorf("trashed tags = %v, want none (bulk must not touch the recycle bin)", trashedRow.Tags)
	}
	if trashedRow.Priority == "high" {
		t.Errorf("trashed priority = %q, must not be updated by bulk", trashedRow.Priority)
	}
	// 软删状态本身不能被批量操作顺带清掉（否则回收站会莫名空掉）。
	if trashedRow.DeletedAt == nil {
		t.Errorf("trashed row is no longer deleted — bulk must not restore rows")
	}
}

// 用户把 OA 截止时间清空后必须真的清掉：显式 clear_due_at 是唯一入口，
// 传 due_at:null 仍然按「没填」处理（保留原值）。
func TestPatchAssessmentClearDueAtIsDeliberate(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ClearDue", "数据工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(2),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubPreparing, Version: 2,
		Assessment: &appservice.AssessmentInput{
			Kind: "online_test", Name: "OA",
			DueAt: submittedAt(-2),
		},
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	oaID := *row.FocusActivityID

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	// 改截止时间：新的时间落库（不是「改不了」）。
	req, _ := http.NewRequest("PATCH", srv.URL+base+"/assessments/"+itoa(oaID),
		bytes.NewBufferString(`{"name":"OA","due_at":"2026-12-01T10:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("patch due_at status = %d, want 200", res.StatusCode)
	}
	round, err := actrepo.New(db).GetAssessment(ctx, db, app.ID, owner, oaID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if round.DueAt == nil || round.DueAt.UTC().Format("2006-01-02") != "2026-12-01" {
		t.Fatalf("due_at = %v, want 2026-12-01", round.DueAt)
	}

	// 清空截止时间：只有 clear_due_at 能真正做到（前端清空输入框时就是这个）。
	req, _ = http.NewRequest("PATCH", srv.URL+base+"/assessments/"+itoa(oaID),
		bytes.NewBufferString(`{"name":"OA","clear_due_at":true}`))
	req.Header.Set("Content-Type", "application/json")
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.NewDecoder(res2.Body).Decode(&body)
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("clear patch status = %d, want 200", res2.StatusCode)
	}
	if body["due_at"] != nil {
		t.Errorf("response due_at = %v, want null after clear_due_at", body["due_at"])
	}
	round, err = actrepo.New(db).GetAssessment(ctx, db, app.ID, owner, oaID)
	if err != nil {
		t.Fatalf("reload after clear: %v", err)
	}
	if round.DueAt != nil {
		t.Errorf("due_at = %v, want nil after clear_due_at (otherwise the UI lies: field empty, value back)", round.DueAt)
	}
}

// 取消面试必须「有入口、看得见、撤得回」。
//
// 这条锁住 listInterviews 的一个缺口：以前它给每个轮次传 nil 的日程元数据，
// 于是 cancelled 永远到不了前端——刚取消的面试在详情里看起来毫无变化，也没有
// 任何办法恢复（只能再排一轮，而错的那轮仍在）。现在列表会带上 schedule，
// cancelled 与 cancelled_reason 都能读回，恢复后也立刻回到未取消。
func TestInterviewCancelIsVisibleAndReversibleInList(t *testing.T) {
	db, svc, _, owner := setup(t)
	app := mustCreate(t, svc, owner, "CancelVisible", "前端工程师")

	srv := newFullActivityServer(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	status, body := postActivityJSON(t, srv, base+"/interviews",
		`{"round_name":"一面","format":"video","scheduled_at":"2026-11-02T06:00:00Z","timezone":"Asia/Shanghai"}`)
	if status != http.StatusCreated {
		t.Fatalf("create interview -> %d body=%v", status, body)
	}
	iid := int64(body["id"].(float64))

	// 取消（前端「取消面试」按钮打的就是这个接口）。
	cancelStatus, _ := postActivityJSON(t, srv, base+"/interviews/"+itoa(iid)+"/cancel", `{"reason":"对方改期"}`)
	if cancelStatus != http.StatusOK {
		t.Fatalf("cancel -> %d, want 200", cancelStatus)
	}

	// 列表里必须能读到 cancelled —— 否则 UI 既显示不出「已取消」，也无从恢复。
	schedule := interviewScheduleInList(t, srv, base+"/interviews", iid)
	if schedule == nil {
		t.Fatalf("list must carry schedule metadata; got nil (cancelled state never reaches the UI)")
	}
	if schedule["cancelled"] != true {
		t.Errorf("cancelled = %v, want true after cancel", schedule["cancelled"])
	}
	if schedule["cancelled_reason"] != "对方改期" {
		t.Errorf("cancelled_reason = %v, want 对方改期", schedule["cancelled_reason"])
	}

	// 恢复：同一份读回的数据必须立刻回到未取消。
	unStatus, _ := postActivityJSON(t, srv, base+"/interviews/"+itoa(iid)+"/uncancel", ``)
	if unStatus != http.StatusOK {
		t.Fatalf("uncancel -> %d, want 200", unStatus)
	}
	schedule = interviewScheduleInList(t, srv, base+"/interviews", iid)
	if schedule == nil || schedule["cancelled"] != false {
		t.Errorf("cancelled after uncancel = %v, want false", schedule)
	}
}

// interviewScheduleInList 从 GET /interviews 的响应里取出某一轮的 schedule 对象。
func interviewScheduleInList(t *testing.T, srv *httptest.Server, path string, interviewID int64) map[string]any {
	t.Helper()
	status, body := getBody(t, srv, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s -> %d, want 200", path, status)
	}
	items, _ := body["items"].([]any)
	for _, raw := range items {
		it, _ := raw.(map[string]any)
		if it == nil {
			continue
		}
		if id, ok := it["id"].(float64); ok && int64(id) == interviewID {
			if it["schedule"] == nil {
				return nil
			}
			sch, _ := it["schedule"].(map[string]any)
			return sch
		}
	}
	t.Fatalf("interview %d not found in %s", interviewID, path)
	return nil
}
