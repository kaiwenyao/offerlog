// Package transport exposes the applications module over gin under /api/v1.
package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	appdomain "offerlog/backend/internal/applications/domain"
	apprepo "offerlog/backend/internal/applications/repository"
	appservice "offerlog/backend/internal/applications/service"
	notifrepo "offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/day"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/platform/observability"
	"offerlog/backend/internal/platform/timeutil"
)

type Handler struct {
	svc  *appservice.Service
	nots *notifrepo.Repo
	url  *parseURLHandler
}

func New(svc *appservice.Service) *Handler { return &Handler{svc: svc} }

// WithParseURL attaches the JD-link prefetch handler (POST parse-url). It is
// optional so the pure-CRUD surface stays testable without outbound HTTP.
func (h *Handler) WithParseURL() *Handler {
	h.url = newParseURLHandler()
	return h
}

// WithNotifications attaches the in-app notification store so application
// lifecycle endpoints (soft delete / archive / first response / ended status)
// can retire the application's reminders instead of leaving dead links.
func (h *Handler) WithNotifications(n *notifrepo.Repo) *Handler {
	h.nots = n
	return h
}

// clearAppReminders deletes every notification that references the application
// (soft-deleted/archived/ended records must not keep producing stale alerts).
// System-side retirement is a DELETE, not a dismiss: the row's idempotency key
// must be released so restore / unarchive / terminal-reopen re-arms the next
// reminder (a dismiss-only row would mute overdue:<action>/stale:<app>:<N>
// forever). Failures are logged and swallowed so they never fail the
// already-successful primary operation or write a second HTTP response.
func (h *Handler) clearAppReminders(c *gin.Context, ownerID, appID int64) {
	if h.nots == nil {
		return
	}
	if err := h.nots.ClearByApplication(c.Request.Context(), ownerID, appID); err != nil {
		observability.L(c.Request.Context()).Warn("clear app reminders",
			"application_id", appID, "error", err)
	}
}

