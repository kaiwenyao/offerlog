package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/identity/domain"
	authservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/observability"
)

// RequestID assigns a request id to every request and stores it in context.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(observability.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

const cookieName = "offerlog_session"

// SessionCookieName is exported for the frontend.
const SessionCookieName = cookieName

// NewSessionCookie returns the cookie storing the opaque session token.
func NewSessionCookie(token string, expires time.Time, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// Auth resolves the session cookie into a user and stores it in context.
// Requests without a valid session proceed without a user; RequireUser guards
// protect individual routes.
func Auth(auth *authservice.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tok, err := c.Cookie(cookieName); err == nil && tok != "" {
			if u, err := auth.ValidateToken(c.Request.Context(), tok); err == nil {
				du := &domain.User{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName,
					Timezone: u.Timezone, Locale: u.Locale, IsAdmin: u.IsAdmin}
				SetUser(c, du)
			}
		}
		c.Next()
	}
}

// CSRF requires a valid CSRF token for state-changing methods; the token
// travels in the X-CSRF-Token header and must match the session cookie.
func CSRF(auth *authservice.Store) gin.HandlerFunc {
	return CSRFWithAllowedOrigins(auth, nil)
}

// CSRFWithAllowedOrigins behaves exactly like CSRF, except that origins in
// extraOrigins are treated as same-origin during the Origin check. It exists
// for the local dev workflow: through the Vite proxy (:5173) the browser keeps
// sending Origin: http://localhost:5173 while the request Host is rewritten to
// the API's target, so the strict sameOrigin check rejects every write with
// 403. Setting DEV_ALLOWED_ORIGINS only in development unblocks local write
// verification without weakening production (production never sets the
// variable, so the behavior is byte-for-byte the original).
func CSRFWithAllowedOrigins(auth *authservice.Store, extraOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		m := c.Request.Method
		if m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions {
			c.Next()
			return
		}
		// Login/register bootstrap the session itself (nothing to anchor a CSRF
		// token to before login), and logout only clears the cookie. They are
		// protected by SameSite=Lax + the Origin check below. NOTE: this is a
		// precise allowlist of the pre-session/self-teardown endpoints — it must
		// NOT exempt authenticated profile mutations like PATCH /api/v1/auth/me,
		// which carry a live session and therefore must present the CSRF token
		// (the frontend attaches it to every non-GET via the api wrapper).
		switch c.Request.URL.Path {
		case "/api/v1/auth/login", "/api/v1/auth/register", "/api/v1/auth/logout":
			c.Next()
			return
		}
		tok, err := c.Cookie(cookieName)
		if err != nil {
			WriteErr(c, Unauthorized("会话缺失"))
			c.Abort()
			return
		}
		// SameSite=Lax already stops cross-site POSTs; Origin check adds
		// defense in depth.
		if origin := c.GetHeader("Origin"); origin != "" && !sameOrigin(origin, c.Request, extraOrigins) {
			WriteErr(c, Forbidden("Origin 校验失败"))
			c.Abort()
			return
		}
		header := c.GetHeader("X-CSRF-Token")
		if !auth.ValidateCSRF(c.Request.Context(), tok, header) {
			WriteErr(c, Forbidden("CSRF 校验失败"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func sameOrigin(origin string, r *http.Request, extraOrigins []string) bool {
	host := r.Host
	if strings.HasPrefix(origin, "http://"+host) || strings.HasPrefix(origin, "https://"+host) {
		return true
	}
	for _, o := range extraOrigins {
		// Trim a trailing slash so "http://localhost:5173/" matches too.
		o = strings.TrimRight(o, "/")
		if o != "" && origin == o {
			return true
		}
	}
	return false
}

// SecurityHeaders sets baseline headers.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("X-Frame-Options", "DENY")
		if c.Request.TLS != nil {
			c.Header("Strict-Transport-Security", "max-age=63072000")
		}
		c.Next()
	}
}

// LoginRateLimiter is a tiny in-memory per-IP limiter for login.
type LoginRateLimiter struct {
	hits map[string][]time.Time
}

func NewLoginRateLimiter() *LoginRateLimiter {
	return &LoginRateLimiter{hits: map[string][]time.Time{}}
}

// Allow reports whether ip may attempt a login now (≤10/min).
func (l *LoginRateLimiter) Allow(ip string) bool {
	now := time.Now()
	l.hits[ip] = append(l.hits[ip], now)
	var recent []time.Time
	for _, t := range l.hits[ip] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	l.hits[ip] = recent
	return len(recent) <= 10
}
