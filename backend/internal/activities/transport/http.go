package transport

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	actrepo "offerlog/backend/internal/activities/repository"
	"offerlog/backend/internal/platform/httpx"
)

type Handler struct {
	repo *actrepo.Repo
}

func New(repo *actrepo.Repo) *Handler { return &Handler{repo: repo} }

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

// cancelInterview marks an interview cancelled (改期/取消). The row is kept
// so history is intact; dashboards and reminders exclude cancelled rounds.
func (h *Handler) cancelInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID, _ := h.appID(c)
	iid, _ := httpx.PathID(c, "interview_id")
	var req cancelReq
	_ = httpx.BindJSON(c, &req)
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
		httpx.WriteErr(c, httpx.NotFound("面试记录不存在"))
		return
	}
	_ = appID
	c.JSON(http.StatusOK, gin.H{"ok": true, "cancelled": true})
}

func (h *Handler) uncancelInterview(c *gin.Context) {
	user := httpx.UserFrom(c)
	iid, _ := httpx.PathID(c, "interview_id")
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
		httpx.WriteErr(c, httpx.NotFound("面试记录不存在"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "cancelled": false})
}

// -- actions --

type actionDTO struct {
	ID            int64      `json:"id"`
	ApplicationID *int64     `json:"application_id"`
	Title         string     `json:"title"`
	DueDate       *time.Time `json:"due_date"`
	DueTs         *time.Time `json:"due_ts"`
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
	return actionDTO{
		ID: a.ID, ApplicationID: a.ApplicationID, Title: a.Title,
		DueDate: a.DueDate, DueTs: a.DueTs, DoneAt: a.DoneAt,
		RemindMe: a.RemindMe, RemindAt: a.RemindAt, Priority: a.Priority, CreatedAt: a.CreatedAt,
		CompanyName: a.CompanyName, Position: a.Position, Status: a.Status,
	}
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
	a := &actrepo.Action{
		ApplicationID: appPtr, OwnerID: user.ID, Title: req.Title, DueDate: req.DueDate,
		DueTs: req.DueTs, DoneAt: req.DoneAt, RemindMe: req.RemindMe, RemindAt: req.RemindAt,
		Priority: req.Priority,
	}
	if err := h.repo.CreateAction(c.Request.Context(), h.repo.Pool(), a); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, actionToDTO(a))
}

func (h *Handler) updateAction(c *gin.Context) {
	user := httpx.UserFrom(c)
	aid, _ := httpx.PathID(c, "action_id")
	var req actionDTO
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	a := &actrepo.Action{
		ID: aid, OwnerID: user.ID, Title: req.Title, DueDate: req.DueDate, DueTs: req.DueTs,
		DoneAt: req.DoneAt, RemindMe: req.RemindMe, RemindAt: req.RemindAt, Priority: req.Priority,
	}
	if err := h.repo.UpdateAction(c.Request.Context(), h.repo.Pool(), a); err != nil {
		httpx.WriteErr(c, httpx.NotFound("行动项不存在"))
		return
	}
	c.JSON(http.StatusOK, actionToDTO(a))
}

func (h *Handler) deleteAction(c *gin.Context) {
	user := httpx.UserFrom(c)
	aid, _ := httpx.PathID(c, "action_id")
	if err := h.repo.DeleteAction(c.Request.Context(), user.ID, aid); err != nil {
		httpx.WriteErr(c, err)
		return
	}
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
		httpx.WriteErr(c, httpx.NotFound("行动项不存在"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "done": done})
}

type postponeReq struct {
	DueDate *time.Time `json:"due_date"`
	DueTs   *time.Time `json:"due_ts"`
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
	if err != nil || a == nil {
		httpx.WriteErr(c, httpx.NotFound("行动项不存在"))
		return
	}
	now := time.Now()
	if req.Days > 0 {
		base := now
		if a.DueTs != nil {
			base = *a.DueTs
		} else if a.DueDate != nil {
			base = *a.DueDate
		}
		nd := base.AddDate(0, 0, req.Days)
		req.DueTs = &nd
		req.DueDate = nil
	} else if req.DueDate != nil || req.DueTs != nil {
		// keep as provided
	} else {
		httpx.WriteErr(c, httpx.BadRequest("no_due", "请提供新的截止日期或延期天数"))
		return
	}
	a.DueTs = req.DueTs
	if req.DueTs == nil {
		a.DueDate = req.DueDate
	}
	if err := h.repo.UpdateAction(c.Request.Context(), h.repo.Pool(), a); err != nil {
		httpx.WriteErr(c, httpx.NotFound("行动项不存在"))
		return
	}
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
		httpx.WriteErr(c, httpx.NotFound("备注不存在"))
		return
	}
	c.JSON(http.StatusOK, n)
}

func (h *Handler) deleteNote(c *gin.Context) {
	user := httpx.UserFrom(c)
	nid, _ := httpx.PathID(c, "note_id")
	if err := h.repo.DeleteNote(c.Request.Context(), user.ID, nid); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	httpx.Ok(c)
}