// appDTO is the wire representation of an application row.
type appDTO struct {
	ID             int64  `json:"id"`
	CompanyID      int64  `json:"company_id"`
	CompanyName    string `json:"company_name"`
	Position       string `json:"position"`
	JobURL         string `json:"job_url"`
	JDSnapshot     string `json:"jd_snapshot"`
	Location       string `json:"location"`
	RemotePolicy   string `json:"remote_policy"`
	EmploymentType string `json:"employment_type"`
	SalaryMin      *int64 `json:"salary_min"`
	SalaryMax      *int64 `json:"salary_max"`
	SalaryCurrency string `json:"salary_currency"`
	Channel        string `json:"channel"`
	Status         string `json:"status"`
	Substatus      string `json:"substatus"`
	// FocusActivityKind/ID point at the round the stage label is derived from
	// (方案 §3.3: 列表主标签展示用户选定的关注阶段).
	FocusActivityKind string          `json:"focus_activity_kind"`
	FocusActivityID   *int64          `json:"focus_activity_id"`
	Priority          string          `json:"priority"`
	Tags              []string        `json:"tags"`
	CustomValues      json.RawMessage `json:"custom_values"`
	Notes             string          `json:"notes"`
	SavedAt           *time.Time      `json:"saved_at"`
	SubmittedAt       *time.Time      `json:"submitted_at"`
	FirstResponseAt   *time.Time      `json:"first_response_at"`
	Deadline          *string         `json:"deadline"` // calendar day YYYY-MM-DD (DATE column)
	AcceptedAt        *time.Time      `json:"accepted_at"`
	RejectedAt        *time.Time      `json:"rejected_at"`
	Reason            string          `json:"reason"`
	NextAction        string          `json:"next_action"`
	NextActionDueAt   *string         `json:"next_action_due_at"` // calendar day YYYY-MM-DD (DATE column)
	Version           int             `json:"version"`
	Archived          bool            `json:"archived"`
	Deleted           bool            `json:"deleted"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	// StageHistory (include=stage_history) maps each reached status to the
	// earliest calendar day (user-zone YYYY-MM-DD) the application entered it.
	StageHistory map[string]string `json:"stage_history,omitempty"`
	// ProgressSince is the calendar day the CURRENT progress was entered
	// (方案 §5). Corrections move it; the superseded record no longer counts.
	ProgressSince string `json:"progress_since,omitempty"`
}

func toDTO(r *apprepo.Row) *appDTO {
	d := &appDTO{
		ID: r.ID, CompanyID: r.CompanyID, CompanyName: r.CompanyName, Position: r.Position,
		JobURL: r.JobURL, JDSnapshot: r.JDSnapshot, Location: r.Location,
		RemotePolicy: r.RemotePolicy, EmploymentType: r.EmploymentType,
		SalaryMin: r.SalaryMin, SalaryMax: r.SalaryMax, SalaryCurrency: r.SalaryCurrency,
		Channel: r.Channel, Status: r.Status, Substatus: r.Substatus,
		FocusActivityKind: r.FocusActivityKind, FocusActivityID: r.FocusActivityID,
		Priority: r.Priority, Tags: r.Tags,
		CustomValues: r.CustomValues, Notes: r.Notes, SavedAt: r.SavedAt,
		SubmittedAt: r.SubmittedAt, FirstResponseAt: r.FirstResponseAt,
		AcceptedAt: r.AcceptedAt, RejectedAt: r.RejectedAt, Reason: r.Reason,
		NextAction: r.NextAction, Version: r.Version,
		Archived: r.ArchivedAt != nil, Deleted: r.DeletedAt != nil,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.Deadline != nil {
		v := day.Format(*r.Deadline)
		d.Deadline = &v
	}
	if r.NextActionDueAt != nil {
		v := day.Format(*r.NextActionDueAt)
		d.NextActionDueAt = &v
	}
	return d
}

// parseDayPtr converts an optional date-only wire string into a UTC-midnight
// time.Time for the DATE column.
func parseDayPtr(v *string) (*time.Time, error) {
	if v == nil {
		return nil, nil
	}
	s := day.Normalize(*v)
	if s == "" {
		return nil, nil
	}
	if !day.Valid(s) {
		return nil, fmt.Errorf("日期格式需为 YYYY-MM-DD")
	}
	t, err := day.Parse(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Routes returns a group that must be mounted under /api/v1/applications with
// the CSRF-protected, authenticated parent group.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	if h.url != nil {
		g.POST("/parse-url", h.url.parseURL)
	}
	g.GET("", h.list)
	g.POST("", h.create)
	g.POST("/query", h.query)
	g.GET("/:id", h.get)
	g.PATCH("/:id", h.patch)
	g.DELETE("/:id", h.softDelete)
	g.POST("/:id/transitions", h.transition)
	g.POST("/:id/correct-current", h.correctCurrent)
	g.GET("/:id/events", h.events)
	g.POST("/:id/corrections", h.correction)
	g.POST("/:id/restore", h.restore)
	g.POST("/:id/archive", h.archive)
	g.POST("/:id/unarchive", h.unarchive)
	g.POST("/bulk", h.bulk)
}

func (h *Handler) list(c *gin.Context) {
	user := httpx.UserFrom(c)
	q := c.Request.URL.Query()
	page, size := parseInt(q.Get("page"), 1), parseInt(q.Get("page_size"), 50)
	o := apprepo.ListOptions{Page: page, PageSize: size, Status: q.Get("status"), Search: q.Get("search")}
	if q.Get("trash") == "1" {
		o.OnlyTrash = true
	}
	if q.Get("archived") == "1" {
		o.ArchivedOnly = true
	}
	rows, total, err := h.svc.List(c.Request.Context(), user.ID, o)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	withStage := listIncludes(q.Get("include"))["stage_history"]
	summary := &apprepo.TimelineSummary{
		StageHistory:  map[int64]map[string]string{},
		ProgressSince: map[int64]string{},
	}
	if withStage && len(rows) > 0 {
		ids := make([]int64, 0, len(rows))
		submitted := make(map[int64]*time.Time, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
			if row.SubmittedAt != nil {
				submitted[row.ID] = row.SubmittedAt
			}
		}
		loc, _ := timeutil.SafeLocation(user.Timezone)
		if summary, err = h.svc.Repo().TimelineSummaryFor(c.Request.Context(), user.ID, ids, loc, submitted); err != nil {
			// Stage history is an enrichment: never fail the page over it.
			summary = &apprepo.TimelineSummary{
				StageHistory:  map[int64]map[string]string{},
				ProgressSince: map[int64]string{},
			}
		}
	}
	out := make([]*appDTO, 0, len(rows))
	for _, row := range rows {
		d := toDTO(row)
		if withStage {
			d.StageHistory = summary.StageHistory[row.ID]
			d.ProgressSince = summary.ProgressSince[row.ID]
		}
		out = append(out, d)
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": total, "page": page, "page_size": size})
}

// listIncludes parses a CSV include= parameter (GET) into a set.
func listIncludes(v string) map[string]bool {
	set := map[string]bool{}
	for _, tok := range strings.Split(v, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			set[tok] = true
		}
	}
	return set
}

type createReq struct {
	CompanyName    string     `json:"company_name"`
	Position       string     `json:"position"`
	JobURL         string     `json:"job_url"`
	Location       string     `json:"location"`
	RemotePolicy   string     `json:"remote_policy"`
	EmploymentType string     `json:"employment_type"`
	SalaryMin      *int64     `json:"salary_min"`
	SalaryMax      *int64     `json:"salary_max"`
	SalaryCurrency string     `json:"salary_currency"`
	Channel        string     `json:"channel"`
	Status         string     `json:"status"`
	Substatus      string     `json:"substatus"`
	Priority       string     `json:"priority"`
	Tags           []string   `json:"tags"`
	Deadline       *string    `json:"deadline"` // YYYY-MM-DD
	Notes          string     `json:"notes"`
	SubmittedAt    *time.Time `json:"submitted_at"`
}

func (h *Handler) create(c *gin.Context) {
	var req createReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	user := httpx.UserFrom(c)
	deadline, err := parseDayPtr(req.Deadline)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_deadline", err.Error()))
		return
	}
	in := &appservice.CreateInput{
		CompanyName: req.CompanyName, Position: req.Position, JobURL: req.JobURL,
		Location: req.Location, RemotePolicy: req.RemotePolicy, EmploymentType: req.EmploymentType,
		SalaryMin: req.SalaryMin, SalaryMax: req.SalaryMax, SalaryCurrency: req.SalaryCurrency,
		Channel: req.Channel, Status: req.Status, Substatus: req.Substatus,
		Priority: req.Priority, Tags: req.Tags,
		Deadline: deadline, Notes: req.Notes, SubmittedAt: req.SubmittedAt,
	}
	row, err := h.svc.Create(c.Request.Context(), user.ID, in)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, toDTO(row))
}

func (h *Handler) query(c *gin.Context) {
	var req struct {
		Filters  []map[string]any `json:"filters"`
		Search   string           `json:"search"`
		Status   string           `json:"status"`
		Archived *bool            `json:"archived"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// NOTE: full nested filter trees are served by views.RunQuery through the
	// /views query endpoint; this handler covers the common flat cases.
	user := httpx.UserFrom(c)
	o := apprepo.ListOptions{Page: req.Page, PageSize: req.PageSize, Search: req.Search, Status: req.Status}
	rows, total, err := h.svc.List(c.Request.Context(), user.ID, o)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	out := make([]*appDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDTO(row))
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": total})
}

