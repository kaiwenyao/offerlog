// CSRF-precision regression (review round 4, P1): the middleware used to
// exempt EVERY non-GET under /api/v1/auth/ — intending login/register/logout —
// which also exempted the authenticated profile mutation PATCH /auth/me. The
// exemption must be an exact allowlist of the three pre-session/teardown
// endpoints; /me is a session-bearing write and must require X-CSRF-Token.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	idrepo "offerlog/backend/internal/identity/repository"
	idservice "offerlog/backend/internal/identity/service"
	idtransport "offerlog/backend/internal/identity/transport"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
	"offerlog/backend/internal/platform/migrate"
)

func TestPatchMeRequiresCSRFToken(t *testing.T) {
	ctx := context.Background()
	db, err := database.New(ctx, dbURL(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	if err := migrate.Up(ctx, db.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := idservice.New(db, idrepo.NewSQLUsers(db), 1, "test-csrf")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpx.Auth(store))
	api := r.Group("/api/v1", httpx.CSRF(store))
	authH := idtransport.New(store, idtransport.Config{RegistrationOpen: true, DefaultTZ: "Europe/Dublin"})
	authH.Routes(api.Group("/auth"))
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Register to get a session + csrf token.
	regBody := `{"email":"csrf.` + time.Now().Format("150405.000000000") + `@test.local","password":"password123"}`
	res, err := http.Post(srv.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(regBody))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.NewDecoder(res.Body).Decode(&reg)
	res.Body.Close()
	session := ""
	for _, c := range res.Cookies() {
		if c.Name == httpx.SessionCookieName {
			session = c.Value
		}
	}

	// 1. PATCH /auth/me WITHOUT the CSRF header → 403.
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/v1/auth/me", bytes.NewBufferString(`{"display_name":"X"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: session})
	bad, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusForbidden {
		t.Fatalf("PATCH /me without CSRF = %d, want 403 (must not be exempt)", bad.StatusCode)
	}

	// 2. WITH the token → 200 and the name persisted.
	req, _ = http.NewRequest("PATCH", srv.URL+"/api/v1/auth/me", bytes.NewBufferString(`{"display_name":"改名"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", reg.CSRFToken)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: session})
	ok, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /me with CSRF = %d, want 200", ok.StatusCode)
	}
}

func TestGetMeReissuesCSRFToken(t *testing.T) {
	ctx := context.Background()
	db, err := database.New(ctx, dbURL(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)
	if err := migrate.Up(ctx, db.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := idservice.New(db, idrepo.NewSQLUsers(db), 1, "test-csrf")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(httpx.Auth(store))
	api := r.Group("/api/v1", httpx.CSRF(store))
	authH := idtransport.New(store, idtransport.Config{RegistrationOpen: true, DefaultTZ: "Europe/Dublin"})
	authH.Routes(api.Group("/auth"))
	srv := httptest.NewServer(r)
	defer srv.Close()

	regBody := `{"email":"csrf.me.` + time.Now().Format("150405.000000000") + `@test.local","password":"password123"}`
	res, err := http.Post(srv.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(regBody))
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.NewDecoder(res.Body).Decode(&reg)
	res.Body.Close()
	if reg.CSRFToken == "" {
		t.Fatal("register did not return csrf_token")
	}
	session := ""
	for _, c := range res.Cookies() {
		if c.Name == httpx.SessionCookieName {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("register did not set session cookie")
	}

	// Simulate localStorage wipe: session cookie still present, no CSRF header
	// and no remembered token. GET /me must re-issue the same csrf_token.
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: session})
	meRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		CSRFToken string `json:"csrf_token"`
		Email     string `json:"email"`
	}
	_ = json.NewDecoder(meRes.Body).Decode(&me)
	meRes.Body.Close()
	if meRes.StatusCode != http.StatusOK {
		t.Fatalf("GET /me = %d, want 200", meRes.StatusCode)
	}
	if me.CSRFToken == "" {
		t.Fatal("GET /me did not re-issue csrf_token — SPA would sit logged-in with every write 403")
	}
	if me.CSRFToken != reg.CSRFToken {
		t.Fatalf("GET /me csrf_token = %q, want the session token %q", me.CSRFToken, reg.CSRFToken)
	}

	// The re-issued token must actually authorize a write.
	req, _ = http.NewRequest("PATCH", srv.URL+"/api/v1/auth/me", bytes.NewBufferString(`{"display_name":"补发"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", me.CSRFToken)
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookieName, Value: session})
	ok, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /me with re-issued CSRF = %d, want 200", ok.StatusCode)
	}
}
