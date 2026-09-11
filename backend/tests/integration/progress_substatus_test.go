// 进度细化与状态回退（docs/求职进度细化与状态回退方案.md §8 验收场景）。
//
// 这些用例跑真实的 PostgreSQL + 真实迁移，覆盖文档验收表里最容易被实现走偏的
// 几条：投递后直接收到 OA、完成 ≠ 通过、一轮结束不等于整个阶段结束、真实回退
// 保留历史、误操作只能靠更正撤销、旧数据保持「未细分」。
package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	actrepo "offerlog/backend/internal/activities/repository"
	"offerlog/backend/internal/analytics"
	"offerlog/backend/internal/applications/domain"
	appservice "offerlog/backend/internal/applications/service"
	"offerlog/backend/internal/platform/database"
)

// newProgressService builds a service wired to the activities repository so a
// transition and the round it records share one transaction (方案 §6.3).
func newProgressService(t *testing.T) (*database.DB, *appservice.Service, *actrepo.Repo, int64) {
	t.Helper()
	db, _, repo, owner := setup(t)
	acts := actrepo.New(db)
	return db, appservice.New(db, repo).WithActivities(acts), acts, owner
}

func submittedAt(daysAgo int) *time.Time {
	t := time.Now().AddDate(0, 0, -daysAgo)
	return &t
}

// 验收：投递后直接收到 OA —— 不强制补初筛，也不自动记录筛选通过。
func TestAppliedToAssessmentDirectly(t *testing.T) {
	_, svc, acts, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "DirectOA", "后端工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(3),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}

	invited := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	planned := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	due := time.Date(2026, 9, 16, 23, 59, 0, 0, time.UTC)
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubPreparing,
		FocusActivityKind: domain.ActivityAssessment, Version: 2,
		Assessment: &appservice.AssessmentInput{
			Kind: "online_test", Name: "OA", InvitedAt: &invited, PlannedAt: &planned, DueAt: &due,
		},
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	if row.Status != domain.StatusAssessment || row.Substatus != domain.SubPreparing {
		t.Fatalf("got %s/%s, want assessment/preparing", row.Status, row.Substatus)
	}
	if row.FocusActivityKind != domain.ActivityAssessment || row.FocusActivityID == nil {
		t.Fatalf("focus = %q/%v, want assessment round", row.FocusActivityKind, row.FocusActivityID)
	}

	// 收到邀请 ≠ 通过简历筛选：不得凭空多出一条 screening。
	evs, err := svc.Events(ctx, owner, app.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	for _, ev := range evs {
		if ev.ToStatus != nil && *ev.ToStatus == domain.StatusScreening {
			t.Fatalf("skipping 初筛 must not record a screening event, got %+v", ev)
		}
	}

	rounds, err := acts.ListAssessments(ctx, app.ID, owner)
	if err != nil || len(rounds) != 1 {
		t.Fatalf("rounds = %d (err %v), want 1", len(rounds), err)
	}
	r := rounds[0]
	// 收到、计划、截止三种时间各自保存，互不覆盖。
	if r.InvitedAt == nil || !r.InvitedAt.Equal(invited) {
		t.Errorf("invited_at = %v, want %v", r.InvitedAt, invited)
	}
	if r.PlannedAt == nil || !r.PlannedAt.Equal(planned) {
		t.Errorf("planned_at = %v, want %v", r.PlannedAt, planned)
	}
	if r.DueAt == nil || !r.DueAt.Equal(due) {
		t.Errorf("due_at = %v, want %v", r.DueAt, due)
	}
	if r.CompletedAt != nil || r.CompletedUnknown {
		t.Errorf("a preparing round has no completion fact, got %v/%v", r.CompletedAt, r.CompletedUnknown)
	}
	if r.Result != domain.ActResultUnknown {
		t.Errorf("result = %q, want unknown", r.Result)
	}
	// 准备中的 OA 绝不能显示成「筛选通过」或已完成。
	if domain.SubstatusForActivity(domain.ActivityAssessment, r.Progress, r.Result) != domain.SubPreparing {
		t.Errorf("derived substatus drifted from preparing")
	}
}

// 验收：OA 已完成但暂无结果 —— 显示已完成等结果，不标为通过。
func TestAssessmentCompletedWithoutResultIsNotPassed(t *testing.T) {
	_, svc, acts, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "NoResult", "数据工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(5),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubPreparing, Version: 2,
		Assessment: &appservice.AssessmentInput{Kind: "take_home", Name: "作业"},
	}); err != nil {
		t.Fatalf("assessment: %v", err)
	}

	// 标记完成：不传结果，也不传精确完成时间（历史时间不详）。
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubCompleted,
		Version: 3,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if row.Substatus != domain.SubCompleted {
		t.Fatalf("substatus = %q, want completed", row.Substatus)
	}
	if label := domain.ComboLabelForKind(row.Status, row.Substatus, "take_home"); label != "已提交作业 · 等结果" {
		t.Errorf("label = %q, want 已提交作业 · 等结果", label)
	}
	rounds, _ := acts.ListAssessments(ctx, app.ID, owner)
	if len(rounds) != 1 {
		t.Fatalf("rounds = %d, want 1", len(rounds))
	}
	// 同一条命令把轮次也推进到「已完成」，两侧不能各说各话（方案 §6.1）。
	if rounds[0].Progress != domain.ActProgressCompleted {
		t.Errorf("round progress = %q, want completed", rounds[0].Progress)
	}
	if rounds[0].Result != domain.ActResultUnknown {
		t.Errorf("completing must not invent a result, got %q", rounds[0].Result)
	}
	if rounds[0].CompletedAt != nil && !rounds[0].CompletedUnknown {
		t.Errorf("no completed_at was known, so completed_unknown must be set")
	}

	// 明确记录「通过」才显示通过。
	row, err = svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubPassed, Version: 4,
	})
	if err != nil {
		t.Fatalf("passed: %v", err)
	}
	if row.Substatus != domain.SubPassed {
		t.Fatalf("substatus = %q, want passed", row.Substatus)
	}
	if label := domain.ComboLabel(row.Status, row.Substatus); label != "OA 已通过 · 等下一步" {
		t.Errorf("label = %q", label)
	}
	rounds, _ = acts.ListAssessments(ctx, app.ID, owner)
	if rounds[0].Result != domain.ActResultPassed {
		t.Errorf("round result = %q, want passed", rounds[0].Result)
	}
}

