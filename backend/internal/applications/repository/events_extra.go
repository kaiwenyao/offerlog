package repository

import (
	"context"
	"errors"
	"slices"
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
	rows, err := r.db.Pool().Query(ctx, `SELECT `+eventCols+`
		FROM application_events WHERE owner_id=$1 AND application_id = ANY($2::bigint[])
		ORDER BY application_id, occurred_at ASC, sequence ASC`, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TimelineSummary is the per-application enrichment the list page needs. Both
// maps come from ONE events query so a page of results costs a single
// round-trip rather than one per derived view.
type TimelineSummary struct {
	// StageHistory: reached status → earliest arrival day (YYYY-MM-DD, user zone).
	StageHistory map[int64]map[string]string
	// ProgressSince: app id → the day its CURRENT progress was entered
	// (方案 §5: 列表补充「最近一次进入当前进度的日期」). A correction moves it to
	// the corrected business time; the superseded original does not count.
	ProgressSince map[int64]string
}

// TimelineSummaryFor computes the list enrichment in one pass over the events.
func (r *Repo) TimelineSummaryFor(ctx context.Context, ownerID int64, appIDs []int64, loc *time.Location, submittedAt map[int64]*time.Time) (*TimelineSummary, error) {
	out := &TimelineSummary{
		StageHistory:  map[int64]map[string]string{},
		ProgressSince: map[int64]string{},
	}
	if len(appIDs) == 0 {
		return out, nil
	}
	events, err := r.ListEventsByApps(ctx, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	out.StageHistory = buildStageHistory(events, loc, submittedAt)
	out.ProgressSince = buildProgressSince(events, loc)
	return out, nil
}

// buildProgressSince replays each application's effective timeline (corrections
// applied first) and keeps the business time of the last change that actually
// moved the progress — status or substatus. Editing only the note does not.
func buildProgressSince(events []*Event, loc *time.Location) map[int64]string {
	byApp := map[int64][]*Event{}
	var order []int64
	for _, e := range events {
		if _, ok := byApp[e.ApplicationID]; !ok {
			order = append(order, e.ApplicationID)
		}
		byApp[e.ApplicationID] = append(byApp[e.ApplicationID], e)
	}
	out := map[int64]string{}
	for _, appID := range order {
		evs := bySequence(byApp[appID])
		corrected := correctionsByEvent(evs)
		var last time.Time
		curStatus, curSub := "", ""
		seen := false
		for _, ev := range evs {
			if ev.EventType == "correction" {
				continue
			}
			eff, at, ok := effectiveOf(ev, corrected)
			if !ok {
				continue
			}
			sub := ""
			if c, ok := corrected[ev.ID]; ok {
				sub = c.substatus
			} else if ev.ToSubstatus != nil {
				sub = *ev.ToSubstatus
			}
			if !seen || eff != curStatus || sub != curSub {
				last, seen = at, true
			}
			curStatus, curSub = eff, sub
		}
		if seen {
			out[appID] = timeutil.DateOnly(last, loc)
		}
	}
	return out
}

// StageHistoryFor returns, per application id, the earliest calendar day
// (YYYY-MM-DD in the user's timezone) at which each effective status was
// reached. Corrections replace the status of the event they point at (later
// corrections win); correction events themselves contribute no arrival. The
// result includes terminal statuses too (rejected/withdrawn/closed), so the
// stage rail can draw how far a dead application actually walked before it
// stopped.
//
// submittedAt carries each application's user-entered 投递时间 snapshot: when
// present it wins for the applied stage — the trail must show when the user
// actually submitted (possibly backfilled), not when the record was touched.
func (r *Repo) StageHistoryFor(ctx context.Context, ownerID int64, appIDs []int64, loc *time.Location, submittedAt map[int64]*time.Time) (map[int64]map[string]string, error) {
	if len(appIDs) == 0 {
		return map[int64]map[string]string{}, nil
	}
	events, err := r.ListEventsByApps(ctx, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	return buildStageHistory(events, loc, submittedAt), nil
}

// effectiveEvent is what a correction replaces on the event it corrects: both
// the target status AND the business time. Time matters because repairing a
// mistyped date is the main reason to correct anything — replacing only the
// status left the timeline showing the fix while submitted_at and the stage
// rail kept the wrong day.
type effectiveEvent struct {
	status    string
	substatus string
	at        time.Time
}

// correctionsByEvent indexes corrections by the event they correct; a later
// correction of the same event wins.
func correctionsByEvent(evs []*Event) map[int64]effectiveEvent {
	out := map[int64]effectiveEvent{}
	for _, ev := range evs {
		if ev.EventType == "correction" && ev.CorrectsEventID != nil && ev.ToStatus != nil {
			sub := ""
			if ev.ToSubstatus != nil {
				sub = *ev.ToSubstatus
			}
			out[*ev.CorrectsEventID] = effectiveEvent{status: *ev.ToStatus, substatus: sub, at: ev.OccurredAt}
		}
	}
	return out
}

// effectiveOf resolves an event to the status and time that actually count.
func effectiveOf(ev *Event, corrected map[int64]effectiveEvent) (string, time.Time, bool) {
	if c, ok := corrected[ev.ID]; ok {
		return c.status, c.at, true
	}
	if ev.ToStatus == nil {
		return "", time.Time{}, false
	}
	return *ev.ToStatus, ev.OccurredAt, true
}

// buildStageHistory replays each application's effective timeline (same
// correction semantics as ResyncStatusFromEvents) and records the FIRST
// arrival date per status.
func buildStageHistory(events []*Event, loc *time.Location, submittedAt map[int64]*time.Time) map[int64]map[string]string {
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
		corrected := correctionsByEvent(evs)
		first := map[string]time.Time{}
		for _, ev := range evs {
			if ev.EventType == "correction" {
				continue
			}
			eff, at, ok := effectiveOf(ev, corrected)
			if !ok {
				continue
			}
			if t, seen := first[eff]; !seen || at.Before(t) {
				first[eff] = at
			}
		}
		// The user-entered 投递时间 is the authoritative arrival for 已投递:
		// it overrides whatever day the events replay produced (legacy rows may
		// carry a "now" occurred_at while submitted_at holds the backfilled day).
		if sub, ok := submittedAt[appID]; ok && sub != nil {
			first[domain.StatusApplied] = *sub
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

// ResyncStatusFromEvents recomputes the current status (and substatus) by
// replaying the effective timeline, then updates the application snapshot.
// Corrections override the status of the event they point to; existing
// corrections are applied first so repeated corrections stay consistent
// (方案 §4.3).
func (r *Repo) ResyncStatusFromEvents(ctx context.Context, q database.Querier, appID, ownerID int64) error {
	events, err := r.ListEvents(ctx, q, appID, ownerID)
	if err != nil {
		return err
	}
	// Replay in sequence (audit/insertion) order, NOT occurred_at order: a
	// backfilled transition (今天补录昨天投递) carries an earlier business time
	// than the created event, and the state machine must still walk the order
	// in which the transitions were recorded.
	events = bySequence(events)
	corrected := correctionsByEvent(events)
	status := domain.StatusSaved
	substatus := ""
	var sub, rej, acc *time.Time
	for _, ev := range events {
		if ev.EventType == "correction" {
			continue
		}
		eff, at, ok := effectiveOf(ev, corrected)
		if !ok {
			continue
		}
		status = eff
		// The substatus follows the effective event too: a correction that
		// points back at 准备 carries its own substatus (or "").
		if c, ok := corrected[ev.ID]; ok {
			substatus = c.substatus
		} else if ev.ToSubstatus != nil {
			substatus = *ev.ToSubstatus
		} else {
			substatus = ""
		}
		switch {
		case eff == domain.StatusApplied && sub == nil:
			t := at
			sub = &t
		case eff == domain.StatusRejected && rej == nil:
			t := at
			rej = &t
		case eff == domain.StatusAccepted && acc == nil:
			t := at
			acc = &t
		}
	}
	// A focus reference only survives while the record still sits in that
	// activity's stage.
	focusKind := ""
	switch status {
	case domain.StatusAssessment:
		focusKind = domain.ActivityAssessment
	case domain.StatusInterviewing:
		focusKind = domain.ActivityInterview
	}
	_, err = q.Exec(ctx, `UPDATE applications SET status=$1, substatus=$2,
		focus_activity_kind = CASE WHEN $7::text IS NOT NULL AND focus_activity_kind = $7 THEN focus_activity_kind ELSE NULL END,
		focus_activity_id = CASE WHEN $7::text IS NOT NULL AND focus_activity_kind = $7 THEN focus_activity_id ELSE NULL END,
		submitted_at=$3, rejected_at=$4, accepted_at=$5, version=version+1, updated_at=now()
		WHERE id=$6 AND owner_id=$8`,
		status, substatus, sub, rej, acc, appID, nullIfEmpty(focusKind), ownerID)
	return err
}

// bySequence returns the events sorted by their per-application sequence —
// the order in which they were recorded. Status-machine replays (correction
// simulation, resync) must use this order; occurred_at only drives display.
func bySequence(evs []*Event) []*Event {
	out := append([]*Event(nil), evs...)
	slices.SortStableFunc(out, func(a, b *Event) int {
		return a.Sequence - b.Sequence
	})
	return out
}
