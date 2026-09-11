package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	actrepo "offerlog/backend/internal/activities/repository"
	"offerlog/backend/internal/applications/domain"
	apprepo "offerlog/backend/internal/applications/repository"
	"offerlog/backend/internal/platform/database"
)

// AssessmentInput records one OA / take-home round. The four timestamps are
// independent: invited / planned / due / completed never overwrite each other.
type AssessmentInput struct {
	Kind             string     `json:"kind"` // online_test | take_home | other
	Name             string     `json:"name"`
	Progress         string     `json:"progress"`
	Result           string     `json:"result"`
	InvitedAt        *time.Time `json:"invited_at"`
	PlannedAt        *time.Time `json:"planned_at"`
	DueAt            *time.Time `json:"due_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	CompletedUnknown bool       `json:"completed_unknown"`
	Link             string     `json:"link"`
	Notes            string     `json:"notes"`
}

// InterviewInput records one interview round's activity state.
type InterviewInput struct {
	RoundName        string     `json:"round_name"`
	Format           string     `json:"format"`
	ScheduledAt      *time.Time `json:"scheduled_at"`
	Timezone         string     `json:"timezone"`
	DurationMinutes  *int       `json:"duration_minutes"`
	Progress         string     `json:"progress"`
	Result           string     `json:"result"`
	InvitedAt        *time.Time `json:"invited_at"`
	CompletedAt      *time.Time `json:"completed_at"`
	CompletedUnknown bool       `json:"completed_unknown"`
	Feedback         string     `json:"feedback"`
	Notes            string     `json:"notes"`
}

func validAssessmentKind(kind string) bool {
	return kind == "" || kind == "online_test" || kind == "take_home" || kind == "other"
}

func (a *AssessmentInput) normalized() error {
	if !validAssessmentKind(a.Kind) {
		return &domain.ValidationError{Code: "invalid_activity_kind", Message: "未知的测评类型"}
	}
	if a.Progress == "" {
		a.Progress = domain.ActProgressPreparing
	}
	if !domain.ValidActivityProgress(domain.ActivityAssessment, a.Progress) {
		return &domain.ValidationError{Code: "invalid_activity_progress", Message: "未知的测评进度"}
	}
	if a.Result == "" {
		a.Result = domain.ActResultUnknown
	}
	if !domain.ValidActivityResult(a.Result) {
		return &domain.ValidationError{Code: "invalid_activity_result", Message: "未知的测评结果"}
	}
	// 完成事实与结果分离：a completed round may legitimately have an unknown
	// result, but a round that never finished cannot already be passed/failed.
	if a.Progress != domain.ActProgressCompleted && a.Result != domain.ActResultUnknown {
		return &domain.ValidationError{Code: "result_before_completion", Message: "尚未完成的测评不能记录通过 / 未通过结果"}
	}
	if a.Progress == domain.ActProgressCompleted && a.CompletedAt == nil && !a.CompletedUnknown {
		// 历史时间不详时允许标记未知，不伪造精确时间。
		a.CompletedUnknown = true
	}
	if a.Result == domain.ActResultPassed && a.Progress != domain.ActProgressCompleted {
		a.Progress = domain.ActProgressCompleted
	}
	return nil
}

func (i *InterviewInput) normalized() error {
	if i.Progress == "" {
		i.Progress = domain.ActProgressAwaiting
	}
	if !domain.ValidActivityProgress(domain.ActivityInterview, i.Progress) {
		return &domain.ValidationError{Code: "invalid_activity_progress", Message: "未知的面试进度"}
	}
	if i.Result == "" {
		i.Result = domain.ActResultUnknown
	}
	if !domain.ValidActivityResult(i.Result) {
		return &domain.ValidationError{Code: "invalid_activity_result", Message: "未知的面试结果"}
	}
	// 「完成面试」不能自动标为通过 (方案 §3.3).
	if i.Progress != domain.ActProgressCompleted && i.Result != domain.ActResultUnknown {
		return &domain.ValidationError{Code: "result_before_completion", Message: "尚未完成的面试不能记录通过 / 未通过结果"}
	}
	if i.Progress == domain.ActProgressCompleted && i.CompletedAt == nil && !i.CompletedUnknown {
		i.CompletedUnknown = true
	}
	return nil
}

// CreateAssessmentTx inserts an OA round inside the caller's transaction.
func (s *Service) CreateAssessmentTx(ctx context.Context, q database.Querier, ownerID, appID int64, in *AssessmentInput) (*actrepo.AssessmentRound, error) {
	if err := in.normalized(); err != nil {
		return nil, err
	}
	round := &actrepo.AssessmentRound{
		ApplicationID: appID, OwnerID: ownerID, Kind: in.Kind, Name: in.Name,
		Progress: in.Progress, Result: in.Result, InvitedAt: in.InvitedAt,
		PlannedAt: in.PlannedAt, DueAt: in.DueAt, CompletedAt: in.CompletedAt,
		CompletedUnknown: in.CompletedUnknown, Link: in.Link, Notes: in.Notes,
	}
	if err := s.acts.CreateAssessment(ctx, q, round); err != nil {
		return nil, err
	}
	return round, nil
}

// CreateInterviewTx inserts an interview round inside the caller's transaction.
func (s *Service) CreateInterviewTx(ctx context.Context, q database.Querier, ownerID, appID int64, in *InterviewInput) (*actrepo.Interview, error) {
	if err := in.normalized(); err != nil {
		return nil, err
	}
	round := &actrepo.Interview{
		ApplicationID: appID, OwnerID: ownerID, RoundName: in.RoundName, Format: in.Format,
		ScheduledAt: in.ScheduledAt, Timezone: in.Timezone, DurationMinutes: in.DurationMinutes,
		Progress: in.Progress, Result: in.Result, InvitedAt: in.InvitedAt,
		CompletedAt: in.CompletedAt, CompletedUnknown: in.CompletedUnknown,
		Feedback: in.Feedback, Notes: in.Notes,
	}
	if err := s.acts.CreateInterview(ctx, q, round); err != nil {
		return nil, err
	}
	return round, nil
}

// progressFromSubstatus maps a concrete progress inside a stage back onto the
// activity round's own (progress, result) pair. 方案 §6.1: the substatus and the
// round are two views of one fact, so the progress dialog and the round's own
// quick actions must not be able to disagree. "" means「不改这一项」——
// 例如「已完成」不携带结果，就不能把已记录的「通过」抹掉。
func progressFromSubstatus(kind, substatus string) (progress, result string) {
	switch kind {
	case domain.ActivityAssessment:
		switch substatus {
		case domain.SubPreparing:
			return domain.ActProgressPreparing, ""
		case domain.SubCompleted:
			return domain.ActProgressCompleted, ""
		case domain.SubPassed:
			return domain.ActProgressCompleted, domain.ActResultPassed
		}
	case domain.ActivityInterview:
		switch substatus {
		case domain.SubAwaitingSchedule:
			return domain.ActProgressAwaiting, ""
		case domain.SubPreparing:
			return domain.ActProgressPreparing, ""
		case domain.SubCompleted:
			return domain.ActProgressCompleted, ""
		}
	}
	return "", ""
}

// applySubstatusToRound keeps the focused round in sync when the caller named a
// concrete progress instead of creating a new round. Only the fields the
// substatus actually asserts are touched: a completion with an unknown result
// never clears a recorded pass/fail, and an already recorded completed_at is
// kept.
func (s *Service) applySubstatusToRound(ctx context.Context, q database.Querier, ownerID, appID int64, kind string, id int64, substatus string) error {
	progress, result := progressFromSubstatus(kind, substatus)
	if progress == "" && result == "" {
		return nil
	}
	switch kind {
	case domain.ActivityAssessment:
		round, err := s.acts.GetAssessment(ctx, q, appID, ownerID, id)
		if err != nil {
			return err
		}
		changed := false
		if progress != "" && round.Progress != progress {
			round.Progress, changed = progress, true
		}
		if result != "" && round.Result != result {
			round.Result, changed = result, true
		}
		if round.Progress == domain.ActProgressCompleted && round.CompletedAt == nil && !round.CompletedUnknown {
			// 历史时间不详时只标「已完成」，不伪造精确时间（方案 §3.2）。
			round.CompletedUnknown, changed = true, true
		}
		if !changed {
			return nil
		}
		return s.acts.UpdateAssessment(ctx, q, round)
	case domain.ActivityInterview:
		round, err := s.acts.GetInterview(ctx, appID, ownerID, id)
		if err != nil {
			return err
		}
		changed := false
		if progress != "" && round.Progress != progress {
			round.Progress, changed = progress, true
		}
		if result != "" && round.Result != result {
			round.Result, changed = result, true
		}
		if round.Progress == domain.ActProgressCompleted && round.CompletedAt == nil && !round.CompletedUnknown {
			round.CompletedUnknown, changed = true, true
		}
		if !changed {
			return nil
		}
		return s.acts.UpdateInterview(ctx, q, round)
	}
	return nil
}

// substatusOfRound loads the round's current progress/result and derives the
// matching substatus, so a transition that only points at an existing round
// still lands on the right concrete progress.
func (s *Service) substatusOfRound(ctx context.Context, q database.Querier, ownerID, appID int64, kind string, id int64) (string, error) {
	switch kind {
	case domain.ActivityAssessment:
		round, err := s.acts.GetAssessment(ctx, q, appID, ownerID, id)
		if err != nil {
			return "", err
		}
		return domain.SubstatusForActivity(domain.ActivityAssessment, round.Progress, round.Result), nil
	case domain.ActivityInterview:
		round, err := s.acts.GetInterview(ctx, appID, ownerID, id)
		if err != nil {
			return "", err
		}
		return domain.SubstatusForActivity(domain.ActivityInterview, round.Progress, round.Result), nil
	}
	return "", nil
}

// SyncActivityProgress pushes an activity round's derived substatus into the
// application snapshot. Used by the activity endpoints (PATCH /complete), so
// editing a round and recording progress are one command, not two (方案 §6.1).
func (s *Service) SyncActivityProgress(ctx context.Context, q database.Querier, ownerID, appID int64, kind string, activityID int64, progress, result string) error {
	return s.repo.SyncFromActivity(ctx, q, ownerID, appID, kind, activityID,
		domain.SubstatusForActivity(kind, progress, result))
}

// SetFocusActivity points the application at one round without changing the
// stage (方案 §3.3: 列表主标签展示用户选定的关注阶段).
func (s *Service) SetFocusActivity(ctx context.Context, ownerID, appID int64, kind string, activityID *int64, version int) (*apprepo.Row, error) {
	if kind != "" && !domain.ValidActivityKind(kind) {
		return nil, &domain.ValidationError{Code: "invalid_activity_kind", Message: "未知的活动类型"}
	}
	out := &apprepo.Row{}
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		row, err := s.repo.GetForUpdate(ctx, tx, ownerID, appID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if version != 0 && row.Version != version {
			return ErrVersionConflict
		}
		if kind != "" && apprepo.StageForActivityKind(kind) != row.Status {
			return &domain.ValidationError{Code: "activity_stage_mismatch", Message: "活动类型与当前阶段不匹配"}
		}
		if err := s.repo.SetFocus(ctx, tx, ownerID, appID, kind, activityID); err != nil {
			return err
		}
		row.FocusActivityKind, row.FocusActivityID = kind, activityID
		row.Version++
		out = row
		return nil
	})
	return out, err
}

// CorrectCurrentInput repairs a mis-recorded stage. It rewrites the LAST
// effective status event (the one that produced the wrong current progress)
// and keeps the original row for audit (方案 §4.2 「之前选错了」).
type CorrectCurrentInput struct {
	ToStatus    string     `json:"to_status"`
	ToSubstatus string     `json:"to_substatus"`
	Reason      string     `json:"reason"`
	OccurredAt  *time.Time `json:"occurred_at"`
	Version     int        `json:"version"`
	// CorrectedEventID targets a specific event instead of the last one.
	CorrectedEventID *int64 `json:"corrected_event_id"`
}

// CorrectCurrent records a correction against the current (or a named) status
// event and recomputes the snapshot from the effective timeline.
func (s *Service) CorrectCurrent(ctx context.Context, ownerID, appID int64, in *CorrectCurrentInput) (*apprepo.Row, error) {
	if !domain.ValidStatus(in.ToStatus) {
		return nil, &domain.ValidationError{Code: "invalid_status", Message: "未知状态"}
	}
	if in.ToSubstatus != "" && !domain.ValidSubstatus(in.ToStatus, in.ToSubstatus) {
		return nil, &domain.ValidationError{Code: "invalid_substatus", Message: "阶段与子状态不匹配"}
	}
	out := &apprepo.Row{}
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		row, err := s.repo.GetForUpdate(ctx, tx, ownerID, appID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if in.Version != 0 && row.Version != in.Version {
			return ErrVersionConflict
		}
		events, err := s.repo.ListEvents(ctx, tx, appID, ownerID)
		if err != nil {
			return err
		}
		target, err := s.pickCorrectableEvent(ctx, tx, appID, ownerID, events, in.CorrectedEventID)
		if err != nil {
			return err
		}
		occ := target.OccurredAt
		if in.OccurredAt != nil {
			occ = *in.OccurredAt
		}
		if err := s.validateEffectiveTimeline(ctx, tx, appID, ownerID, events, target.ID, in.ToStatus, in.ToSubstatus, in.Reason); err != nil {
			return err
		}
		ev := &apprepo.Event{
			ApplicationID: appID, OwnerID: ownerID, EventType: "correction",
			FromStatus: target.ToStatus, ToStatus: &in.ToStatus,
			FromSubstatus: target.ToSubstatus, ToSubstatus: nullStr(in.ToSubstatus),
			ActivityKind: target.ActivityKind, ActivityID: target.ActivityID,
			Reason: in.Reason, OccurredAt: occ, CorrectsEventID: &target.ID, ActorID: &ownerID,
		}
		if err := s.repo.InsertEvent(ctx, tx, ev); err != nil {
			return err
		}
		if err := s.repo.ResyncStatusFromEvents(ctx, tx, appID, ownerID); err != nil {
			return err
		}
		// Read the recomputed snapshot through the SAME transaction. Going
		// through the pool would hit another connection and return the
		// pre-correction row (the write is still uncommitted), which is how a
		// 「更正成功了但进度没变」 bug hides.
		fresh, err := s.repo.GetForUpdate(ctx, tx, ownerID, appID)
		if err != nil {
			return err
		}
		out = fresh
		return nil
	})
	return out, err
}

// pickCorrectableEvent chooses the event a correction should point at: either
// an explicitly named one, or the last status-changing event that no correction
// has replaced yet.
func (s *Service) pickCorrectableEvent(ctx context.Context, q database.Querier, appID, ownerID int64, events []*apprepo.Event, want *int64) (*apprepo.Event, error) {
	if want != nil {
		for _, ev := range events {
			if ev.ID == *want && ev.EventType != "correction" {
				return ev, nil
			}
		}
		return nil, &domain.ValidationError{Code: "correction_target_missing", Message: "找不到要更正的记录"}
	}
	corrected := map[int64]bool{}
	for _, ev := range events {
		if ev.EventType == "correction" && ev.CorrectsEventID != nil {
			corrected[*ev.CorrectsEventID] = true
		}
	}
	var pick *apprepo.Event
	for _, ev := range events {
		if ev.EventType == "correction" || ev.ToStatus == nil || corrected[ev.ID] {
			continue
		}
		if pick == nil || ev.Sequence > pick.Sequence {
			pick = ev
		}
	}
	if pick == nil {
		return nil, &domain.ValidationError{Code: "correction_target_missing", Message: "找不到要更正的记录"}
	}
	return pick, nil
}

// validateEffectiveTimeline replays the effective history with one event
// overridden and refuses a result that the state model would not have produced
// (方案 §4.3: 更正的验证和快照重算都必须先应用已有更正).
func (s *Service) validateEffectiveTimeline(ctx context.Context, q database.Querier, appID, ownerID int64, events []*apprepo.Event, overrideID int64, status, substatus, reason string) error {
	type effective struct {
		status    string
		substatus string
	}
	overrides := map[int64]effective{overrideID: {status: status, substatus: substatus}}
	corrected := map[int64]effective{}
	for _, ev := range events {
		if ev.EventType == "correction" && ev.CorrectsEventID != nil && ev.ToStatus != nil {
			st := ""
			if ev.ToSubstatus != nil {
				st = *ev.ToSubstatus
			}
			corrected[*ev.CorrectsEventID] = effective{status: *ev.ToStatus, substatus: st}
		}
	}
	ordered := append([]*apprepo.Event(nil), events...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j-1].Sequence > ordered[j].Sequence; j-- {
			ordered[j-1], ordered[j] = ordered[j], ordered[j-1]
		}
	}
	curStatus, curSub := "", ""
	for _, ev := range ordered {
		if ev.EventType == "correction" {
			continue
		}
		var eff effective
		if o, ok := overrides[ev.ID]; ok {
			eff = o
		} else if c, ok := corrected[ev.ID]; ok {
			eff = c
		} else if ev.ToStatus != nil {
			eff = effective{status: *ev.ToStatus}
			if ev.ToSubstatus != nil {
				eff.substatus = *ev.ToSubstatus
			}
		} else {
			continue
		}
		if curStatus != "" && curStatus != eff.status {
			if err := domain.ValidateTransition(domain.Transition{
				FromStatus: curStatus, FromSubstatus: curSub,
				ToStatus: eff.status, ToSubstatus: eff.substatus,
				OccurredAt: ev.OccurredAt, Now: time.Now().Add(time.Hour),
				WasSubmitted: true, HadOffer: true, Reason: reason,
			}); err != nil {
				return &domain.ValidationError{Code: "correction_invalid", Message: "更正后的时间线不合法: " + err.Error()}
			}
		}
		curStatus, curSub = eff.status, eff.substatus
	}
	return nil
}

// StatusModel exposes the single whitelist the frontend renders (方案 §6.5).
func (s *Service) StatusModel() domain.StatusModel { return domain.StatusModelFor() }
