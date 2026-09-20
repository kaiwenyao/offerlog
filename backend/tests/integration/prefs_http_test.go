// HTTP-level preferences round-trip coverage (review P1): the settings page
// PUTs remind_stale_days=0 (关闭) and must GET 0 back — the DTO layer is what
// the UI reads, so this exercises the actual transport (toDTO) rather than the
// repo row.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/identity/domain"
	idrepo "offerlog/backend/internal/identity/repository"
	idservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
	prefsrepo "offerlog/backend/internal/prefs"
	prefstransport "offerlog/backend/internal/prefs/transport"
)

// stubProfileWriter stands in for the identity service with no users row: it
// keeps the profile in memory and mirrors UpdateProfileTx's COALESCE semantics
// (nil = 这次不改，回读当前值).
type stubProfileWriter struct{ cur *idservice.UserRow }

func (s *stubProfileWriter) row(id int64) *idservice.UserRow {
	if s.cur == nil {
		s.cur = &idservice.UserRow{ID: id, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"}
	}
	return s.cur
}

func (s *stubProfileWriter) UpdateProfile(ctx context.Context, id int64, displayName, timezone string) (*idservice.UserRow, error) {
	return s.UpdateProfileTx(ctx, nil, id, &displayName, &timezone)
}

func (s *stubProfileWriter) UpdateProfileTx(ctx context.Context, q database.Querier, id int64, displayName, timezone *string) (*idservice.UserRow, error) {
	cur := s.row(id)
	if displayName != nil {
		cur.DisplayName = *displayName
	}
	if timezone != nil {
		cur.Timezone = *timezone
	}
	copied := *cur
	return &copied, nil
}

// newPrefsServer builds an authenticated /preferences surface over the shared
// DB with a fake session (owner_id from setup).
func newPrefsServer(t *testing.T, db *database.DB, owner int64) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Fake auth that injects the given owner for every request.
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &domain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	prefsh := prefstransport.NewWithProfile(prefsrepo.New(db), &stubProfileWriter{})
	prefsh.Routes(r.Group("/api/v1/preferences"))
	return httptest.NewServer(r)
}

func TestStaleDaysZeroHTTPRoundTrip(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newPrefsServer(t, db, owner)
	defer srv.Close()

	put := func(body string) map[string]any {
		t.Helper()
		req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/preferences", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			b := new(bytes.Buffer)
			_, _ = b.ReadFrom(res.Body)
			t.Fatalf("PUT %s -> %d %s", body, res.StatusCode, b.String())
		}
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return out
	}
	get := func() map[string]any {
		t.Helper()
		res, err := http.Get(srv.URL + "/api/v1/preferences")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return out
	}

	out := put(`{"remind_stale_days":0}`)
	if v, _ := out["remind_stale_days"].(float64); v != 0 {
		t.Fatalf("PUT response remind_stale_days = %v, want 0", out["remind_stale_days"])
	}
	got := get()
	if v, _ := got["remind_stale_days"].(float64); v != 0 {
		t.Fatalf("GET remind_stale_days = %v, want 0 (must not collapse to default 14)", got["remind_stale_days"])
	}
	// A normal value still round-trips.
	put(`{"remind_stale_days":7}`)
	got = get()
	if v, _ := got["remind_stale_days"].(float64); v != 7 {
		t.Fatalf("GET remind_stale_days = %v, want 7", got["remind_stale_days"])
	}
}

func TestStaleDaysRejectOutOfRange(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newPrefsServer(t, db, owner)
	defer srv.Close()
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/preferences", bytes.NewBufferString(`{"remind_stale_days":-1}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("stale_days=-1 status = %d, want 400", res.StatusCode)
	}
}

