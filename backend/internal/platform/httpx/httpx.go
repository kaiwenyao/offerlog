// Package httpx provides gin-friendly response helpers, the authenticated
// user in context, error mapping and shared middleware.
package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"

	"offerlogs/backend/internal/identity/domain"
	authservice "offerlogs/backend/internal/identity/service"
	"offerlogs/backend/internal/platform/observability"
)

// ErrBody is the canonical error envelope (plan §9): {code,message,request_id}.
type ErrBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// ErrorKind carries an explicit HTTP status.
type ErrorKind struct {
	Status  int
	Code    string
	Message string
}

func (e *ErrorKind) Error() string { return e.Message }

func BadRequest(code, msg string) error {
	return &ErrorKind{Status: http.StatusBadRequest, Code: code, Message: msg}
}
func Unauthorized(msg string) error {
	return &ErrorKind{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}
func Forbidden(msg string) error {
	return &ErrorKind{Status: http.StatusForbidden, Code: "forbidden", Message: msg}
}
func NotFound(msg string) error {
	return &ErrorKind{Status: http.StatusNotFound, Code: "not_found", Message: msg}
}
func Conflict(code, msg string) error {
	return &ErrorKind{Status: http.StatusConflict, Code: code, Message: msg}
}

// codedError is implemented by domain errors that carry a stable code.
type codedError interface{ ErrorCode() string }

// WriteErr maps an error to the canonical envelope. SQL and stack details are
// never exposed to clients.
func WriteErr(c *gin.Context, err error) {
	ctx := c.Request.Context()
	status := http.StatusInternalServerError
	code := "internal_error"
	msg := "服务器内部错误"

	var ek *ErrorKind
	switch {
	case errors.As(err, &ek):
		status, code, msg = ek.Status, ek.Code, ek.Message
	case errors.Is(err, authservice.ErrBadLogin):
		status, code, msg = http.StatusUnauthorized, "invalid_credentials", "邮箱或密码错误"
	case errors.Is(err, authservice.ErrNoPermission):
		status, code, msg = http.StatusForbidden, "forbidden", "无权访问该资源"
	case errors.Is(err, authservice.ErrSessionExpired):
		status, code, msg = http.StatusUnauthorized, "session_expired", "会话已过期，请重新登录"
	default:
		var ce codedError
		if errors.As(err, &ce) {
			status = http.StatusUnprocessableEntity
			code = ce.ErrorCode()
			msg = err.Error()
		}
	}

	if status >= 500 {
		observability.L(ctx).Error("request failed", "error", err, "stack", fmt.Sprintf("%+v", err))
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		observability.L(ctx).Error("stack", "trace", string(buf[:n]))
	} else {
		observability.L(ctx).Debug("request rejected", "error", err)
	}

	c.JSON(status, ErrBody{Code: code, Message: msg, RequestID: observability.RequestID(ctx)})
}

const userKey = "offerlogs.user"

// SetUser stores the authenticated principal in the gin context.
func SetUser(c *gin.Context, u *domain.User) { c.Set(userKey, u) }

// UserFrom reads the authenticated principal; nil when anonymous.
func UserFrom(c *gin.Context) *domain.User {
	v, _ := c.Get(userKey)
	u, _ := v.(*domain.User)
	return u
}

// RequireUser rejects anonymous requests with 401.
func RequireUser(c *gin.Context) {
	if UserFrom(c) == nil {
		WriteErr(c, Unauthorized("请先登录"))
		c.Abort()
		return
	}
	c.Next()
}

// BindJSON decodes the request body into v with error mapping.
func BindJSON(c *gin.Context, v any) error {
	if err := c.ShouldBindJSON(v); err != nil {
		return BadRequest("invalid_json", "请求体格式错误")
	}
	return nil
}

// PathID parses an int64 URL path parameter (gin :id style).
func PathID(c *gin.Context, name string) (int64, error) {
	s := c.Param(name)
	var n int64
	if s == "" {
		return 0, errors.New("empty path param")
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errors.New("path param is not a number")
		}
		n = n*10 + int64(ch-'0')
	}
	return n, nil
}

// RealIP extracts the client IP, honoring a single trusted proxy hop.
func RealIP(c *gin.Context) string {
	if xf := c.GetHeader("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	return c.ClientIP()
}

// Ok writes 200 {"ok":true}.
func Ok(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
