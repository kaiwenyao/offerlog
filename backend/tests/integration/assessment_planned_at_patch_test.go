// 「计划开做」(planned_at) 与「收到邀请」(invited_at) 的可编辑 / 可清空回归。
//
// 症状（用户反馈）：OA 编辑表单只有 名称 / 截止时间 / 测试链接。而 planned_at 才是
// 首页「即将到来的面试 / OA」的时间源与过滤条件（home/repo.go：WHERE
// r.planned_at IS NOT NULL）、日历 kind='assessment' 事件的时刻、本周工序条 OA chip
// 落在哪一天；due_at 只负责 assessment_due 那条截止线。于是「计划开做」填错一位，
// 只能删了重建 —— 和这个 PR 要消灭的症状是同一个洞，只是换了个字段。
//
// 这一层测试同时钉住「清空」的语义：PATCH /assessments 是合并式的（没传 / 传 null
// 都保留旧值），所以清空必须走显式的 clear_planned_at / clear_invited_at 旗标
// （与 clear_due_at 同一套做法）。没有旗标时，界面上看着清空了，刷新后旧时刻会
// 一直挂在首页和日历上。
//
// 读取端不需要改动（首页直接按 planned_at 过滤 / 排序），所以要断言的是「改完
// 首页真的跟着变」——这正是这个洞会造成用户可见伤害的地方。
package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	actrepo "offerlog/backend/internal/activities/repository"
	appservice "offerlog/backend/internal/applications/service"
	"offerlog/backend/internal/calendar"
	"offerlog/backend/internal/home"
	"offerlog/backend/internal/platform/database"
)

// homeUpcomingAssessmentTimes returns, keyed by round id, the planned time the
// home dashboard currently shows for this owner's upcoming OA list. A round
// whose planned_at is NULL is absent from the list (the list filters on it).
func homeUpcomingAssessmentTimes(t *testing.T, db *database.DB, owner int64, now time.Time) map[int64]time.Time {
	t.Helper()
	ctx := context.Background()
	var tz string
	if err := db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz); err != nil {
		t.Fatalf("load tz: %v", err)
	}
	s, err := home.New(db).Get(ctx, owner, tz, time.Monday, now, 20)
	if err != nil {
		t.Fatalf("home summary: %v", err)
	}
	out := map[int64]time.Time{}
	for _, u := range s.UpcomingAssessments {
		out[u.ID] = u.PlannedAt
	}
	return out
}

// calendarAssessmentAt returns the start time the calendar shows for this OA
// round within the next week (the calendar keys kind='assessment' events off
// planned_at), and whether such an event exists at all.
func calendarAssessmentAt(t *testing.T, db *database.DB, owner int64, now time.Time, roundID int64) (time.Time, bool) {
	t.Helper()
	events, err := calendar.New(db).Range(context.Background(), owner, "Europe/Dublin",
		now.Add(-24*time.Hour), now.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("calendar range: %v", err)
	}
	for _, e := range events {
		if e.Kind == "assessment" && e.ID == roundID && e.Start != nil {
			return *e.Start, true
		}
	}
	return time.Time{}, false
}