// Regression (review round 4, P1): the timezone picker is a preset Select, but
// a crafted PATCH/PUT with "Local" (which Go's time.LoadLocation happily
// resolves) must be REJECTED at the transport, and an empty value must be
// normalized to the app default instead of being stored blank (a blank zone
// makes home/calendar SQL AT TIME ZONE fail forever).
func TestTimezoneFieldRejectsLocalAndNormalizesEmpty(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newPrefsServer(t, db, owner)
	defer srv.Close()

	// "Local" → 400.
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/preferences", bytes.NewBufferString(`{"timezone":"Local"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("timezone=Local status = %d, want 400 (must never reach SQL AT TIME ZONE 'Local')", res.StatusCode)
	}

	// Empty string → stored as the app default, and home/calendar can still
	// load it (no permanent 500).
	req, _ = http.NewRequest("PUT", srv.URL+"/api/v1/preferences", bytes.NewBufferString(`{"timezone":""}`))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("timezone=empty status = %d, want 200 (normalized to default)", res.StatusCode)
	}
	var stored string
	if err := db.Pool().QueryRow(context.Background(), `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "" || stored == "Local" {
		t.Fatalf("empty timezone persisted verbatim: %q", stored)
	}
	if _, err := time.LoadLocation(stored); err != nil {
		t.Fatalf("stored zone %q does not load: %v", stored, err)
	}
}

// errInjected is returned by the failing writer's preferences-write phase.
var errInjected = errors.New("injected prefs write failure")

// failingPrefsWriter performs the REAL users-row UPDATE through the identity
// service (transaction-scoped), then reports failure for the preferences
// write. Because both writes share one transaction, an error on the second
// must roll back the first — a pre-fix two-step implementation (users update
// committed first, prefs upsert second) would leave the users row changed.
type failingPrefsWriter struct {
	svc *idservice.Store
	// failPrefs causes the preferences-write phase to return an error after the
	// users-row UPDATE inside the same transaction already succeeded.
	failPrefs bool
}

func (f *failingPrefsWriter) UpdateProfile(ctx context.Context, id int64, displayName, timezone string) (*idservice.UserRow, error) {
	return f.svc.UpdateProfile(ctx, id, displayName, timezone)
}

func (f *failingPrefsWriter) UpdateProfileTx(ctx context.Context, q database.Querier, id int64, displayName, timezone *string) (*idservice.UserRow, error) {
	// Phase 1: real users-row UPDATE on the tx.
	row, err := f.svc.UpdateProfileTx(ctx, q, id, displayName, timezone)
	if err != nil {
		return nil, err
	}
	// Phase 2: simulate the preferences upsert failing.
	if f.failPrefs {
		return row, errInjected
	}
	return row, nil
}

// Regression (round 5, P1): PUT /preferences persists the users profile row and
// the preferences row in ONE transaction. When the preferences write fails the
// users-row UPDATE (which ran first inside the same tx) must roll back — no
// half-saved settings.
func TestPreferencesPutIsAtomicOnSecondWriteFailure(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	// Persist a known-good users row timezone first.
	if _, err := db.Pool().Exec(ctx, `UPDATE users SET timezone='Europe/Dublin', display_name='' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}

	failing := &failingPrefsWriter{svc: idservice.New(db, idrepo.NewSQLUsers(db), 24, "test"), failPrefs: true}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &domain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	prefsh := prefstransport.NewWithProfile(prefsrepo.New(db), failing)
	prefsh.Routes(r.Group("/api/v1/preferences"))
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/preferences", bytes.NewBufferString(`{"timezone":"Asia/Shanghai","display_name":"原子"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d want 500 (preferences-write failure must surface)", res.StatusCode)
	}

	// The users-row UPDATE ran first in the tx but must have rolled back with
	// the failing preferences write: users row keeps its original values.
	var tz, name string
	if err := db.Pool().QueryRow(ctx, `SELECT timezone, display_name FROM users WHERE id=$1`, owner).Scan(&tz, &name); err != nil {
		t.Fatalf("users row missing: %v", err)
	}
	if tz != "Europe/Dublin" || name != "" {
		t.Fatalf("half-state committed: users.timezone=%q display_name=%q; want Europe/Dublin/'' (users-row write must roll back)", tz, name)
	}
	// And no preferences row may exist either.
	var prefsCount int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM user_preferences WHERE user_id=$1`, owner).Scan(&prefsCount); err != nil {
		t.Fatal(err)
	}
	if prefsCount != 0 {
		t.Fatalf("user_preferences rows = %d, want 0 (whole save must roll back)", prefsCount)
	}
}

func TestPreferencesConcurrentPartialPutsKeepBothFields(t *testing.T) {
	db, _, _, owner := setup(t)
	srv := newPrefsServer(t, db, owner)
	defer srv.Close()

	put := func(body string) error {
		req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/preferences", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("PUT %s -> %d", body, res.StatusCode)
		}
		return nil
	}
	// Seed a row so both PUTs are updates, not first-inserts.
	if err := put(`{"remind_overdue":true,"remind_interview":true}`); err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 2)
	go func() { errCh <- put(`{"remind_overdue":false}`) }()
	go func() { errCh <- put(`{"remind_interview":false}`) }()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}

	res, err := http.Get(srv.URL + "/api/v1/preferences")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out["remind_overdue"] != false || out["remind_interview"] != false {
		t.Fatalf("after concurrent PUTs prefs = %#v, want both reminder switches off", out)
	}
}

// Regression: PUT /preferences 只带提醒开关时，不能把 users.display_name 写回
// 会话快照里的旧名字。
//
// 原始 bug：处理器把 p.DisplayName 无条件塞成 user.DisplayName——那是进事务之前
// 的会话快照。设置页上「保存显示名称」和「翻一个提醒开关」是两个独立的 PUT：后发
// 的那个手上的 user 还是改名前加载的，于是它把旧名字又写回 users 行，刚保存的
// 改名被静默回滚了。修复后这两个字段只在请求真的带了的时候才写（COALESCE 保留）。
func TestReminderOnlyPutKeepsConcurrentRename(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	// 前一个 PUT 已经提交的改名。
	if _, err := db.Pool().Exec(ctx,
		`UPDATE users SET display_name='新名字', timezone='Europe/Dublin' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}

	// 后一个 PUT 的会话快照是改名之前的：display_name 还是旧的。
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &domain.User{ID: owner, Email: "x@y.z", DisplayName: "旧名字",
			Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	prefsh := prefstransport.NewWithProfile(prefsrepo.New(db),
		idservice.New(db, idrepo.NewSQLUsers(db), 24, "test"))
	prefsh.Routes(r.Group("/api/v1/preferences"))
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/preferences",
		bytes.NewBufferString(`{"remind_overdue":false}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d want 200", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	var name string
	if err := db.Pool().QueryRow(ctx, `SELECT display_name FROM users WHERE id=$1`, owner).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "新名字" {
		t.Fatalf("提醒开关的 PUT 把改名回滚了: users.display_name = %q, want 新名字", name)
	}
	// 响应体也要给出提交后的真实名字，否则侧栏会把旧名字显示回去。
	if body["display_name"] != "新名字" {
		t.Fatalf("响应体 display_name = %v, want 新名字", body["display_name"])
	}
	if body["remind_overdue"] != false {
		t.Fatalf("这次要改的字段没存上: remind_overdue = %v", body["remind_overdue"])
	}
}
