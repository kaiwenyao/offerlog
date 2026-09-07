// HTTP-level preferences round-trip coverage (review P1): the settings page
// PUTs remind_stale_days=0 (关闭) and must GET 0 back — the DTO layer is what
// the UI reads, so this exercises the actual transport (toDTO) rather than the
// repo row.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"offerlog/backend/internal/identity/domain"
	idservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
	prefsrepo "offerlog/backend/internal/prefs"
	prefstransport "offerlog/backend/internal/prefs/transport"
)

type stubProfileWriter struct{}

func (stubProfileWriter) UpdateProfile(ctx context.Context, id int64, displayName, timezone string) (*idservice.UserRow, error) {
	return &idservice.UserRow{ID: id, DisplayName: displayName, Timezone: timezone, Email: "x@y.z", Locale: "zh-CN"}, nil
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
	prefsh := prefstransport.NewWithProfile(prefsrepo.New(db), stubProfileWriter{})
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
