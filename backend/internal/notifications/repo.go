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

// InsertIdempotent creates a notification unless a row with the same
// (owner_id, kind, application_id, idempotency_key) already exists — read or
// dismissed alike. Returns whether a row was inserted.
//
// Exactly one notification is generated per event occurrence, ever: the key
// identifies the occurrence (overdue:action, stale:app, interview:round:day),
// and read (已读) / dismiss (忽略) only change visibility and history. This is
// the migration note “dismissable so the same event cannot notify twice” and
// the acceptance “同一事件不重复提醒”. A genuinely new occurrence (rescheduled
// interview day, an action re-overdue after postpone) uses a new key and
// notifies afresh — postpone clears prior rows so the new due period re-arms.
func (r *Repo) InsertIdempotent(ctx context.Context, n *Notification, key string) (bool, error) {
	if key == "" {
		return false, errors.New("idempotency key required")
	}
	// Direct INSERT relying on the partial UNIQUE index as the hard guard:
	// ON CONFLICT DO NOTHING makes a concurrent duplicate a clean no-op instead
	// of a unique-violation error, so a racing generator pass can never fail a
	// whole reminder scan. Returns whether this call inserted the row.
	var id int64
	err := r.db.Pool().QueryRow(ctx, `INSERT INTO notifications(owner_id, kind, title, body, application_id, idempotency_key)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT (owner_id, kind, COALESCE(application_id, 0), idempotency_key)
		WHERE idempotency_key <> '' DO NOTHING
		RETURNING id`,
		n.OwnerID, n.Kind, n.Title, n.Body, n.ApplicationID, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // conflict — the occurrence already has a notification
	}
	if err != nil {
		return false, err
	}
	return true, nil
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
		return ErrNotFound
	}
	return nil
}

// Dismiss hides a notification without deleting it. Dismissing twice is a
// no-op (COALESCE keeps the first dismissal); the read_at column is untouched
// (dismissing an unread notification merely hides it — a later MarkRead has no
// visible effect on a dismissed row).
func (r *Repo) Dismiss(ctx context.Context, ownerID, id int64) error {
	tag, err := r.db.Pool().Exec(ctx, `UPDATE notifications SET dismissed_at=COALESCE(dismissed_at, now())
		WHERE id=$1 AND owner_id=$2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
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

// ClearOccurrence removes every notification of an occurrence (matched by
// kind + idempotency key), dismissed or not. Used when the occurrence is
// actively changed — e.g. an action is postponed to a new due date — so the
// next time it becomes overdue it is a fresh occurrence that notifies again
// (postpone clears the mute instead of carrying it forward).
func (r *Repo) ClearOccurrence(ctx context.Context, ownerID int64, kind, key string) error {
	_, err := r.db.Pool().Exec(ctx, `DELETE FROM notifications
		WHERE owner_id=$1 AND kind=$2 AND idempotency_key=$3`, ownerID, kind, key)
	return err
}

// MarkAllRead marks every open notification of the user as read.
func (r *Repo) MarkAllRead(ctx context.Context, ownerID int64) error {
	_, err := r.db.Pool().Exec(ctx, `UPDATE notifications SET read_at=COALESCE(read_at, now())
		WHERE owner_id=$1 AND dismissed_at IS NULL AND read_at IS NULL`, ownerID)
	return err
}

// ErrNotFound is returned for read/dismiss on a notification the user does
// not own (or that does not exist). Only this error maps to HTTP 404.
var ErrNotFound = errors.New("通知不存在")

// ErrNoRows is re-exported for callers that distinguish absent rows.
var ErrNoRows = pgx.ErrNoRows
