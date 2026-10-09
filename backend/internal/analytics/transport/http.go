package transport

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/analytics"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/day"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/platform/timeutil"
)

type Handler struct {
	repo *analytics.Repo
	db   *database.DB
}

func New(repo *analytics.Repo, db *database.DB) *Handler { return &Handler{repo: repo, db: db} }

// Routes mounts analytics under /api/v1/analytics.
func (h *Handler) Routes(g *gin.RouterGroup) {
	g.Use(httpx.RequireUser)
	g.POST("/summary", h.summary)
	g.POST("/sankey", h.sankey)
	g.GET("/drilldowns/:token", h.drilldown)
	g.GET("/overview", h.overview)
	g.GET("/heatmap", h.heatmap)
}

// heatmapMaxDays bounds the requested window: 366 inclusive days fits a full
// leap year (2028-01-01 … 2028-12-31) while capping the response size.
const heatmapMaxDays = 366

// defaultHeatmapDays is 365 inclusive days — 用户时区的今天往前 364 天 — which
// lays out as exactly 53 week columns on the frontend.
const defaultHeatmapDays = 365

// scopeReq mirrors the shared filter inputs of the analytics endpoints.
type scopeReq struct {
	From          *time.Time `json:"from"`
	To            *time.Time `json:"to"`
	Status        string     `json:"status"`
	Channel       string     `json:"channel"`
	Tags          []string   `json:"tags"`
	Company       string     `json:"company"`
	Search        string     `json:"search"`
	SubmittedFrom *time.Time `json:"submitted_from"`
	SubmittedTo   *time.Time `json:"submitted_to"`
	Mode          string     `json:"mode"` // current | history
}

func (h *Handler) toRequest(c *gin.Context, req *scopeReq, tz string) *analytics.SnapshotRequest {
	return &analytics.SnapshotRequest{
		OwnerID: httpx.UserFrom(c).ID, From: req.From, To: req.To, Status: req.Status,
		Channel: req.Channel, Tags: req.Tags, Company: req.Company, Search: req.Search,
		SubmittedFrom: req.SubmittedFrom, SubmittedTo: req.SubmittedTo,
		Timezone: tz, Now: time.Now(),
	}
}

func (h *Handler) summary(c *gin.Context) {
	var req scopeReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	user := httpx.UserFrom(c)
	sr := h.toRequest(c, &req, user.Timezone)
	m, err := h.repo.Counts(c.Request.Context(), sr)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	ch, err := h.repo.ByChannel(c.Request.Context(), sr)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	m.ByChannel = ch
	// status name map for frontend convenience
	names := map[string]string{}
	for k, v := range analytics.StatusName {
		names[k] = v
	}
	c.JSON(http.StatusOK, gin.H{
		"metrics": m, "status_names": names, "as_of": sr.Now.Format(time.RFC3339),
		"timezone": user.Timezone, "scope": req,
	})
}

func (h *Handler) sankey(c *gin.Context) {
	var req scopeReq
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.WriteErr(c, err)
		return
	}
	user := httpx.UserFrom(c)
	sr := h.toRequest(c, &req, user.Timezone)
	var sk *analytics.Sankey
	var err error
	if req.Mode == "history" {
		sk, err = h.repo.SankeyB(c.Request.Context(), sr, 12)
	} else {
		sk, err = h.repo.SankeyA(c.Request.Context(), sr)
	}
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	// Persist the cohort membership so node/edge drilldowns can never disagree
	// with the chart (token bound to user+mode+filter+as_of; expires ~10min).
	members, err := h.repo.ScopeMemberIDs(c.Request.Context(), sr)
	if err == nil {
		tok, serr := h.SaveSnapshot(c, req.Mode, members, map[string]any{"scope": req})
		if serr == nil {
			sk.DrilldownToken = tok
		}
	}
	c.JSON(http.StatusOK, sk)
}