// 验收：一面结束、二面未定 —— 一轮完成不等于整个面试阶段结束。
func TestInterviewRoundCompletionDoesNotEndStage(t *testing.T) {
	_, svc, acts, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "RoundOne", "前端工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(6),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	scheduled := time.Now().Add(48 * time.Hour)
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, ToSubstatus: domain.SubPreparing, Version: 2,
		Interview: &appservice.InterviewInput{
			RoundName: "一面", Format: "video", ScheduledAt: &scheduled, Timezone: "Europe/Dublin",
		},
	})
	if err != nil {
		t.Fatalf("interviewing: %v", err)
	}
	if row.Status != domain.StatusInterviewing || row.Substatus != domain.SubPreparing {
		t.Fatalf("got %s/%s, want interviewing/preparing", row.Status, row.Substatus)
	}

	// 一面结束。
	row, err = svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, ToSubstatus: domain.SubCompleted, Version: 3,
	})
	if err != nil {
		t.Fatalf("complete round: %v", err)
	}
	if row.Status != domain.StatusInterviewing {
		t.Fatalf("status = %q, want interviewing still", row.Status)
	}
	if row.Substatus != domain.SubCompleted {
		t.Fatalf("substatus = %q, want completed", row.Substatus)
	}
	rounds, err := acts.ListInterviews(ctx, app.ID, owner)
	if err != nil || len(rounds) != 1 {
		t.Fatalf("rounds = %d (err %v), want 1", len(rounds), err)
	}
	if rounds[0].Result != domain.ActResultUnknown {
		t.Fatalf("完成面试不能自动标为通过，got result %q", rounds[0].Result)
	}
	if rounds[0].Progress != domain.ActProgressCompleted {
		t.Fatalf("round progress = %q, want completed", rounds[0].Progress)
	}

	// 二面还没定 → 依然是面试阶段，只是回到「待安排」。
	row, err = svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, ToSubstatus: domain.SubAwaitingSchedule,
		FocusActivityKind: domain.ActivityInterview, Version: 4,
	})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	if row.Status != domain.StatusInterviewing || row.Substatus != domain.SubAwaitingSchedule {
		t.Fatalf("got %s/%s, want interviewing/awaiting_schedule", row.Status, row.Substatus)
	}
}

