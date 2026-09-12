package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	actrepo "offerlog/backend/internal/activities/repository"
	appdomain "offerlog/backend/internal/applications/domain"
	notifrepo "offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/day"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/platform/observability"
	"offerlog/backend/internal/platform/timeutil"
)

type Handler struct {
	repo *actrepo.Repo
	nots *notifrepo.Repo
	// sync keeps the owning application's substatus / focus round in step with
	// the activity's own progress (方案 §6.1). Wired by bootstrap to the
	// applications repository; nil in tests that only exercise activities.
	sync ProgressSync
}

// ProgressSync keeps the owning application's snapshot in step with what this
// package writes. Implemented by the applications repository, so the activity
// write and the application update commit in one transaction.
//
//   - SyncFromActivity mirrors an activity round's progress onto the substatus;
//   - RecomputeStatus re-derives the whole stage from the application's
//     timeline (迁移 00006), and is what every milestone write ends with.
type ProgressSync interface {
	SyncFromActivity(ctx context.Context, q database.Querier, ownerID, appID int64, kind string, activityID int64, substatus string) error
	RecomputeStatus(ctx context.Context, q database.Querier, appID, ownerID int64) error
}

func New(repo *actrepo.Repo) *Handler { return &Handler{repo: repo} }

// WithApplicationSync attaches the application progress mirror.
func (h *Handler) WithApplicationSync(s ProgressSync) *Handler {
	h.sync = s
	return h
}

// mirrorProgress pushes a round's derived substatus onto the application inside
// the caller's transaction. A no-op when no syncer is wired.
func (h *Handler) mirrorProgress(ctx context.Context, q database.Querier, ownerID, appID int64, kind string, activityID int64, progress, result string) error {
	if h.sync == nil {
		return nil
	}
	return h.sync.SyncFromActivity(ctx, q, ownerID, appID, kind, activityID,
		appdomain.SubstatusForActivity(kind, progress, result))
}

// recomputeStage re-derives the application's stage from its timeline inside
// the caller's transaction. A no-op when no syncer is wired (tests that only
// exercise activities). 每一条改动时间线的写路径都必须以它结尾——这是
// 「状态 = 时间线最后一格」唯一的兑现点。
func (h *Handler) recomputeStage(ctx context.Context, q database.Querier, ownerID, appID int64) error {
	if h.sync == nil {
		return nil
	}
	return h.sync.RecomputeStatus(ctx, q, appID, ownerID)
}

// WithNotifications attaches the in-app notification store so action lifecycle
// endpoints (done / postpone) can keep reminders in sync with the action.
func (h *Handler) WithNotifications(n *notifrepo.Repo) *Handler {
	h.nots = n
	return h
}

// Routes mounts the activities sub-resources under an authenticated group.
// The group must be created with path /applications/:app_id and CSRF already
// applied by the parent router.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	// interviews
	g.GET("/interviews", h.listInterviews)
	g.POST("/interviews", h.createInterview)
	g.PATCH("/interviews/:interview_id", h.updateInterview)
	g.DELETE("/interviews/:interview_id", h.deleteInterview)
	g.POST("/interviews/:interview_id/cancel", h.cancelInterview)
	g.POST("/interviews/:interview_id/uncancel", h.uncancelInterview)
	// 面试自身的完成事实（≠ 通过结果）：方案 §3.3
	g.POST("/interviews/:interview_id/complete", h.completeInterview)
	g.POST("/interviews/:interview_id/reopen", h.reopenInterview)
	// assessments (OA / 作业轮次)
	g.GET("/assessments", h.listAssessments)
	g.POST("/assessments", h.createAssessment)
	g.PATCH("/assessments/:assessment_id", h.updateAssessment)
	g.DELETE("/assessments/:assessment_id", h.deleteAssessment)
	g.POST("/assessments/:assessment_id/complete", h.completeAssessment)
	g.POST("/assessments/:assessment_id/reopen", h.reopenAssessment)
	// milestones (用户自定义时间线节点，migration 00005)
	g.GET("/milestones", h.listMilestones)
	g.POST("/milestones", h.createMilestone)
	g.PATCH("/milestones/:milestone_id", h.updateMilestone)
	g.DELETE("/milestones/:milestone_id", h.deleteMilestone)
	// actions
	g.GET("/actions", h.listActions)
	g.POST("/actions", h.createAction)
	g.PATCH("/actions/:action_id", h.updateAction)
	g.DELETE("/actions/:action_id", h.deleteAction)
	g.POST("/actions/:action_id/done", h.markDone)
	g.POST("/actions/:action_id/postpone", h.postponeAction)
	// notes
	g.GET("/notes", h.listNotes)
	g.POST("/notes", h.createNote)
	g.PATCH("/notes/:note_id", h.updateNote)
	g.DELETE("/notes/:note_id", h.deleteNote)
}

func (h *Handler) appID(c *gin.Context) (int64, error) { return httpx.PathID(c, "id") }

// ActionsRoot exposes the cross-application action endpoints used by the
// today dashboard (open items across every application).
func (h *Handler) ActionsRoot(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.listActions)
	g.PATCH("/:action_id", h.updateAction)
	g.DELETE("/:action_id", h.deleteAction)
	g.POST("/:action_id/done", h.markDone)
	g.POST("/:action_id/postpone", h.postponeAction)
}

// -- interviews --

type scheduleDTO struct {
	MeetingURL       string `json:"meeting_url"`
	Location         string `json:"location"`
	ContactName      string `json:"contact_name"`
	ContactEmail     string `json:"contact_email"`
	Notes            string `json:"notes"`
	Cancelled        bool   `json:"cancelled"`
	CancelledReason  string `json:"cancelled_reason"`
	OriginalTimezone string `json:"original_timezone"`
}

