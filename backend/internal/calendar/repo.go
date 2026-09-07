// Package calendar backs the interview/agenda calendar (plan §5.5): a date
// range of cross-application events — interviews (non-cancelled), action due
// dates, application deadlines and (where present) offer decision deadlines.
// Every row carries the original timezone alongside the UTC instant so the UI
// can show "原始时区 / 用户时区" distinctly, and DST/cross-midnight events
// round-trip correctly because the backend stores instants and the original
// zone label.
package calendar

import (
	"context"
	"time"

	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/timeutil"
)

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

// Event is one calendar row. Kind: interview | action | deadline | offer_decision.
type Event struct {
	ID            int64      `json:"id"`
	Kind          string     `json:"kind"`
	ApplicationID int64      `json:"application_id"`
	CompanyName   string     `json:"company_name"`
	Position      string     `json:"position"`
	Title         string     `json:"title"`
	Start         *time.Time `json:"start"` // instant
	Timezone      string     `json:"timezone"`
	AllDay        bool       `json:"all_day"`
	Location      string     `json:"location"`
	MeetingURL    string     `json:"meeting_url"`
	Cancelled     bool       `json:"cancelled"`
	Done          bool       `json:"done"`
	RoundName     string     `json:"round_name"`
	Format        string     `json:"format"`
}

// Range returns every calendar event whose window intersects [from, to)
// (half-open, UTC). Interviews/offer decisions are point events at their
// scheduled instant; actions/deadlines are all-day (date) events converted to
// the user's local midnight and compared in the half-open window.
func (r *Repo) Range(ctx context.Context, ownerID int64, tz string, from, to time.Time) ([]Event, error) {
	loc, tzSafe := timeutil.SafeLocation(tz)
	var out []Event

	// Interviews (non-cancelled) scheduled in the window.
	rows, err := r.db.Pool().Query(ctx, `SELECT i.id, a.id, a.company_name, a.position,
		i.round_name, i.format, i.scheduled_at, i.timezone, COALESCE(i.duration_minutes, 0),
		COALESCE(sl.location,''), COALESCE(sl.meeting_url,''), COALESCE(sl.cancelled,FALSE), COALESCE(i.result,'')
		FROM interviews i
		JOIN applications a ON a.id=i.application_id AND a.owner_id=i.owner_id
		LEFT JOIN schedule_links sl ON sl.interview_id=i.id AND sl.owner_id=i.owner_id
		WHERE i.owner_id=$1 AND i.scheduled_at IS NOT NULL
		  AND COALESCE(sl.cancelled,FALSE) = FALSE
		  AND i.scheduled_at >= $2 AND i.scheduled_at < $3
		  AND a.deleted_at IS NULL AND a.archived_at IS NULL
		ORDER BY i.scheduled_at`, ownerID, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e Event
		var appID int64
		var fmt, result string
		var minutes int
		if err := rows.Scan(&e.ID, &appID, &e.CompanyName, &e.Position, &e.RoundName, &fmt,
			&e.Start, &e.Timezone, &minutes, &e.Location, &e.MeetingURL, &e.Cancelled, &result); err != nil {
			rows.Close()
			return nil, err
		}
		e.ApplicationID = appID
		e.Kind = "interview"
		e.Title = e.CompanyName + " · " + e.RoundName
		e.Format = fmt
		e.AllDay = false
		e.Done = result == "passed" || result == "failed"
		if e.Timezone == "" {
			e.Timezone = tzSafe
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Action due dates (open) with date/ts in the window. due_date is the
	// user's calendar day — its instant is the user's *local* midnight, so the
	// single conversion `due_date::timestamp AT TIME ZONE $tz` (naive day →
	// user-zone instant) is correct; `::timestamptz AT TIME ZONE` would double-
	// shift it through the session zone and drop the first window day for
	// west-of-UTC users.
	arows, err := r.db.Pool().Query(ctx, `SELECT x.id, x.application_id, ap.company_name, ap.position,
		x.title, x.due_date, x.due_ts, (x.done_at IS NOT NULL), x.priority
		FROM actions x JOIN applications ap ON ap.id=x.application_id AND ap.owner_id=x.owner_id
		WHERE x.owner_id=$1 AND ap.deleted_at IS NULL AND ap.archived_at IS NULL
		  AND ( (x.due_ts IS NOT NULL AND x.due_ts >= $2 AND x.due_ts < $3)
		     OR (x.due_ts IS NULL AND x.due_date IS NOT NULL
		         AND (x.due_date::timestamp AT TIME ZONE $4) >= $2 AND (x.due_date::timestamp AT TIME ZONE $4) < $3) )
		ORDER BY COALESCE(x.due_ts, x.due_date::timestamp AT TIME ZONE $4)`, ownerID, from, to, tzSafe)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var e Event
		var appID int64
		var dueDate, dueTs *time.Time
		var done bool
		var prio string
		if err := arows.Scan(&e.ID, &appID, &e.CompanyName, &e.Position, &e.Title, &dueDate, &dueTs, &done, &prio); err != nil {
			return nil, err
		}
		e.ApplicationID = appID
		e.Kind = "action"
		e.Done = done
		e.AllDay = dueTs == nil
		e.Timezone = tzSafe
		// Start: use the precise instant when present, else local-midnight of
		// the date column so the UI buckets it on the right day.
		if dueTs != nil {
			e.Start = dueTs
		} else if dueDate != nil {
			// pgx decodes the DATE at UTC midnight; the calendar day is the UTC
			// date part (session is pinned UTC), regardless of any user zone.
			// The event's instant is that day at the *user's* local midnight.
			yy, mm, dd := dueDate.UTC().Date()
			mid := time.Date(yy, mm, dd, 0, 0, 0, 0, loc)
			e.Start = &mid
		}
		if e.CompanyName != "" {
			e.Title = e.CompanyName + " · " + e.Title
		}
		out = append(out, e)
	}
	if err := arows.Err(); err != nil {
		return nil, err
	}

	// Application deadlines (date) and offer-decision windows use the
	// application's own deadline/status columns. deadline is a calendar day;
	// its instant is the user's local midnight (single AT TIME ZONE).
	drows, err := r.db.Pool().Query(ctx, `SELECT a.id, a.company_name, a.position, a.deadline, a.status
		FROM applications a
		WHERE a.owner_id=$1 AND a.deleted_at IS NULL AND a.archived_at IS NULL AND a.deadline IS NOT NULL
		  AND (a.deadline::timestamp AT TIME ZONE $2) >= $3 AND (a.deadline::timestamp AT TIME ZONE $2) < $4
		ORDER BY a.deadline`, ownerID, tzSafe, from, to)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var e Event
		var deadline *time.Time
		var status string
		if err := drows.Scan(&e.ID, &e.CompanyName, &e.Position, &deadline, &status); err != nil {
			return nil, err
		}
		e.ApplicationID = e.ID
		e.Kind = "deadline"
		e.Title = e.CompanyName + " · 截止"
		e.AllDay = true
		e.Timezone = tzSafe
		if deadline != nil {
			// date-only application deadline → user-local midnight of that day.
			yy, mm, dd := deadline.UTC().Date()
			mid := time.Date(yy, mm, dd, 0, 0, 0, 0, loc)
			e.Start = &mid
		}
		e.Done = status == "accepted" || status == "rejected" || status == "withdrawn" || status == "closed"
		out = append(out, e)
	}
	if err := drows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
