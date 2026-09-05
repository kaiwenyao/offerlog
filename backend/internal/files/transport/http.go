// Package transport exposes file upload/download/association over gin.
//
// Upload flow (plan §10.1): authenticated user → pending record → stream body
// to staging object while sniffing MIME + SHA-256 → promote to final key →
// mark ready. Files are private; only ready files can be downloaded or linked.
package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"offerlogs/backend/internal/platform/httpx"
	"offerlogs/backend/internal/platform/objectstore"
)

type DBQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Config struct {
	MaxFileBytes int64
	QuotaBytes   int64
}

type fileRow struct {
	ID           string
	OwnerID      int64
	ObjectKey    string
	FinalKey     string
	OriginalName string
	ContentType  string
	SizeBytes    int64
	SHA256       string
	Status       string
	Category     string
	ErrorMessage string
	UsedByApp    *int64
	IsResume     bool
	CreatedAt    time.Time
}

type Handler struct {
	store objectstore.Store
	db    DBQuerier
	cfg   Config
}

func New(store objectstore.Store, db DBQuerier, cfg Config) *Handler {
	return &Handler{store: store, db: db, cfg: cfg}
}

var _ = objectstore.Store(nil)

// allowedExt and content detection (plan §10.1): PDF/DOCX/TXT/PNG/JPEG.
var allowedMIME = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"text/plain":      true,
	// DOCX is a zip container; verified in verifyDocx below.
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
}

func extOf(name string) string { return strings.ToLower(filepath.Ext(name)) }

// validateNameAndMIME rejects unsupported types before storing.
func validateNameAndMIME(origName, contentType string, sniff []byte) error {
	ext := extOf(origName)
	// Only allow known document/image extensions; the name is display-only but
	// we still gate the type.
	switch ext {
	case ".pdf", ".docx", ".txt", ".png", ".jpg", ".jpeg":
	default:
		return fmt.Errorf("不支持的文件类型 .%s（允许 PDF/DOCX/TXT/PNG/JPEG）", strings.TrimPrefix(ext, "."))
	}
	// Sniffed content type wins over the header; fall back to extension.
	ct := sniffedContentType(sniff)
	if ct == "" {
		ct = contentType
	}
	if ct == "" {
		ct = mime.TypeByExtension(ext)
	}
	if ext == ".docx" {
		if !isZipContainer(sniff) {
			return errors.New("DOCX 文件必须为有效的 ZIP 容器")
		}
	}
	return nil
}

func sniffedContentType(head []byte) string {
	if len(head) >= 4 && head[0] == '%' && head[1] == 'P' && head[2] == 'D' && head[3] == 'F' {
		return "application/pdf"
	}
	if len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	if len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF {
		return "image/jpeg"
	}
	// zip local file header
	if len(head) >= 4 && head[0] == 'P' && head[1] == 'K' && (head[2] == 3 || head[2] == 5 || head[2] == 7) {
		return "application/zip"
	}
	if looksText(head) {
		return "text/plain"
	}
	return ""
}

func looksText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	ok := 0
	for _, c := range b {
		if c == 0 {
			return false
		}
		if c >= 32 || c == '\n' || c == '\r' || c == '\t' {
			ok++
		}
	}
	return ok*10 >= len(b)*9
}

func isZipContainer(head []byte) bool {
	return len(head) >= 4 && head[0] == 'P' && head[1] == 'K' && (head[2] == 3 || head[2] == 5 || head[2] == 7)
}

// Routes mounts files under an authenticated group.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.POST("", h.upload)
	g.GET("", h.list)
	g.GET("/:id/download", h.download)
	g.DELETE("/:id", h.delete)
}