func (h *Handler) get(c *gin.Context) {
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	user := httpx.UserFrom(c)
	// includeDeleted: 回收站里的记录也要能打开详情——数据库页的回收站点击行
	// 后打开的就是这个抽屉，RowMenu 依据响应里的 deleted 标识给出「恢复」入口，
	// 且 events/interviews/notes 等子资源本就对软删除行照常应答。可见性过滤
	// 由列表接口（trash=1 / 默认排除）负责；按 id 直读对属主返回记录本身。
	row, err := h.svc.Get(c.Request.Context(), user.ID, id, true)
	if err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	c.JSON(http.StatusOK, toDTO(row))
}

type patchReq struct {
	Version         int             `json:"version"`
	CompanyName     *string         `json:"company_name"`
	Position        *string         `json:"position"`
	JobURL          *string         `json:"job_url"`
	Location        *string         `json:"location"`
	RemotePolicy    *string         `json:"remote_policy"`
	Channel         *string         `json:"channel"`
	Priority        *string         `json:"priority"`
	Tags            []string        `json:"tags"`
	Deadline        *string         `json:"deadline"` // YYYY-MM-DD
	Notes           *string         `json:"notes"`
	NextAction      *string         `json:"next_action"`
	NextActionDueAt *string         `json:"next_action_due_at"` // YYYY-MM-DD
	CustomValues    json.RawMessage `json:"custom_values"`
}