type interviewDTO struct {
	ID              int64      `json:"id"`
	ApplicationID   int64      `json:"application_id"`
	RoundName       string     `json:"round_name"`
	Format          string     `json:"format"`
	ScheduledAt     *time.Time `json:"scheduled_at"`
	Timezone        string     `json:"timezone"`
	DurationMinutes *int       `json:"duration_minutes"`
	Result          string     `json:"result"`
	// Progress is the activity state — 待安排 / 准备中 / 已完成 / 已取消 —
	// deliberately separate from Result so 「面完了」 never auto-reads 「过了」.
	Progress         string     `json:"progress"`
	InvitedAt        *time.Time `json:"invited_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	CompletedUnknown bool       `json:"completed_unknown"`
	Feedback         string     `json:"feedback"`
	Notes            string     `json:"notes"`
	CreatedAt        time.Time  `json:"created_at"`
	// schedule (upserted into schedule_links)
	Schedule *scheduleDTO `json:"schedule,omitempty"`
}

func (h *Handler) listInterviews(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	items, err := h.repo.ListInterviews(c.Request.Context(), appID, user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	out := make([]interviewDTO, 0, len(items))
	for _, it := range items {
		out = append(out, interviewToDTO(it, nil))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// resultOrUnknown maps the legacy 'pending' / ” result values onto the
// explicit 'unknown' the round model uses: 没有反馈就是不未知，不是未通过。
func resultOrUnknown(r string) string {
	if r == "" || r == "pending" {
		return actrepo.ResultUnknown
	}
	return r
}

func interviewToDTO(it *actrepo.Interview, sch *actrepo.ScheduleLink) interviewDTO {
	d := interviewDTO{
		ID: it.ID, ApplicationID: it.ApplicationID, RoundName: it.RoundName, Format: it.Format,
		ScheduledAt: it.ScheduledAt, Timezone: it.Timezone, DurationMinutes: it.DurationMinutes,
		Result: resultOrUnknown(it.Result), Progress: it.Progress, InvitedAt: it.InvitedAt,
		CompletedAt: it.CompletedAt, CompletedUnknown: it.CompletedUnknown,
		Feedback: it.Feedback, Notes: it.Notes, CreatedAt: it.CreatedAt,
	}
	if sch != nil {
		d.Schedule = &scheduleDTO{
			MeetingURL: sch.MeetingURL, Location: sch.Location, ContactName: sch.ContactName,
			ContactEmail: sch.ContactEmail, Notes: sch.Notes, Cancelled: sch.Cancelled,
			CancelledReason: sch.CancelledReason, OriginalTimezone: sch.OriginalTimezone,
		}
	}
	return d
}

// loadSchedule decorates an interview DTO with its scheduling metadata (nil
// when none). Returns the raw link too so callers can inspect cancellation.
func (h *Handler) loadSchedule(c *gin.Context, it *actrepo.Interview) (*actrepo.ScheduleLink, error) {
	sch, err := h.repo.GetScheduleLink(c.Request.Context(), it.ID, it.OwnerID)
	if err != nil {
		return nil, err
	}
	return sch, nil
}

func (h *Handler) createInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	var req interviewDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Ownership gate: an interview may only be attached to an application the
	// caller owns. The FK alone proves the application exists, not that it is
	// the caller's — without this check an attacker could write rows (and, via
	// the joined home todo, leak company/status) against someone else's
	// application.
	if err := h.repo.AppOwnedBy(c.Request.Context(), h.repo.Pool(), appID, user.ID); err != nil {
		httpx.WriteErr(c, ownershipErr(err))
		return
	}
	it := &actrepo.Interview{
		ApplicationID: appID, OwnerID: user.ID, RoundName: req.RoundName, Format: req.Format,
		ScheduledAt: req.ScheduledAt, Timezone: req.Timezone, DurationMinutes: req.DurationMinutes,
		Result: req.Result, Progress: req.Progress, InvitedAt: req.InvitedAt,
		CompletedAt: req.CompletedAt, CompletedUnknown: req.CompletedUnknown,
		Feedback: req.Feedback, Notes: req.Notes,
	}
	if it.Progress == "" {
		it.Progress = actrepo.ProgressAwaitingSchedule
	}
	it.Result = resultOrUnknown(it.Result)
	if !appdomain.ValidActivityProgress(appdomain.ActivityInterview, it.Progress) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_progress", "未知的面试进度"))
		return
	}
	if !appdomain.ValidActivityResult(it.Result) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_result", "未知的面试结果"))
		return
	}
	// 「完成面试」不等于「通过」：未完成的轮次不能先有结果。
	if it.Progress != actrepo.ProgressCompleted && it.Result != actrepo.ResultUnknown {
		httpx.WriteErr(c, httpx.BadRequest("result_before_completion", "尚未完成的面试不能记录通过 / 未通过结果"))
		return
	}
	// Interview + its scheduling metadata are created in ONE transaction so a
	// failure mid-way cannot leave an interview without its (optional) link or
	// a link pointing at a half-created interview. The application's substatus
	// is mirrored in the SAME transaction (方案 §6.1).
	var sch *actrepo.ScheduleLink
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := h.repo.CreateInterview(ctx, tx, it); err != nil {
			return err
		}
		if req.Schedule != nil {
			sch = &actrepo.ScheduleLink{
				InterviewID: it.ID, OwnerID: user.ID, MeetingURL: req.Schedule.MeetingURL,
				Location: req.Schedule.Location, ContactName: req.Schedule.ContactName,
				ContactEmail: req.Schedule.ContactEmail, Notes: req.Schedule.Notes,
				Cancelled: req.Schedule.Cancelled, CancelledReason: req.Schedule.CancelledReason,
				OriginalTimezone: req.Schedule.OriginalTimezone,
			}
			if err := h.repo.CreateScheduleLink(ctx, tx, sch); err != nil {
				return err
			}
		}
		return h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityInterview, it.ID, it.Progress, it.Result)
	})
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, interviewToDTO(it, sch))
}

func (h *Handler) updateInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, iid, ok := h.mustInterviewIDs(c)
	if !ok {
		return
	}
	var req interviewDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Ownership gate before mutating: only a genuine not-found is a 404; DB
	// errors surface (previously every UpdateInterview failure — including a
	// real DB outage — was masked as “面试记录不存在”).
	if _, err := h.repo.GetInterview(c.Request.Context(), h.repo.Pool(), appID, user.ID, iid); err != nil {
		writeInterviewOwnershipErr(c, err)
		return
	}
	// The row itself is re-read INSIDE the transaction with a lock: building
	// the patch from a pre-transaction copy is the classic stale-snapshot race
	// — a concurrent complete/PATCH between this read and the write would be
	// silently overwritten (PR #23 review, same class as CorrectCurrent).
	var it *actrepo.Interview
	var sch *actrepo.ScheduleLink
	var fresh *actrepo.Interview
	err := h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		existing, err := h.repo.GetInterviewForUpdate(ctx, tx, appID, user.ID, iid)
		if err != nil {
			return err
		}
		fresh = existing
		it = &actrepo.Interview{
			ID: iid, ApplicationID: appID, OwnerID: user.ID, RoundName: req.RoundName, Format: req.Format,
			ScheduledAt: req.ScheduledAt, Timezone: req.Timezone, DurationMinutes: req.DurationMinutes,
			Result: req.Result, Progress: req.Progress, InvitedAt: req.InvitedAt,
			CompletedAt: req.CompletedAt, CompletedUnknown: req.CompletedUnknown,
			Feedback: req.Feedback, Notes: req.Notes,
		}
		if it.Progress == "" {
			it.Progress = existing.Progress
		}
		if it.Result == "" {
			it.Result = existing.Result
		}
		// A PATCH that does not mention the completion facts must not erase them:
		// the wire shape cannot distinguish "omitted" from "null" for a timestamp,
		// and silently dropping a recorded completed_at would lose real history.
		if it.CompletedAt == nil {
			it.CompletedAt = existing.CompletedAt
		}
		if it.InvitedAt == nil {
			it.InvitedAt = existing.InvitedAt
		}
		it.Result = resultOrUnknown(it.Result)
		// A legacy round carries progress='' (未细分). Updating its schedule must not
		// be blocked by a refinement it never had (方案 §7): only a progress the
		// caller actually names — or one already recorded — is validated.
		if it.Progress != "" && !appdomain.ValidActivityProgress(appdomain.ActivityInterview, it.Progress) {
			return httpx.BadRequest("invalid_progress", "未知的面试进度")
		}
		if !appdomain.ValidActivityResult(it.Result) {
			return httpx.BadRequest("invalid_result", "未知的面试结果")
		}
		if it.Progress != "" && it.Progress != actrepo.ProgressCompleted && it.Result != actrepo.ResultUnknown {
			return httpx.BadRequest("result_before_completion", "尚未完成的面试不能记录通过 / 未通过结果")
		}
		if it.Progress == actrepo.ProgressCompleted && it.CompletedAt == nil {
			// 完成的轮次至少要知道「完成了」，不能既没有时间也没有完成标记；
			// 时间不详就标未知，而不是填一个看起来精确的假时间。
			it.CompletedUnknown = true
		}
		// Interview update + its scheduling metadata are written in ONE transaction
		// (same shape as createInterview) so a failure mid-way cannot leave the
		// interview updated without its (optional) schedule link, or a link
		// pointing at a stale interview.
		if err := h.repo.UpdateInterview(ctx, tx, it); err != nil {
			return err
		}
		if req.Schedule != nil {
			sch = &actrepo.ScheduleLink{
				InterviewID: iid, OwnerID: user.ID, MeetingURL: req.Schedule.MeetingURL,
				Location: req.Schedule.Location, ContactName: req.Schedule.ContactName,
				ContactEmail: req.Schedule.ContactEmail, Notes: req.Schedule.Notes,
				Cancelled: req.Schedule.Cancelled, CancelledReason: req.Schedule.CancelledReason,
				OriginalTimezone: req.Schedule.OriginalTimezone,
			}
			if err := h.repo.UpsertScheduleLink(ctx, tx, sch); err != nil {
				return err
			}
		}
		return h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityInterview, iid, it.Progress, it.Result)
	})
	if err != nil {
		writeUpsertErr(c, err)
		return
	}
	// Rescheduling must clear the stale “明天有面试” occurrence: the idempotency
	// key is interview:<id>:<day> and the body carries the interview time, so
	// ANY change to scheduled_at (day OR time) leaves the old notification
	// pinned and stale — it would also mute the fresh one. Clearing here frees
	// the key; the daily generator re-arms the reminder for the new time on its
	// next pass (same lifecycle as cancel/uncancel). Best-effort.
	if h.nots != nil && scheduledAtChanged(fresh, req.ScheduledAt) {
		if err := h.nots.ClearInterviewReminders(c.Request.Context(), user.ID, iid); err != nil {
			observability.L(c.Request.Context()).Warn("clear interview reminders on reschedule",
				"interview_id", iid, "error", err)
		}
	}
	c.JSON(http.StatusOK, interviewToDTO(it, sch))
}

func (h *Handler) deleteInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, iid, ok := h.mustInterviewIDs(c)
	if !ok {
		return
	}
	// Ownership gate first so a foreign interview is a clean 404 and a real DB
	// failure is not reported as “记录不存在”.
	if _, err := h.repo.GetInterview(c.Request.Context(), h.repo.Pool(), appID, user.ID, iid); err != nil {
		writeInterviewOwnershipErr(c, err)
		return
	}
	if err := h.repo.DeleteInterview(c.Request.Context(), h.repo.Pool(), appID, user.ID, iid); err != nil {
		writeUpsertErr(c, err)
		return
	}
	// Deleting an interview must not leave its generated reminders behind
	// (best-effort, same as cancel/reschedule).
	if h.nots != nil {
		if err := h.nots.ClearInterviewReminders(c.Request.Context(), user.ID, iid); err != nil {
			observability.L(c.Request.Context()).Warn("clear interview reminders on delete",
				"interview_id", iid, "error", err)
		}
	}
	httpx.Ok(c)
}

type cancelReq struct {
	Reason string `json:"reason"`
}

// completeReq records that a round actually happened. completed_at is optional
// on purpose: a user who forgot the exact date marks it unknown rather than
// having a precise-looking timestamp invented for them (方案 §3.3).
type completeReq struct {
	CompletedAt *time.Time `json:"completed_at"`
	Unknown     bool       `json:"completed_unknown"`
	Result      string     `json:"result"`
	Feedback    string     `json:"feedback"`
}

// -- interviews: progress lifecycle --

func (h *Handler) completeInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, iid, ok := h.mustInterviewIDs(c)
	if !ok {
		return
	}
	var req completeReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if req.Result != "" && !appdomain.ValidActivityResult(req.Result) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_result", "未知的面试结果"))
		return
	}
	var out *actrepo.Interview
	err := h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		it, err := h.repo.GetInterviewForUpdate(ctx, tx, appID, user.ID, iid)
		if err != nil {
			return err
		}
		it.Progress = actrepo.ProgressCompleted
		if req.Result != "" {
			it.Result = req.Result
		}
		if req.Feedback != "" {
			it.Feedback = req.Feedback
		}
		// 只知道「面完了」但不能确定哪天：保留 completed_at 为空并标记未知。
		if req.CompletedAt != nil {
			at := req.CompletedAt.UTC()
			it.CompletedAt = &at
			it.CompletedUnknown = false
		} else if it.CompletedAt == nil {
			// 只知道「面完了」但不能确定哪天：completed_at 留空并标记未知，
			// 不填一个看起来精确的假时间。
			it.CompletedUnknown = true
		}
		if err := h.repo.UpdateInterview(ctx, tx, it); err != nil {
			return err
		}
		if err := h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityInterview, iid, it.Progress, it.Result); err != nil {
			return err
		}
		out = it
		return nil
	})
	if err != nil {
		writeInterviewOwnershipErr(c, err)
		return
	}
	c.JSON(http.StatusOK, interviewToDTO(out, nil))
}

// reopenInterview walks a round back from 已完成 to 准备中 — a wrong「面完了」
// tap is a correction of the round, not a stage rollback (方案 §3.3).
func (h *Handler) reopenInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, iid, ok := h.mustInterviewIDs(c)
	if !ok {
		return
	}
	var out *actrepo.Interview
	err := h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		it, err := h.repo.GetInterviewForUpdate(ctx, tx, appID, user.ID, iid)
		if err != nil {
			return err
		}
		it.Progress = actrepo.ProgressPreparing
		// A reopened round has no result yet; keep the feedback text.
		it.Result = actrepo.ResultUnknown
		it.CompletedAt = nil
		it.CompletedUnknown = false
		if err := h.repo.UpdateInterview(ctx, tx, it); err != nil {
			return err
		}
		if err := h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityInterview, iid, it.Progress, it.Result); err != nil {
			return err
		}
		out = it
		return nil
	})
	if err != nil {
		writeInterviewOwnershipErr(c, err)
		return
	}
	c.JSON(http.StatusOK, interviewToDTO(out, nil))
}

// -- assessments (OA / 作业) --

type assessmentDTO struct {
	ID               int64      `json:"id"`
	ApplicationID    int64      `json:"application_id"`
	Kind             string     `json:"kind"`
	Name             string     `json:"name"`
	Progress         string     `json:"progress"`
	Result           string     `json:"result"`
	InvitedAt        *time.Time `json:"invited_at"`
	PlannedAt        *time.Time `json:"planned_at"`
	DueAt            *time.Time `json:"due_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	CompletedUnknown bool       `json:"completed_unknown"`
	Link             string     `json:"link"`
	Notes            string     `json:"notes"`
	CreatedAt        time.Time  `json:"created_at"`
}

