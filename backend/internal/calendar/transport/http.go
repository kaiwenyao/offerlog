package transport

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/calendar"
	"offerlog/backend/internal/platform/httpx"
)

type Handler struct {
	repo *calendar.Repo
}

func New(repo *calendar.Repo) *Handler { return &Handler{repo: repo} }

// Routes mounts /api/v1/calendar. Query: ?from=ISO&to=ISO (required, RFC3339);
// window is half-open [from,to).
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.list)
}

func (h *Handler) list(c *gin.Context) {
	user := httpx.UserFrom(c)
	from, err := time.Parse(time.RFC3339, c.Query("from"))
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_from", "from 需为 ISO8601 时间（如 2026-09-07T00:00:00Z）"))
		return
	}
	to, err := time.Parse(time.RFC3339, c.Query("to"))
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("bad_to", "to 需为 ISO8601 时间"))
		return
	}
	if !to.After(from) {
		httpx.WriteErr(c, httpx.BadRequest("bad_range", "to 必须晚于 from"))
		return
	}
	if to.Sub(from) > 366*24*time.Hour {
		httpx.WriteErr(c, httpx.BadRequest("range_too_large", "时间范围不能超过一年"))
		return
	}
	items, err := h.repo.Range(c.Request.Context(), user.ID, user.Timezone, from, to)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "from": from, "to": to, "timezone": user.Timezone})
}
