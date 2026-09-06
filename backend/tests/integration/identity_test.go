// Integration coverage for the identity transport: public registration,
// the REGISTRATION_OPEN switch, and the public auth config endpoint. Runs
// against a real PostgreSQL (see dbURL in applications_test.go).
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// newAuthServer spins up an isolated auth endpoint surface with its own rate
// limiter budget and a store over the shared test database.
func newAuthServer(t *testing.T, registrationOpen bool) *httptest.Server {
	t.Helper()
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
	authH := idtransport.New(store, idtransport.Config{
		RegistrationOpen: registrationOpen,
		DefaultTZ:        "Europe/Dublin",
	})
	authH.Routes(r.Group("/api/v1/auth"))
	return httptest.NewServer(r)
}

type authRes struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	CSRFToken string `json:"csrf_token"`
}

func postJSON(t *testing.T, url string, body any) (*http.Response, authRes) {
	t.Helper()
	buf, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { res.Body.Close() })
	var parsed authRes
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return res, parsed
}

func TestRegisterCreatesSessionAndAllowsLogin(t *testing.T) {
	srv := newAuthServer(t, true)
	defer srv.Close()

	email := fmt.Sprintf("reg.%d@test.local", time.Now().UnixNano())
	res, body := postJSON(t, srv.URL+"/api/v1/auth/register",
		map[string]any{"email": email, "password": "password123", "display_name": "Tester"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want 201 (body: %+v)", res.StatusCode, body)
	}
	if body.Email != email || body.ID == 0 || body.CSRFToken == "" {
		t.Fatalf("register body incomplete: %+v", body)
	}
	var cookies []string
	for _, c := range res.Cookies() {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	if len(cookies) == 0 {
		t.Fatal("register did not set session cookie")
	}

	// Same credentials must log in afterwards.
	res, body = postJSON(t, srv.URL+"/api/v1/auth/login",
		map[string]any{"email": email, "password": "password123"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body: %+v)", res.StatusCode, body)
	}

	// Duplicate signup is rejected with a stable code.
	_, body = postJSON(t, srv.URL+"/api/v1/auth/register",
		map[string]any{"email": email, "password": "password123"})
	if body.Code != "email_taken" {
		t.Fatalf("duplicate register code = %q, want email_taken", body.Code)
	}
}

func TestRegisterValidation(t *testing.T) {
	srv := newAuthServer(t, true)
	defer srv.Close()

	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"weak password", map[string]any{"email": "a.1@test.local", "password": "short"}, "weak_password"},
		{"bad email", map[string]any{"email": "not-an-email", "password": "password123"}, "invalid_signup"},
		{"missing fields", map[string]any{"email": "", "password": ""}, "invalid_request"},
	}
	for _, tc := range cases {
		res, body := postJSON(t, srv.URL+"/api/v1/auth/register", tc.body)
		if res.StatusCode != http.StatusBadRequest || body.Code != tc.want {
			t.Fatalf("%s: status=%d code=%q, want 400/%s", tc.name, res.StatusCode, body.Code, tc.want)
		}
	}
}

func TestRegistrationSwitch(t *testing.T) {
	open := newAuthServer(t, true)
	defer open.Close()
	closed := newAuthServer(t, false)
	defer closed.Close()

	// GET /auth/config mirrors the switch for the login page.
	for srv, want := range map[*httptest.Server]bool{open: true, closed: false} {
		res, err := http.Get(srv.URL + "/api/v1/auth/config")
		if err != nil {
			t.Fatalf("config: %v", err)
		}
		var cfg struct {
			RegistrationOpen bool `json:"registration_open"`
		}
		err = json.NewDecoder(res.Body).Decode(&cfg)
		_ = res.Body.Close()
		if err != nil {
			t.Fatalf("decode config: %v", err)
		}
		if cfg.RegistrationOpen != want {
			t.Fatalf("config registration_open = %v, want %v", cfg.RegistrationOpen, want)
		}
	}

	// Closed instance rejects signup with 403 registration_closed.
	res, body := postJSON(t, closed.URL+"/api/v1/auth/register",
		map[string]any{"email": "closed@test.local", "password": "password123"})
	if res.StatusCode != http.StatusForbidden || body.Code != "registration_closed" {
		t.Fatalf("closed register status=%d code=%q, want 403/registration_closed", res.StatusCode, body.Code)
	}
}
