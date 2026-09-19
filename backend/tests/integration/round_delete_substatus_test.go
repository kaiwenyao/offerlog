// 删除轮次时必须把「由这一轮派生的 substatus」一并清掉（带阶段守卫）的回归。
//
// 症状（实测，先标记完成再 DELETE）：
//
//	删除轮次后: status="assessment" substatus="completed" focus=<nil>
//
// DeleteAssessment / DeleteInterview 里那条 UPDATE 已经在清 focus_activity_kind/id
// （防悬挂），但没清 substatus。于是界面上：详情头部芯片、数据库表格的「状态」列、
// 看板卡片标签都显示「已完成 OA · 等结果」，而这个岗位一轮测评都没有；顶部
// 「OA 后等结果」快捷筛选也还会把它捞出来。
//
// 这是既有后端行为，但这个 PR 第一次让删除可达（以前没有删除入口）。同一类问题
// 当年修过一次——SyncFromActivity 的注释明写着「a cancelled round derives no
// substatus ("")，必须清掉由同一轮派生的 substatus」（PR #23 review）：取消路径
// 修了，删除路径漏了。
//
// 守卫（本次按 SyncFromActivity 先例加的）：只有岗位 status 等于该轮次所属阶段时
// 才清 substatus —— 岗位已经推进到别的阶段时，substatus 是那一阶段派生的，不能
// 因为删掉一个老轮次就清掉。focus 的清理不受守卫限制（悬挂引用任何时候都不能留）。
package integration

import (
	"context"
	"net/http"
	"testing"

	"offerlog/backend/internal/applications/domain"
	appservice "offerlog/backend/internal/applications/service"
)

// deleteRoundHTTP deletes one round through the app-scoped activities routes.
func deleteRoundHTTP(t *testing.T, srvURL, base, kind string, id int64) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", srvURL+base+"/"+kind+"/"+itoa(id), nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

// (a) 完成一轮 OA（substatus 派生成「已完成 OA · 等结果」）后删掉它：
// 岗位不能继续显示「已完成 OA」——它一轮测评都没有了。
func TestDeleteCompletedAssessmentClearsDerivedSubstatus(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "DelOA", "后端工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(2),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, Version: 2,
		Assessment: &appservice.AssessmentInput{Kind: "online_test", Name: "唯一一轮"},
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	oaID := *row.FocusActivityID

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	// 先标记完成：substatus 跟着变成「已完成 · 等结果」。
	if code, body := postActivityJSON(t, srv, base+"/assessments/"+itoa(oaID)+"/complete", `{}`); code != http.StatusOK {
		t.Fatalf("complete -> %d (%v), want 200", code, body)
	}
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Substatus != domain.SubCompleted {
		t.Fatalf("substatus = %q, want completed before the delete", row.Substatus)
	}

	// 再删除这一轮（focus 所在轮）：substatus 必须一起清掉。
	if code := deleteRoundHTTP(t, srv.URL, base, "assessments", oaID); code != http.StatusOK {
		t.Fatalf("delete round -> %d, want 200", code)
	}
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Substatus != "" {
		t.Fatalf("deleting the focused round must clear its derived substatus, got %q "+
			"(否则界面会一直显示「已完成 OA · 等结果」)", row.Substatus)
	}
	if row.FocusActivityKind != "" || row.FocusActivityID != nil {
		t.Fatalf("focus = %q/%v after delete, want cleared", row.FocusActivityKind, row.FocusActivityID)
	}
	// 阶段本身是用户的显式选择：删掉一轮不该把岗位退回上一阶段。
	if row.Status != domain.StatusAssessment {
		t.Fatalf("status = %q, want assessment (deleting a round must not move the stage)", row.Status)
	}
}

