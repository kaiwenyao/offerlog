// 投递热力图 integration coverage (docs/投递热力图方案.md §6): the day bucket
// follows the USER's timezone (not UTC, not the server's), the live set is
// 「回收站不计 / 已归档计入」 — deliberately different from every other
// analytics query — 无投递日期 but 已进入流程 rows land in undated, a correction
// moves the count with the replayed submitted_at, parameter validation returns
// 400, and the query never crosses owners.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	antrepo "offerlog/backend/internal/analytics"
	anttransport "offerlog/backend/internal/analytics/transport"
	apprepo "offerlog/backend/internal/applications/repository"
	appservice "offerlog/backend/internal/applications/service"
	iddomain "offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/day"
	"offerlog/backend/internal/platform/httpx"
)

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := day.Parse(s)
	if err != nil {
		t.Fatalf("parse day %q: %v", s, err)
	}
	return d
}

// heatmapWindow is a wide window covering every fixture date in this file.
func heatmapWindow(t *testing.T) (time.Time, time.Time) {
	t.Helper()
	return mustDay(t, "2026-09-01"), mustDay(t, "2026-11-30")
}

// mustApply creates one 已投递 record at the given instant and hands back the
// row so a test can trash / archive / correct it afterwards.
func mustApply(t *testing.T, svc *appservice.Service, owner int64, company string, submitted time.Time) *apprepo.Row {
	t.Helper()
	row, err := svc.Create(context.Background(), owner, &appservice.CreateInput{
		CompanyName: company, Position: "岗位", Status: "applied", SubmittedAt: &submitted,
	})
	if err != nil {
		t.Fatalf("create %s: %v", company, err)
	}
	return row
}

func dayCount(h *antrepo.Heatmap, date string) int64 {
	for _, d := range h.Days {
		if d.Date == date {
			return d.Count
		}
	}
	return 0
}