func assessmentToDTO(a *actrepo.AssessmentRound) assessmentDTO {
	return assessmentDTO{
		ID: a.ID, ApplicationID: a.ApplicationID, Kind: a.Kind, Name: a.Name,
		Progress: a.Progress, Result: resultOrUnknown(a.Result), InvitedAt: a.InvitedAt,
		PlannedAt: a.PlannedAt, DueAt: a.DueAt, CompletedAt: a.CompletedAt,
		CompletedUnknown: a.CompletedUnknown, Link: a.Link, Notes: a.Notes, CreatedAt: a.CreatedAt,
	}
}

func (h *Handler) listAssessments(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	items, err := h.repo.ListAssessments(c.Request.Context(), appID, user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	out := make([]assessmentDTO, 0, len(items))
	for _, a := range items {
		out = append(out, assessmentToDTO(a))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// assessmentPayload validates an OA round request. Completion and result stay
// independent: an OA the user finished but has not heard back on is
// progress=completed, result=unknown — never inferred as passed.
func assessmentPayload(c *gin.Context, req *assessmentDTO) (*actrepo.AssessmentRound, bool) {
	a := &actrepo.AssessmentRound{
		ID: req.ID, ApplicationID: req.ApplicationID, Kind: req.Kind, Name: req.Name,
		Progress: req.Progress, Result: req.Result, InvitedAt: req.InvitedAt,
		PlannedAt: req.PlannedAt, DueAt: req.DueAt, CompletedAt: req.CompletedAt,
		CompletedUnknown: req.CompletedUnknown, Link: req.Link, Notes: req.Notes,
	}
	if a.Kind == "" {
		a.Kind = "online_test"
	}
	if a.Kind != "online_test" && a.Kind != "take_home" && a.Kind != "other" {
		httpx.WriteErr(c, httpx.BadRequest("invalid_kind", "未知的测评类型"))
		return nil, false
	}
	if a.Progress == "" {
		a.Progress = actrepo.ProgressPreparing
	}
	if !appdomain.ValidActivityProgress(appdomain.ActivityAssessment, a.Progress) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_progress", "未知的测评进度"))
		return nil, false
	}
	a.Result = resultOrUnknown(a.Result)
	if !appdomain.ValidActivityResult(a.Result) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_result", "未知的测评结果"))
		return nil, false
	}
	if a.Progress != actrepo.ProgressCompleted && a.Result != actrepo.ResultUnknown {
		httpx.WriteErr(c, httpx.BadRequest("result_before_completion", "尚未完成的测评不能记录通过 / 未通过结果"))
		return nil, false
	}
	if a.Progress == actrepo.ProgressCompleted && a.CompletedAt == nil {
		a.CompletedUnknown = true
	}
	return a, true
}

func (h *Handler) createAssessment(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	var req assessmentDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if err := h.repo.AppOwnedBy(c.Request.Context(), h.repo.Pool(), appID, user.ID); err != nil {
		httpx.WriteErr(c, ownershipErr(err))
		return
	}
	a, ok := assessmentPayload(c, &req)
	if !ok {
		return
	}
	a.ApplicationID, a.OwnerID = appID, user.ID
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := h.repo.CreateAssessment(ctx, tx, a); err != nil {
			return err
		}
		return h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityAssessment, a.ID, a.Progress, a.Result)
	})
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, assessmentToDTO(a))
}