// 验收：Offer 退回面试 —— 追加真实回退，保留 Offer 历史事实。
func TestRollbackOfferToInterviewingKeepsHistory(t *testing.T) {
	_, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "OfferBack", "平台工程师")

	step := func(to string, v int) {
		t.Helper()
		in := &appservice.TransitionInput{ToStatus: to, Version: v, Reason: "跟进"}
		if to == domain.StatusApplied {
			in.SubmittedAt = submittedAt(10)
		}
		if _, err := svc.Transition(ctx, owner, app.ID, in); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	step(domain.StatusApplied, 1)
	step(domain.StatusScreening, 2)
	step(domain.StatusOffer, 3)

	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: 4,
	})
	if err != nil {
		t.Fatalf("rollback to interviewing: %v", err)
	}
	if row.Status != domain.StatusInterviewing {
		t.Fatalf("status = %q, want interviewing", row.Status)
	}
	// 真实回退不清历史（方案 §4.2）。
	if row.SubmittedAt == nil {
		t.Errorf("rollback must not erase submitted_at")
	}
	if row.FirstResponseAt == nil && row.SubmittedAt == nil {
		t.Errorf("rollback must not erase the reply history")
	}

	evs, err := svc.Events(ctx, owner, app.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var offerCount, rollbacks int
	for _, ev := range evs {
		if ev.ToStatus != nil && *ev.ToStatus == domain.StatusOffer && ev.EventType == "status_change" {
			offerCount++
		}
		if ev.ChangeType == domain.ChangeRollback {
			rollbacks++
		}
	}
	if offerCount != 1 {
		t.Errorf("the Offer event must survive the rollback, got %d", offerCount)
	}
	if rollbacks != 1 {
		t.Errorf("rollback events = %d, want 1", rollbacks)
	}
}

// 验收：误点完成并更正 —— 错误记录保留审计，但不计入有效完成。
func TestCorrectionOfWrongCompletion(t *testing.T) {
	_, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "MisClick", "测试工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(4),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubPreparing, Version: 2,
		Assessment: &appservice.AssessmentInput{Kind: "online_test", Name: "OA"},
	}); err != nil {
		t.Fatalf("assessment: %v", err)
	}
	// 误点「已完成 OA」。
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubCompleted, Version: 3,
	})
	if err != nil {
		t.Fatalf("wrong completion: %v", err)
	}
	if row.Substatus != domain.SubCompleted {
		t.Fatalf("setup: substatus = %q, want completed", row.Substatus)
	}

	// 「之前选错了」→ 更正回准备中。
	row, err = svc.CorrectCurrent(ctx, owner, app.ID, &appservice.CorrectCurrentInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubPreparing,
		Reason: "其实还没提交", Version: row.Version,
	})
	if err != nil {
		t.Fatalf("correction: %v", err)
	}
	if row.Status != domain.StatusAssessment || row.Substatus != domain.SubPreparing {
		t.Fatalf("after correction got %s/%s, want assessment/preparing", row.Status, row.Substatus)
	}

	evs, err := svc.Events(ctx, owner, app.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var corrections int
	for _, ev := range evs {
		if ev.EventType == "correction" {
			corrections++
			if ev.CorrectsEventID == nil {
				t.Errorf("a correction must point at the event it repairs")
			}
		}
	}
	if corrections != 1 {
		t.Errorf("corrections = %d, want 1", corrections)
	}
}