// The same instant is two different calendar days depending on the user's zone:
// 2026-10-08T16:30Z is 00:30 on 10-09 in Shanghai but 09:30 on 10-08 in LA.
// Counting by UTC would put a late-evening Shanghai submission on the wrong
// day, which is the whole reason the query takes a timezone.
func TestHeatmapBucketsByUserTimezone(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	repo := antrepo.New(db)
	sub := time.Date(2026, 10, 8, 16, 30, 0, 0, time.UTC)
	mustApply(t, svc, owner, "TZCo", sub)

	from, to := heatmapWindow(t)
	sh, err := repo.Heatmap(ctx, owner, "Asia/Shanghai", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(sh.Days) != 1 || sh.Days[0].Date != "2026-10-09" || sh.Days[0].Count != 1 {
		t.Fatalf("Asia/Shanghai days = %+v, want [{2026-10-09 1}]", sh.Days)
	}

	la, err := repo.Heatmap(ctx, owner, "America/Los_Angeles", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(la.Days) != 1 || la.Days[0].Date != "2026-10-08" || la.Days[0].Count != 1 {
		t.Fatalf("America/Los_Angeles days = %+v, want [{2026-10-08 1}]", la.Days)
	}
	if sh.Total != 1 || la.Total != 1 {
		t.Fatalf("totals = %d/%d, want 1/1", sh.Total, la.Total)
	}
}

// 口径 §2: 回收站不计（用户已经不认这条记录），已归档计入（归档只表示不想再
// 看到，不改变那天确实投过）。This is the one place analytics deliberately
// diverges from the 已投递 card, which drops archived rows.
func TestHeatmapExcludesDeletedIncludesArchived(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	repo := antrepo.New(db)

	trashed := mustApply(t, svc, owner, "TrashedCo", time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	kept := mustApply(t, svc, owner, "ArchivedCo", time.Date(2026, 10, 4, 13, 0, 0, 0, time.UTC))
	mustApply(t, svc, owner, "LiveCo", time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))

	if err := svc.SoftDelete(ctx, owner, trashed.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Archive(ctx, owner, kept.ID, true); err != nil {
		t.Fatal(err)
	}

	from, to := heatmapWindow(t)
	h, err := repo.Heatmap(ctx, owner, "UTC", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if got := dayCount(h, "2026-10-04"); got != 1 {
		t.Fatalf("2026-10-04 count = %d, want 1 (deleted dropped, archived kept)", got)
	}
	if got := dayCount(h, "2026-10-05"); got != 1 {
		t.Fatalf("2026-10-05 count = %d, want 1", got)
	}
	if h.Total != 2 {
		t.Fatalf("total = %d, want 2 (3 rows, 1 trashed)", h.Total)
	}
}

// 内推 / 猎头 legitimately reach the pipeline with no submitted_at. Those rows
// cannot land on a day, so they must surface as undated rather than vanish —
// while a 待投递 row (still saved, never submitted) must not inflate the count.
func TestHeatmapUndatedCountsPipelineRowsOnly(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	repo := antrepo.New(db)

	// 还没投出去：不算 undated（就是「待投递」）。
	if _, err := svc.Create(ctx, owner, &appservice.CreateInput{CompanyName: "NotYet", Position: "岗位"}); err != nil {
		t.Fatal(err)
	}
	// 免正式投递直接约面：已进入流程、没有日期 → undated。
	referred := mustCreate(t, svc, owner, "ReferralCo", "岗位")
	if _, err := svc.Transition(ctx, owner, referred.ID, &appservice.TransitionInput{
		ToStatus: "interviewing", Version: 1, NoFormalSubmission: true,
	}); err != nil {
		t.Fatalf("no-formal-submission transition: %v", err)
	}

	from, to := heatmapWindow(t)
	h, err := repo.Heatmap(ctx, owner, "UTC", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Days) != 0 || h.Total != 0 {
		t.Fatalf("days = %+v total = %d, want none (no submitted_at anywhere)", h.Days, h.Total)
	}
	if h.Undated != 1 {
		t.Fatalf("undated = %d, want 1 (interviewing row with no submitted_at)", h.Undated)
	}
}

// A correction rewrites the timeline, and submitted_at is back-derived from the
// first 已投递 point on every RecomputeStatus. So fixing 「投递时间填错了」 must
// move the cell — this asserts the end-to-end result, not the SQL alone.
func TestHeatmapCorrectionMovesDay(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	repo := antrepo.New(db)

	app := mustApply(t, svc, owner, "CorrectCo", time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC))

	from, to := heatmapWindow(t)
	before, err := repo.Heatmap(ctx, owner, "UTC", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if dayCount(before, "2026-10-06") != 1 {
		t.Fatalf("before correction days = %+v, want a cell on 2026-10-06", before.Days)
	}

	fixed := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	if _, err := svc.CorrectCurrent(ctx, owner, app.ID, &appservice.CorrectCurrentInput{
		ToStatus: "applied", OccurredAt: &fixed, Reason: "投递时间填错了", Version: app.Version,
	}); err != nil {
		t.Fatalf("correction: %v", err)
	}

	after, err := repo.Heatmap(ctx, owner, "UTC", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if got := dayCount(after, "2026-10-06"); got != 0 {
		t.Fatalf("2026-10-06 count after correction = %d, want 0", got)
	}
	if got := dayCount(after, "2026-10-07"); got != 1 {
		t.Fatalf("2026-10-07 count after correction = %d, want 1 (days=%+v)", got, after.Days)
	}
	if after.Total != 1 {
		t.Fatalf("total after correction = %d, want 1", after.Total)
	}
}

// Only the caller's own rows: the heatmap takes ownerID from the session, never
// from a parameter, and a second account's submissions must not bleed in.
func TestHeatmapScopedToOwner(t *testing.T) {
	ctx := context.Background()
	db, svc, _, ownerA := setup(t)
	repo := antrepo.New(db)
	ownerB := createOwner(t, db)

	mustApply(t, svc, ownerA, "MineCo", time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC))
	mustApply(t, svc, ownerB, "TheirsCo", time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC))
	mustApply(t, svc, ownerB, "Theirs2Co", time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC))

	from, to := heatmapWindow(t)
	h, err := repo.Heatmap(ctx, ownerA, "UTC", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if h.Total != 1 || len(h.Days) != 1 || dayCount(h, "2026-10-02") != 1 {
		t.Fatalf("owner A heatmap = %+v, want only its own single submission", h)
	}
	if h.Undated != 0 {
		t.Fatalf("undated = %d, want 0 (owner B's rows must not leak)", h.Undated)
	}
}

func newHeatmapServer(t *testing.T, db *database.DB, owner int64, tz string) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &iddomain.User{ID: owner, Email: "x@y.z", Timezone: tz, Locale: "zh-CN"})
		c.Next()
	})
	h := anttransport.New(antrepo.New(db), db)
	h.Routes(r.Group("/api/v1/analytics"))
	return httptest.NewServer(r)
}

