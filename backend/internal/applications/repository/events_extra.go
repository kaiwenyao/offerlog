package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
)

// HadOffer reports whether the application ever reached offer. It reads the
// unified stage-point view (迁移 00006), so an Offer the user recorded as a
// timeline node counts exactly like one produced by the legacy state machine.
func (r *Repo) HadOffer(ctx context.Context, q database.Querier, appID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM application_stage_points
		WHERE application_id = $1 AND status = $2
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

// TimelineSummary is the per-application enrichment the list page needs. Both
// maps come from ONE stage-point query so a page of results costs a single
// round-trip rather than one per derived view.
type TimelineSummary struct {
	// StageHistory: reached status → earliest arrival day (YYYY-MM-DD, user zone).
	StageHistory map[int64]map[string]string
	// ProgressSince: app id → the day its CURRENT progress was entered
	// (方案 §5: 列表补充「最近一次进入当前进度的日期」).
	ProgressSince map[int64]string
}

// TimelineSummaryFor computes the list enrichment in one pass over the stage
// points (status events + user-added nodes, corrections already applied by the
// view).
func (r *Repo) TimelineSummaryFor(ctx context.Context, ownerID int64, appIDs []int64, loc *time.Location, submittedAt map[int64]*time.Time) (*TimelineSummary, error) {
	out := &TimelineSummary{
		StageHistory:  map[int64]map[string]string{},
		ProgressSince: map[int64]string{},
	}
	if len(appIDs) == 0 {
		return out, nil
	}
	points, err := r.ListStagePointsByApps(ctx, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	out.StageHistory = buildStageHistoryFromPoints(points, loc, submittedAt)
	out.ProgressSince = buildProgressSinceFromPoints(points, loc)
	return out, nil
}

// StageHistoryFor returns, per application id, the earliest calendar day
// (YYYY-MM-DD in the user's timezone) at which each status was reached. The
// result includes terminal statuses too, so the stage rail can draw how far a
// dead application actually walked before it stopped.
func (r *Repo) StageHistoryFor(ctx context.Context, ownerID int64, appIDs []int64, loc *time.Location, submittedAt map[int64]*time.Time) (map[int64]map[string]string, error) {
	if len(appIDs) == 0 {
		return map[int64]map[string]string{}, nil
	}
	points, err := r.ListStagePointsByApps(ctx, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	return buildStageHistoryFromPoints(points, loc, submittedAt), nil
}
