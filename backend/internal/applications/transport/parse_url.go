package transport

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/jdparse"
	"offerlog/backend/internal/platform/httpx"
)

// urlParser abstracts the JD fetch/heuristic so tests can stub it. The default
// implementation calls jdparse.Parse (real HTTP, SSRF-hardened).
type urlParser interface {
	Parse(ctx context.Context, rawURL string) (jdparse.Result, error)
}

type defaultParser struct{}

func (defaultParser) Parse(ctx context.Context, rawURL string) (jdparse.Result, error) {
	return jdparse.Parse(ctx, rawURL)
}

// rateLimiter is a tiny fixed-window per-user limiter (each user ≤ N requests
// per minute). In-memory only — adequate for a personal instance; production
// behind a single API replica sees the same behavior.
type rateLimiter struct {
	mu    sync.Mutex
	limit int
	hits  map[int64][]time.Time
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{limit: limit, hits: map[int64][]time.Time{}}
}

func (l *rateLimiter) Allow(id int64) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.hits[id][:0]
	for _, t := range l.hits[id] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	l.hits[id] = recent
	if len(recent) >= l.limit {
		return false
	}
	l.hits[id] = append(l.hits[id], now)
	return true
}

// parseURLHandler is a sub-handler of the applications transport that exposes
// POST /applications/parse-url. It is a separate file to keep the SSRF-hardened
// outbound fetch clearly isolated from the CRUD surface.
type parseURLHandler struct {
	parser urlParser
	rl     *rateLimiter
}

// newParseURLHandler returns the handler with the real parser and a per-user
// allowance of 10 JD fetches per minute.
func newParseURLHandler() *parseURLHandler {
	return &parseURLHandler{parser: defaultParser{}, rl: newRateLimiter(10)}
}

// newParseURLHandlerWith returns a handler with an injected parser and rate
// limit (tests only: no outbound HTTP).
func newParseURLHandlerWith(parser urlParser, perMinute int) *parseURLHandler {
	return &parseURLHandler{parser: parser, rl: newRateLimiter(perMinute)}
}

// parseURLRoute registers the parse-url endpoint on a (RequireUser-wrapped)
// group. The CRUD surface calls it from Routes; tests mount it standalone.
func (h *parseURLHandler) parseURLRoute(g *gin.RouterGroup) {
	g.POST("/applications/parse-url", h.parseURL)
}

type parseURLReq struct {
	URL string `json:"url"`
}

// parseURL answers 200 with the parsed fields when the fetch+heuristics
// succeed and with empty fields when they do not — the frontend always falls
// back to manual entry. Only the rate limit (429) and a missing/blank url
// (400) are hard failures.
func (h *parseURLHandler) parseURL(c *gin.Context) {
	user := httpx.UserFrom(c)
	var req parseURLReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	if req.URL == "" || len(req.URL) > 2048 {
		httpx.WriteErr(c, httpx.BadRequest("bad_url", "缺少 JD 链接"))
		return
	}
	if !jdparse.LooksLikeURL(req.URL) {
		httpx.WriteErr(c, httpx.BadRequest("bad_url", "仅支持 http/https 链接"))
		return
	}
	if !h.rl.Allow(user.ID) {
		httpx.WriteErr(c, &httpx.ErrorKind{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "解析请求过于频繁，请稍后再试"})
		return
	}
	// Fetch with the request context so a client disconnect cancels the
	// outbound call too.
	res, err := h.parser.Parse(c.Request.Context(), req.URL)
	if err != nil {
		// SSRF / network / parse failures all degrade to empty fields; the
		// dialog stays usable (200 is not a lie about the request — it is the
		// documented contract for "预填失败, 退回手填").
		c.JSON(http.StatusOK, jdparse.Result{})
		return
	}
	c.JSON(http.StatusOK, res)
}