// Parameter envelope (§3.1): from > to, a window wider than 366 days and a
// non-date all return 400 — never a silently clamped chart. The default window
// is 365 inclusive days ending on 用户时区的今天, and the response carries that
// 今天 so the frontend's streak math never depends on the browser clock.
func TestHeatmapHTTPValidationAndDefaults(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newHeatmapServer(t, db, owner, "Asia/Shanghai")
	defer srv.Close()

	get := func(qs string) (int, *antrepo.Heatmap) {
		t.Helper()
		res, err := http.Get(srv.URL + "/api/v1/analytics/heatmap" + qs)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body antrepo.Heatmap
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, &body
	}

	if code, _ := get("?from=2026-10-09&to=2026-10-01"); code != http.StatusBadRequest {
		t.Fatalf("from > to -> %d, want 400", code)
	}
	if code, _ := get("?from=2025-01-01&to=2026-09-30"); code != http.StatusBadRequest {
		t.Fatalf("span > 366 days -> %d, want 400", code)
	}
	if code, _ := get("?from=2026-13-01&to=2026-13-02"); code != http.StatusBadRequest {
		t.Fatalf("malformed date -> %d, want 400", code)
	}
	if code, _ := get("?from=20261001&to=20261002"); code != http.StatusBadRequest {
		t.Fatalf("non-YYYY-MM-DD date -> %d, want 400", code)
	}
	// 2028 is a leap year: a full year window is 366 inclusive days and allowed.
	if code, _ := get("?from=2028-01-01&to=2028-12-31"); code != http.StatusOK {
		t.Fatalf("full leap year -> %d, want 200", code)
	}

	code, def := get("")
	if code != http.StatusOK {
		t.Fatalf("default window -> %d, want 200", code)
	}
	if def.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want Asia/Shanghai", def.Timezone)
	}
	if def.Today == "" {
		t.Fatalf("today must be echoed so the frontend never guesses it")
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Now().In(loc).Format("2006-01-02"); def.Today != want {
		t.Fatalf("today = %q, want %q (user zone)", def.Today, want)
	}
	if def.To != def.Today {
		t.Fatalf("default to = %q, want today %q", def.To, def.Today)
	}
	f, err := day.Parse(def.From)
	if err != nil {
		t.Fatal(err)
	}
	tt, err := day.Parse(def.To)
	if err != nil {
		t.Fatal(err)
	}
	if days := int(tt.Sub(f).Hours()/24) + 1; days != 365 {
		t.Fatalf("default span = %d days, want 365 (53 week columns)", days)
	}

	// An open-ended window is accepted: one bound may be given on its own.
	if code, _ := get("?from=2026-10-01"); code != http.StatusOK {
		t.Fatalf("from-only -> %d, want 200", code)
	}
}
