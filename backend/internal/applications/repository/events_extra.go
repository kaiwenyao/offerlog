package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/timeutil"
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

// ListEventsByApps returns every timeline event for the given application ids
// (owner-scoped) ordered by (application_id, occurred_at, sequence), so one
// round-trip can feed the stage-history computation for a whole list page.
func (r *Repo) ListEventsByApps(ctx context.Context, ownerID int64, appIDs []int64) ([]*Event, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT id, application_id, sequence, event_type, from_status, to_status,
		note, reason, occurred_at, recorded_at, corrects_event_id, actor_id
		FROM application_events WHERE owner_id=$1 AND application_id = ANY($2::bigint[])
		ORDER BY application_id, occurred_at ASC, sequence ASC`, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ApplicationID, &e.Sequence, &e.EventType, &e.FromStatus,
			&e.ToStatus, &e.Note, &e.Reason, &e.OccurredAt, &e.RecordedAt, &e.CorrectsEventID, &e.ActorID); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// StageHistoryFor returns, per application id, the earliest calendar day
// (YYYY-MM-DD in the user's timezone) at which each effective status was
// reached. Corrections replace the status of the event they point at (later
// corrections win); correction events themselves contribute no arrival. The
// result includes terminal statuses too (rejected/withdrawn/closed), so the
// stage rail can draw how far a dead application actually walked before it
// stopped.
func (r *Repo) StageHistoryFor(ctx context.Context, ownerID int64, appIDs []int64, loc *time.Location) (map[int64]map[string]string, error) {
	if len(appIDs) == 0 {
		return map[int64]map[string]string{}, nil
	}
	events, err := r.ListEventsByApps(ctx, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	return buildStageHistory(events, loc), nil
}

// buildStageHistory replays each application's effective timeline (same
// correction semantics as ResyncStatusFromEvents) and records the FIRST
// arrival date per status.
func buildStageHistory(events []*Event, loc *time.Location) map[int64]map[string]string {
	byApp := map[int64][]*Event{}
	var order []int64
	for _, e := range events {
		if _, ok := byApp[e.ApplicationID]; !ok {
			order = append(order, e.ApplicationID)
		}
		byApp[e.ApplicationID] = append(byApp[e.ApplicationID], e)
	}
	out := map[int64]map[string]string{}
	for _, appID := range order {
		evs := byApp[appID]
		// corrected: original event id → replacement status (later wins).
		corrected := map[int64]string{}
		for _, ev := range evs {
			if ev.EventType == "correction" && ev.CorrectsEventID != nil && ev.ToStatus != nil {
				corrected[*ev.CorrectsEventID] = *ev.ToStatus
			}
		}
		first := map[string]time.Time{}
		for _, ev := range evs {
			if ev.EventType == "correction" || ev.ToStatus == nil {
				continue
			}
			eff := *ev.ToStatus
			if repl, ok := corrected[ev.ID]; ok {
				eff = repl
			}
			if t, ok := first[eff]; !ok || ev.OccurredAt.Before(t) {
				first[eff] = ev.OccurredAt
			}
		}
		if len(first) == 0 {
			continue
		}
		m := make(map[string]string, len(first))
		for st, t := range first {
			m[st] = timeutil.DateOnly(t, loc)
		}
		out[appID] = m
	}
	return out
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