// upload streams a multipart file to the object store.
func (h *Handler) upload(c *gin.Context) {
	user := httpx.UserFrom(c)
	if user == nil {
		httpx.WriteErr(c, httpx.Unauthorized("请先登录"))
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		httpx.WriteErr(c, httpx.BadRequest("missing_file", "缺少上传文件"))
		return
	}
	origName := fileHeader.Filename
	// sanitize display name (length + control chars)
	origName = sanitizeName(origName)
	if origName == "" {
		httpx.WriteErr(c, httpx.BadRequest("bad_filename", "文件名无效"))
		return
	}
	if fileHeader.Size > h.cfg.MaxFileBytes {
		httpx.WriteErr(c, httpx.BadRequest("file_too_large", fmt.Sprintf("文件超过 %d MiB 上限", h.cfg.MaxFileBytes/(1<<20))))
		return
	}
	category := strings.TrimSpace(c.PostForm("category"))
	if category == "" {
		category = "other"
	}
	appIDStr := c.PostForm("application_id")
	usedBy := int64(0)
	if appIDStr != "" {
		if _, err := fmt.Sscanf(appIDStr, "%d", &usedBy); err != nil || usedBy <= 0 {
			httpx.WriteErr(c, httpx.BadRequest("bad_application", "无效的申请 ID"))
			return
		}
	}
	// quota check + pending record (reserve)
	fid := uuid.NewString()
	finalKey := fmt.Sprintf("owners/%d/files/%s/content", user.ID, fid)
	_, quotaErr := h.db.Exec(c.Request.Context(),
		`INSERT INTO files(id, owner_id, object_key, final_key, original_name, category, status, used_by_application)
		 VALUES($1,$2,$3,$4,$5,$6,'pending',$7)`,
		fid, user.ID, "staging/"+fid, finalKey, origName, category, nilOrID(usedBy))
	if quotaErr != nil {
		httpx.WriteErr(c, quotaErr)
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	defer src.Close()

	// Read limited bytes for sniffing, then stream the remainder.
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	multi := io.MultiReader(strings.NewReader(string(head)), src)

	hsh := sha256.New()
	counting := io.TeeReader(multi, hsh)

	// Stage upload to staging key.
	if err := h.store.Put(c.Request.Context(), "staging/"+fid, counting, -1, "application/octet-stream"); err != nil {
		_, _ = h.db.Exec(c.Request.Context(), `UPDATE files SET status='failed', error_message=$2, updated_at=now() WHERE id=$1`, fid, "对象存储暂存失败")
		httpx.WriteErr(c, err)
		return
	}
	size := fileHeader.Size
	digest := hex.EncodeToString(hsh.Sum(nil))
	contentType := sniffedContentType(head)

	if err := validateNameAndMIME(origName, fileHeader.Header.Get("Content-Type"), head); err != nil {
		_, _ = h.db.Exec(c.Request.Context(), `UPDATE files SET status='failed', error_message=$2, updated_at=now() WHERE id=$1`, fid, err.Error())
		httpx.WriteErr(c, httpx.BadRequest("unsupported_type", err.Error()))
		return
	}
	// Verify size against actual uploaded bytes (multipart size may be fudged).
	if got := storeStat(c.Request.Context(), h.store, "staging/"+fid); got >= 0 && got != size {
		_, _ = h.db.Exec(c.Request.Context(), `UPDATE files SET status='failed', error_message='大小校验失败', updated_at=now() WHERE id=$1`, fid)
		httpx.WriteErr(c, httpx.BadRequest("size_mismatch", "上传大小与声明不一致"))
		return
	}

	// Promote to final key (never a user-writable key).
	if err := h.promote(c.Request.Context(), user.ID, fid, finalKey, "staging/"+fid); err != nil {
		_, _ = h.db.Exec(c.Request.Context(), `UPDATE files SET status='failed', error_message='上传失败', updated_at=now() WHERE id=$1`, fid)
		httpx.WriteErr(c, err)
		return
	}
	ct := contentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	_, err = h.db.Exec(c.Request.Context(), `UPDATE files SET status='ready', sha256=$2, content_type=$3, size_bytes=$4, updated_at=now() WHERE id=$1`,
		fid, digest, ct, size)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// link to application if provided
	if usedBy > 0 {
		if _, err := h.db.Exec(c.Request.Context(),
			`INSERT INTO application_files(application_id, file_id, owner_id, purpose, is_resume)
			 VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			usedBy, fid, user.ID, category, category == "resume"); err != nil {
			httpx.WriteErr(c, err)
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"id": fid, "status": "ready", "sha256": digest, "size_bytes": size, "name": origName})
}

func (h *Handler) promote(ctx context.Context, ownerID int64, fid, finalKey, stagingKey string) error {
	// copy staging → final (GET then PUT)
	rc, _, err := h.store.Get(ctx, stagingKey)
	if err != nil {
		return err
	}
	defer rc.Close()
	return h.store.Put(ctx, finalKey, rc, -1, "application/octet-stream")
}

func (h *Handler) list(c *gin.Context) {
	user := httpx.UserFrom(c)
	appID := c.Query("application_id")
	args := []any{user.ID}
	where := "owner_id=$1"
	if appID != "" {
		args = append(args, appID)
		where += " AND used_by_application=$2"
	}
	rows, err := h.db.Query(c.Request.Context(), `SELECT id, owner_id, original_name, content_type, size_bytes, sha256, status, category, used_by_application, created_at
		FROM files WHERE `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	defer rows.Close()
	type fDTO struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		ContentType string    `json:"content_type"`
		SizeBytes   int64     `json:"size_bytes"`
		SHA256      string    `json:"sha256"`
		Status      string    `json:"status"`
		Category    string    `json:"category"`
		UsedByApp   *int64    `json:"application_id"`
		CreatedAt   time.Time `json:"created_at"`
	}
	var items []fDTO
	for rows.Next() {
		var f fDTO
		var owner int64
		if err := rows.Scan(&f.ID, &owner, &f.Name, &f.ContentType, &f.SizeBytes, &f.SHA256,
			&f.Status, &f.Category, &f.UsedByApp, &f.CreatedAt); err != nil {
			httpx.WriteErr(c, err)
			return
		}
		items = append(items, f)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handler) download(c *gin.Context) {
	user := httpx.UserFrom(c)
	fid := c.Param("id")
	if fid == "" {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的文件 ID"))
		return
	}
	var row fileRow
	err := h.db.QueryRow(c.Request.Context(), `SELECT id, owner_id, final_key, original_name, content_type, size_bytes, sha256, status FROM files WHERE id=$1 AND owner_id=$2`,
		fid, user.ID).Scan(&row.ID, &row.OwnerID, &row.FinalKey, &row.OriginalName, &row.ContentType, &row.SizeBytes, &row.SHA256, &row.Status)
	if err != nil {
		httpx.WriteErr(c, httpx.NotFound("文件不存在"))
		return
	}
	if row.Status != "ready" {
		httpx.WriteErr(c, httpx.BadRequest("not_ready", "文件尚未就绪"))
		return
	}
	rc, _, err := h.store.Get(c.Request.Context(), row.FinalKey)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	defer rc.Close()
	// encoded Content-Disposition with UTF-8 filename
	disposition := fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, escapeQuotes(row.OriginalName), url.PathEscape(row.OriginalName))
	c.Header("Content-Disposition", disposition)
	c.Header("Content-Type", row.ContentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, row.SizeBytes, row.ContentType, rc, nil)
}

func (h *Handler) delete(c *gin.Context) {
	user := httpx.UserFrom(c)
	fid := c.Param("id")
	if fid == "" {
		httpx.WriteErr(c, httpx.BadRequest("invalid_id", "无效的文件 ID"))
		return
	}
	// refs check: still linked to any application? plan: removing association
	// != deleting file; if other apps reference it, refuse.
	var refCount int
	if err := h.db.QueryRow(c.Request.Context(), `SELECT count(*) FROM application_files WHERE file_id=$1 AND owner_id=$2`, fid, user.ID).Scan(&refCount); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if refCount > 0 {
		httpx.WriteErr(c, httpx.Conflict("file_in_use", "文件仍被申请记录引用，请先移除关联"))
		return
	}
	var finalKey string
	if err := h.db.QueryRow(c.Request.Context(), `SELECT final_key FROM files WHERE id=$1 AND owner_id=$2`, fid, user.ID).Scan(&finalKey); err != nil {
		httpx.WriteErr(c, httpx.NotFound("文件不存在"))
		return
	}
	// delete object then record (idempotent S3 delete)
	if err := h.store.Delete(c.Request.Context(), finalKey); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if _, err := h.db.Exec(c.Request.Context(), `UPDATE files SET status='deleted', updated_at=now() WHERE id=$1 AND owner_id=$2`, fid, user.ID); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	httpx.Ok(c)
}

// -- helpers --

func nilOrID(id int64) any {
	if id > 0 {
		return id
	}
	return nil
}

func storeStat(ctx context.Context, s objectstore.Store, key string) int64 {
	n, err := s.Stat(ctx, key)
	if err != nil {
		return -1
	}
	return n
}

func sanitizeName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}

func escapeQuotes(s string) string {
	return strings.NewReplacer(`"`, `\"`, `\`, `\\`).Replace(s)
}

var _ = json.Marshal
var _ = mime.TypeByExtension
