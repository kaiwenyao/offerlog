package transport

import (
	"encoding/csv"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/transfers"
)

type Handler struct {
	repo *transfers.Repo
}

func New(repo *transfers.Repo) *Handler { return &Handler{repo: repo} }

// Routes mounts transfers under /api/v1/imports and /api/v1/exports.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.POST("/preview", h.preview)
	g.POST("/:batch_id/commit", h.commit)
	g.GET("/:batch_id/errors", h.errors)
}

func (h *Handler) preview(c *gin.Context) {
	user := httpx.UserFrom(c)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("missing_file", "缺少上传文件"))
		return
	}
	if fileHeader.Size > 8<<20 {
		httpx.WriteErr(c, httpx.BadRequest("file_too_large", "导入文件超过 8 MiB 上限"))
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	defer f.Close()
	pv, err := h.repo.ParseCSV(c.Request.Context(), user.ID, fileHeader.Filename, f)
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("csv_parse_error", err.Error()))
		return
	}
	c.JSON(http.StatusOK, pv)
}

func (h *Handler) commit(c *gin.Context) {
	user := httpx.UserFrom(c)
	batchID, err := httpx.PathID(c, "batch_id")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的批次 ID"))
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("missing_file", "缺少上传文件"))
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	defer f.Close()
	n, err := h.repo.CommitImport(c.Request.Context(), user.ID, batchID, f)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"inserted": n, "batch_id": batchID})
}

func (h *Handler) errors(c *gin.Context) {
	user := httpx.UserFrom(c)
	batchID, _ := httpx.PathID(c, "batch_id")
	var preview json.RawMessage
	if err := h.repo.Errors(c.Request.Context(), user.ID, batchID, &preview); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"batch_id": batchID, "preview": preview})
}

// ExportRoutes adds the plain CSV download endpoint under /exports.
func (h *Handler) ExportRoutes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.GET("/applications.csv", h.exportCSV)
}

func (h *Handler) exportCSV(c *gin.Context) {
	user := httpx.UserFrom(c)
	rows, err := h.repo.ExportRows(c.Request.Context(), user.ID)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="applications.csv"`)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"公司", "岗位", "链接", "地点", "远程", "类型", "渠道", "状态", "优先级", "标签", "截止日期", "投递时间", "薪资下限", "薪资上限", "币种", "备注"})
	for _, r := range rows {
		rec := make([]string, len(r))
		for i, v := range r {
			rec[i] = safeCell(v)
		}
		_ = w.Write(rec)
	}
	w.Flush()
}

// safeCell neutralizes spreadsheet formula injection (§12.3): values that
// start with =,+,-,@ or tab are prefixed with a single quote.
func safeCell(s string) string {
	if s == "" {
		return ""
	}
	switch s[0] {
	case '=', '+', '-', '@':
		return "'" + s
	}
	return s
}
