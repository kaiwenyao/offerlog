// Package reminders scans the current user's data on a schedule and inserts
// in-app notifications (plan §5.1). Generation respects the user's preference
// switches (when a reminder type is off no new rows of that type are created).
//
// Exactly one notification is created per event occurrence — deduplicated by
// idempotency key backed by a unique index — so repeated scans never notify
// twice about the same event, and read (已读) or dismiss (忽略) only change
// visibility, never re-create the row (ignoring a reminder mutes that
// occurrence permanently). New occurrences (a rescheduled interview day, a
// re-overdue action after postpone, a changed stale threshold) use new keys
// and therefore notify afresh. When a preference is turned off no new rows of
// that type are generated; already-created rows keep their lifecycle.
package reminders

import (
	"context"
	"fmt"
	"time"

	"offerlog/backend/internal/notifications"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/timeutil"
	"offerlog/backend/internal/prefs"
)

// Gen is the generator entry: it computes the "today" reminder set for every
// user that has preferences (or effective defaults) and inserts idempotent
// notifications. Because each user's timezone changes "today", the scan
// resolves windows per-user rather than with one global clock.
type Gen struct {
	db    *database.DB
	prefs *prefs.Repo
	nots  *notifications.Repo
}

func New(db *database.DB) *Gen {
	return &Gen{db: db, prefs: prefs.New(db), nots: notifications.New(db)}
}

// user is a light row used to fan the scan.
type userRow struct {
	ID       int64
	Timezone string
}

