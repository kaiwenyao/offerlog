// 用户自定义时间线节点（milestones，migration 00005）的 HTTP 契约：
// 用户可以给任何岗位自行添加事件（OA / 初筛 / 面试 / 自定义…）并选择时间；
// 时间线按业务时间合并排序；时间未定的节点排在最后；节点可编辑可删除。
// 状态机审计（application_events）完全不动。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	apprepo "offerlog/backend/internal/applications/repository"
)

func reqMilestoneJSON(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

// TestMilestonesCRUDAndOrdering 走一遍完整契约：创建 → 列表排序 → 编辑改时间
// 重新排序 → 清空时间（时间未定）→ 删除。
func TestMilestonesCRUDAndOrdering(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	_ = context.Background()
	app := mustCreate(t, svc, owner, "MilestoneCo", "后端工程师")

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	t0 := time.Now().UTC().Format(time.RFC3339)
	t1 := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	t2 := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)

	// 三个节点：OA（过去）、初筛（现在）、面试（时间未定）。
	created := map[string]int64{}
	for _, m := range []struct{ kind, label, at, note string }{
		{"oa", "", t0, "第一轮 OA"},
		{"screen", "简历初筛", t1, ""},
		{"interview", "", "", "还没约时间"},
	} {
		code, body := 0, map[string]any{}
		if m.at != "" {
			code, body = reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones",
				`{"kind":"`+m.kind+`","label":"`+m.label+`","occurred_at":"`+m.at+`","note":"`+m.note+`"}`)
		} else {
			code, body = reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones",
				`{"kind":"`+m.kind+`","label":"`+m.label+`","note":"`+m.note+`"}`)
		}
		if code != http.StatusCreated {
			t.Fatalf("create %s = %d (%v)", m.kind, code, body)
		}
		if m.at == "" && body["occurred_at"] != nil {
			t.Fatalf("timeless milestone must round-trip null occurred_at, got %v", body["occurred_at"])
		}
		created[m.kind] = int64(body["id"].(float64))
	}

	// 空 label 的 OA 节点按 kind 给默认名；custom 也一样（默认「自定义事件」）。
	code, body := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones", `{"kind":"custom","label":""}`)
	if code != http.StatusCreated {
		t.Fatalf("create custom = %d (%v)", code, body)
	}
	if body["label"] != "自定义事件" {
		t.Fatalf("custom default label = %q", body["label"])
	}
	// 建议清单之外的 kind 仍然照存不误（开放集合），只是拿不到好名字。
	code, body = reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones", `{"kind":"coffee-chat","label":""}`)
	if code != http.StatusCreated {
		t.Fatalf("create unknown kind = %d (%v)", code, body)
	}
	if body["label"] != "自定义节点" {
		t.Fatalf("unknown kind default label = %q, want 自定义节点", body["label"])
	}
	if body["status_effect"] != "" {
		t.Fatalf("unknown kind must not move the stage; status_effect = %q", body["status_effect"])
	}

	// 列表：按 occurred_at 升序，时间未定排最后。
	code, body = reqMilestoneJSON(t, "GET", srv.URL+base+"/milestones", "")
	if code != http.StatusOK {
		t.Fatalf("list = %d (%v)", code, body)
	}
	items := body["items"].([]any)
	// 2 个带时间的节点（oa / screen）在前；3 个时间未定的按创建顺序排最后。
	if len(items) != 5 {
		t.Fatalf("list len = %d, want 5", len(items))
	}
	want := []string{"oa", "screen", "interview", "custom", "coffee-chat"}
	for i, w := range want {
		if got := items[i].(map[string]any)["kind"].(string); got != w {
			t.Fatalf("order[%d] = %q, want %q", i, got, w)
		}
	}
	oaRow := items[0].(map[string]any)
	if oaRow["label"] != "OA / 笔试" {
		t.Fatalf("empty label falls back to the kind default name, got %q", oaRow["label"])
	}

	// 编辑：把面试节点的时间设到过去 → 重新排序后应排最前，label 同步更新。
	code, _ = reqMilestoneJSON(t, "PATCH", srv.URL+base+"/milestones/"+itoa(created["interview"]),
		`{"kind":"interview","label":"一面","occurred_at":"`+t2+`"}`)
	if code != http.StatusOK {
		t.Fatalf("patch interview = %d", code)
	}
	_, body = reqMilestoneJSON(t, "GET", srv.URL+base+"/milestones", "")
	items = body["items"].([]any)
	if got := items[0].(map[string]any)["label"]; got != "一面" {
		t.Fatalf("patched label = %v", got)
	}
	if second := items[1].(map[string]any)["kind"]; second != "oa" {
		t.Fatalf("after time change the re-sorted head must be the interview node, then oa; got %v", second)
	}

	// clear_occurred_at：显式清空时间 → 时间未定，排到所有有时间的节点之后
	//（与其他时间未定的节点按创建顺序排）。
	code, _ = reqMilestoneJSON(t, "PATCH", srv.URL+base+"/milestones/"+itoa(created["interview"]),
		`{"kind":"interview","label":"一面","clear_occurred_at":true}`)
	if code != http.StatusOK {
		t.Fatalf("patch clear = %d", code)
	}
	_, body = reqMilestoneJSON(t, "GET", srv.URL+base+"/milestones", "")
	items = body["items"].([]any)
	idx := -1
	for i, it := range items {
		if it.(map[string]any)["kind"] == "interview" {
			idx = i
		}
	}
	if idx < 2 {
		t.Fatalf("cleared node must sort after every timed node, got index %d", idx)
	}
	if items[idx].(map[string]any)["occurred_at"] != nil {
		t.Fatalf("cleared node must have null time, got %v", items[idx])
	}

	// 不带 kind/label 的 PATCH：保留原类型与名称（只改备注）。
	code, _ = reqMilestoneJSON(t, "PATCH", srv.URL+base+"/milestones/"+itoa(created["interview"]),
		`{"note":"改个备注"}`)
	if code != http.StatusOK {
		t.Fatalf("patch note-only = %d", code)
	}
	_, body = reqMilestoneJSON(t, "GET", srv.URL+base+"/milestones", "")
	items = body["items"].([]any)
	var intRow map[string]any
	for _, it := range items {
		if it.(map[string]any)["kind"] == "interview" {
			intRow = it.(map[string]any)
		}
	}
	if intRow == nil || intRow["label"] != "一面" || intRow["note"] != "改个备注" {
		t.Fatalf("note-only patch kept kind/label/time: %v", intRow)
	}

	// 删除：用户自建节点可删除（与追加式状态审计不同）。
	code, _ = reqMilestoneJSON(t, "DELETE", srv.URL+base+"/milestones/"+itoa(created["oa"]), "")
	if code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}
	_, body = reqMilestoneJSON(t, "GET", srv.URL+base+"/milestones", "")
	if got := len(body["items"].([]any)); got != 4 {
		t.Fatalf("after delete len = %d, want 4", got)
	}

	// 不存在的节点 → 404，不是 500。
	code, _ = reqMilestoneJSON(t, "DELETE", srv.URL+base+"/milestones/999999", "")
	if code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", code)
	}
}