// 验收：重开已接受 —— 可选任意非终态并记录原因，不删除真实历史。
func TestReopenAcceptedToInterviewing(t *testing.T) {
	_, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ReopenAccepted", "算法工程师")

	for v, to := range []string{domain.StatusApplied, domain.StatusScreening, domain.StatusOffer} {
		in := &appservice.TransitionInput{ToStatus: to, Version: v + 1}
		if to == domain.StatusApplied {
			in.SubmittedAt = submittedAt(20)
		}
		if _, err := svc.Transition(ctx, owner, app.ID, in); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAccepted, Version: 4,
	}); err != nil {
		t.Fatalf("accepted: %v", err)
	}

	// 终态重开必须给原因。
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: 5,
	}); err == nil {
		t.Fatalf("reopening without a reason must be refused")
	} else {
		var ve *domain.ValidationError
		if !errors.As(err, &ve) || ve.Code != "missing_reason" {
			t.Fatalf("err = %v, want missing_reason", err)
		}
	}

	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: 5, Reason: "招聘方重新联系，加一轮面试",
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if row.Status != domain.StatusInterviewing {
		t.Fatalf("status = %q, want interviewing", row.Status)
	}
	if row.AcceptedAt == nil {
		t.Errorf("重开不删除真实历史：accepted_at must survive")
	}
	evs, _ := svc.Events(ctx, owner, app.ID)
	last := evs[len(evs)-1]
	if last.ChangeType != domain.ChangeReopen {
		t.Errorf("change_type = %q, want reopen", last.ChangeType)
	}
	if last.Reason == "" {
		t.Errorf("the reopen reason must be recorded on the event")
	}
}

// 终态之间只能更正，不能当流转做（否则会静默丢掉一次真实结局）。
func TestTerminalToTerminalMustBeCorrection(t *testing.T) {
	_, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "EndedEnded", "运维工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(2),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusRejected, Version: 2, Reason: "未通过",
	}); err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusClosed, Version: 3, Reason: "岗位关闭",
	}); err == nil {
		t.Fatalf("终态之间不得以流转方式变更")
	}

	// 更正才是通路。
	row, err := svc.CorrectCurrent(ctx, owner, app.ID, &appservice.CorrectCurrentInput{
		ToStatus: domain.StatusClosed, Reason: "记录写错了", Version: 3,
	})
	if err != nil {
		t.Fatalf("correction: %v", err)
	}
	if row.Status != domain.StatusClosed {
		t.Fatalf("status = %q, want closed", row.Status)
	}
}

// 「之前选错了」不能伪装成普通回退：否则误操作会被记成真实经历。
func TestTransitionRefusesCorrectChangeType(t *testing.T) {
	_, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "NoFakeCorrect", "后端工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(3),
		ChangeType: domain.ChangeAdvance,
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	_, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusSaved, Version: 2, ChangeType: "correct", Reason: "点错了",
	})
	if err == nil {
		t.Fatalf("change_type=correct must be refused on /transitions")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Code != "use_correction" {
		t.Fatalf("err = %v, want use_correction", err)
	}
}

// 验收：老数据未补充子状态 —— 可以继续更新，不被误判成准备或完成。
func TestLegacyNullSubstatusStaysUnsubdivided(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "Legacy", "iOS 工程师")

	// 模拟迁移前的行：直接写库，substatus 为 NULL。
	if _, err := db.Pool().Exec(ctx,
		`UPDATE applications SET status='assessment', submitted_at=now(), substatus=NULL WHERE id=$1`, app.ID,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	row, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Substatus != "" {
		t.Fatalf("legacy NULL substatus must read as 未细分, got %q", row.Substatus)
	}
	if label := domain.ComboLabel(row.Status, row.Substatus); label != "OA / 作业 · 进度未细分" {
		t.Errorf("label = %q, want 未细分 wording", label)
	}

	// 旧客户端只发 status（不带 substatus）：同阶段必须保留「未细分」，
	// 而不是擅自生成准备/完成事实（方案 §6.8）。
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, Version: row.Version, Note: "只改备注",
	}); err == nil {
		t.Fatalf("真的一点都没变时必须答 same_status")
	}
	// 失败的转换不会推进版本号，重新读一次再继续。
	row, err = svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	row, err = svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: row.Version, Reason: "约面试",
	})
	if err != nil {
		t.Fatalf("legacy row must remain updatable: %v", err)
	}
	if row.Substatus != "" {
		t.Errorf("跨阶段不得沿用旧子状态，也不得凭空生成，got %q", row.Substatus)
	}
}

