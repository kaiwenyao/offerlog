package transport

import (
	"net/http"

	"github.com/gin-gonic/gin"

	idrepo "offerlog/backend/internal/identity/repository"
	idservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/httpx"
)

type Handler struct {
	auth    *idservice.Store
	limiter *httpx.LoginRateLimiter
	secure  bool
}

func New(auth *idservice.Store, secure bool) *Handler {
	return &Handler{auth: auth, limiter: httpx.NewLoginRateLimiter(), secure: secure}
}

// Routes mounts auth endpoints. Login/logout/me sit outside CSRF because the
// session itself is the CSRF anchor; the Origin check in CSRF middleware
// protects them from cross-site requests when a cookie is already present.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.POST("/login", h.login)
	g.POST("/logout", h.logout)
	g.GET("/me", h.me)
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

var _ = idrepo.NewSQLUsers