func (h *Handler) updateAssessment(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	aid, err := httpx.PathID(c, "assessment_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的测评 ID"))
		return
	}
	var req assessmentDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Ownership gate before mutating (see updateInterview): a missing row is a
	// clean 404, a real DB failure is not masked as one.
	if _, err := h.repo.GetAssessment(c.Request.Context(), h.repo.Pool(), appID, user.ID, aid); err != nil {
		writeAssessmentErr(c, err)
		return
	}
	// The row itself is re-read INSIDE the transaction with a lock, exactly like
	// updateInterview: merging the patch into a pre-transaction copy is the
	// stale-snapshot race d62f433 fixed for interviews (PR #23 review).
	a, ok := assessmentPayload(c, &req)
	if !ok {
		return
	}
	a.ID, a.ApplicationID, a.OwnerID = aid, appID, user.ID
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		existing, err := h.repo.GetAssessmentForUpdate(ctx, tx, appID, user.ID, aid)
		if err != nil {
			return err
		}
		if req.Progress == "" {
			a.Progress = existing.Progress
		}
		if req.Result == "" {
			a.Result = existing.Result
		}
		if req.Kind == "" {
			a.Kind = existing.Kind
		}
		// Mirror updateInterview: a PATCH that does not mention a recorded time
		// must not erase it. The wire shape cannot distinguish 「omitted」 from
		// 「null」 for a timestamp, so a note-only PATCH would otherwise drop the
		// invited/planned/due/completed facts one by one (方案 §3.2 四种时间各自保存).
		if req.InvitedAt == nil {
			a.InvitedAt = existing.InvitedAt
		}
		if req.PlannedAt == nil {
			a.PlannedAt = existing.PlannedAt
		}
		if req.DueAt == nil {
			a.DueAt = existing.DueAt
		}
		if req.CompletedAt == nil {
			a.CompletedAt = existing.CompletedAt
		}
		if req.CompletedAt == nil && existing.CompletedUnknown {
			// 完成时间不详的标记同样不能被顺带清掉。
			a.CompletedUnknown = true
		}
		if a.Progress == actrepo.ProgressCompleted && a.CompletedAt == nil && !a.CompletedUnknown {
			// Same normalization as assessmentPayload: a completed round must at
			// least record that it completed — a nil time with no unknown flag
			// would read as 「没做完」 in every derived view. Payload ran before the
			// merge, so it could not see the merged facts; do it here instead.
			a.CompletedUnknown = true
		}
		if err := h.repo.UpdateAssessment(ctx, tx, a); err != nil {
			return err
		}
		return h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityAssessment, aid, a.Progress, a.Result)
	})
	if err != nil {
		writeAssessmentErr(c, err)
		return
	}
	c.JSON(http.StatusOK, assessmentToDTO(a))
}

