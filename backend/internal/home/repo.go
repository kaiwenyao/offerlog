// Package home backs the today dashboard with server-side aggregates. Every
// count is computed over the full data set in SQL — never extrapolated from a
// paginated list — and week windows are half-open [start, end) resolved in the
// user's configured timezone (IANA, DST-safe), with a bounded end so a timezone
// with an extreme offset cannot produce an open-ended "this week".
//
// Scope notes (plan §4.1): the dashboard's working list and KPI set default to
// excluding archived rows (archived means "kept for the record, out of the
// active workflow"); analytics keeps archived and explains the difference.
// 本周投递 = submitted_at within the week window; 本周回复 = first_response_at
// within the window (the first *valid* reply by construction); 本周面试 = the
// non-cancelled interview rounds scheduled inside the window; completed rounds
// are reported under a separate label (interviews_done) so a count of scheduled
// interviews is never confused with a completion count.
package home

import (
	"context"
	"time"

	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/timeutil"
)

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Pool() *database.DB { return r.db }

// UpcomingInterview is one row of the cross-application upcoming calendar.
type UpcomingInterview struct {
	ID            int64     `json:"id"`
	ApplicationID int64     `json:"application_id"`
	CompanyName   string    `json:"company_name"`
	Position      string    `json:"position"`
	RoundName     string    `json:"round_name"`
	Format        string    `json:"format"`
	ScheduledAt   time.Time `json:"scheduled_at"`
	Timezone      string    `json:"timezone"`
	DurationMin   *int      `json:"duration_minutes"`
	Location      string    `json:"location"`
	MeetingURL    string    `json:"meeting_url"`
	Cancelled     bool      `json:"cancelled"`
	Result        string    `json:"result"`
}

