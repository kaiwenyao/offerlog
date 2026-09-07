// Transport-level regression (review round 2): postpone with a blank
// due_date must clear the action's due date — never store year-1 — and must
// not resurrect an overdue reminder for a cleared date.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"offerlog/backend/internal/reminders"

	"github.com/gin-gonic/gin"

	actrepo "offerlog/backend/internal/activities/repository"
	acttransport "offerlog/backend/internal/activities/transport"
	"offerlog/backend/internal/identity/domain"
	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/httpx"
)

func newActivityServer(t *testing.T, db *database.DB, owner int64) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetUser(c, &domain.User{ID: owner, Email: "x@y.z", Timezone: "Europe/Dublin", Locale: "zh-CN"})
		c.Next()
	})
	h := acttransport.New(actrepo.New(db)).WithNotifications(notifications.New(db))
	api := r.Group("/api/v1")
	// standalone actions root (postpone lives here)
	h.ActionsRoot(api.Group("/actions"))
	return httptest.NewServer(r)
}

func TestPostponeBlankDateClearsDue(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "PostBlank", "Role")
	var actionID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		VALUES($1,$2,'任务', CURRENT_DATE - 1, NULL, FALSE, 'medium','manual') RETURNING id`, app.ID, owner).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	srv := newActivityServer(t, db, owner)
	defer srv.Close()

	body := `{"due_date":"  "}` // blank date-only value
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/actions/"+itoa(actionID)+"/postpone", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(res.Body)
		t.Fatalf("postpone blank status=%d body=%s", res.StatusCode, b.String())
	}
	var out struct {
		DueDate *string `json:"due_date"`
		DueTs   *string `json:"due_ts"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.DueDate != nil || out.DueTs != nil {
		t.Fatalf("blank postpone must clear both dates; got due_date=%v due_ts=%v", out.DueDate, out.DueTs)
	}
	// DB must hold NULL, not 0001-01-01.
	var day *string
	if err := db.Pool().QueryRow(ctx, `SELECT to_char(due_date,'YYYY-MM-DD') FROM actions WHERE id=$1`, actionID).Scan(&day); err != nil {
		t.Fatal(err)
	}
	if day != nil {
		t.Fatalf("due_date = %q, want NULL after blank postpone", *day)
	}
}

func itoa(v int64) string {
	b := []byte{}
	if v == 0 {
		return "0"
	}
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// Regression (round 2): deleting an action via the API must also clear its
// overdue reminder (done/postpone/update did; delete was missed).
func TestDeleteActionClearsOverdueReminder(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	app := mustCreate(t, svc, owner, "DelCo", "Role")
	var actionID int64
	if err := db.Pool().QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, done_at, remind_me, priority, source)
		VALUES($1,$2,'跟进', CURRENT_DATE - 2, NULL, FALSE, 'medium','manual') RETURNING id`, app.ID, owner).Scan(&actionID); err != nil {
		t.Fatal(err)
	}
	// Generate the overdue reminder.
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	var before int
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='overdue' AND dismissed_at IS NULL`, owner).Scan(&before)
	if before == 0 {
		t.Fatal("expected an open overdue reminder before delete")
	}
	srv := newActivityServer(t, db, owner)
	defer srv.Close()
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/v1/actions/"+itoa(actionID), nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d", res.StatusCode)
	}
	var after int
	_ = db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE owner_id=$1 AND kind='overdue' AND dismissed_at IS NULL`, owner).Scan(&after)
	if after != 0 {
		t.Fatalf("overdue reminders open before=%d after=%d — delete must clear the deleted action's reminder", before, after)
	}
	_ = svc
	_ = actionID
}
