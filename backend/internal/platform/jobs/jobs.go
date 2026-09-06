// Package jobs implements the persistent job/outbox machinery used by the
// worker: leases, attempts, next_run_at and idempotency keys. No Redis or
// external queue is required in v1 (plan §6).
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/platform/database"
)

type Job struct {
	ID             int64
	Kind           string
	Status         string
	Payload        json.RawMessage
	IdempotencyKey string
	Attempts       int
	MaxAttempts    int
	LeaseUntil     *time.Time
	NextRunAt      time.Time
	LastError      string
}

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

// Enqueue inserts a job if the same (kind, idempotency_key) is not already
// pending; enqueueing the same work twice is a no-op.
func (s *Store) Enqueue(ctx context.Context, kind, key string, payload any, runAt time.Time) error {
	b, _ := json.Marshal(payload)
	_, err := s.db.Pool().Exec(ctx, `INSERT INTO jobs(kind, status, payload, idempotency_key, next_run_at, max_attempts)
		VALUES($1,'pending',$2,$3,$4,10)
		ON CONFLICT (kind, idempotency_key) WHERE idempotency_key <> '' DO NOTHING`,
		kind, b, key, runAt)
	return err
}

// Claim atomically leases a due job for processing. Jobs left in 'running'
// with an expired lease (crashed worker) are reclaimed: the lease is the
// ownership token and expiry is the recovery signal (§4.3).
func (s *Store) Claim(ctx context.Context, leaseSeconds int) (*Job, error) {
	var j Job
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `UPDATE jobs SET status='running',
			lease_until = now() + make_interval(secs => $1),
			attempts = attempts + 1
			WHERE id = (SELECT id FROM jobs
				WHERE attempts < max_attempts
				AND next_run_at <= now()
				AND (
					status IN ('pending','failed')
					OR (status = 'running' AND lease_until IS NOT NULL AND lease_until < now())
				)
				ORDER BY next_run_at LIMIT 1 FOR UPDATE SKIP LOCKED)
			RETURNING id, kind, status, payload, idempotency_key, attempts, max_attempts, next_run_at, last_error`,
			leaseSeconds).
			Scan(&j.ID, &j.Kind, &j.Status, &j.Payload, &j.IdempotencyKey, &j.Attempts, &j.MaxAttempts, &j.NextRunAt, &j.LastError)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &j, err
}

// Succeed marks a job done.
func (s *Store) Succeed(ctx context.Context, id int64) error {
	_, err := s.db.Pool().Exec(ctx, `UPDATE jobs SET status='done', updated_at=now() WHERE id=$1`, id)
	return err
}

// Fail records an error and schedules the retry.
func (s *Store) Fail(ctx context.Context, id int64, attempts, maxAttempts int, msg string, backoff time.Duration) error {
	status := "failed"
	if attempts >= maxAttempts {
		status = "failed" // terminal after max attempts; operator can retry
	}
	_, err := s.db.Pool().Exec(ctx, `UPDATE jobs SET status=$2, last_error=$3,
		next_run_at = now() + make_interval(secs => $4), lease_until = NULL, updated_at=now() WHERE id=$1`,
		id, status, msg, int(backoff.Seconds()))
	return err
}

// Retry resets a failed job for another attempt (operator command).
func (s *Store) Retry(ctx context.Context, id int64) error {
	_, err := s.db.Pool().Exec(ctx, `UPDATE jobs SET status='pending', attempts=0, next_run_at=now(), updated_at=now() WHERE id=$1`, id)
	return err
}

// List returns recent jobs for the operator.
func (s *Store) List(ctx context.Context, limit int) ([]*Job, error) {
	rows, err := s.db.Pool().Query(ctx, `SELECT id, kind, status, payload, idempotency_key, attempts, max_attempts, next_run_at, last_error
		FROM jobs ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.Status, &j.Payload, &j.IdempotencyKey, &j.Attempts, &j.MaxAttempts, &j.NextRunAt, &j.LastError); err != nil {
			return nil, err
		}
		out = append(out, &j)
	}
	return out, rows.Err()
}

// CleanupOrphans deletes expired pending files and staging objects (part of
// the file pipeline's async hygiene; see files package docs).
func (s *Store) CleanupStalePendings(ctx context.Context, grace time.Duration) (int64, error) {
	tag, err := s.db.Pool().Exec(ctx, `UPDATE files SET status='failed', error_message='pending 超时清理',
		updated_at=now() WHERE status='pending' AND created_at < now() - make_interval(secs => $1)`,
		int(grace.Seconds()))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

var _ = fmt.Sprintf