// 完成一个已经离开的阶段里的旧 OA，不能把申请拖回 OA 阶段。
func TestSyncingOldAssessmentDoesNotDragStageBack(t *testing.T) {
	db, svc, acts, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "StageScope", "全栈工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(9),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAssessment, ToSubstatus: domain.SubPreparing, Version: 2,
		Assessment: &appservice.AssessmentInput{Kind: "online_test", Name: "OA"},
	})
	if err != nil {
		t.Fatalf("assessment: %v", err)
	}
	oaID := *row.FocusActivityID

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusInterviewing, Version: 3, Reason: "HR 直接约面",
	}); err != nil {
		t.Fatalf("interviewing: %v", err)
	}

	// 用户在轮次卡片上补点「已完成」——这是离开 OA 阶段之后的事。
	if err := acts.UpdateAssessment(ctx, db, &actrepo.AssessmentRound{
		ID: oaID, ApplicationID: app.ID, OwnerID: owner, Kind: "online_test", Name: "OA",
		Progress: domain.ActProgressCompleted, Result: domain.ActResultUnknown, CompletedUnknown: true,
	}); err != nil {
		t.Fatalf("update round: %v", err)
	}
	if err := svc.SyncActivityProgress(ctx, db, owner, app.ID, domain.ActivityAssessment, oaID,
		domain.ActProgressCompleted, domain.ActResultUnknown); err != nil {
		t.Fatalf("sync: %v", err)
	}

	after, err := svc.Get(ctx, owner, app.ID, false)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Status != domain.StatusInterviewing {
		t.Fatalf("status = %q, want interviewing (an old OA must not drag it back)", after.Status)
	}
}

// 验收：错误事件不计入转化 —— 误点「拿到 Offer」被更正后，转化率必须回落，
// 而真实回退（面试 → 准备材料）仍保留「曾到达面试」的记录（方案 §5）。
func TestAnalyticsExcludesCorrectedEvents(t *testing.T) {
	db, svc, _, owner := newProgressService(t)
	ctx := context.Background()

	app := mustCreate(t, svc, owner, "FunnelCorrection", "数据工程师")
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(4),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusOffer, Version: 2, Reason: "误点",
	})
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	before, err := analytics.New(db).Counts(ctx, &analytics.SnapshotRequest{OwnerID: owner, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if before.ReceivedOffer != 1 {
		t.Fatalf("setup: received_offer = %d, want 1", before.ReceivedOffer)
	}

	// 更正这个错误的 Offer：其实只是初筛沟通。
	if _, err := svc.CorrectCurrent(ctx, owner, app.ID, &appservice.CorrectCurrentInput{
		ToStatus: domain.StatusScreening, Reason: "其实还没到 Offer", Version: row.Version,
	}); err != nil {
		t.Fatalf("correction: %v", err)
	}
	after, err := analytics.New(db).Counts(ctx, &analytics.SnapshotRequest{OwnerID: owner, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if after.ReceivedOffer != 0 {
		t.Errorf("a corrected Offer must leave the funnel, got received_offer = %d", after.ReceivedOffer)
	}
	if after.OfferRate == nil || *after.OfferRate != 0 {
		t.Errorf("offer_rate = %v, want 0", after.OfferRate)
	}
}

// review 修复回归：更正重放必须用「各事件自己记录的原因」验证历史跳变，
// 否则合法时间线无法更正 —— applied → 被拒绝(有原因) → 重开回 Offer(有原因)，
// 用户想把误点的 Offer 更正掉时，弹窗根本没给原因输入框，重放却拿本次更正的
// 空原因去验 rejected→X 这条需要原因的历史跳变。
func TestCorrectionReplayUsesEachEventsOwnReason(t *testing.T) {
	_, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "ReplayReason", "后端工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(6),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusRejected, Version: 2, Reason: "简历未通过",
	}); err != nil {
		t.Fatalf("rejected: %v", err)
	}
	row, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusOffer, Version: 3, Reason: "招聘方重新联系",
	})
	if err != nil {
		t.Fatalf("reopen to offer: %v", err)
	}

	// 更正最后一条（重开回 Offer）为面试，原因留空 —— 前端在 needsReason=false
	// 时不显示原因框。被改写的跳变会回退用原事件记录的原因，历史跳变用各自的。
	row, err = svc.CorrectCurrent(ctx, owner, app.ID, &appservice.CorrectCurrentInput{
		ToStatus: domain.StatusInterviewing, ToSubstatus: domain.SubAwaitingSchedule,
		Reason: "", Version: row.Version,
	})
	if err != nil {
		t.Fatalf("correction without a caller reason must fall back to the event's own reason: %v", err)
	}
	if row.Status != domain.StatusInterviewing || row.Substatus != domain.SubAwaitingSchedule {
		t.Fatalf("after correction got %s/%s, want interviewing/awaiting_schedule", row.Status, row.Substatus)
	}
}