func (h *Handler) patch(c *gin.Context) {
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	var req patchReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	user := httpx.UserFrom(c)
	in := &appservice.UpdateInput{Version: req.Version}
	in.CompanyName = req.CompanyName
	in.Position = req.Position
	in.JobURL = req.JobURL
	in.Location = req.Location
	in.RemotePolicy = req.RemotePolicy
	in.Channel = req.Channel
	in.Priority = req.Priority
	if req.Tags != nil {
		in.Tags = req.Tags
	}
	deadline, err := parseDayPtr(req.Deadline)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_deadline", err.Error()))
		return
	}
	if req.Deadline != nil {
		in.Deadline = deadline
	}
	naDue, err := parseDayPtr(req.NextActionDueAt)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_next_action_due_at", err.Error()))
		return
	}
	if req.NextActionDueAt != nil {
		in.NextActionDueAt = naDue
	}
	if req.Notes != nil {
		in.Notes = req.Notes
	}
	in.NextAction = req.NextAction
	if len(req.CustomValues) > 0 && string(req.CustomValues) != "null" {
		var cv map[string]any
		if err := json.Unmarshal(req.CustomValues, &cv); err != nil {
			httpx.WriteErr(c, httpx.BadRequest("invalid_json", "自定义属性格式错误"))
			return
		}
		in.CustomValues = cv
	}
	row, err := h.svc.Update(c.Request.Context(), user.ID, id, in)
	if err != nil {
		httpx.WriteErr(c, mapConflict(err))
		return
	}
	c.JSON(http.StatusOK, toDTO(row))
}

func (h *Handler) softDelete(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	if err := h.svc.SoftDelete(c.Request.Context(), user.ID, id); err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	h.clearAppReminders(c, user.ID, id)
	httpx.Ok(c)
}

func (h *Handler) restore(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	if err := h.svc.Restore(c.Request.Context(), user.ID, id); err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	// Restoring re-arms reminders: the lifecycle DELETE (soft delete) freed the
	// idempotency keys, so the next generator pass recreates whatever is again
	// applicable (e.g. an action still overdue). Nothing to clear here — the
	// removal already happened when the record went into the trash.
	httpx.Ok(c)
}

func (h *Handler) archive(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	if err := h.svc.Archive(c.Request.Context(), user.ID, id, true); err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	h.clearAppReminders(c, user.ID, id)
	httpx.Ok(c)
}

func (h *Handler) unarchive(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	if err := h.svc.Archive(c.Request.Context(), user.ID, id, false); err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	// Un-archiving restores the application to the reminder-eligible set. The
	// archive-time DELETE freed the idempotency keys, so the next generator
	// pass re-arms reminders for whatever is again applicable (overdue actions
	// / stale applications / upcoming interviews) instead of staying muted
	// forever under a dismissed row that still occupies its key.
	httpx.Ok(c)
}

