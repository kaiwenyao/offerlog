// Ownership regression (review round 4, P0 越权写): createInterview /
// createAction / createNote only validated that the target application EXISTS
// (FK) — never that it belongs to the caller — so an attacker could write rows
// against another user's application and (via the joined home open_actions
// CTE) read back its company/status. All three creates must now 404 on a
// foreign application id.
package integration

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	actrepo "offerlog/backend/internal/activities/repository"
	acttransport "offerlog/backend/internal/activities/transport"
	"offerlog/backend/internal/home"
	"offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
)

// newFullActivityServer mounts the app-scoped activities routes (the
// /applications/:id sub-router) for a given acting owner.
func newFullActivityServer(t *testing.T, db *database.DB, owner int64) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &domain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	h := acttransport.New(actrepo.New(db)).WithNotifications(notifications.New(db))
	api := r.Group("/api/v1")
	h.Routes(api.Group("/applications/:id"))
	return httptest.NewServer(r)
}

func createOnForeignApp(t *testing.T, srv *httptest.Server, appID int64, path, body string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/applications/"+itoa(appID)+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}

func TestCreatesRejectForeignApplication(t *testing.T) {
	ctx := context.Background()
	db, svc, _, ownerA := setup(t)
	ownerB := createOwner(t, db)

	// A owns an application; B must not write interviews/actions/notes to it.
	appA := mustCreate(t, svc, ownerA, "VictimCo", "SecretRole")

	srvB := newFullActivityServer(t, db, ownerB)
	defer srvB.Close()

	cases := []struct {
		path string
		body string
	}{
		{"/interviews", `{"round_name":"一面","format":"video"}`},
		{"/actions", `{"title":"窥探"}`},
		{"/notes", `{"content_md":"偷写"}`},
	}
	for _, tc := range cases {
		status := createOnForeignApp(t, srvB, appA.ID, tc.path, tc.body)
		if status != http.StatusNotFound {
			t.Fatalf("B create%s on A's app -> %d, want 404", tc.path, status)
		}
	}

	// No rows may have been inserted.
	for _, table := range []string{"interviews", "actions", "notes"} {
		var n int
		if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE owner_id=$1`, ownerB).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("foreign create leaked into %s: %d rows owned by B", table, n)
		}
	}

	// The legitimate owner still creates fine (guard must not be a blanket 404).
	srvA := newFullActivityServer(t, db, ownerA)
	defer srvA.Close()
	if status := createOnForeignApp(t, srvA, appA.ID, "/interviews", `{"round_name":"一面","format":"video"}`); status != http.StatusCreated {
		t.Fatalf("owner A create interview -> %d, want 201", status)
	}
	if status := createOnForeignApp(t, srvA, appA.ID, "/actions", `{"title":"跟进"}`); status != http.StatusCreated {
		t.Fatalf("owner A create action -> %d, want 201", status)
	}
	if status := createOnForeignApp(t, srvA, appA.ID, "/notes", `{"content_md":"记录"}`); status != http.StatusCreated {
		t.Fatalf("owner A create note -> %d, want 201", status)
	}
	// A nonexistent application id is also a 404, not a 500.
	if status := createOnForeignApp(t, srvA, 999999999, "/interviews", `{"round_name":"x"}`); status != http.StatusNotFound {
		t.Fatalf("create on nonexistent app -> %d, want 404", status)
	}
}

// Regression (review round 4, P1): B attaching an open action to A's app used
// to surface A's company/status inside B's /home/summary todo list via the
// open_actions CTE join (which lacked the ap.owner_id = a.owner_id predicate).
// The home repo query must not leak the victim's row into the attacker's
// dashboard.
func TestForeignOpenActionDoesNotLeakIntoHomeTodos(t *testing.T) {
	ctx := context.Background()
	db, svc, _, ownerA := setup(t)
	ownerB := createOwner(t, db)

	appA := mustCreate(t, svc, ownerA, "VictimCo", "SecretRole")
	// Simulate the legacy attack row directly at the SQL level (a row created
	// through the pre-fix API, or by a direct INSERT): B owns an open action
	// whose application_id points at A's application.
	if _, err := db.Pool().Exec(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, priority, source)
		VALUES($1,$2,'越权遗留行', CURRENT_DATE - 1, 'high','manual')`, appA.ID, ownerB); err != nil {
		t.Fatal(err)
	}

	repo := home.New(db)
	s, err := repo.Get(ctx, ownerB, "Europe/Dublin", time.Monday, time.Now(), 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range s.TodoItems {
		if it.CompanyName == "VictimCo" || it.Position == "SecretRole" {
			t.Fatalf("B's home todo leaked A's application data: %+v", it)
		}
	}
	// The attacker's own row must not count toward the todo badge either
	// (it has no legitimate application to display).
	if s.Todos.Open != 0 {
		t.Fatalf("B todos.open = %d, want 0 (the foreign action is not B's application's todo)", s.Todos.Open)
	}
}
