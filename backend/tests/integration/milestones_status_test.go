// 迁移 00006 的推导契约：**当前状态 = 时间线上最后一个带阶段效果的节点**。
//
// 「最后一个」用的就是用户在时间线面板上看到的排序——业务时间升序、时间未定的
// 排最后——所以往回补一个更早的事件不会改变当前状态，而把一个节点的时间改到
// 最后面会。这些测试把这条规则逐条钉死，因为它是整个改版唯一需要用户理解的规则。
//
// 同目录的 milestones_http_test.go 管的是另一件事：节点自己的 CRUD / 排序 /
// 归属 / 字段校验这些 HTTP 契约。
package integration

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// milestoneAt posts one node and returns its id.
func milestoneAt(t *testing.T, srvURL, base, kind string, at time.Time) int64 {
	t.Helper()
	body := `{"kind":"` + kind + `","occurred_at":"` + at.UTC().Format(time.RFC3339) + `"}`
	code, out := reqMilestoneJSON(t, "POST", srvURL+base+"/milestones", body)
	if code != http.StatusCreated {
		t.Fatalf("create %s = %d (%v)", kind, code, out)
	}
	return int64(out["id"].(float64))
}

func TestMilestoneOrderDecidesStatusNotInsertionOrder(t *testing.T) {
	// Arrange: 先记面试（较晚），再补一个更早的初筛——用户经常这样回头补录。
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "OrderCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	now := time.Now()

	milestoneAt(t, srv.URL, base, "interview", now.Add(-24*time.Hour))
	milestoneAt(t, srv.URL, base, "screen", now.Add(-72*time.Hour))

	// Act
	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	// Assert: 补录的初筛在时间线上排在面试之前，所以当前状态仍是面试。
	if row.Status != "interviewing" {
		t.Fatalf("status = %s, want interviewing (补录更早的事件不能改变当前状态)", row.Status)
	}
}

func TestEditingAMilestoneTimeMovesTheStatus(t *testing.T) {
	// Arrange: 初筛 → 面试 → Offer，当前是 offer。
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "EditCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	now := time.Now()

	milestoneAt(t, srv.URL, base, "screen", now.Add(-72*time.Hour))
	milestoneAt(t, srv.URL, base, "interview", now.Add(-48*time.Hour))
	offerID := milestoneAt(t, srv.URL, base, "offer", now.Add(-24*time.Hour))
	if row, _ := svc.Get(ctx, owner, app.ID, false); row.Status != "offer" {
		t.Fatalf("precondition status = %s, want offer", row.Status)
	}

	// Act: 把 Offer 的时间改到最早（记错了日期这类修正）。
	earlier := now.Add(-96 * time.Hour).UTC().Format(time.RFC3339)
	code, out := reqMilestoneJSON(t, "PATCH", srv.URL+base+"/milestones/"+itoa(offerID),
		`{"occurred_at":"`+earlier+`"}`)
	if code != http.StatusOK {
		t.Fatalf("patch = %d (%v)", code, out)
	}

	// Assert: Offer 不再是时间线最后一格，状态回落到面试。
	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "interviewing" {
		t.Fatalf("status = %s, want interviewing (Offer 已不是最后一格)", row.Status)
	}
}

func TestRemovingTheLastMilestoneRollsTheStatusBack(t *testing.T) {
	// Arrange
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "RemoveCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	now := time.Now()

	milestoneAt(t, srv.URL, base, "oa", now.Add(-48*time.Hour))
	interviewID := milestoneAt(t, srv.URL, base, "interview", now.Add(-24*time.Hour))

	// Act: 误加的面试节点被移除。
	if code, out := reqMilestoneJSON(t, "DELETE", srv.URL+base+"/milestones/"+itoa(interviewID), ""); code != http.StatusOK {
		t.Fatalf("remove = %d (%v)", code, out)
	}

	// Assert: 状态退回上一格，而不是停在一个时间线上已不存在的阶段。
	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "assessment" {
		t.Fatalf("status = %s, want assessment", row.Status)
	}
}