// TestMilestoneOwnershipAndValidation 他人岗位不可写；非法 kind / 超长 label 被拒。
func TestMilestoneOwnershipAndValidation(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	app := mustCreate(t, svc, owner, "MilestoneGuard", "前端工程师")
	base := "/api/v1/applications/" + itoa(app.ID)

	// 第二个用户：无权在 owner 的岗位上加节点（404，不泄露存在性）。
	other := createOwner(t, db)
	srvOther, _ := activityServerWithSync(t, db, other)
	defer srvOther.Close()
	if code, _ := reqMilestoneJSON(t, "POST", srvOther.URL+base+"/milestones", `{"kind":"oa","label":"OA"}`); code != http.StatusNotFound {
		t.Fatalf("foreign create = %d, want 404", code)
	}

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	if code, _ := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones", `{"kind":"oa bad","label":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("kind with space = %d, want 400", code)
	}
	long := make([]rune, 101)
	for i := range long {
		long[i] = '字'
	}
	if code, _ := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones", `{"kind":"oa","label":"`+string(long)+`"}`); code != http.StatusBadRequest {
		t.Fatalf("long label = %d, want 400", code)
	}
	if code, _ := reqMilestoneJSON(t, "POST", srv.URL+"/api/v1/applications/999999/milestones", `{"kind":"oa"}`); code != http.StatusNotFound {
		t.Fatalf("missing app create = %d, want 404", code)
	}
}

// TestMilestoneDrivesStatusWithoutAppendingAuditEvents 是迁移 00006 的核心契约：
// 添加一个节点就是记录进度——岗位阶段随之推导——但审计表 application_events
// 一行都不多。两者生命周期不同（节点可编辑可删除，事件是追加式证据链），阶段由
// 视图 application_stage_points 在**读**的一侧合并得出。
func TestMilestoneDrivesStatusWithoutAppendingAuditEvents(t *testing.T) {
	// Arrange
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "MilestoneDrives", "数据工程师")
	appsRepo := apprepo.New(db)
	before, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != "saved" {
		t.Fatalf("precondition: new record status = %s, want saved", before.Status)
	}
	eventsBefore, err := appsRepo.ListEvents(ctx, db.Pool(), app.ID, owner)
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	at := time.Now().UTC().Format(time.RFC3339)

	// Act
	code, body := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones", `{"kind":"offer","label":"口头 Offer","occurred_at":"`+at+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d (%v)", code, body)
	}

	// Assert: the stage followed the event…
	if body["status_effect"] != "offer" {
		t.Fatalf("status_effect = %q, want offer", body["status_effect"])
	}
	after, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "offer" {
		t.Fatalf("after adding an Offer node status = %s, want offer", after.Status)
	}
	// …and the audit trail stayed exactly as long as it was.
	eventsAfter, err := appsRepo.ListEvents(ctx, db.Pool(), app.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("milestone must not append status-machine events: %d → %d", len(eventsBefore), len(eventsAfter))
	}
}

// 「电话沟通」「自定义事件」只记事：它们出现在时间线上，但绝不改动阶段。
func TestMilestoneWithoutStatusEffectLeavesTheStageAlone(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "MilestoneNeutral", "后端工程师")
	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)
	at := time.Now().UTC().Format(time.RFC3339)

	for _, kind := range []string{"phone", "custom"} {
		if code, body := reqMilestoneJSON(t, "POST", srv.URL+base+"/milestones",
			`{"kind":"`+kind+`","occurred_at":"`+at+`"}`); code != http.StatusCreated {
			t.Fatalf("create %s = %d (%v)", kind, code, body)
		}
	}

	after, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "saved" {
		t.Fatalf("neutral nodes moved the stage to %s, want saved", after.Status)
	}
}
