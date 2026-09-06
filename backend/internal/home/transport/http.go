package transport

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/home"
	"offerlog/backend/internal/platform/httpx"
)

type Handler struct {
	repo *home.Repo
}

func New(repo *home.Repo) *Handler { return &Handler{repo: repo} }

// Routes mounts the dashboard aggregate under /api/v1/home.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("/summary", h.summary)
}

func (h *Handler) summary(c *gin.Context) {
	user := httpx.UserFrom(c)
	limit := 5
	if v := c.Query("limit"); v != "" {
		if n := atoi(v); n > 0 && n <= 50 {
			limit = n
		}
	}
	s, err := h.repo.Get(c.Request.Context(), user.ID, user.Timezone, time.Now(), limit)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
