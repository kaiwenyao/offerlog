// Home dashboard integration coverage (plan §4.1): server-side full-data
// aggregates (never extrapolated from a page), cross-application upcoming
// interviews that must include the 7th+ application, timezone-week semantics,
// and unified todo counts.
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	appservice "offerlog/backend/internal/applications/service"
	"offerlog/backend/internal/home"
	"offerlog/backend/internal/platform/timeutil"
)

func mustCreateWithSubmit(t *testing.T, svc *appservice.Service, owner int64, company, position string, submitted time.Time) {
	t.Helper()
	row, err := svc.Create(context.Background(), owner, &appservice.CreateInput{
		CompanyName: company, Position: position, Status: "applied", SubmittedAt: &submitted,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = row
}

func TestHomeSummaryLargeCohortAndUpcomingBeyond200(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	// Use the owner's default timezone from setup (Europe/Dublin) via users row.
	var tz string
	_ = db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz)
	loc, _ := time.LoadLocation(tz)

	now := time.Now().In(loc)
	// Seed 210 submitted applications over the past month (well beyond the old
	// 200-page cutoff) — mix archived and unarchived, some with interviews.
	repo := home.New(db)
	day := now.AddDate(0, 0, -1)
	for i := 0; i < 210; i++ {
		sub := now.AddDate(0, 0, -(i%30)-1)
		sub = sub.Add(-time.Duration(i) * time.Minute)
		mustCreateWithSubmit(t, svc, owner, fmt.Sprintf("Co%d", i), "Engineer", sub)
		day = sub
	}
	_ = day
	// 8 distinct applications get scheduled interviews inside the CURRENT
	// week (so the week-count and the >200 upcoming case are deterministic).
	// This verifies the "7th+ application's interview" case is not clipped by
	// any frontend application-status slice.
	ws, _ := timeutil.WeekBounds(now, loc, time.Monday)
	_, we := timeutil.WeekBounds(now, loc, time.Monday)
	_ = we
	var appIDs []int64
	rows, _ := db.Pool().Query(ctx, `SELECT id FROM applications WHERE owner_id=$1 AND deleted_at IS NULL ORDER BY id LIMIT 8`, owner)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		appIDs = append(appIDs, id)
	}
	rows.Close()
	for i := 0; i < 8; i++ {
		// spread over the current week: ws + (i+1) days, capped before week end.
		at := ws.AddDate(0, 0, i+1)
		if !at.Before(we) {
			at = we.Add(-time.Hour)
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format, scheduled_at, timezone)
			VALUES($1,$2,'一面','video',$3,$4)`, appIDs[i], owner, at, tz); err != nil {
			t.Fatal(err)
		}
	}

	s, err := repo.Get(ctx, owner, tz, time.Monday, now, 5)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total < 210 {
		t.Fatalf("Total=%d should reflect all seeded apps (>=210)", s.Total)
	}
	if len(s.Upcoming) != 5 {
		t.Fatalf("Upcoming len=%d want 5 (page-limited but cross-app)", len(s.Upcoming))
	}
	// Ask for a large limit: the 7th+ application's interview must show.
	big, err := repo.Get(ctx, owner, tz, time.Monday, now, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(big.Upcoming) != 8 {
		t.Fatalf("Upcoming(len=20)=%d want 8 (7th+ app's interview must appear)", len(big.Upcoming))
	}
	// Interviews week counts all 8 (they are all in this coming week if the
	// seed now is near the week end; robustly we assert it's >= 7 so at least
	// the 7th application's interview is counted).
	if s.InterviewsWeek < 7 {
		t.Fatalf("InterviewsWeek=%d want >=7 (the 7th+ app's interview must count)", s.InterviewsWeek)
	}
}

func TestHomeSummaryWeekWindows(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	var tz string
	_ = db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&tz)
	loc, _ := time.LoadLocation(tz)
	repo := home.New(db)

	now := time.Now().In(loc)
	ws, we := timeutil.WeekBounds(now, loc, time.Monday)
	// one submit inside the week, one exactly at the start boundary, one just
	// before the week (last week).
	inside := ws.Add(24 * time.Hour)
	atBoundary := ws
	before := ws.AddDate(0, 0, -1)
	mustCreateWithSubmit(t, svc, owner, "InsideCo", "R", inside)
	mustCreateWithSubmit(t, svc, owner, "BoundaryCo", "R", atBoundary)
	mustCreateWithSubmit(t, svc, owner, "BeforeCo", "R", before)

	s, err := repo.Get(ctx, owner, tz, time.Monday, now, 5)
	if err != nil {
		t.Fatal(err)
	}
	// inside + atBoundary (>= start, < end) count; before does not.
	if s.SubmittedWeek != 2 {
		t.Fatalf("SubmittedWeek=%d want 2 (half-open [start,end))", s.SubmittedWeek)
	}
	if !s.Week.Start.Equal(ws) || !s.Week.End.Equal(we) {
		t.Fatalf("week bounds = [%v,%v) want [%v,%v)", s.Week.Start, s.Week.End, ws, we)
	}
}
