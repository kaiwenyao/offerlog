package transport

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	authservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/prefs"
)

// ProfileWriter persists the account display name/timezone on the users row
// (implemented by the identity service); the settings page saves profile +
// reminder prefs in one PUT.
type ProfileWriter interface {
	UpdateProfile(ctx context.Context, id int64, displayName, timezone string) (*authservice.UserRow, error)
}

type Handler struct {
	repo     *prefs.Repo
	profiles ProfileWriter
}

func New(repo *prefs.Repo) *Handler { return &Handler{repo: repo} }
func NewWithProfile(repo *prefs.Repo, profiles ProfileWriter) *Handler {
	return &Handler{repo: repo, profiles: profiles}
}

// Routes mounts /api/v1/preferences. The endpoint returns the effective
// preference set (defaults merged when the row does not exist yet) and saves
// the full set atomically (UPSERT). Saves carry no version: prefs are
// last-writer-wins for a personal app; the UI shows a saving state and keeps
// the input on failure.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.get)
	g.PUT("", h.put)
}

func (h *Handler) get(c *gin.Context) {
	user := httpx.UserFrom(c)
	p, err := h.repo.Get(c.Request.Context(), user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(user.ID, user.DisplayName, user.Timezone, p))
}

type prefDTO struct {
	UserID          int64  `json:"user_id"`
	DisplayName     string `json:"display_name"`
	Timezone        string `json:"timezone"`
	WeekStart       int    `json:"week_start"`
	RemindOverdue   bool   `json:"remind_overdue"`
	RemindInterview bool   `json:"remind_interview"`
	RemindStaleDays int    `json:"remind_stale_days"`
	RemindWeekly    bool   `json:"remind_weekly"`
	Locale          string `json:"locale"`
}

func toDTO(userID int64, fallbackName, fallbackTZ string, p *prefs.Preferences) prefDTO {
	d := prefDTO{UserID: userID, DisplayName: fallbackName, Timezone: fallbackTZ,
		WeekStart: 1, RemindOverdue: true, RemindInterview: true, RemindStaleDays: 14,
		RemindWeekly: false, Locale: "zh-CN"}
	if p != nil {
		d.DisplayName = firstNonEmpty(p.DisplayName, fallbackName)
		// users.timezone is the single source of truth — always surface it
		// (fallbackTZ) even when a stale prefs.timezone exists, so a user who
		// changed their zone via PATCH /auth/me sees the same zone here.
		d.Timezone = fallbackTZ
		if p.WeekStart >= 0 && p.WeekStart <= 6 {
			d.WeekStart = p.WeekStart
		}
		d.RemindOverdue = p.RemindOverdue
		d.RemindInterview = p.RemindInterview
		// 0 is a valid stored value meaning “stale reminders off”; do not
		// collapse it back to the default on read, or the settings dropdown
		// snaps back to 14 after saving 关闭.
		d.RemindStaleDays = p.RemindStaleDays
		d.RemindWeekly = p.RemindWeekly
		if p.Locale != "" {
			d.Locale = p.Locale
		}
	}
	return d
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type putReq struct {
	DisplayName     *string `json:"display_name"`
	Timezone        *string `json:"timezone"`
	WeekStart       *int    `json:"week_start"`
	RemindOverdue   *bool   `json:"remind_overdue"`
	RemindInterview *bool   `json:"remind_interview"`
	RemindStaleDays *int    `json:"remind_stale_days"`
	RemindWeekly    *bool   `json:"remind_weekly"`
}

func (h *Handler) put(c *gin.Context) {
	user := httpx.UserFrom(c)
	var req putReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	cur, err := h.repo.Get(c.Request.Context(), user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	p := &prefs.Preferences{UserID: user.ID}
	if cur != nil {
		*p = *cur
	} else {
		p.DisplayName = user.DisplayName
		p.WeekStart = 1
		p.RemindOverdue = true
		p.RemindInterview = true
		p.RemindStaleDays = 14
		p.RemindWeekly = false
		p.Locale = user.Locale
	}
	// users.timezone is canonical (PATCH /auth/me may have changed it since the
	// prefs row was written). Seed from the session user so a reminder-only PUT
	// cannot revert a zone change made through /auth/me.
	p.Timezone = user.Timezone
	p.DisplayName = user.DisplayName

	if req.DisplayName != nil {
		name := *req.DisplayName
		if len(name) > 80 {
			httpx.WriteErr(c, httpx.BadRequest("display_name_too_long", "显示名称不能超过 80 个字符"))
			return
		}
		p.DisplayName = name
	}
	if req.Timezone != nil {
		if _, err := timeLoad(*req.Timezone); err != nil {
			httpx.WriteErr(c, httpx.BadRequest("invalid_timezone", "无效的时区（需 IANA 名称，如 Europe/Dublin）"))
			return
		}
		p.Timezone = *req.Timezone
	}
	if req.WeekStart != nil {
		if *req.WeekStart < 0 || *req.WeekStart > 6 {
			httpx.WriteErr(c, httpx.BadRequest("invalid_week_start", "每周起始日必须是 0（周日）到 6（周六）"))
			return
		}
		p.WeekStart = *req.WeekStart
	}
	if req.RemindOverdue != nil {
		p.RemindOverdue = *req.RemindOverdue
	}
	if req.RemindInterview != nil {
		p.RemindInterview = *req.RemindInterview
	}
	if req.RemindStaleDays != nil {
		if *req.RemindStaleDays < 0 || *req.RemindStaleDays > 365 {
			httpx.WriteErr(c, httpx.BadRequest("invalid_stale_days", "未回复提醒天数需在 0–365 之间（0 表示关闭）"))
			return
		}
		p.RemindStaleDays = *req.RemindStaleDays
	}
	if req.RemindWeekly != nil {
		p.RemindWeekly = *req.RemindWeekly
	}

	// Persist the profile fields on the users row too (single source for the
	// session/sidebar display). The reminder preferences always go to prefs.
	if h.profiles != nil {
		if _, err := h.profiles.UpdateProfile(c.Request.Context(), user.ID, p.DisplayName, p.Timezone); err != nil {
			httpx.WriteErr(c, err)
			return
		}
	}

	if err := h.repo.Upsert(c.Request.Context(), p); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Keep the session's own user snapshot in step so /auth/me and the sidebar
	// reflect the saved profile immediately.
	if p.DisplayName != "" {
		user.DisplayName = p.DisplayName
	}
	user.Timezone = p.Timezone
	c.JSON(http.StatusOK, toDTO(user.ID, user.DisplayName, user.Timezone, p))
}

func timeLoad(tz string) (*time.Location, error) {
	return time.LoadLocation(tz)
}

var _ = http.StatusOK
