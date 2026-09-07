package transport

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/httpx"
)

type Handler struct {
	repo *notifications.Repo
}

// writeNotifErr maps only a real not-found to 404; genuine DB failures pass
// through as 500 rather than masquerading as "通知不存在".
func writeNotifErr(c *gin.Context, err error) {
	if errors.Is(err, notifications.ErrNotFound) {
		httpx.WriteErr(c, httpx.NotFound("通知不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

func New(repo *notifications.Repo) *Handler { return &Handler{repo: repo} }

// Routes mounts /api/v1/notifications.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.list)
	g.POST("/read-all", h.readAll)
	g.POST("/:id/read", h.read)
	g.POST("/:id/dismiss", h.dismiss)
}

func (h *Handler) readAll(c *gin.Context) {
	user := httpx.UserFrom(c)
	if err := h.repo.MarkAllRead(c.Request.Context(), user.ID); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	httpx.Ok(c)
}

func (h *Handler) list(c *gin.Context) {
	user := httpx.UserFrom(c)
	open := c.Query("open") == "1"
	items, err := h.repo.List(c.Request.Context(), user.ID, open, 50)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) read(c *gin.Context) {
	user := httpx.UserFrom(c)
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的通知 ID"))
		return
	}
	if err := h.repo.MarkRead(c.Request.Context(), user.ID, id); err != nil {
		writeNotifErr(c, err)
		return
	}
	httpx.Ok(c)
}

func (h *Handler) dismiss(c *gin.Context) {
	user := httpx.UserFrom(c)
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的通知 ID"))
		return
	}
	if err := h.repo.Dismiss(c.Request.Context(), user.ID, id); err != nil {
		writeNotifErr(c, err)
		return
	}
	httpx.Ok(c)
}