func TestPatchAssessmentPlannedAndInvitedTimes(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "PatchPlanned", "前端工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: "applied", Version: 1, SubmittedAt: submittedAt(2),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	// 初始：收到邀请 -8d、计划开做 -3d（已过期，但列表仍按 planned_at 列出）、截止 +2d。
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: "assessment", ToSubstatus: "preparing", Version: 2,
		Assessment: &appservice.AssessmentInput{
			Kind: "take_home", Name: "作业",
			InvitedAt: submittedAt(8),
			PlannedAt: submittedAt(3),
			DueAt:     submittedAt(-2),
		},
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	oaID := *row.FocusActivityID

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID) + "/assessments/" + itoa(oaID)

	newPlanned := time.Now().Add(50 * time.Hour).UTC().Truncate(time.Second)
	newInvited := time.Now().Add(-100 * time.Hour).UTC().Truncate(time.Second)

	// 1) 改「计划开做」：必须落库，并且首页那条 OA 的时间跟着变。
	code, body := patchActivityJSON(t, srv, base, `{"planned_at":"`+newPlanned.Format(time.RFC3339)+`"}`)
	if code != http.StatusOK {
		t.Fatalf("patch planned_at -> %d (%v), want 200", code, body)
	}
	round, err := actrepo.New(db).GetAssessment(ctx, db, app.ID, owner, oaID)
	if err != nil {
		t.Fatalf("reload round: %v", err)
	}
	if round.PlannedAt == nil || !round.PlannedAt.Equal(newPlanned) {
		t.Fatalf("planned_at = %v, want %v (the form field must actually persist)", round.PlannedAt, newPlanned)
	}
	// 只改 planned_at 不该动到「收到邀请」与「截止时间」两条独立事实。
	if round.InvitedAt == nil || round.DueAt == nil {
		t.Fatalf("editing planned_at erased another recorded time: invited=%v due=%v", round.InvitedAt, round.DueAt)
	}
	if got := homeUpcomingAssessmentTimes(t, db, owner, time.Now())[oaID]; !got.Equal(newPlanned) {
		t.Fatalf("首页「即将到来的 OA」显示 %v, want %v —— planned_at 才是这一列的时间源", got, newPlanned)
	}
	// 日历是另一个读取端（kind='assessment' 事件的时刻就是 planned_at），它按
	// planned_at 落在窗口内来筛选，所以改完必须能在那一天找到这轮 OA。
	if ev, ok := calendarAssessmentAt(t, db, owner, time.Now(), oaID); !ok || !ev.Equal(newPlanned) {
		t.Fatalf("日历 OA 事件 = %v（存在=%v）, want %v", ev, ok, newPlanned)
	}

	// 2) 改「收到邀请」：同样落库，且不碰 planned_at / due_at。
	code, body = patchActivityJSON(t, srv, base, `{"invited_at":"`+newInvited.Format(time.RFC3339)+`"}`)
	if code != http.StatusOK {
		t.Fatalf("patch invited_at -> %d (%v), want 200", code, body)
	}
	round, _ = actrepo.New(db).GetAssessment(ctx, db, app.ID, owner, oaID)
	if round.InvitedAt == nil || !round.InvitedAt.Equal(newInvited) {
		t.Fatalf("invited_at = %v, want %v", round.InvitedAt, newInvited)
	}
	if round.PlannedAt == nil || !round.PlannedAt.Equal(newPlanned) {
		t.Fatalf("editing invited_at moved planned_at: %v", round.PlannedAt)
	}

	// 3) 清空「计划开做」：没有显式旗标时，传 null 只会被合并回旧值（用户会以为
	//    「清空了，刷新又回来」）——先钉住这个合并语义本身。
	code, body = patchActivityJSON(t, srv, base, `{"planned_at":null}`)
	if code != http.StatusOK {
		t.Fatalf("patch planned_at=null -> %d (%v), want 200", code, body)
	}
	round, _ = actrepo.New(db).GetAssessment(ctx, db, app.ID, owner, oaID)
	if round.PlannedAt == nil {
		t.Fatalf("bare null must still be 'omitted' for planned_at (clearing is deliberate), got nil")
	}

	// 4) 显式清空：clear_planned_at=true → planned_at 真被清掉，首页那条 OA 消失。
	code, body = patchActivityJSON(t, srv, base, `{"clear_planned_at":true}`)
	if code != http.StatusOK {
		t.Fatalf("patch clear_planned_at -> %d (%v), want 200", code, body)
	}
	round, _ = actrepo.New(db).GetAssessment(ctx, db, app.ID, owner, oaID)
	if round.PlannedAt != nil {
		t.Fatalf("clear_planned_at must actually clear planned_at, got %v", round.PlannedAt)
	}
	if _, listed := homeUpcomingAssessmentTimes(t, db, owner, time.Now())[oaID]; listed {
		t.Fatalf("cleared planned_at still shows on 首页「即将到来的 OA」")
	}
	if _, listed := calendarAssessmentAt(t, db, owner, time.Now(), oaID); listed {
		t.Fatalf("cleared planned_at still shows on the calendar")
	}
	// 清 planned_at 不该顺手清掉截止时间（那条截止线属于 due_at，另有一套提醒）。
	if round.DueAt == nil {
		t.Fatalf("clearing planned_at must not clear due_at")
	}

	// 5) 显式清空「收到邀请」。
	code, body = patchActivityJSON(t, srv, base, `{"clear_invited_at":true}`)
	if code != http.StatusOK {
		t.Fatalf("patch clear_invited_at -> %d (%v), want 200", code, body)
	}
	round, _ = actrepo.New(db).GetAssessment(ctx, db, app.ID, owner, oaID)
	if round.InvitedAt != nil {
		t.Fatalf("clear_invited_at must actually clear invited_at, got %v", round.InvitedAt)
	}
	if round.DueAt == nil {
		t.Fatalf("clearing invited_at must not clear due_at")
	}
}
