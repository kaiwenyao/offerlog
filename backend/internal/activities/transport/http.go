package transport

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	actrepo "offerlog/backend/internal/activities/repository"
	notifrepo "offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/day"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/platform/observability"
)

type Handler struct {
	repo *actrepo.Repo
	nots *notifrepo.Repo
}

func New(repo *actrepo.Repo) *Handler { return &Handler{repo: repo} }

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
	Feedback        string     `json:"feedback"`
	Notes           string     `json:"notes"`
	CreatedAt       time.Time  `json:"created_at"`
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

func interviewToDTO(it *actrepo.Interview, sch *actrepo.ScheduleLink) interviewDTO {
	d := interviewDTO{
		ID: it.ID, ApplicationID: it.ApplicationID, RoundName: it.RoundName, Format: it.Format,
		ScheduledAt: it.ScheduledAt, Timezone: it.Timezone, DurationMinutes: it.DurationMinutes,
		Result: it.Result, Feedback: it.Feedback, Notes: it.Notes, CreatedAt: it.CreatedAt,
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
	it := &actrepo.Interview{
		ApplicationID: appID, OwnerID: user.ID, RoundName: req.RoundName, Format: req.Format,
		ScheduledAt: req.ScheduledAt, Timezone: req.Timezone, DurationMinutes: req.DurationMinutes,
		Result: req.Result, Feedback: req.Feedback, Notes: req.Notes,
	}
	if err := h.repo.CreateInterview(c.Request.Context(), h.repo.Pool(), it); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// persist scheduling metadata when supplied
	var sch *actrepo.ScheduleLink
	if req.Schedule != nil {
		sch = &actrepo.ScheduleLink{
			InterviewID: it.ID, OwnerID: user.ID, MeetingURL: req.Schedule.MeetingURL,
			Location: req.Schedule.Location, ContactName: req.Schedule.ContactName,
			ContactEmail: req.Schedule.ContactEmail, Notes: req.Schedule.Notes,
			Cancelled: req.Schedule.Cancelled, CancelledReason: req.Schedule.CancelledReason,
			OriginalTimezone: req.Schedule.OriginalTimezone,
		}
		if err := h.repo.CreateScheduleLink(c.Request.Context(), h.repo.Pool(), sch); err != nil {
			httpx.WriteErr(c, err)
			return
		}
	}
	c.JSON(http.StatusCreated, interviewToDTO(it, sch))
}

func (h *Handler) updateInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, _ := h.appID(c)
	iid, _ := httpx.PathID(c, "interview_id")
	var req interviewDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	it := &actrepo.Interview{
		ID: iid, ApplicationID: appID, OwnerID: user.ID, RoundName: req.RoundName, Format: req.Format,
		ScheduledAt: req.ScheduledAt, Timezone: req.Timezone, DurationMinutes: req.DurationMinutes,
		Result: req.Result, Feedback: req.Feedback, Notes: req.Notes,
	}
	if err := h.repo.UpdateInterview(c.Request.Context(), h.repo.Pool(), it); err != nil {
		httpx.WriteErr(c, httpx.NotFound("面试记录不存在"))
		return
	}
	var sch *actrepo.ScheduleLink
	if req.Schedule != nil {
		sch = &actrepo.ScheduleLink{
			InterviewID: iid, OwnerID: user.ID, MeetingURL: req.Schedule.MeetingURL,
			Location: req.Schedule.Location, ContactName: req.Schedule.ContactName,
			ContactEmail: req.Schedule.ContactEmail, Notes: req.Schedule.Notes,
			Cancelled: req.Schedule.Cancelled, CancelledReason: req.Schedule.CancelledReason,
			OriginalTimezone: req.Schedule.OriginalTimezone,
		}
		if err := h.repo.UpsertScheduleLink(c.Request.Context(), h.repo.Pool(), sch); err != nil {
			httpx.WriteErr(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, interviewToDTO(it, sch))
}

func (h *Handler) deleteInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, _ := h.appID(c)
	iid, _ := httpx.PathID(c, "interview_id")
	if err := h.repo.DeleteInterview(c.Request.Context(), h.repo.Pool(), appID, user.ID, iid); err != nil {
		httpx.WriteErr(c, httpx.NotFound("面试记录不存在"))
		return
	}
	httpx.Ok(c)
}

type cancelReq struct {
	Reason string `json:"reason"`
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
	if _, err := h.repo.GetInterview(c.Request.Context(), appID, user.ID, iid); err != nil {
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
	if _, err := h.repo.GetInterview(c.Request.Context(), appID, user.ID, iid); err != nil {
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
		// "延期 N 天" keeps the same due flavor as today: a date-only action
		// moves N calendar days; an instant action moves N×24h from now when
		// it has no anchor of its own.
		if a.DueDate != nil && a.DueTs == nil {
			nd := a.DueDate.AddDate(0, 0, req.Days)
			ds := day.Format(nd)
			req.DueDate = &ds
		} else {
			base := now
			if a.DueTs != nil {
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
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) createNote(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, _ := h.appID(c)
	var req noteDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	n := &actrepo.Note{ApplicationID: appID, OwnerID: user.ID, ContentMD: req.ContentMD}
	if err := h.repo.CreateNote(c.Request.Context(), h.repo.Pool(), n); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, n)
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
	c.JSON(http.StatusOK, n)
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
