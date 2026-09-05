// Package transport exposes the applications module over gin under /api/v1.
package transport

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	appdomain "offerlogs/backend/internal/applications/domain"
	apprepo "offerlogs/backend/internal/applications/repository"
	appservice "offerlogs/backend/internal/applications/service"
	"offerlogs/backend/internal/platform/httpx"
)

type Handler struct {
	svc *appservice.Service
}

func New(svc *appservice.Service) *Handler { return &Handler{svc: svc} }

// appDTO is the wire representation of an application row.
type appDTO struct {
	ID              int64           `json:"id"`
	CompanyID       int64           `json:"company_id"`
	CompanyName     string          `json:"company_name"`
	Position        string          `json:"position"`
	JobURL          string          `json:"job_url"`
	JDSnapshot      string          `json:"jd_snapshot"`
	Location        string          `json:"location"`
	RemotePolicy    string          `json:"remote_policy"`
	EmploymentType  string          `json:"employment_type"`
	SalaryMin       *int64          `json:"salary_min"`
	SalaryMax       *int64          `json:"salary_max"`
	SalaryCurrency  string          `json:"salary_currency"`
	Channel         string          `json:"channel"`
	Status          string          `json:"status"`
	Priority        string          `json:"priority"`
	Tags            []string        `json:"tags"`
	CustomValues    json.RawMessage `json:"custom_values"`
	Notes           string          `json:"notes"`
	SavedAt         *time.Time      `json:"saved_at"`
	SubmittedAt     *time.Time      `json:"submitted_at"`
	FirstResponseAt *time.Time      `json:"first_response_at"`
	Deadline        *time.Time      `json:"deadline"`
	AcceptedAt      *time.Time      `json:"accepted_at"`
	RejectedAt      *time.Time      `json:"rejected_at"`
	Reason          string          `json:"reason"`
	NextAction      string          `json:"next_action"`
	NextActionDueAt *time.Time      `json:"next_action_due_at"`
	Version         int             `json:"version"`
	Archived        bool            `json:"archived"`
	Deleted         bool            `json:"deleted"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func toDTO(r *apprepo.Row) *appDTO {
	return &appDTO{
		ID: r.ID, CompanyID: r.CompanyID, CompanyName: r.CompanyName, Position: r.Position,
		JobURL: r.JobURL, JDSnapshot: r.JDSnapshot, Location: r.Location,
		RemotePolicy: r.RemotePolicy, EmploymentType: r.EmploymentType,
		SalaryMin: r.SalaryMin, SalaryMax: r.SalaryMax, SalaryCurrency: r.SalaryCurrency,
		Channel: r.Channel, Status: r.Status, Priority: r.Priority, Tags: r.Tags,
		CustomValues: r.CustomValues, Notes: r.Notes, SavedAt: r.SavedAt,
		SubmittedAt: r.SubmittedAt, FirstResponseAt: r.FirstResponseAt, Deadline: r.Deadline,
		AcceptedAt: r.AcceptedAt, RejectedAt: r.RejectedAt, Reason: r.Reason,
		NextAction: r.NextAction, NextActionDueAt: r.NextActionDueAt, Version: r.Version,
		Archived: r.ArchivedAt != nil, Deleted: r.DeletedAt != nil,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// Routes returns a group that must be mounted under /api/v1/applications with
// the CSRF-protected, authenticated parent group.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.list)
	g.POST("", h.create)
	g.POST("/query", h.query)
	g.GET("/:id", h.get)
	g.PATCH("/:id", h.patch)
	g.DELETE("/:id", h.softDelete)
	g.POST("/:id/transitions", h.transition)
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
	out := make([]*appDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDTO(row))
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": total, "page": page, "page_size": size})
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
	Priority       string     `json:"priority"`
	Tags           []string   `json:"tags"`
	Deadline       *time.Time `json:"deadline"`
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
	in := &appservice.CreateInput{
		CompanyName: req.CompanyName, Position: req.Position, JobURL: req.JobURL,
		Location: req.Location, RemotePolicy: req.RemotePolicy, EmploymentType: req.EmploymentType,
		SalaryMin: req.SalaryMin, SalaryMax: req.SalaryMax, SalaryCurrency: req.SalaryCurrency,
		Channel: req.Channel, Status: req.Status, Priority: req.Priority, Tags: req.Tags,
		Deadline: req.Deadline, Notes: req.Notes, SubmittedAt: req.SubmittedAt,
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
	row, err := h.svc.Get(c.Request.Context(), user.ID, id, false)
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
	Deadline        *time.Time      `json:"deadline"`
	Notes           *string         `json:"notes"`
	NextAction      *string         `json:"next_action"`
	NextActionDueAt *time.Time      `json:"next_action_due_at"`
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
	if req.Deadline != nil {
		in.Deadline = req.Deadline
	}
	if req.Notes != nil {
		in.Notes = req.Notes
	}
	in.NextAction = req.NextAction
	in.NextActionDueAt = req.NextActionDueAt
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
	httpx.Ok(c)
}

func (h *Handler) restore(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	if err := h.svc.Restore(c.Request.Context(), user.ID, id); err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	httpx.Ok(c)
}

func (h *Handler) archive(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	if err := h.svc.Archive(c.Request.Context(), user.ID, id, true); err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	httpx.Ok(c)
}

func (h *Handler) unarchive(c *gin.Context) {
	id, _ := httpx.PathID(c, "id")
	user := httpx.UserFrom(c)
	if err := h.svc.Archive(c.Request.Context(), user.ID, id, false); err != nil {
		httpx.WriteErr(c, mapNotFound(err))
		return
	}
	httpx.Ok(c)
}

type transitionReq struct {
	ToStatus        string     `json:"to_status"`
	OccurredAt      *time.Time `json:"occurred_at"`
	Reason          string     `json:"reason"`
	Note            string     `json:"note"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	FirstResponseAt *time.Time `json:"first_response_at"`
	Version         int        `json:"version"`
	IdempotencyKey  string     `json:"idempotency_key"`
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
		ToStatus: req.ToStatus, OccurredAt: req.OccurredAt, Reason: req.Reason, Note: req.Note,
		SubmittedAt: req.SubmittedAt, FirstResponseAt: req.FirstResponseAt,
		Version: req.Version, IdempotencyKey: req.IdempotencyKey,
	}
	row, err := h.svc.Transition(c.Request.Context(), user.ID, id, in)
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
		ID        int64     `json:"id"`
		Sequence  int       `json:"sequence"`
		EventType string    `json:"event_type"`
		From      *string   `json:"from_status"`
		To        *string   `json:"to_status"`
		Note      string    `json:"note"`
		Reason    string    `json:"reason"`
		Occurred  time.Time `json:"occurred_at"`
		Recorded  time.Time `json:"recorded_at"`
		Corrects  *int64    `json:"corrects_event_id"`
		Actor     *int64    `json:"actor_id"`
	}
	out := make([]evDTO, 0, len(evs))
	for _, e := range evs {
		out = append(out, evDTO{ID: e.ID, Sequence: e.Sequence, EventType: e.EventType,
			From: e.FromStatus, To: e.ToStatus, Note: e.Note, Reason: e.Reason,
			Occurred: e.OccurredAt, Recorded: e.RecordedAt, Corrects: e.CorrectsEventID, Actor: e.ActorID})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type correctionReq struct {
	EventID    int64     `json:"event_id"`
	NewStatus  string    `json:"new_status"`
	OccurredAt time.Time `json:"occurred_at"`
	Reason     string    `json:"reason"`
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
		EventID: req.EventID, NewStatus: req.NewStatus, OccurredAt: req.OccurredAt, Reason: req.Reason,
	}); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	httpx.Ok(c)
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