// review 修复回归：更正不能凭空捏造 Offer。→accepted 只可能从 Offer 位置的
// 跳变改出来（allowedTarget 已挡住其它入口），重放据此重构 HadOffer —— 旧实现
// 把它硬编码成 true，等于把这条证据检查整个交给了调用方。
func TestCorrectionCannotMintAnAcceptance(t *testing.T) {
	_, svc, _, owner := newProgressService(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "MintGuard", "算法工程师")

	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusApplied, Version: 1, SubmittedAt: submittedAt(3),
	}); err != nil {
		t.Fatalf("applied: %v", err)
	}
	// 没到过 Offer：把 已投递 那步直接更正成 已接受 —— 必须被拒。
	_, err := svc.CorrectCurrent(ctx, owner, app.ID, &appservice.CorrectCurrentInput{
		ToStatus: domain.StatusAccepted, Reason: "直接改成已接受", Version: 2,
	})
	if err == nil {
		t.Fatalf("correcting straight to 已接受 without any Offer record must be refused")
	}
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Code != "correction_invalid" {
		t.Fatalf("err = %v, want correction_invalid", err)
	}

	// 对照 A：真实到达过 Offer 的记录，把 Offer 之后那步更正成 已接受 可以。
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusOffer, Version: 2, Reason: "约谈",
	}); err != nil {
		t.Fatalf("offer: %v", err)
	}
	if _, err := svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAccepted, Version: 3,
	}); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	// 现在把「误点成已接受」那步更正回 Offer，再对照一次正向路径仍然合法。
	row, err := svc.CorrectCurrent(ctx, owner, app.ID, &appservice.CorrectCurrentInput{
		ToStatus: domain.StatusOffer, Reason: "其实还没接受", Version: 4,
	})
	if err != nil {
		t.Fatalf("correcting accepted back to offer: %v", err)
	}
	if row.Status != domain.StatusOffer {
		t.Fatalf("status = %q, want offer", row.Status)
	}
	// 更正已把错误那步作废：当前进度回到 Offer，重新决定接受是一次真实的
	// 状态变更，走普通流转（唯一能进 已接受 的入口），而不是再更正一次。
	row, err = svc.Transition(ctx, owner, app.ID, &appservice.TransitionInput{
		ToStatus: domain.StatusAccepted, Version: row.Version,
	})
	if err != nil {
		t.Fatalf("re-accepting after the correction: %v", err)
	}
	if row.Status != domain.StatusAccepted {
		t.Fatalf("status = %q, want accepted", row.Status)
	}
}