// drilldown returns the current snapshot membership for a token so charts and
// lists never disagree; the token is bound to (user, mode, filter, as_of).
func (h *Handler) drilldown(c *gin.Context) {
	token := c.Param("token")
	user := httpx.UserFrom(c)
	var payload []byte
	var mode string
	var memberIDs []int64
	var expires time.Time
	err := h.db.Pool().QueryRow(c.Request.Context(),
		`SELECT payload, mode, member_ids, expires_at FROM analytics_snapshots WHERE token=$1 AND owner_id=$2`,
		token, user.ID).Scan(&payload, &mode, &memberIDs, &expires)
	if err != nil || time.Now().After(expires) {
		httpx.WriteErr(c, httpx.NotFound("下钻令牌已过期，请刷新图表"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"mode": mode, "member_ids": memberIDs, "expires_at": expires, "meta": payload})
}

// SaveSnapshot caches a stats membership set for short-lived drilldown.
func (h *Handler) SaveSnapshot(c *gin.Context, mode string, members []int64, meta any) (string, error) {
	tok := newToken()
	b, _ := json.Marshal(meta)
	var arr []int64
	if members != nil {
		arr = members
	}
	_, err := h.db.Pool().Exec(c.Request.Context(),
		`INSERT INTO analytics_snapshots(owner_id, mode, payload, member_ids, token, expires_at)
		 VALUES($1,$2,$3,$4,$5, now() + interval '10 minutes')
		 ON CONFLICT (token) DO NOTHING`,
		httpx.UserFrom(c).ID, mode, b, arr, tok)
	if err != nil {
		return "", err
	}
	return tok, nil
}

func (h *Handler) overview(c *gin.Context) {
	// /analytics/overview returns aggregate counts without deep filters
	// (cheap route for the mobile list header).
	user := httpx.UserFrom(c)
	sr := &analytics.SnapshotRequest{OwnerID: user.ID, Now: time.Now()}
	m, err := h.repo.Counts(c.Request.Context(), sr)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	c.JSON(http.StatusOK, m)
}

// heatmap returns the 投递热力图 payload (docs/投递热力图方案.md §3.1).
//
// from/to are date-only (YYYY-MM-DD) calendar days in the USER'S timezone,
// closed interval; each is independently optional and defaults to a 365-day
// window ending on 用户时区的今天. 今天 is resolved server-side so the frontend's
// 当前连续天数 never depends on the browser timezone.
func (h *Handler) heatmap(c *gin.Context) {
	user := httpx.UserFrom(c)
	loc, tz := timeutil.SafeLocation(user.Timezone)
	todayStr := timeutil.DateOnly(time.Now(), loc)
	today, err := day.Parse(todayStr)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	from := today.AddDate(0, 0, -(defaultHeatmapDays - 1))
	to := today
	if s := c.Query("from"); s != "" {
		t, err := day.Parse(s)
		if err != nil {
			httpx.WriteErr(c, httpx.BadRequest("invalid_from", err.Error()))
			return
		}
		from = t
	}
	if s := c.Query("to"); s != "" {
		t, err := day.Parse(s)
		if err != nil {
			httpx.WriteErr(c, httpx.BadRequest("invalid_to", err.Error()))
			return
		}
		to = t
	}
	if from.After(to) {
		httpx.WriteErr(c, httpx.BadRequest("invalid_range", "from 不能晚于 to"))
		return
	}
	// Both bounds are UTC-midnight instants (day.Parse), so the arithmetic is
	// exact — no DST-shortened day can skew the count.
	if days := int(to.Sub(from).Hours()/24) + 1; days > heatmapMaxDays {
		httpx.WriteErr(c, httpx.BadRequest("range_too_wide", "时间跨度不能超过 366 天"))
		return
	}
	hm, err := h.repo.Heatmap(c.Request.Context(), user.ID, tz, from, to)
	if err != nil {
		httpx.WriteErr(c, err)
		return
	}
	hm.Today = todayStr
	c.JSON(http.StatusOK, hm)
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
