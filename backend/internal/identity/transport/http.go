package transport

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	idrepo "offerlog/backend/internal/identity/repository"
	idservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/httpx"
)

type Handler struct {
	auth             *idservice.Store
	limiter          *httpx.LoginRateLimiter
	regLimiter       *httpx.LoginRateLimiter
	secure           bool
	registrationOpen bool
	defaultTZ        string
}

// Config carries the transport-level switches resolved from app config.
type Config struct {
	Secure           bool
	RegistrationOpen bool
	DefaultTZ        string
}

func New(auth *idservice.Store, cfg Config) *Handler {
	return &Handler{
		auth:             auth,
		limiter:          httpx.NewLoginRateLimiter(),
		regLimiter:       httpx.NewLoginRateLimiter(),
		secure:           cfg.Secure,
		registrationOpen: cfg.RegistrationOpen,
		defaultTZ:        cfg.DefaultTZ,
	}
}

// Routes mounts auth endpoints. Login/register/logout/me sit outside CSRF
// because the session itself is the CSRF anchor; the Origin check in CSRF
// middleware protects them from cross-site requests when a cookie is already
// present.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.POST("/login", h.login)
	g.POST("/register", h.register)
	g.GET("/config", h.authConfig)
	g.POST("/logout", h.logout)
	g.GET("/me", h.me)
	g.PATCH("/me", h.updateMe)
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(c *gin.Context) {
	if !h.limiter.Allow(httpx.RealIP(c)) {
		httpx.WriteErr(c, httpx.BadRequest("rate_limited", "尝试过于频繁，请一分钟后再试"))
		return
	}
	var req loginReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if req.Email == "" || req.Password == "" {
		httpx.WriteErr(c, httpx.BadRequest("invalid_credentials", "请输入邮箱和密码"))
		return
	}
	sess, u, err := h.auth.CreateSession(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	http.SetCookie(c.Writer, httpx.NewSessionCookie(sess.Token, sess.Expires, h.secure))
	c.JSON(http.StatusOK, gin.H{
		"id": u.ID, "email": u.Email, "display_name": u.DisplayName,
		"timezone": u.Timezone, "locale": u.Locale,
		"csrf_token": sess.CSRF,
	})
}

// registerReq mirrors loginReq plus an optional display name.
type registerReq struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (h *Handler) register(c *gin.Context) {
	if !h.regLimiter.Allow(httpx.RealIP(c)) {
		httpx.WriteErr(c, httpx.BadRequest("rate_limited", "尝试过于频繁，请一分钟后再试"))
		return
	}
	var req registerReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if req.Email == "" || req.Password == "" {
		httpx.WriteErr(c, httpx.BadRequest("invalid_request", "请输入邮箱和密码"))
		return
	}
	if !h.registrationOpen {
		httpx.WriteErr(c, &httpx.ErrorKind{
			Status: http.StatusForbidden, Code: "registration_closed",
			Message: "当前实例未开放注册，请联系管理员创建账号",
		})
		return
	}
	u, err := h.auth.Register(c.Request.Context(), req.Email, req.Password, req.DisplayName, h.defaultTZ)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Signup signs the user in immediately: one argon2 verify more than a
	// dedicated session mint, but it reuses the exact login session path.
	sess, u, err := h.auth.CreateSession(c.Request.Context(), u.Email, req.Password)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	http.SetCookie(c.Writer, httpx.NewSessionCookie(sess.Token, sess.Expires, h.secure))
	c.JSON(http.StatusCreated, gin.H{
		"id": u.ID, "email": u.Email, "display_name": u.DisplayName,
		"timezone": u.Timezone, "locale": u.Locale,
		"csrf_token": sess.CSRF,
	})
}

// authConfig is the public, unauthenticated settings surface for the login
// page (which fields to render before any session exists).
func (h *Handler) authConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"registration_open": h.registrationOpen})
}

func (h *Handler) logout(c *gin.Context) {
	if tok, err := c.Cookie(httpx.SessionCookieName); err == nil {
		_ = h.auth.Logout(c.Request.Context(), tok)
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: httpx.SessionCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
	httpx.Ok(c)
}

func (h *Handler) me(c *gin.Context) {
	u := httpx.UserFrom(c)
	if u == nil {
		httpx.WriteErr(c, httpx.Unauthorized("未登录"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id": u.ID, "email": u.Email, "display_name": u.DisplayName,
		"timezone": u.Timezone, "locale": u.Locale,
	})
}

// updateMe persists display name / timezone from the settings page. It runs
// on the CSRF-protected parent group because a session already exists. Both
// fields are optional pointers (only present ones change).
func (h *Handler) updateMe(c *gin.Context) {
	u := httpx.UserFrom(c)
	if u == nil {
		httpx.WriteErr(c, httpx.Unauthorized("未登录"))
		return
	}
	var req struct {
		DisplayName *string `json:"display_name"`
		Timezone    *string `json:"timezone"`
	}
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	displayName := u.DisplayName
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if len(displayName) > 80 {
			httpx.WriteErr(c, httpx.BadRequest("display_name_too_long", "显示名称不能超过 80 个字符"))
			return
		}
	}
	tz := u.Timezone
	if req.Timezone != nil {
		tz = strings.TrimSpace(*req.Timezone)
		if _, err := time.LoadLocation(tz); err != nil {
			httpx.WriteErr(c, httpx.BadRequest("invalid_timezone", "无效的时区（需 IANA 名称，如 Europe/Dublin）"))
			return
		}
	}
	row, err := h.auth.UpdateProfile(c.Request.Context(), u.ID, displayName, tz)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Keep the in-context user fresh for the remainder of this request.
	u.DisplayName = row.DisplayName
	u.Timezone = row.Timezone
	c.JSON(http.StatusOK, gin.H{
		"id": row.ID, "email": row.Email, "display_name": row.DisplayName,
		"timezone": row.Timezone, "locale": row.Locale,
	})
}

var _ = idrepo.NewSQLUsers