type transitionReq struct {
	ToStatus        string     `json:"to_status"`
	ToSubstatus     string     `json:"to_substatus"`
	OccurredAt      *time.Time `json:"occurred_at"`
	Reason          string     `json:"reason"`
	Note            string     `json:"note"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	FirstResponseAt *time.Time `json:"first_response_at"`
	// 未经正式投递（内推 / 猎头直接约面）：允许直接进入招聘阶段而不编造投递时间。
	NoFormalSubmission bool   `json:"no_formal_submission"`
	Version            int    `json:"version"`
	IdempotencyKey     string `json:"idempotency_key"`
	// change_type: "" 自动判定 / rollback 流程实际退回 / reopen 重新开启。
	// 选错了要用「更正」而不是回退，见 POST /:id/correct-current。
	ChangeType string `json:"change_type"`
	// 关注轮次：把阶段指向某一轮活动。
	FocusActivityKind string `json:"focus_activity_kind"`
	FocusActivityID   *int64 `json:"focus_activity_id"`
	ClearFocus        bool   `json:"clear_focus"`
	// 与状态变更同事务写入的新轮次（方案 §6.3）。
	Assessment *assessmentReq `json:"assessment"`
	Interview  *interviewReq  `json:"interview"`
}

func (h *Handler) transition(c *gin.Context) {
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	var req transitionReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	user := httpx.UserFrom(c)
	in := &appservice.TransitionInput{
		ToStatus: req.ToStatus, ToSubstatus: req.ToSubstatus,
		OccurredAt: req.OccurredAt, Reason: req.Reason, Note: req.Note,
		SubmittedAt: req.SubmittedAt, FirstResponseAt: req.FirstResponseAt,
		NoFormalSubmission: req.NoFormalSubmission,
		Version:            req.Version, IdempotencyKey: req.IdempotencyKey,
		ChangeType:        req.ChangeType,
		FocusActivityKind: req.FocusActivityKind, FocusActivityID: req.FocusActivityID,
		ClearFocus: req.ClearFocus,
	}
	if req.Assessment != nil {
		in.Assessment = req.Assessment.toInput()
	}
	if req.Interview != nil {
		in.Interview = req.Interview.toInput()
	}
	row, err := h.svc.Transition(c.Request.Context(), user.ID, id, in)
	if err != nil {
		httpx.WriteErr(c, mapConflict(err))
		return
	}
	// A first response (stale-follow-up no longer applies) or an ended status
	// retires the application's reminders; the UI then won't show alerts for an
	// application that has moved on. System retirement DELETEs the rows (not a
	// dismiss) so the idempotency keys are freed: if the record is later
	// reopened to a non-terminal status or the first response is cleared, the
	// next generator pass re-arms whatever is again applicable.
	if req.FirstResponseAt != nil || appdomain.IsTerminal(req.ToStatus) {
		h.clearAppReminders(c, user.ID, id)
	}
	c.JSON(http.StatusOK, toDTO(row))
}

// correctCurrent repairs a mis-recorded stage (方案 §4.2 「之前选错了」): the
// wrong event is kept for audit and a correction row points at it.
func (h *Handler) correctCurrent(c *gin.Context) {
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的岗位 ID"))
		return
	}
	var req struct {
		ToStatus         string     `json:"to_status"`
		ToSubstatus      string     `json:"to_substatus"`
		Reason           string     `json:"reason"`
		OccurredAt       *time.Time `json:"occurred_at"`
		Version          int        `json:"version"`
		CorrectedEventID *int64     `json:"corrected_event_id"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	user := httpx.UserFrom(c)
	row, err := h.svc.CorrectCurrent(c.Request.Context(), user.ID, id, &appservice.CorrectCurrentInput{
		ToStatus: req.ToStatus, ToSubstatus: req.ToSubstatus, Reason: req.Reason,
		OccurredAt: req.OccurredAt, Version: req.Version, CorrectedEventID: req.CorrectedEventID,
	})
	if err != nil {
		httpx.WriteErr(c, mapConflict(err))
		return
	}
	c.JSON(http.StatusOK, toDTO(row))
}

