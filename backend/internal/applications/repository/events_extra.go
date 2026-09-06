package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
)

// HadOffer reports whether the application ever reached offer (either the
// current snapshot is offer/accepted or an offer event exists).
func (r *Repo) HadOffer(ctx context.Context, q database.Querier, appID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM application_events
		WHERE application_id = $1 AND to_status = $2
	)`, appID, domain.StatusOffer).Scan(&ok)
	return ok, err
}

// RunIdempotentGuard checks whether this transition was already applied under
// the same (application, key). Returns done=true with the current row when a
// duplicate is detected, so the caller can short-circuit.
func (r *Repo) RunIdempotentGuard(ctx context.Context, ownerID, appID int64, key string) (bool, *Row, error) {
	var exists bool
	err := r.db.Pool().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM application_events e
			WHERE e.application_id = $1 AND e.owner_id = $2 AND e.event_type='status_change'
			  AND e.note LIKE '%' || '|idem:' || $3 || '%')`,
		appID, ownerID, key).Scan(&exists)
	if err != nil {
		return false, nil, err
	}
	if !exists {
		return false, nil, nil
	}
	row, err := r.GetByID(ctx, ownerID, appID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	return true, row, err
}

// RecordIdempotency marks the just-created transition event with the
// idempotency key so a retry of the same request is recognized.
func (r *Repo) RecordIdempotency(ctx context.Context, q database.Querier, ownerID, appID int64, key, _ string) error {
	_, err := q.Exec(ctx, `UPDATE application_events e SET note = note || '|idem:' || $3
		WHERE e.application_id = $1 AND e.owner_id = $2 AND e.event_type='status_change'
		  AND e.id = (SELECT MAX(id) FROM application_events
					  WHERE application_id = $1 AND event_type='status_change')`,
		appID, ownerID, key)
	return err
}

// ResyncStatusFromEvents recomputes the current status by replaying the
// effective timeline, then updates the application snapshot. Corrections
// override the status of the event they point to.
func (r *Repo) ResyncStatusFromEvents(ctx context.Context, q database.Querier, appID, ownerID int64) error {
	events, err := r.ListEvents(ctx, q, appID, ownerID)
	if err != nil {
		return err
	}
	// correction map: corrected event id → replacement status
	corrected := map[int64]string{}
	for _, ev := range events {
		if ev.EventType == "correction" && ev.CorrectsEventID != nil && ev.ToStatus != nil {
			corrected[*ev.CorrectsEventID] = *ev.ToStatus
		}
	}
	status := domain.StatusSaved
	var sub, rej, acc *time.Time
	for _, ev := range events {
		if ev.EventType == "correction" {
			continue
		}
		eff := ev.ToStatus
		if repl, ok := corrected[ev.ID]; ok {
			eff = &repl
		}
		if eff == nil {
			continue
		}
		status = *eff
		switch {
		case *eff == domain.StatusApplied && sub == nil:
			t := ev.OccurredAt
			sub = &t
		case *eff == domain.StatusRejected && rej == nil:
			t := ev.OccurredAt
			rej = &t
		case *eff == domain.StatusAccepted && acc == nil:
			t := ev.OccurredAt
			acc = &t
		}
	}
	_, err = q.Exec(ctx, `UPDATE applications SET status=$1, submitted_at=$2, rejected_at=$3,
		accepted_at=$4, version=version+1, updated_at=now() WHERE id=$5 AND owner_id=$6`,
		status, sub, rej, acc, appID, ownerID)
	return err
}
