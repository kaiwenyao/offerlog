// Snippet endpoints (/api/v1/snippets): 常用片段的增删改查。
package transport

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/snippets"
)

const (
	maxTitle    = 100
	maxContent  = 10000
	maxCategory = 40
)

type Handler struct {
	repo *snippets.Repo
}

func New(repo *snippets.Repo) *Handler { return &Handler{repo: repo} }

// Routes mounts /api/v1/snippets.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("", h.list)
	g.POST("", h.create)
	g.PATCH("/:id", h.update)
	g.DELETE("/:id", h.remove)
}

type snippetDTO struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// snippetReq is shared by create and PATCH; pointers tell 「省略」 from 「清空」
// (PATCH with category:"" moves the snippet to 未分类).
type snippetReq struct {
	Title    *string `json:"title"`
	Content  *string `json:"content"`
	Category *string `json:"category"`
}

func toDTO(s *snippets.Snippet) snippetDTO {
	return snippetDTO{ID: s.ID, Title: s.Title, Content: s.Content, Category: s.Category,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

func writeSnippetErr(c *gin.Context, err error) {
	if errors.Is(err, snippets.ErrNotFound) {
		httpx.WriteErr(c, httpx.NotFound("片段不存在"))
		return
	}
	httpx.WriteErr(c, err)
}

// normalize trims title/category in place and validates every present field.
// Content is kept verbatim — leading indentation and blank lines are part of
// what the user wants to paste.
func (req *snippetReq) normalize() error {
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" {
			return httpx.BadRequest("invalid_title", "标题不能为空")
		}
		if utf8.RuneCountInString(t) > maxTitle {
			return httpx.BadRequest("invalid_title", "标题过长（≤100 字）")
		}
		req.Title = &t
	}
	if req.Content != nil && utf8.RuneCountInString(*req.Content) > maxContent {
		return httpx.BadRequest("invalid_content", "内容过长（≤10000 字）")
	}
	if req.Category != nil {
		cat := strings.TrimSpace(*req.Category)
		if utf8.RuneCountInString(cat) > maxCategory {
			return httpx.BadRequest("invalid_category", "分类过长（≤40 字）")
		}
		req.Category = &cat
	}
	return nil
}

func (h *Handler) list(c *gin.Context) {
	user := httpx.UserFrom(c)
	items, err := h.repo.List(c.Request.Context(), user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	out := make([]snippetDTO, 0, len(items))
	for _, s := range items {
		out = append(out, toDTO(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h *Handler) create(c *gin.Context) {
	user := httpx.UserFrom(c)
	var req snippetReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if req.Title == nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_title", "标题不能为空"))
		return
	}
	if err := req.normalize(); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	s := &snippets.Snippet{OwnerID: user.ID, Title: *req.Title}
	if req.Content != nil {
		s.Content = *req.Content
	}
	if req.Category != nil {
		s.Category = *req.Category
	}
	if err := h.repo.Create(c.Request.Context(), s); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, toDTO(s))
}

func (h *Handler) update(c *gin.Context) {
	user := httpx.UserFrom(c)
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的片段 ID"))
		return
	}
	var req snippetReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if err := req.normalize(); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	s, err := h.repo.Update(c.Request.Context(), user.ID, id, req.Title, req.Content, req.Category)
	if err != nil {
		writeSnippetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, toDTO(s))
}

func (h *Handler) remove(c *gin.Context) {
	user := httpx.UserFrom(c)
	id, err := httpx.PathID(c, "id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的片段 ID"))
		return
	}
	if err := h.repo.Delete(c.Request.Context(), user.ID, id); err != nil {
		writeSnippetErr(c, err)
		return
	}
	httpx.Ok(c)
}