// (b) 守卫：substatus 由「另一个阶段」派生时，删掉一个仍被 focus 引用、但属于别的
// 阶段的轮次 —— 不能把那个 substatus 一起清掉。
//
// 这个状态不能靠正常流转造出来（service 在「离开关注轮次所属阶段」时就自动清掉了
// focus，见 service.go 的 “Leaving the focused activity's stage drops the
// reference”），所以这里直接写成库里那种边界状态：岗位已经在面试阶段、substatus 是
// 「待安排面试」（一个 OA 轮次永远派生不出来、只属于面试阶段的值），而 focus 还挂在
// 那轮老 OA 上——例如历史数据、或以后新增的其它写入路径留下的形状。守卫就是为了
// 让这种时候的删除不至于把面试派生的子状态抹掉。
func TestDeleteRoundKeepsOtherStageDerivedSubstatus(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "DelGuard", "运维工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(2),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, Version: 2,
		Assessment: &appservice.AssessmentInput{Kind: "online_test", Name: "老 OA"},
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	oaID := *row.FocusActivityID

	// 边界状态：阶段已到面试、子状态是面试派生值，但 focus 仍指向那轮老 OA。
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET status=$1, substatus=$2,
		focus_activity_kind=$3, focus_activity_id=$4, version=version+1, updated_at=now()
		WHERE id=$5 AND owner_id=$6`,
		domain.StatusInterviewing, domain.SubAwaitingSchedule,
		domain.ActivityAssessment, oaID, app.ID, owner); err != nil {
		t.Fatal(err)
	}

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	// 删掉那轮老 OA：status 已经是面试阶段，面试派生的子状态不能被误清。
	if code := deleteRoundHTTP(t, srv.URL, base, "assessments", oaID); code != http.StatusOK {
		t.Fatalf("delete old OA round -> %d, want 200", code)
	}
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Substatus != domain.SubAwaitingSchedule {
		t.Fatalf("substatus = %q, want %q (a round from another stage must not clear it)",
			row.Substatus, domain.SubAwaitingSchedule)
	}
	// 但悬挂的 focus 引用无论如何都要清掉。
	if row.FocusActivityKind != "" || row.FocusActivityID != nil {
		t.Fatalf("dangling focus = %q/%v after delete, want cleared",
			row.FocusActivityKind, row.FocusActivityID)
	}
}

// 面试侧同一条路径（DeleteInterview）也要清派生 substatus：完成一轮面试后删掉它，
// 岗位不能继续显示「已完成面试 · 等反馈」。
func TestDeleteCompletedInterviewClearsDerivedSubstatus(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "DelInterview", "数据工程师")

	srv, _ := activityServerWithSync(t, db, owner)
	defer srv.Close()
	base := "/api/v1/applications/" + itoa(app.ID)

	// 先投递再进面试阶段（跨越「待投递」需要投递时间），然后建一轮面试
	// （focus 指向它），接着标记完成。
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(2),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: 2,
	}); err != nil {
		t.Fatalf("interviewing: %v", err)
	}
	code, body := postActivityJSON(t, srv, base+"/interviews", `{"round_name":"一面","format":"video"}`)
	if code != http.StatusCreated {
		t.Fatalf("create interview -> %d (%v), want 201", code, body)
	}
	iid := int64(body["id"].(float64))
	if code, body := postActivityJSON(t, srv, base+"/interviews/"+itoa(iid)+"/complete", `{}`); code != http.StatusOK {
		t.Fatalf("complete interview -> %d (%v), want 200", code, body)
	}
	row, _ := svc.Get(ctx, owner, app.ID, false)
	if row.Substatus != domain.SubCompleted {
		t.Fatalf("substatus = %q, want completed before the delete", row.Substatus)
	}

	if code := deleteRoundHTTP(t, srv.URL, base, "interviews", iid); code != http.StatusOK {
		t.Fatalf("delete interview -> %d, want 200", code)
	}
	row, _ = svc.Get(ctx, owner, app.ID, false)
	if row.Substatus != "" {
		t.Fatalf("deleting the focused interview must clear its derived substatus, got %q", row.Substatus)
	}
	if row.FocusActivityKind != "" || row.FocusActivityID != nil {
		t.Fatalf("focus = %q/%v after delete, want cleared", row.FocusActivityKind, row.FocusActivityID)
	}
}
