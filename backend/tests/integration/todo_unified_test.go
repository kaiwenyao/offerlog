// Unified-todo consistency regression (review P1): summary.todo_items is the
// single source for both the sidebar badge (todos.open) and the rendered
// checklist. A legacy derived next_action (no standalone action) and a real
// standalone action must both appear exactly once, and todos.open must equal
// len(todo_items) so the count can never disagree with the list.
package integration

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/home"
)

func TestHomeTodoBadgeEqualsUnifiedList(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	var tz string
	_ = db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz)
	now := time.Now()

	// App A: legacy next_action with no standalone action → derived row.
	appA := mustCreate(t, svc, owner, "DerivedCo", "Role")
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET next_action='写跟进邮件', next_action_due_at=CURRENT_DATE+2 WHERE id=$1`, appA.ID); err != nil {
		t.Fatal(err)
	}
	// App B: a standalone open action → action row.
	appB := mustCreate(t, svc, owner, "ActionCo", "Role")
	if _, err := db.Pool().Exec(ctx, `INSERT INTO actions(application_id, owner_id, title, due_ts, done_at, remind_me, priority, source)
		VALUES($1,$2,'约面试时间', now() + interval '1 day', NULL, FALSE, 'high','manual')`, appB.ID, owner); err != nil {
		t.Fatal(err)
	}

	repo := home.New(db)
	s, err := repo.Get(ctx, owner, tz, time.Monday, now, 5)
	if err != nil {
		t.Fatal(err)
	}
	if s.Todos.Open != int64(len(s.TodoItems)) {
		t.Fatalf("todos.open=%d but todo_items=%d (badge must equal the list)", s.Todos.Open, len(s.TodoItems))
	}
	if s.Todos.Open != 2 {
		t.Fatalf("open = %d, want 2 (one derived + one action)", s.Todos.Open)
	}
	// Exactly one derived (action_id nil) and one standalone.
	derived, actions := 0, 0
	for _, it := range s.TodoItems {
		if it.ActionID == nil {
			derived++
			if it.CompanyName != "DerivedCo" || it.Title != "写跟进邮件" {
				t.Fatalf("derived item wrong: %+v", it)
			}
		} else {
			actions++
			if it.CompanyName != "ActionCo" {
				t.Fatalf("action item wrong: %+v", it)
			}
		}
	}
	if derived != 1 || actions != 1 {
		t.Fatalf("derived=%d actions=%d, want 1/1", derived, actions)
	}
}

// Date-only derived rows must surface due_day as YYYY-MM-DD (never a
// UTC-midnight timestamp) so the client renders the exact calendar day.
func TestHomeTodoDueDayIsDateOnlyString(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	var tz string
	_ = db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz)
	app := mustCreate(t, svc, owner, "DueCo", "Role")
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET next_action='准备材料', next_action_due_at='2026-09-10'::date WHERE id=$1`, app.ID); err != nil {
		t.Fatal(err)
	}
	repo := home.New(db)
	s, err := repo.Get(ctx, owner, tz, time.Monday, time.Now(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.TodoItems) != 1 {
		t.Fatalf("items=%d want 1", len(s.TodoItems))
	}
	it := s.TodoItems[0]
	if it.DueDay == nil || *it.DueDay != "2026-09-10" {
		t.Fatalf("due_day = %v, want '2026-09-10' (date-only string)", it.DueDay)
	}
	if it.DueTs != nil {
		t.Fatalf("derived date-only row must not expose due_ts: %+v", it.DueTs)
	}
}
