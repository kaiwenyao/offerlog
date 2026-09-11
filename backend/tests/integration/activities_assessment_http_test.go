// 活动轮次的 HTTP 契约（方案 §3.2/§3.3/§6.1）：新建 / 完成 / 撤回一轮 OA 时，
// 申请的子状态必须在同一事务里跟着变，不能出现「轮次完成了但申请还说准备中」。
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	actrepo "offerlog/backend/internal/activities/repository"
	acttransport "offerlog/backend/internal/activities/transport"
	"offerlog/backend/internal/applications/domain"
	apprepo "offerlog/backend/internal/applications/repository"
	appservice "offerlog/backend/internal/applications/service"
	identitydomain "offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
)

// activityServerWithSync mounts the activities routes with the application
// progress mirror wired, exactly like bootstrap does.
func activityServerWithSync(t *testing.T, db *database.DB, owner int64) (*httptest.Server, *apprepo.Repo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	appsRepo := apprepo.New(db)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &identitydomain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	h := acttransport.New(actrepo.New(db)).WithApplicationSync(appsRepo)
	api := r.Group("/api/v1")
	h.Routes(api.Group("/applications/:id"))
	return httptest.NewServer(r), appsRepo
}

func postActivityJSON(t *testing.T, srv *httptest.Server, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewBufferString(body))
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

func TestAssessmentEndpointsMirrorProgress(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "OARoundHTTP", "后端工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(2),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	// 进入 OA 阶段但不带轮次：子状态未细分。
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, Version: 2,
	}); err != nil {
		t.Fatalf("assessment: %v", err)
	}

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	// 新建一轮：收到邀请、计划、截止三种时间各自保存。
	invited := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	due := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	code, body := postActivityJSON(t, srv, base+"/assessments",
		`{"kind":"take_home","name":"作业","progress":"preparing","invited_at":"`+invited+`","due_at":"`+due+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create status = %d (%v), want 201", code, body)
	}
	roundID := int64(body["id"].(float64))
	if body["invited_at"] == nil || body["due_at"] == nil {
		t.Fatalf("invited_at / due_at must both round-trip: %v", body)
	}
	if body["completed_at"] != nil {
		t.Fatalf("a preparing round has no completed_at: %v", body)
	}

	// 新建轮次后申请已处于「准备 OA」（这是我们在完成/取消之外唯一写入子状态的时机）。
	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != domain.StatusAssessment || row.Substatus != domain.SubPreparing {
		t.Fatalf("app = %s/%s, want assessment/preparing", row.Status, row.Substatus)
	}
	if row.FocusActivityKind != domain.ActivityAssessment || row.FocusActivityID == nil || *row.FocusActivityID != roundID {
		t.Fatalf("focus = %q/%v, want assessment/%d", row.FocusActivityKind, row.FocusActivityID, roundID)
	}

	// 标记完成：申请同步到「已完成 · 等结果」，且不产生通过结果。
	code, body = postActivityJSON(t, srv, base+"/assessments/"+itoa(roundID)+"/complete", `{}`)
	if code != http.StatusOK {
		t.Fatalf("complete status = %d (%v), want 200", code, body)
	}
	if body["result"] != "unknown" {
		t.Fatalf("completing must not set a result, got %v", body["result"])
	}
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Substatus != domain.SubCompleted {
		t.Fatalf("app substatus = %q, want completed", row.Substatus)
	}

	// 撤回完成：回到准备中，且不得把申请拖出 OA 阶段。
	code, _ = postActivityJSON(t, srv, base+"/assessments/"+itoa(roundID)+"/reopen", `{"progress":"preparing"}`)
	if code != http.StatusOK {
		t.Fatalf("reopen status = %d, want 200", code)
	}
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Status != domain.StatusAssessment || row.Substatus != domain.SubPreparing {
		t.Fatalf("after reopen = %s/%s, want assessment/preparing", row.Status, row.Substatus)
	}
}

// 已经离开 OA 阶段之后，再补点旧轮次的「已完成」，不能把申请拖回 OA。
func TestAssessmentCompleteDoesNotDragStageBackHTTP(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "StageGuardHTTP", "运维工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(3),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, Version: 2,
		Assessment: &appservice.AssessmentInput{Kind: "online_test", Name: "OA"},
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	oaID := *row.FocusActivityID
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: 3, Reason: "直接约面",
	}); err != nil {
		t.Fatalf("interviewing: %v", err)
	}

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	code, body := postActivityJSON(t, srv, "/api/v1/applications/"+itoa(app.ID)+"/assessments/"+itoa(oaID)+"/complete", `{}`)
	if code != http.StatusOK {
		t.Fatalf("complete status = %d (%v), want 200", code, body)
	}
	after, _ := svc.Get(ctx, owner, app.ID, false)
	if after.Status != domain.StatusInterviewing {
		t.Fatalf("status = %q, want interviewing (an old OA must not drag it back)", after.Status)
	}
}
