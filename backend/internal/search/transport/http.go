package transport

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/search"
)

type Handler struct {
	repo *search.Repo
}

func New(repo *search.Repo) *Handler { return &Handler{repo: repo} }

// Routes mounts the cross-entity search under /api/v1/search.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.search)
}

func (h *Handler) search(c *gin.Context) {
	user := httpx.UserFrom(c)
	kw := c.Query("q")
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n := atoi(v); n > 0 && n <= 50 {
			limit = n
		}
	}
	items, err := h.repo.Search(c.Request.Context(), user.ID, kw, limit)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if items == nil {
		items = []*search.Item{}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
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