// RecentApplication is a light row for the "最近动态" feed.
type RecentApplication struct {
	ID          int64     `json:"id"`
	CompanyName string    `json:"company_name"`
	Position    string    `json:"position"`
	Status      string    `json:"status"`
	NextAction  string    `json:"next_action"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TodoCounts are the unified open-action aggregates (source of truth for the
// dashboard number and the checklist itself).
type TodoCounts struct {
	Open     int64 `json:"open"`
	Overdue  int64 `json:"overdue"`
	DueToday int64 `json:"due_today"`
}

// Week is the user-local half-open week window used by the response.
type Week struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// WeekItem is one chip in the current-week strip (kind=投递/回复/面试/截止/待办;
// who = company/round label; tone = the strip color family).
type WeekItem struct {
	Day  int    `json:"day"` // 0=Mon .. 6=Sun (within the week window)
	Kind string `json:"kind"`
	Who  string `json:"who"`
	Tone string `json:"tone"` // info | warn | good | acc | bad
}

// Summary is the full dashboard payload.
type Summary struct {
	Total              int64               `json:"total"`       // active (non-deleted) applications, incl archived
	Active             int64               `json:"active"`      // non-deleted + non-archived (working set)
	ToApply            int64               `json:"to_apply"`    // saved/preparing (active)
	InProgress         int64               `json:"in_progress"` // applied…interviewing (active)
	WithResult         int64               `json:"with_result"` // offer+accepted+rejected+withdrawn+closed (active)
	Archived           int64               `json:"archived"`
	SubmittedWeek      int64               `json:"submitted_week"`       // 本周投递 (active)
	RepliedWeek        int64               `json:"replied_week"`         // 本周首次有效回复 (active)
	AwaitingReply      int64               `json:"awaiting_reply"`       // 已投递且尚无首次回复 (active, in-progress)
	InterviewsWeek     int64               `json:"interviews_week"`      // 本周安排的非取消面试轮次 (active)
	InterviewsDoneWeek int64               `json:"interviews_done_week"` // 本周已完成轮次 (独立标签)
	Todos              TodoCounts          `json:"todos"`
	Week               Week                `json:"week"`
	WeekItems          []WeekItem          `json:"week_items"` // chips for the Mon-Sun strip (server-side)
	Upcoming           []UpcomingInterview `json:"upcoming"` // cross-app, actual-time sorted, next N
	Recent             []RecentApplication `json:"recent"`
	AsOf               time.Time           `json:"as_of"`
	Timezone           string              `json:"timezone"`
	ScopeNote          string              `json:"scope_note"`
}

// Get computes the dashboard summary. Active set = deleted_at IS NULL AND
// archived_at IS NULL; archived rows count into Total/Archived only.
func (r *Repo) Get(ctx context.Context, ownerID int64, tz string, now time.Time, upcomingLimit int) (*Summary, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	weekStart, weekEnd := timeutil.WeekBounds(now, loc, time.Monday)
	dayStart, _ := timeutil.TodayBounds(now, loc)
	if upcomingLimit <= 0 {
		upcomingLimit = 5
	}

	s := &Summary{Timezone: tz, AsOf: now, Week: Week{Start: weekStart, End: weekEnd}}
	s.ScopeNote = "工作清单与周统计排除已归档；归档记录计入总数与归档数。周 = 周一开始的半开区间（用户时区）。本周面试 = 本周安排的非取消轮次；已完成轮次单独标注。"

	q := r.db.Pool()

	// Lifecycle buckets over the working set (exclude archived + deleted).
	if err := q.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status IN ('saved','preparing')),
		count(*) FILTER (WHERE status IN ('applied','screening','assessment','interviewing')),
		count(*) FILTER (WHERE status IN ('offer','accepted','rejected','withdrawn','closed')),
		count(*)
		FROM applications WHERE owner_id=$1 AND deleted_at IS NULL AND archived_at IS NULL`,
		ownerID).Scan(&s.ToApply, &s.InProgress, &s.WithResult, &s.Active); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM applications WHERE owner_id=$1 AND deleted_at IS NULL`,
		ownerID).Scan(&s.Total); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM applications WHERE owner_id=$1 AND deleted_at IS NULL AND archived_at IS NOT NULL`,
		ownerID).Scan(&s.Archived); err != nil {
		return nil, err
	}

	// 本周投递 / 本周首次有效回复 + 待回复存量 (working set).
	if err := q.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE submitted_at >= $2 AND submitted_at < $3),
		count(*) FILTER (WHERE first_response_at >= $2 AND first_response_at < $3),
		count(*) FILTER (WHERE submitted_at IS NOT NULL AND first_response_at IS NULL
		                 AND status IN ('applied','screening','assessment','interviewing'))
		FROM applications WHERE owner_id=$1 AND deleted_at IS NULL AND archived_at IS NULL`,
		ownerID, weekStart, weekEnd).Scan(&s.SubmittedWeek, &s.RepliedWeek, &s.AwaitingReply); err != nil {
		return nil, err
	}

	// 本周面试: non-cancelled interview rounds scheduled in [weekStart, weekEnd),
	// over applications that are active (not archived / not deleted). Completed
	// rounds (result passed/failed) counted separately.
	if err := q.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE COALESCE(sl.cancelled, FALSE) = FALSE),
		count(*) FILTER (WHERE COALESCE(sl.cancelled, FALSE) = FALSE AND i.result IN ('passed','failed'))
		FROM interviews i
		JOIN applications a ON a.id = i.application_id AND a.owner_id = i.owner_id
		LEFT JOIN schedule_links sl ON sl.interview_id = i.id AND sl.owner_id = i.owner_id
		WHERE i.owner_id=$1 AND i.scheduled_at IS NOT NULL
		  AND i.scheduled_at >= $2 AND i.scheduled_at < $3
		  AND a.deleted_at IS NULL AND a.archived_at IS NULL`,
		ownerID, weekStart, weekEnd).Scan(&s.InterviewsWeek, &s.InterviewsDoneWeek); err != nil {
		return nil, err
	}

	// Week strip chips: one row per event-day for submitted_at (投递),
	// first_response_at (回复), scheduled interviews (面试), action due dates
	// (待办) and application deadlines (截止), bounded to the active set. Day
	// index is local-weekday from the user's week start.
	strip, err := q.Query(ctx, `WITH ev AS (
		SELECT (EXTRACT(ISODOW FROM a.submitted_at AT TIME ZONE $2)::int) - 1 AS day, '投递' AS kind, a.company_name AS who, 'info' AS tone
		FROM applications a WHERE a.owner_id=$1 AND a.deleted_at IS NULL AND a.archived_at IS NULL
		  AND a.submitted_at >= $3 AND a.submitted_at < $4
		UNION ALL
		SELECT (EXTRACT(ISODOW FROM a.first_response_at AT TIME ZONE $2)::int) - 1, '回复', a.company_name, 'good'
		FROM applications a WHERE a.owner_id=$1 AND a.deleted_at IS NULL AND a.archived_at IS NULL
		  AND a.first_response_at >= $3 AND a.first_response_at < $4
		UNION ALL
		SELECT (EXTRACT(ISODOW FROM i.scheduled_at AT TIME ZONE $2)::int) - 1, '面试', a.company_name || ' · ' || i.round_name, 'acc'
		FROM interviews i JOIN applications a ON a.id=i.application_id AND a.owner_id=i.owner_id
		LEFT JOIN schedule_links sl ON sl.interview_id=i.id AND sl.owner_id=i.owner_id
		WHERE i.owner_id=$1 AND i.scheduled_at >= $3 AND i.scheduled_at < $4
		  AND COALESCE(sl.cancelled,FALSE)=FALSE AND a.deleted_at IS NULL
		UNION ALL
		SELECT (EXTRACT(ISODOW FROM COALESCE(x.due_ts, x.due_date::timestamptz) AT TIME ZONE $2)::int) - 1,
		       '待办', COALESCE(ap.company_name,''), CASE WHEN COALESCE(x.due_ts, x.due_date::timestamptz) < $5 THEN 'bad' ELSE 'warn' END
		FROM actions x LEFT JOIN applications ap ON ap.id=x.application_id AND ap.owner_id=x.owner_id
		WHERE x.owner_id=$1 AND x.done_at IS NULL AND COALESCE(x.due_ts, x.due_date::timestamptz) >= $3
		  AND COALESCE(x.due_ts, x.due_date::timestamptz) < $4
	)
	SELECT day, kind, who, tone FROM ev ORDER BY day, kind, who`, ownerID, tz, weekStart, weekEnd, dayStart)
	if err != nil {
		return nil, err
	}
	for strip.Next() {
		var wi WeekItem
		if err := strip.Scan(&wi.Day, &wi.Kind, &wi.Who, &wi.Tone); err != nil {
			strip.Close()
			return nil, err
		}
		s.WeekItems = append(s.WeekItems, wi)
	}
	strip.Close()

	// Upcoming interviews: cross-application, actual-time sorted (candidate
	// set is never clipped by application status or by page size).
	rows, err := q.Query(ctx, `SELECT i.id, a.id, a.company_name, a.position, i.round_name, i.format,
		i.scheduled_at, i.timezone, i.duration_minutes, COALESCE(sl.location,''), COALESCE(sl.meeting_url,''),
		COALESCE(sl.cancelled, FALSE), COALESCE(i.result,'')
		FROM interviews i
		JOIN applications a ON a.id = i.application_id AND a.owner_id = i.owner_id
		LEFT JOIN schedule_links sl ON sl.interview_id = i.id AND sl.owner_id = i.owner_id
		WHERE i.owner_id=$1 AND i.scheduled_at IS NOT NULL AND COALESCE(sl.cancelled, FALSE) = FALSE
		  AND a.deleted_at IS NULL
		  AND i.scheduled_at >= $2
		ORDER BY i.scheduled_at ASC
		LIMIT $3`, ownerID, now, upcomingLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u UpcomingInterview
		if err := rows.Scan(&u.ID, &u.ApplicationID, &u.CompanyName, &u.Position, &u.RoundName,
			&u.Format, &u.ScheduledAt, &u.Timezone, &u.DurationMin, &u.Location, &u.MeetingURL,
			&u.Cancelled, &u.Result); err != nil {
			return nil, err
		}
		s.Upcoming = append(s.Upcoming, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Unified todo counts: open standalone actions (the source of truth) plus
	// legacy derived todos — applications with a next_action text but no open
	// standalone action at all — so the dashboard count matches the checklist
	// and no double-entry exists while legacy rows still surface (§5.3).
	if err := q.QueryRow(ctx, `WITH open_actions AS (
			SELECT a.id AS action_id, a.application_id, a.due_date, a.due_ts
			FROM actions a JOIN applications ap ON ap.id = a.application_id
			WHERE a.owner_id=$1 AND a.done_at IS NULL AND ap.deleted_at IS NULL
		),
		derived AS (
			SELECT NULL::bigint AS action_id, ap.id AS application_id, ap.next_action_due_at::date AS due_date, NULL::timestamptz AS due_ts
			FROM applications ap
			WHERE ap.owner_id=$1 AND ap.deleted_at IS NULL AND ap.archived_at IS NULL
			  AND trim(ap.next_action) <> ''
			  AND NOT EXISTS (SELECT 1 FROM open_actions oa WHERE oa.application_id = ap.id)
		),
		all_todos AS (SELECT * FROM open_actions UNION ALL SELECT * FROM derived)
		SELECT count(*),
			count(*) FILTER (WHERE COALESCE(all_todos.due_ts, all_todos.due_date::timestamptz) < $2),
			count(*) FILTER (WHERE COALESCE(all_todos.due_ts, all_todos.due_date::timestamptz) >= $2
			                 AND COALESCE(all_todos.due_ts, all_todos.due_date::timestamptz) < $3)
		FROM all_todos`,
		ownerID, dayStart, dayStart.AddDate(0, 0, 1)).Scan(&s.Todos.Open, &s.Todos.Overdue, &s.Todos.DueToday); err != nil {
		return nil, err
	}

	// Recent activity feed (active rows, updated desc).
	recent, err := q.Query(ctx, `SELECT id, company_name, position, status, next_action, updated_at
		FROM applications WHERE owner_id=$1 AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC LIMIT 6`, ownerID)
	if err != nil {
		return nil, err
	}
	defer recent.Close()
	for recent.Next() {
		var ra RecentApplication
		if err := recent.Scan(&ra.ID, &ra.CompanyName, &ra.Position, &ra.Status, &ra.NextAction, &ra.UpdatedAt); err != nil {
			return nil, err
		}
		s.Recent = append(s.Recent, ra)
	}
	if err := recent.Err(); err != nil {
		return nil, err
	}
	return s, nil
}