func (h *Handler) events(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	evs, err := h.svc.Events(c.Request.Context(), user.ID, id)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	type evDTO struct {
		ID         int64     `json:"id"`
		Sequence   int       `json:"sequence"`
		EventType  string    `json:"event_type"`
		From       *string   `json:"from_status"`
		To         *string   `json:"to_status"`
		FromSub    *string   `json:"from_substatus"`
		ToSub      *string   `json:"to_substatus"`
		ActKind    *string   `json:"activity_kind"`
		ActID      *int64    `json:"activity_id"`
		ChangeType string    `json:"change_type"`
		Note       string    `json:"note"`
		Reason     string    `json:"reason"`
		Occurred   time.Time `json:"occurred_at"`
		Recorded   time.Time `json:"recorded_at"`
		Corrects   *int64    `json:"corrects_event_id"`
		Actor      *int64    `json:"actor_id"`
	}
	out := make([]evDTO, 0, len(evs))
	for _, e := range evs {
		out = append(out, evDTO{ID: e.ID, Sequence: e.Sequence, EventType: e.EventType,
			From: e.FromStatus, To: e.ToStatus, FromSub: e.FromSubstatus, ToSub: e.ToSubstatus,
			ActKind: e.ActivityKind, ActID: e.ActivityID, ChangeType: e.ChangeType,
			Note: e.Note, Reason: e.Reason,
			Occurred: e.OccurredAt, Recorded: e.RecordedAt, Corrects: e.CorrectsEventID, Actor: e.ActorID})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type correctionReq struct {
	EventID      int64     `json:"event_id"`
	NewStatus    string    `json:"new_status"`
	NewSubstatus string    `json:"new_substatus"`
	OccurredAt   time.Time `json:"occurred_at"`
	Reason       string    `json:"reason"`
	Version      int       `json:"version"`
}

func (h *Handler) correction(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	var req correctionReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	user := httpx.UserFrom(c)
	if err := h.svc.Correct(c.Request.Context(), user.ID, id, &appservice.CorrectionInput{
		EventID: req.EventID, NewStatus: req.NewStatus, NewSubstatus: req.NewSubstatus,
		OccurredAt: req.OccurredAt, Reason: req.Reason, Version: req.Version,
	}); err != nil {
		httpx.WriteErr(c, mapConflict(err))
		return
	}
	httpx.Ok(c)
}

// -- activity payloads accepted inline on a transition (方案 §6.3) --

type assessmentReq struct {
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
}

func (r *assessmentReq) toInput() *appservice.AssessmentInput {
	return &appservice.AssessmentInput{
		Kind: r.Kind, Name: r.Name, Progress: r.Progress, Result: r.Result,
		InvitedAt: r.InvitedAt, PlannedAt: r.PlannedAt, DueAt: r.DueAt,
		CompletedAt: r.CompletedAt, CompletedUnknown: r.CompletedUnknown,
		Link: r.Link, Notes: r.Notes,
	}
}

type interviewReq struct {
	RoundName        string     `json:"round_name"`
	Format           string     `json:"format"`
	ScheduledAt      *time.Time `json:"scheduled_at"`
	Timezone         string     `json:"timezone"`
	DurationMinutes  *int       `json:"duration_minutes"`
	Progress         string     `json:"progress"`
	Result           string     `json:"result"`
	InvitedAt        *time.Time `json:"invited_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	CompletedUnknown bool       `json:"completed_unknown"`
	Feedback         string     `json:"feedback"`
	Notes            string     `json:"notes"`
}

func (r *interviewReq) toInput() *appservice.InterviewInput {
	return &appservice.InterviewInput{
		RoundName: r.RoundName, Format: r.Format, ScheduledAt: r.ScheduledAt,
		Timezone: r.Timezone, DurationMinutes: r.DurationMinutes,
		Progress: r.Progress, Result: r.Result, InvitedAt: r.InvitedAt,
		CompletedAt: r.CompletedAt, CompletedUnknown: r.CompletedUnknown,
		Feedback: r.Feedback, Notes: r.Notes,
	}
}

// bulk performs the limited batch ops shipped in v1: tag, priority, archive
// (plan §3.3). Batch status changes are intentionally not bulk — every record
// needs its own transition context.
func (h *Handler) bulk(c *gin.Context) {
	var req struct {
		IDs      []int64  `json:"ids"`
		AddTags  []string `json:"add_tags"`
		Priority *string  `json:"priority"`
		Archive  *bool    `json:"archive"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 100 {
		httpx.WriteErr(c, httpx.BadRequest("bad_ids", "请选择 1–100 条记录"))
		return
	}
	user := httpx.UserFrom(c)
	n, err := h.svc.Bulk(c.Request.Context(), user.ID, req.IDs, req.AddTags, req.Priority, req.Archive)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": n})
}

func mapNotFound(err error) error {
	if errors.Is(err, appservice.ErrNotFound) {
		return httpx.NotFound("申请记录不存在")
	}
	return err
}

func mapConflict(err error) error {
	if errors.Is(err, appservice.ErrVersionConflict) {
		return httpx.Conflict("version_conflict", "记录已被其他操作修改，请刷新后重试")
	}
	return err
}

func parseInt(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

var _ = appdomain.StatusSaved

// MetaRoutes mounts the server-owned status/substatus whitelist. The frontend
// renders from this single source instead of hand-maintaining a second copy of
// the state model (方案 §6.5).
func (h *Handler) MetaRoutes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("/status-model", func(c *gin.Context) {
		c.JSON(http.StatusOK, h.svc.StatusModel())
	})
}