// 移除所有带阶段效果的节点后，只剩建档事件，状态回到 saved。
func TestRemovingEveryMilestoneFallsBackToTheCreationEvent(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "EmptyCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	id := milestoneAt(t, srv.URL, base, "interview", time.Now())
	if code, _ := reqMilestoneJSON(t, "DELETE", srv.URL+base+"/milestones/"+itoa(id), ""); code != http.StatusOK {
		t.Fatal("remove failed")
	}

	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "saved" {
		t.Fatalf("status = %s, want saved", row.Status)
	}
}

// 改事件类型 = 改阶段效果：把「自定义事件」改成「面试」要把岗位带进面试。
func TestChangingAMilestoneKindChangesTheStage(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "KindCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	id := milestoneAt(t, srv.URL, base, "custom", time.Now())
	if row, _ := svc.Get(ctx, owner, app.ID, false); row.Status != "saved" {
		t.Fatalf("precondition status = %s, want saved", row.Status)
	}

	if code, out := reqMilestoneJSON(t, "PATCH", srv.URL+base+"/milestones/"+itoa(id),
		`{"kind":"interview"}`); code != http.StatusOK {
		t.Fatalf("patch = %d (%v)", code, out)
	}

	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "interviewing" {
		t.Fatalf("status = %s, want interviewing", row.Status)
	}
}

// 「投递」节点的时间就是投递时间——分析漏斗与等待天数都读这个快照字段。
func TestApplyMilestoneFillsSubmittedAt(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ApplyCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	submitted := time.Now().Add(-120 * time.Hour).Truncate(time.Second)

	milestoneAt(t, srv.URL, base, "apply", submitted)

	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.SubmittedAt == nil {
		t.Fatal("submitted_at is nil, want the 投递 node's time")
	}
	if !row.SubmittedAt.UTC().Truncate(time.Second).Equal(submitted.UTC()) {
		t.Fatalf("submitted_at = %v, want %v", row.SubmittedAt.UTC(), submitted.UTC())
	}
}

// 终态节点的备注回填 applications.reason，抽屉里的「原因」卡片据此展示；
// 重开（再加一个更晚的非终态节点）必须把它清掉，而不是让旧结局解释一个活着的申请。
func TestTerminalMilestoneNoteBecomesTheReasonAndClearsOnReopen(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ReasonCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	now := time.Now()

	rejectedAt := now.Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	if code, out := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones",
		`{"kind":"reject","occurred_at":"`+rejectedAt+`","note":"HC 关闭"}`); code != http.StatusCreated {
		t.Fatalf("create reject = %d (%v)", code, out)
	}
	row, _ := svc.Get(ctx, owner, app.ID, false)
	if row.Status != "rejected" || row.Reason != "HC 关闭" {
		t.Fatalf("after reject got %s/%q, want rejected + HC 关闭", row.Status, row.Reason)
	}
	if row.RejectedAt == nil {
		t.Fatal("rejected_at is nil, want the node's time")
	}

	// 岗位重开：补一个更晚的面试节点。
	milestoneAt(t, srv.URL, base, "interview", now)

	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Status != "interviewing" {
		t.Fatalf("status = %s, want interviewing", row.Status)
	}
	if row.Reason != "" {
		t.Fatalf("reason = %q, want cleared once the record is live again", row.Reason)
	}
}

// 时间未定的节点排在时间线最后，因此决定当前阶段——与用户看到的排序一致——
// 但它没有时间，绝不能伪造一个投递时间出来。
func TestTimelessMilestoneSetsTheStageButNotTheDate(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "TimelessCo", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	milestoneAt(t, srv.URL, base, "oa", time.Now().Add(-48*time.Hour))
	if code, out := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones", `{"kind":"apply"}`); code != http.StatusCreated {
		t.Fatalf("create timeless apply = %d (%v)", code, out)
	}

	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "applied" {
		t.Fatalf("status = %s, want applied (时间未定的节点排最后，就是最后一格)", row.Status)
	}
	if row.SubmittedAt != nil {
		t.Fatalf("submitted_at = %v, want nil (不伪造时间)", row.SubmittedAt)
	}
}
