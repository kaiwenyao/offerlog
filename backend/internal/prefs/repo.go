// Package prefs persists the user account profile and reminder preferences
// (plan §5.1 账户偏好与提醒). The settings page previously wrote to
// localStorage only; prefs now round-trip to the server so another device or
// a fresh browser sees the same values.
package prefs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/platform/database"
)

// Preferences is the persisted profile + reminder preference set.
type Preferences struct {
	UserID          int64
	DisplayName     string
	Timezone        string // IANA name, e.g. Europe/Dublin
	WeekStart       int    // 0=Sunday .. 6=Saturday (default Monday=1)
	RemindOverdue   bool
	RemindInterview bool
	RemindStaleDays int
	RemindWeekly    bool
	Locale          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Pool() *database.DB { return r.db }

// UpsertTx writes the preference row inside an existing transaction (the same
// shape as Upsert but on a Querier so PUT /preferences can persist the users
// profile row and the preferences row atomically).
func (r *Repo) UpsertTx(ctx context.Context, q database.Querier, p *Preferences) error {
	_, err := q.Exec(ctx, `INSERT INTO user_preferences
		(user_id, display_name, timezone, week_start, remind_overdue, remind_interview,
		 remind_stale_days, remind_weekly, locale)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (user_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			timezone = EXCLUDED.timezone,
			week_start = EXCLUDED.week_start,
			remind_overdue = EXCLUDED.remind_overdue,
			remind_interview = EXCLUDED.remind_interview,
			remind_stale_days = EXCLUDED.remind_stale_days,
			remind_weekly = EXCLUDED.remind_weekly,
			locale = EXCLUDED.locale,
			updated_at = now()`,
		p.UserID, p.DisplayName, p.Timezone, p.WeekStart, p.RemindOverdue, p.RemindInterview,
		p.RemindStaleDays, p.RemindWeekly, p.Locale)
	return err
}

// Upsert writes the preference row, defaulting on first insert.
func (r *Repo) Upsert(ctx context.Context, p *Preferences) error {
	return r.UpsertTx(ctx, r.db, p)
}

// Get returns the preference row for a user; when the row is absent it
// returns the effective defaults (so /auth/me and settings agree before the
// user saves anything).
func (r *Repo) Get(ctx context.Context, userID int64) (*Preferences, error) {
	var p Preferences
	err := r.db.Pool().QueryRow(ctx, `SELECT user_id, display_name, timezone, week_start,
		remind_overdue, remind_interview, remind_stale_days, remind_weekly, locale, created_at, updated_at
		FROM user_preferences WHERE user_id=$1`, userID).
		Scan(&p.UserID, &p.DisplayName, &p.Timezone, &p.WeekStart, &p.RemindOverdue,
			&p.RemindInterview, &p.RemindStaleDays, &p.RemindWeekly, &p.Locale, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// Delete removes the preference row (used when a user is deleted).
func (r *Repo) Delete(ctx context.Context, userID int64) error {
	_, err := r.db.Pool().Exec(ctx, `DELETE FROM user_preferences WHERE user_id=$1`, userID)
	return err
}