// users returns every user id + effective timezone.
func (g *Gen) users(ctx context.Context) ([]userRow, error) {
	// users.timezone is the single source of truth (written by both PATCH
	// /auth/me and PUT /preferences via UpdateProfile). user_preferences.timezone
	// is a legacy mirror that can drift when only /auth/me is used — never read
	// it for reminder windows, or a user's reminder zone would silently lag
	// their profile zone.
	rows, err := g.db.Pool().Query(ctx, `SELECT u.id, u.timezone FROM users u ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userRow
	for rows.Next() {
		var u userRow
		if err := rows.Scan(&u.ID, &u.Timezone); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Run executes one reminder pass at now (injectable for tests).
func (g *Gen) Run(ctx context.Context, now time.Time) (int, error) {
	users, err := g.users(ctx)
	if err != nil {
		return 0, err
	}
	inserted := 0
	for _, u := range users {
		loc, tzSafe := timeutil.SafeLocation(u.Timezone)
		n, err := g.runOne(ctx, u.ID, loc, now, tzSafe)
		if err != nil {
			return inserted, err
		}
		inserted += n
	}
	return inserted, nil
}

func (g *Gen) runOne(ctx context.Context, ownerID int64, loc *time.Location, now time.Time, tz string) (int, error) {
	p, err := g.prefs.Get(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	// Effective preference set (defaults until first save).
	remindOverdue := true
	remindInterview := true
	remindStale := true
	staleDays := 14
	if p != nil {
		remindOverdue = p.RemindOverdue
		remindInterview = p.RemindInterview
		remindStale = p.RemindStaleDays > 0
		staleDays = p.RemindStaleDays
	}
	inserted := 0

	// 1) 逾期待办：open actions whose due falls before today's start. A
	// date-only due_date is the user's calendar day → compared as the user's
	// local midnight (due_date::timestamp AT TIME ZONE $3).
	if remindOverdue {
		dayStart, _ := timeutil.TodayBounds(now, loc)
		rows, err := g.db.Pool().Query(ctx, `SELECT a.id, a.title, COALESCE(ap.company_name,''), ap.id
			FROM actions a JOIN applications ap ON ap.id = a.application_id AND ap.owner_id = a.owner_id
			WHERE a.owner_id=$1 AND a.done_at IS NULL
			  AND ap.deleted_at IS NULL AND ap.archived_at IS NULL
			  AND ( (a.due_ts IS NOT NULL AND a.due_ts < $2)
			     OR (a.due_ts IS NULL AND a.due_date IS NOT NULL
			         AND (a.due_date::timestamp AT TIME ZONE $3) < $2) )
			ORDER BY a.id`, ownerID, dayStart, tz)
		if err != nil {
			return inserted, err
		}
		for rows.Next() {
			var id int64
			var title, company string
			var appID *int64
			if err := rows.Scan(&id, &title, &company, &appID); err != nil {
				rows.Close()
				return inserted, err
			}
			ok, err := g.nots.InsertIdempotent(ctx, &notifications.Notification{
				OwnerID: ownerID, Kind: "overdue", Title: "逾期待办",
				Body: fmt.Sprintf("%s · %s", company, title), ApplicationID: appID,
			}, fmt.Sprintf("overdue:%d", id))
			if err != nil {
				rows.Close()
				return inserted, err
			}
			if ok {
				inserted++
			}
		}
		if err := rows.Err(); err != nil {
			return inserted, err
		}
		rows.Close()
	}

	// 2) 面试前一天：interviews scheduled in [tomorrow, tomorrow+1d) local.
	if remindInterview {
		tomorrowStart, tomorrowEnd := timeutil.NextDayBounds(now, loc)
		rows, err := g.db.Pool().Query(ctx, `SELECT i.id, COALESCE(ap.company_name,''), COALESCE(ap.position,''),
				i.round_name, i.scheduled_at, COALESCE(ap.id, 0)
			FROM interviews i JOIN applications ap ON ap.id = i.application_id AND ap.owner_id = i.owner_id
			LEFT JOIN schedule_links sl ON sl.interview_id = i.id AND sl.owner_id = i.owner_id
			WHERE i.owner_id=$1 AND i.scheduled_at IS NOT NULL
			  AND COALESCE(sl.cancelled, FALSE) = FALSE
			  -- 已完成 / 已取消的轮次不再提醒：用户在轮次卡片上标过「已完成」就不该
			  -- 再收到「明天有面试」（方案 §3.3 完成事实独立于排期）。
			  AND COALESCE(i.progress,'') NOT IN ('completed','cancelled')
			  AND i.scheduled_at >= $2 AND i.scheduled_at < $3
			ORDER BY i.id`, ownerID, tomorrowStart, tomorrowEnd)
		if err != nil {
			return inserted, err
		}
		for rows.Next() {
			var id int64
			var company, position, round string
			var scheduled time.Time
			var appID int64
			if err := rows.Scan(&id, &company, &position, &round, &scheduled, &appID); err != nil {
				rows.Close()
				return inserted, err
			}
			when := scheduled.In(loc).Format("15:04")
			body := fmt.Sprintf("%s · %s（%s）明天 %s", company, position, round, when)
			ok, err := g.nots.InsertIdempotent(ctx, &notifications.Notification{
				OwnerID: ownerID, Kind: "interview", Title: "明天有面试",
				Body: body, ApplicationID: &appID,
			}, fmt.Sprintf("interview:%d:%s", id, timeutil.DateOnly(scheduled, loc)))
			if err != nil {
				rows.Close()
				return inserted, err
			}
			if ok {
				inserted++
			}
		}
		if err := rows.Err(); err != nil {
			return inserted, err
		}
		rows.Close()
	}

	// 3) 投递满 N 天未回复（有效回复停止跟进提醒，不自动判定拒绝）。
	if remindStale {
		cutoff := now.AddDate(0, 0, -staleDays)
		rows, err := g.db.Pool().Query(ctx, `SELECT a.id, a.company_name, a.position, a.submitted_at
			FROM applications a
			WHERE a.owner_id=$1 AND a.deleted_at IS NULL AND a.archived_at IS NULL
			  AND a.submitted_at IS NOT NULL AND a.first_response_at IS NULL
			  AND a.status NOT IN ('rejected','withdrawn','closed','accepted')
			  AND a.submitted_at < $2
			ORDER BY a.id`, ownerID, cutoff)
		if err != nil {
			return inserted, err
		}
		for rows.Next() {
			var id int64
			var company, position string
			var submitted time.Time
			if err := rows.Scan(&id, &company, &position, &submitted); err != nil {
				rows.Close()
				return inserted, err
			}
			days := timeutil.DaysSince(submitted, now, loc)
			body := fmt.Sprintf("%s · %s 已投递 %d 天未回复，可考虑跟进", company, position, days)
			ok, err := g.nots.InsertIdempotent(ctx, &notifications.Notification{
				OwnerID: ownerID, Kind: "stale", Title: "投递后未回复",
				Body: body, ApplicationID: &id,
			}, fmt.Sprintf("stale:%d:%d", id, staleDays))
			if err != nil {
				rows.Close()
				return inserted, err
			}
			if ok {
				inserted++
			}
		}
		if err := rows.Err(); err != nil {
			return inserted, err
		}
		rows.Close()
	}

	return inserted, nil
}
