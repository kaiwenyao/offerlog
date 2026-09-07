// Package notifications implements the in-app reminder list (plan §5.1 站内
// 提醒列表). The worker generates rows server-side; users read/dismiss them in
// the UI and jump to the linked application. Notifications respect the user's
// preference switches: when a preference is off no new rows of that kind are
// generated, and a notification idempotency key stops the same event
// (overdue action / interview day / stale application) from duplicating.
package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/platform/database"
)

type Notification struct {
	ID            int64      `json:"id"`
	OwnerID       int64      `json:"owner_id"`
	Kind          string     `json:"kind"` // overdue | interview | stale | weekly
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	ApplicationID *int64     `json:"application_id"`
	ReadAt        *time.Time `json:"read_at"`
	DismissedAt   *time.Time `json:"dismissed_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Pool() *database.DB { return r.db }

// InsertIdempotent creates a notification unless one with the same
// (owner_id, kind, idempotency_key) is still pending; returns whether a row
// was inserted. The key is application-scoped for overdue/stale/interview
// kinds so re-running the generator never spams duplicates.
func (r *Repo) InsertIdempotent(ctx context.Context, n *Notification, key string) (bool, error) {
	var inserted bool
	err := r.db.Pool().QueryRow(ctx, `WITH ins AS (
		INSERT INTO notifications(owner_id, kind, title, body, application_id, idempotency_key)
		SELECT $1,$2,$3,$4,$5,$6
		WHERE NOT EXISTS (
			SELECT 1 FROM notifications
			WHERE owner_id=$1 AND kind=$2 AND application_id IS NOT DISTINCT FROM $5
			  AND idempotency_key=$6 AND dismissed_at IS NULL AND read_at IS NULL
		)
		RETURNING 1
	) SELECT EXISTS (SELECT 1 FROM ins)`,
		n.OwnerID, n.Kind, n.Title, n.Body, n.ApplicationID, key).Scan(&inserted)
	return inserted, err
}

// List returns the user's notifications, optionally only open (unread and
// not dismissed) ones, newest first.
func (r *Repo) List(ctx context.Context, ownerID int64, openOnly bool, limit int) ([]*Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	where := "owner_id=$1"
	if openOnly {
		where += " AND read_at IS NULL AND dismissed_at IS NULL"
	}
	rows, err := r.db.Pool().Query(ctx, `SELECT id, owner_id, kind, title, body, application_id,
		read_at, dismissed_at, created_at FROM notifications WHERE `+where+` ORDER BY id DESC LIMIT $2`,
		ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.OwnerID, &n.Kind, &n.Title, &n.Body, &n.ApplicationID,
			&n.ReadAt, &n.DismissedAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

// MarkRead marks a notification read (idempotent).
func (r *Repo) MarkRead(ctx context.Context, ownerID, id int64) error {
	tag, err := r.db.Pool().Exec(ctx, `UPDATE notifications SET read_at=COALESCE(read_at, now())
		WHERE id=$1 AND owner_id=$2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("通知不存在")
	}
	return nil
}

// Dismiss hides a notification without deleting it (idempotent — dismissing
// twice is fine; the dismissal also clears any read_at).
func (r *Repo) Dismiss(ctx context.Context, ownerID, id int64) error {
	tag, err := r.db.Pool().Exec(ctx, `UPDATE notifications SET dismissed_at=COALESCE(dismissed_at, now())
		WHERE id=$1 AND owner_id=$2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("通知不存在")
	}
	return nil
}

// DismissByApplication dismisses every open notification referencing an
// application (used when a record is deleted, archived, or its status ends so
// no stale reminder survives).
func (r *Repo) DismissByApplication(ctx context.Context, ownerID, appID int64) error {
	_, err := r.db.Pool().Exec(ctx, `UPDATE notifications SET dismissed_at=now()
		WHERE owner_id=$1 AND application_id=$2 AND dismissed_at IS NULL`, ownerID, appID)
	return err
}

// MarkAllRead marks every open notification of the user as read.
func (r *Repo) MarkAllRead(ctx context.Context, ownerID int64) error {
	_, err := r.db.Pool().Exec(ctx, `UPDATE notifications SET read_at=COALESCE(read_at, now())
		WHERE owner_id=$1 AND dismissed_at IS NULL AND read_at IS NULL`, ownerID)
	return err
}

// ErrNoRows is re-exported for callers that distinguish absent rows.
var ErrNoRows = pgx.ErrNoRows