func (h *Handler) deleteAssessment(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	aid, err := httpx.PathID(c, "assessment_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的测评 ID"))
		return
	}
	if _, err := h.repo.GetAssessment(c.Request.Context(), h.repo.Pool(), appID, user.ID, aid); err != nil {
		writeAssessmentErr(c, err)
		return
	}
	if err := h.repo.DeleteAssessment(c.Request.Context(), h.repo.Pool(), appID, user.ID, aid); err != nil {
		writeAssessmentErr(c, err)
		return
	}
	httpx.Ok(c)
}

func (h *Handler) completeAssessment(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	aid, err := httpx.PathID(c, "assessment_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的测评 ID"))
		return
	}
	var req completeReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if req.Result != "" && !appdomain.ValidActivityResult(req.Result) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_result", "未知的测评结果"))
		return
	}
	var out *actrepo.AssessmentRound
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		a, err := h.repo.GetAssessmentForUpdate(ctx, tx, appID, user.ID, aid)
		if err != nil {
			return err
		}
		a.Progress = actrepo.ProgressCompleted
		if req.Result != "" {
			a.Result = req.Result
		}
		if req.CompletedAt != nil {
			at := req.CompletedAt.UTC()
			a.CompletedAt = &at
			a.CompletedUnknown = false
		} else if a.CompletedAt == nil {
			a.CompletedUnknown = true
		}
		if err := h.repo.UpdateAssessment(ctx, tx, a); err != nil {
			return err
		}
		if err := h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityAssessment, aid, a.Progress, a.Result); err != nil {
			return err
		}
		out = a
		return nil
	})
	if err != nil {
		writeAssessmentErr(c, err)
		return
	}
	c.JSON(http.StatusOK, assessmentToDTO(out))
}

