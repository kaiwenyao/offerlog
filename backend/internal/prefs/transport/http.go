package transport

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	authservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/prefs"
)

// ProfileWriter persists the account display name/timezone on the users row
// (implemented by the identity service); the settings page saves profile +
// reminder prefs in one PUT. Both writes must commit together, so the seam
// exposes a transaction-scoped variant.
type ProfileWriter interface {
	UpdateProfile(ctx context.Context, id int64, displayName, timezone string) (*authservice.UserRow, error)
	UpdateProfileTx(ctx context.Context, q database.Querier, id int64, displayName, timezone *string) (*authservice.UserRow, error)
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
		// users row is canonical for display_name + timezone (both written by
		// PATCH /auth/me and PUT /preferences via UpdateProfile); the prefs row
		// mirrors them and can drift when only /auth/me is used, so never let
		// the stale copy win.
		d.DisplayName = fallbackName
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
	// Validate the patch before opening a transaction so a bad payload never
	// takes the per-user prefs lock.
	if req.DisplayName != nil && len(*req.DisplayName) > 80 {
		httpx.WriteErr(c, httpx.BadRequest("display_name_too_long", "显示名称不能超过 80 个字符"))
		return
	}
	if req.Timezone != nil {
		if _, err := authservice.NormalizeTimezone(*req.Timezone); err != nil {
			httpx.WriteErr(c, httpx.BadRequest("invalid_timezone", err.Error()))
			return
		}
	}
	if req.WeekStart != nil && (*req.WeekStart < 0 || *req.WeekStart > 6) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_week_start", "每周起始日必须是 0（周日）到 6（周六）"))
		return
	}
	if req.RemindStaleDays != nil && (*req.RemindStaleDays < 0 || *req.RemindStaleDays > 365) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_stale_days", "未回复提醒天数需在 0–365 之间（0 表示关闭）"))
		return
	}

	// Read-modify-write is atomic: lock the user, re-read the current row
	// inside the same transaction, apply only the fields this PUT sent, then
	// write. Two overlapping PUTs (settings page fires one PUT per toggle)
	// used to each snapshot the row outside the tx and last-writer-wins the
	// whole row — the first toggle silently snapped back.
	p := &prefs.Preferences{UserID: user.ID}
	err := h.repo.Pool().RunInTx(c.Request.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := h.repo.LockUser(ctx, tx, user.ID); err != nil {
			return err
		}
		cur, err := h.repo.GetTx(ctx, tx, user.ID)
		if err != nil {
			return err
		}
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
		// users 行才是 display_name / timezone 的真相（PATCH /auth/me 也写它）。
		// 但不能拿 user 这个「进事务之前的会话快照」去填：改名和改提醒开关是两个
		// 独立的 PUT，后发的那个手上还是旧名字，无条件写回去就把改名静默回滚了。
		// 所以这两个字段只在本次请求真的带了的时候才写，其余交给 COALESCE 保留，
		// 由 UpdateProfileTx 回读锁内的当前值。
		var nameArg, tzArg *string
		if req.DisplayName != nil {
			nameArg = req.DisplayName
		}
		if req.Timezone != nil {
			norm, err := authservice.NormalizeTimezone(*req.Timezone)
			if err != nil {
				return err
			}
			tzArg = &norm
		}
		if req.WeekStart != nil {
			p.WeekStart = *req.WeekStart
		}
		if req.RemindOverdue != nil {
			p.RemindOverdue = *req.RemindOverdue
		}
		if req.RemindInterview != nil {
			p.RemindInterview = *req.RemindInterview
		}
		if req.RemindStaleDays != nil {
			p.RemindStaleDays = *req.RemindStaleDays
		}
		if req.RemindWeekly != nil {
			p.RemindWeekly = *req.RemindWeekly
		}

		// Persist the profile fields on the users row AND the reminder
		// preferences on the preferences row in ONE transaction: a mid-way
		// failure would otherwise leave half of the settings saved.
		p.DisplayName = user.DisplayName
		p.Timezone = user.Timezone
		if h.profiles != nil {
			row, err := h.profiles.UpdateProfileTx(ctx, tx, user.ID, nameArg, tzArg)
			if err != nil {
				return err
			}
			// 镜像到 prefs 行、也用于响应体：这是提交后 users 行的真实内容，
			// 既包含本次的改动，也包含别的请求刚写进去的。
			p.DisplayName = row.DisplayName
			p.Timezone = row.Timezone
		} else {
			if nameArg != nil {
				p.DisplayName = *nameArg
			}
			if tzArg != nil {
				p.Timezone = *tzArg
			}
		}
		return h.repo.UpsertTx(ctx, tx, p)
	})
	if err != nil {
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