func (h *Handler) reopenAssessment(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	aid, err := httpx.PathID(c, "assessment_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的测评 ID"))
		return
	}
	var out *actrepo.AssessmentRound
	err = h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		a, err := h.repo.GetAssessmentForUpdate(ctx, tx, appID, user.ID, aid)
		if err != nil {
			return err
		}
		a.Progress = actrepo.ProgressPreparing
		a.Result = actrepo.ResultUnknown
		a.CompletedAt = nil
		a.CompletedUnknown = false
		if err := h.repo.UpdateAssessment(ctx, tx, a); err != nil {
			return err
		}
		if err := h.mirrorProgress(ctx, tx, user.ID, appID, appdomain.ActivityAssessment, aid, a.Progress, a.Result); err != nil {
			return err
		}
		out = a
		return nil
	})
	if err != nil {
		writeAssessmentErr(c, err)
		return
	}
	c.JSON(http.StatusOK, assessmentToDTO(out))
}

// writeAssessmentErr maps OA-round repo errors: only a genuine not-found is a
// 404; real DB failures pass through untouched.
func writeAssessmentErr(c *gin.Context, err error) {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, actrepo.ErrNotFound) {
		httpx.WriteErr(c, httpx.NotFound("测评记录不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

// writeUpsertErr maps the schedule-link repo errors: not-found → 404; any
// other (genuine DB) error is passed through untouched, never masked as 404.
func writeUpsertErr(c *gin.Context, err error) {
	if errors.Is(err, actrepo.ErrNotFound) {
		httpx.WriteErr(c, httpx.NotFound("面试记录不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

// writeActionErr maps action-repo errors: only a genuine not-found (pgx
// ErrNoRows) is a 404; real DB failures pass through as 500 instead of being
// masked as “行动项不存在”.
func writeActionErr(c *gin.Context, err error) {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, actrepo.ErrNotFound) {
		httpx.WriteErr(c, httpx.NotFound("行动项不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

// writeNoteErr is the notes counterpart.
func writeNoteErr(c *gin.Context, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteErr(c, httpx.NotFound("备注不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

// mustInterviewIDs parses both path ids, returning false after writing a 400
// when either is malformed (callers must not swallow these).
func (h *Handler) mustInterviewIDs(c *gin.Context) (appID, iid int64, ok bool) {
	var err error
	appID, err = h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return 0, 0, false
	}
	iid, err = httpx.PathID(c, "interview_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的面试 ID"))
		return 0, 0, false
	}
	return appID, iid, true
}

// writeInterviewOwnershipErr maps an ownership-check failure: only a genuine
// not-found (missing row / wrong owner) is a 404; DB errors pass through.
func writeInterviewOwnershipErr(c *gin.Context, err error) {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, actrepo.ErrNotFound) {
		httpx.WriteErr(c, httpx.NotFound("面试记录不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

// ownershipErr maps an AppOwnedBy failure for a create: the application does
// not exist or belongs to someone else → 404 (same shape as reads of a
// foreign application); genuine DB failures pass through untouched.
func ownershipErr(err error) error {
	if errors.Is(err, actrepo.ErrNotFound) || errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("申请记录不存在")
	}
	return err
}

// scheduledAtChanged reports whether the incoming scheduled_at differs from the
// stored row (nil on either side counts as a change: a cleared schedule drops
// the stale reminder too).
func scheduledAtChanged(existing *actrepo.Interview, next *time.Time) bool {
	if existing == nil {
		return true
	}
	if existing.ScheduledAt == nil || next == nil {
		return existing.ScheduledAt != nil || next != nil
	}
	return !existing.ScheduledAt.Equal(*next)
}

// cancelInterview marks an interview cancelled (改期/取消). The row is kept
// so history is intact; dashboards and reminders exclude cancelled rounds.
func (h *Handler) cancelInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, iid, ok := h.mustInterviewIDs(c)
	if !ok {
		return
	}
	var req cancelReq
	// reason is optional — an empty body (or no JSON) is fine for cancel.
	_ = httpx.BindJSON(c, &req)
	// Ownership check first: only a real not-found is 404; DB errors surface.
	if _, err := h.repo.GetInterview(c.Request.Context(), h.repo.Pool(), appID, user.ID, iid); err != nil {
		writeInterviewOwnershipErr(c, err)
		return
	}
	sch, err := h.repo.GetScheduleLink(c.Request.Context(), iid, user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if sch == nil {
		sch = &actrepo.ScheduleLink{InterviewID: iid, OwnerID: user.ID}
	}
	sch.Cancelled = true
	sch.CancelledReason = req.Reason
	if err := h.repo.UpsertScheduleLink(c.Request.Context(), h.repo.Pool(), sch); err != nil {
		writeUpsertErr(c, err)
		return
	}
	// A cancelled interview must not keep its generated “明天有面试” reminder.
	if h.nots != nil {
		if err := h.nots.ClearInterviewReminders(c.Request.Context(), user.ID, iid); err != nil {
			observability.L(c.Request.Context()).Warn("clear interview reminders",
				"interview_id", iid, "error", err)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "cancelled": true})
}

func (h *Handler) uncancelInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, iid, ok := h.mustInterviewIDs(c)
	if !ok {
		return
	}
	// Ownership check first: only a real not-found is 404; DB errors surface.
	if _, err := h.repo.GetInterview(c.Request.Context(), h.repo.Pool(), appID, user.ID, iid); err != nil {
		writeInterviewOwnershipErr(c, err)
		return
	}
	sch, err := h.repo.GetScheduleLink(c.Request.Context(), iid, user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if sch == nil {
		sch = &actrepo.ScheduleLink{InterviewID: iid, OwnerID: user.ID}
	}
	sch.Cancelled = false
	sch.CancelledReason = ""
	if err := h.repo.UpsertScheduleLink(c.Request.Context(), h.repo.Pool(), sch); err != nil {
		writeUpsertErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "cancelled": false})
}

// -- actions --

type actionDTO struct {
	ID            int64      `json:"id"`
	ApplicationID *int64     `json:"application_id"`
	Title         string     `json:"title"`
	DueDate       *string    `json:"due_date"` // calendar day YYYY-MM-DD (DATE column)
	DueTs         *time.Time `json:"due_ts"`   // precise instant (TIMESTAMPTZ column)
	DoneAt        *time.Time `json:"done_at"`
	RemindMe      bool       `json:"remind_me"`
	RemindAt      *time.Time `json:"remind_at"`
	Priority      string     `json:"priority"`
	CreatedAt     time.Time  `json:"created_at"`
	CompanyName   string     `json:"company_name"`
	Position      string     `json:"position"`
	Status        string     `json:"status"`
}

func actionToDTO(a *actrepo.Action) actionDTO {
	d := actionDTO{
		ID: a.ID, ApplicationID: a.ApplicationID, Title: a.Title,
		DueTs: a.DueTs, DoneAt: a.DoneAt,
		RemindMe: a.RemindMe, RemindAt: a.RemindAt, Priority: a.Priority, CreatedAt: a.CreatedAt,
		CompanyName: a.CompanyName, Position: a.Position, Status: a.Status,
	}
	if a.DueDate != nil {
		s := day.Format(*a.DueDate)
		d.DueDate = &s
	}
	return d
}

func (h *Handler) listActions(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	var appPtr *int64
	if err == nil && appID > 0 {
		appPtr = &appID
	}
	openOnly := c.Query("open") == "1"
	items, err := h.repo.ListActions(c.Request.Context(), appPtr, user.ID, openOnly)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Repo structs have no JSON tags; serialize through the DTO so the
	// contract stays camelCase like every other endpoint.
	out := make([]actionDTO, 0, len(items))
	for _, a := range items {
		out = append(out, actionToDTO(a))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *Handler) createAction(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, _ := h.appID(c)
	var req actionDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	var appPtr *int64
	if appID > 0 {
		appPtr = &appID
		// Ownership gate for app-scoped creates (ActionsRoot has no app path
		// segment): actions may only attach to applications the caller owns.
		if err := h.repo.AppOwnedBy(c.Request.Context(), h.repo.Pool(), appID, user.ID); err != nil {
			httpx.WriteErr(c, ownershipErr(err))
			return
		}
	}
	dueDate, err := parseDueDate(req.DueDate)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_due_date", err.Error()))
		return
	}
	a := &actrepo.Action{
		ApplicationID: appPtr, OwnerID: user.ID, Title: req.Title, DueDate: dueDate,
		DueTs: req.DueTs, DoneAt: req.DoneAt, RemindMe: req.RemindMe, RemindAt: req.RemindAt,
		Priority: req.Priority,
	}
	if err := h.repo.CreateAction(c.Request.Context(), h.repo.Pool(), a); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, actionToDTO(a))
}

// parseDueDate validates an optional date-only string into a UTC-midnight
// instant for the DATE column (the calendar day itself is tz-independent).
func parseDueDate(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	v := day.Normalize(*s)
	if v == "" {
		return nil, nil
	}
	if !day.Valid(v) {
		return nil, fmt.Errorf("日期格式需为 YYYY-MM-DD")
	}
	t, err := day.Parse(v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (h *Handler) updateAction(c *gin.Context) {
	user := httpx.UserFrom(c)
	aid, _ := httpx.PathID(c, "action_id")
	var req actionDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	dueDate, err := parseDueDate(req.DueDate)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_due_date", err.Error()))
		return
	}
	a := &actrepo.Action{
		ID: aid, OwnerID: user.ID, Title: req.Title, DueDate: dueDate, DueTs: req.DueTs,
		DoneAt: req.DoneAt, RemindMe: req.RemindMe, RemindAt: req.RemindAt, Priority: req.Priority,
	}
	if err := h.repo.UpdateAction(c.Request.Context(), h.repo.Pool(), a); err != nil {
		writeActionErr(c, err)
		return
	}
	h.clearOverdueReminderBestEffort(c, user.ID, aid)
	c.JSON(http.StatusOK, actionToDTO(a))
}

func (h *Handler) deleteAction(c *gin.Context) {
	user := httpx.UserFrom(c)
	aid, _ := httpx.PathID(c, "action_id")
	if err := h.repo.DeleteAction(c.Request.Context(), user.ID, aid); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// A deleted action must not leave an overdue reminder pointing at a
	// now-nonexistent item (same lifecycle sync as done/postpone/update).
	h.clearOverdueReminderBestEffort(c, user.ID, aid)
	httpx.Ok(c)
}

func (h *Handler) markDone(c *gin.Context) {
	user := httpx.UserFrom(c)
	aid, _ := httpx.PathID(c, "action_id")
	var req struct {
		Done *bool `json:"done"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	done := true
	if req.Done != nil {
		done = *req.Done
	}
	if err := h.repo.MarkActionDone(c.Request.Context(), h.repo.Pool(), user.ID, aid, done); err != nil {
		writeActionErr(c, err)
		return
	}
	h.clearOverdueReminderBestEffort(c, user.ID, aid)
	c.JSON(http.StatusOK, gin.H{"ok": true, "done": done})
}

// clearOverdueReminderBestEffort removes the action's overdue notifications so
// the next generator pass reflects the action's current state exactly once
// (done → no reminder; reopened while overdue → one fresh reminder);
// failures are logged and swallowed so a reminder-cleanup hiccup never fails an
// already-successful action mutation or writes a second response after it.
func (h *Handler) clearOverdueReminderBestEffort(c *gin.Context, ownerID, actionID int64) {
	if h.nots == nil {
		return
	}
	key := fmt.Sprintf("overdue:%d", actionID)
	if err := h.nots.ClearOccurrence(c.Request.Context(), ownerID, "overdue", key); err != nil {
		observability.L(c.Request.Context()).Warn("clear overdue reminder",
			"action_id", actionID, "error", err)
	}
}

type postponeReq struct {
	DueDate *string    `json:"due_date"` // calendar day YYYY-MM-DD
	DueTs   *time.Time `json:"due_ts"`   // precise instant
	Days    int        `json:"days"`
}

// postponeAction defers an open action to a new due date (or by N days) and
// refreshes its reminder so the delayed due date stops the old "overdue" alert.
func (h *Handler) postponeAction(c *gin.Context) {
	user := httpx.UserFrom(c)
	aid, _ := httpx.PathID(c, "action_id")
	var req postponeReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	a, err := h.repo.GetAction(c.Request.Context(), user.ID, aid)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if a == nil {
		httpx.WriteErr(c, httpx.NotFound("行动项不存在"))
		return
	}
	now := time.Now()
	if req.Days > 0 {
		// “延期 N 天”是拖延，不是从旧截止日平移：基准取 max(当前截止, 今天)。
		// 前端只在逾期项上渲染「延期」按钮 — 若从旧截止日 +N，一个逾期多日的
		// 待办延期后仍然逾期，按钮永远无法把项推出逾期区。日期口径跟随用户
		// 时区（day 键 = 用户的今天 +N），保持原 flavor：date-only 仍是
		// 日历日，instant 仍是精确时刻。
		if a.DueDate != nil && a.DueTs == nil {
			loc, _ := timeutil.SafeLocation(user.Timezone)
			todayStart, _ := timeutil.TodayBounds(now, loc)
			if a.DueDate.Before(todayStart) {
				// overdue (or due before the user's today): anchor at the
				// user's today so +N lands on a non-overdue calendar day.
				key := timeutil.DateOnly(todayStart.AddDate(0, 0, req.Days), loc)
				req.DueDate = &key
			} else {
				nd := a.DueDate.AddDate(0, 0, req.Days)
				ds := day.Format(nd)
				req.DueDate = &ds
			}
		} else {
			base := now
			if a.DueTs != nil && a.DueTs.After(now) {
				base = *a.DueTs
			}
			nd := base.AddDate(0, 0, req.Days)
			req.DueTs = &nd
			req.DueDate = nil
		}
	}
	if req.DueDate != nil || req.DueTs != nil {
		// explicit new due provided
	} else {
		httpx.WriteErr(c, httpx.BadRequest("no_due", "请提供新的截止日期或延期天数"))
		return
	}
	var dueDate *time.Time
	if req.DueDate != nil {
		ds := day.Normalize(*req.DueDate)
		if ds == "" {
			// blank date-only value means “clear the date”, mirroring the
			// create/update parseDueDate behavior — never store year-1.
			req.DueDate = nil
			req.DueTs = nil
		} else {
			t, err := day.Parse(ds)
			if err != nil {
				httpx.WriteErr(c, httpx.BadRequest("bad_due_date", err.Error()))
				return
			}
			dueDate = &t
		}
	}
	a.DueTs = req.DueTs
	if req.DueTs == nil {
		a.DueDate = dueDate
	} else {
		a.DueDate = nil
	}
	if err := h.repo.UpdateAction(c.Request.Context(), h.repo.Pool(), a); err != nil {
		writeActionErr(c, err)
		return
	}
	h.clearOverdueReminderBestEffort(c, user.ID, aid)
	c.JSON(http.StatusOK, actionToDTO(a))
}

type noteDTO struct {
	ID            int64     `json:"id"`
	ApplicationID int64     `json:"application_id"`
	ContentMD     string    `json:"content_md"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func noteToDTO(n *actrepo.Note) noteDTO {
	return noteDTO{
		ID:            n.ID,
		ApplicationID: n.ApplicationID,
		ContentMD:     n.ContentMD,
		CreatedAt:     n.CreatedAt,
		UpdatedAt:     n.UpdatedAt,
	}
}

func (h *Handler) listNotes(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, err := h.appID(c)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	items, err := h.repo.ListNotes(c.Request.Context(), appID, user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Repo struct has no JSON tags — always map through the DTO, otherwise the
	// wire keys are "ContentMD" etc. and the frontend sees no content_md.
	out := make([]noteDTO, len(items))
	for i, n := range items {
		out[i] = noteToDTO(n)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *Handler) createNote(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, _ := h.appID(c)
	var req noteDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Ownership gate (see createInterview).
	if err := h.repo.AppOwnedBy(c.Request.Context(), h.repo.Pool(), appID, user.ID); err != nil {
		httpx.WriteErr(c, ownershipErr(err))
		return
	}
	n := &actrepo.Note{ApplicationID: appID, OwnerID: user.ID, ContentMD: req.ContentMD}
	if err := h.repo.CreateNote(c.Request.Context(), h.repo.Pool(), n); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, noteToDTO(n))
}

func (h *Handler) updateNote(c *gin.Context) {
	user := httpx.UserFrom(c)
	nid, _ := httpx.PathID(c, "note_id")
	var req noteDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	n := &actrepo.Note{ID: nid, OwnerID: user.ID, ContentMD: req.ContentMD}
	if err := h.repo.UpdateNote(c.Request.Context(), h.repo.Pool(), n); err != nil {
		writeNoteErr(c, err)
		return
	}
	// Refetch so created_at/updated_at in the response are the stored values,
	// not the zero time left on the in-memory struct.
	cur, err := h.repo.GetNote(c.Request.Context(), user.ID, nid)
	if err != nil {
		writeNoteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, noteToDTO(cur))
}

func (h *Handler) deleteNote(c *gin.Context) {
	user := httpx.UserFrom(c)
	nid, _ := httpx.PathID(c, "note_id")
	if err := h.repo.DeleteNote(c.Request.Context(), user.ID, nid); err != nil {
		writeNoteErr(c, err)
		return
	}
	httpx.Ok(c)
}
