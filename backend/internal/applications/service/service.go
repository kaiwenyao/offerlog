// Package service implements application use-cases: create/update, status
// transitions, corrections and soft delete/restore, all inside transactions
// that also write the event audit trail.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	actrepo "offerlog/backend/internal/activities/repository"
	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/applications/repository"
	"offerlog/backend/internal/platform/database"
)

var ErrNotFound = errors.New("not found")

// ErrVersionConflict maps to HTTP 409 (optimistic locking).
var ErrVersionConflict = errors.New("version conflict")

// CreateInput carries the minimal fields accepted on creation plus optional
// advanced fields (plan §4.3 "新增岗位：先只要求公司和岗位，链接可选").
type CreateInput struct {
	CompanyName    string
	Position       string
	JobURL         string
	Location       string
	RemotePolicy   string
	EmploymentType string
	SalaryMin      *int64
	SalaryMax      *int64
	SalaryCurrency string
	Channel        string
	Status         string
	Priority       string
	Tags           []string
	Deadline       *time.Time
	Notes          string
	SubmittedAt    *time.Time
	SavedAt        *time.Time
	// Substatus refines Status inside the stage at creation time (optional).
	Substatus string
}

type UpdateInput struct {
	CompanyID       *int64
	CompanyName     *string
	Position        *string
	JobURL          *string
	Location        *string
	RemotePolicy    *string
	Channel         *string
	Priority        *string
	Tags            []string
	Deadline        *time.Time
	Notes           *string
	NextAction      *string
	NextActionDueAt *time.Time
	CustomValues    map[string]any
	Version         int
}

type Service struct {
	db   *database.DB
	repo *repository.Repo
	// acts gives the service access to interview / assessment rounds so a
	// transition and the round it references are written in one transaction
	// (方案 §6.3). Optional: nil-safe for tests that only exercise status.
	acts *actrepo.Repo
}

func New(db *database.DB, repo *repository.Repo) *Service { return &Service{db: db, repo: repo} }

// WithActivities attaches the activities repository (interviews, OA rounds).
func (s *Service) WithActivities(acts *actrepo.Repo) *Service {
	s.acts = acts
	return s
}

func (s *Service) Repo() *repository.Repo { return s.repo }

func normalizeCreate(in *CreateInput) error {
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	in.Position = strings.TrimSpace(in.Position)
	if in.CompanyName == "" {
		return &domain.ValidationError{Code: "company_required", Message: "请填写公司名称"}
	}
	if in.Position == "" {
		return &domain.ValidationError{Code: "position_required", Message: "请填写岗位名称"}
	}
	if in.Status == "" {
		in.Status = domain.StatusSaved
	}
	if !domain.ValidStatus(in.Status) {
		return &domain.ValidationError{Code: "invalid_status", Message: "未知状态"}
	}
	if in.Substatus != "" && !domain.ValidSubstatus(in.Status, in.Substatus) {
		return &domain.ValidationError{Code: "invalid_substatus", Message: "阶段与子状态不匹配"}
	}
	if in.Priority == "" {
		in.Priority = "medium"
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	if in.SavedAt == nil {
		now := time.Now()
		in.SavedAt = &now
	}
	// Snapshot time semantics (same rules the transition machine enforces):
	// pre-submission statuses carry no submitted_at, while statuses at or
	// beyond 已投递 need submission evidence — default it to the saved time so
	// the created event's business time is never a silent server clock.
	if in.Status == domain.StatusSaved || in.Status == domain.StatusPreparing {
		in.SubmittedAt = nil
	} else if in.SubmittedAt == nil {
		t := *in.SavedAt
		in.SubmittedAt = &t
	}
	if in.SubmittedAt != nil && in.SubmittedAt.After(time.Now().Add(5*time.Minute)) {
		return &domain.ValidationError{Code: "future_occurred_at", Message: "投递时间不能晚于现在"}
	}
	return nil
}

// insertAppliedEvent records 投递 as a first-class timeline row at the time the
// user actually submitted, rather than leaving it to live only in the
// applications.submitted_at column. Without it the 阶段轨迹 (which patches the
// applied day in from the snapshot) and the 时间线 (which has no such patch)
// disagree, and the user sees a write-clock timestamp where they typed a date.
func (s *Service) insertAppliedEvent(ctx context.Context, tx pgx.Tx, appID, ownerID int64, from string, at time.Time) error {
	return s.repo.InsertEvent(ctx, tx, &repository.Event{
		ApplicationID: appID, OwnerID: ownerID, EventType: "status_change",
		FromStatus: ptrString(from), ToStatus: ptrString(domain.StatusApplied),
		OccurredAt: at, ActorID: &ownerID,
	})
}

// Create creates an application together with its initial event.
func (s *Service) Create(ctx context.Context, ownerID int64, in *CreateInput) (*repository.Row, error) {
	// Captured BEFORE normalizeCreate defaults it to saved_at: only a time the
	// user actually typed earns its own 投递 event.
	userSubmitted := in.SubmittedAt
	if err := normalizeCreate(in); err != nil {
		return nil, err
	}
	// validate status-specific fields now: submitted only allowed if not
	// pre-submission stage and in preparing statuses saved/preparing must not
	// carry submitted_at.
	out := &repository.Row{}
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// company: reuse by (owner,name), otherwise create
		company, err := s.repo.FindCompany(ctx, ownerID, in.CompanyName)
		if errors.Is(err, repository.ErrNoRows) {
			company = &repository.Company{OwnerID: ownerID, Name: in.CompanyName}
			if err := s.repo.CreateCompany(ctx, tx, company); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		now := time.Now()
		row := &repository.Row{
			OwnerID: ownerID, CompanyID: company.ID, CompanyName: in.CompanyName,
			Position: in.Position, JobURL: in.JobURL, Location: in.Location,
			RemotePolicy: in.RemotePolicy, EmploymentType: in.EmploymentType,
			SalaryMin: in.SalaryMin, SalaryMax: in.SalaryMax, SalaryCurrency: in.SalaryCurrency,
			Channel: in.Channel, Status: in.Status, Substatus: in.Substatus, Priority: in.Priority,
			Tags: in.Tags, Notes: in.Notes, Deadline: in.Deadline,
			SavedAt: in.SavedAt, SubmittedAt: in.SubmittedAt,
			CustomValues: json.RawMessage(`{}`), Version: 1, CreatedAt: now,
		}
		id, err := s.repo.Create(ctx, tx, row)
		if err != nil {
			return err
		}
		row.ID = id
		out = row

		// Created straight into 已投递 with a real 投递时间: that submission is a
		// separate business fact and gets its own row. Restricted to exactly
		// applied so the event replay (which walks to_status by sequence) still
		// lands on the status the record was created with.
		emitApplied := userSubmitted != nil && row.Status == domain.StatusApplied
		// 建档 means "I started tracking this", so its business time is the
		// creation instant. It used to borrow submitted_at, which made the row
		// claim the user created the record on the day they had applied.
		//
		// When the submission gets its own row, 建档 must describe the state
		// BEFORE it: the replay takes submitted_at from the first event whose
		// effective status is applied, so a 建档 row also claiming applied would
		// hand it the creation clock and silently overwrite the backfilled 投递
		// 时间 that feeds analytics and reminders.
		createdTo := row.Status
		if emitApplied {
			createdTo = domain.StatusSaved
		}
		ev := &repository.Event{
			ApplicationID: id, OwnerID: ownerID, EventType: "created",
			FromStatus: nil, ToStatus: &createdTo, OccurredAt: *in.SavedAt,
		}
		if err := s.repo.InsertEvent(ctx, tx, ev); err != nil {
			return err
		}
		if emitApplied {
			return s.insertAppliedEvent(ctx, tx, id, ownerID, domain.StatusSaved, *userSubmitted)
		}
		return nil
	})
	return out, err
}

func (s *Service) Get(ctx context.Context, ownerID, id int64, includeDeleted bool) (*repository.Row, error) {
	row, err := s.repo.GetByID(ctx, ownerID, id, includeDeleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return row, err
}

// Update updates plain mutable fields with optimistic locking.
func (s *Service) Update(ctx context.Context, ownerID, id int64, in *UpdateInput) (*repository.Row, error) {
	out := &repository.Row{}
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		row, err := s.repo.GetForUpdate(ctx, tx, ownerID, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.Version != in.Version {
			return ErrVersionConflict
		}
		// apply changes
		if in.CompanyName != nil {
			name := strings.TrimSpace(*in.CompanyName)
			if name == "" {
				return &domain.ValidationError{Code: "company_required", Message: "公司名称不能为空"}
			}
			company, cerr := s.repo.FindCompany(ctx, ownerID, name)
			if errors.Is(cerr, repository.ErrNoRows) {
				company = &repository.Company{OwnerID: ownerID, Name: name}
				if err := s.repo.CreateCompany(ctx, tx, company); err != nil {
					return err
				}
			} else if cerr != nil {
				return cerr
			}
			row.CompanyID = company.ID
			row.CompanyName = name
		}
		if in.Position != nil {
			p := strings.TrimSpace(*in.Position)
			if p == "" {
				return &domain.ValidationError{Code: "position_required", Message: "岗位名称不能为空"}
			}
			row.Position = p
		}
		setStr := func(dst *string, v *string) {
			if v != nil {
				*dst = *v
			}
		}
		setStr(&row.JobURL, in.JobURL)
		setStr(&row.Location, in.Location)
		setStr(&row.RemotePolicy, in.RemotePolicy)
		setStr(&row.Channel, in.Channel)
		if in.Priority != nil {
			row.Priority = *in.Priority
		}
		if in.Tags != nil {
			row.Tags = in.Tags
		}
		if in.Deadline != nil {
			row.Deadline = in.Deadline
		}
		if in.Notes != nil {
			row.Notes = *in.Notes
		}
		if in.NextAction != nil {
			row.NextAction = *in.NextAction
		}
		if in.NextActionDueAt != nil {
			row.NextActionDueAt = in.NextActionDueAt
		}
		if in.CustomValues != nil {
			b, err := json.Marshal(in.CustomValues)
			if err != nil {
				return err
			}
			row.CustomValues = b
		}
		if _, err := s.repo.UpdateFields(ctx, tx, row); err != nil {
			return err
		}
		row.Version++ // reflect the DB increment for the caller
		out = row
		return nil
	})
	return out, err
}

// TransitionInput is the request for a status change.
type TransitionInput struct {
	ToStatus        string     `json:"to_status"`
	ToSubstatus     string     `json:"to_substatus"`
	OccurredAt      *time.Time `json:"occurred_at"`
	Reason          string     `json:"reason"`
	Note            string     `json:"note"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	FirstResponseAt *time.Time `json:"first_response_at"`
	// NoFormalSubmission lets a record enter a recruiter-driven status without a
	// submitted_at (内推 / 猎头直接约面). It is a per-request assertion, not a
	// stored column: submitted_at stays NULL so 投递→回复 analytics stay honest.
	NoFormalSubmission bool   `json:"no_formal_submission"`
	Version            int    `json:"version"`
	IdempotencyKey     string `json:"idempotency_key"`
	// ChangeType records WHY the change happened. "" derives advance /
	// rollback / reopen from the flow order; "rollback" asserts 流程实际退回.
	// "correct" is refused here — a mistake is repaired through
	// CorrectCurrent so the wrong stage keeps an audit trail (方案 §4.2).
	ChangeType string `json:"change_type"`
	// FocusActivity points the stage at one specific round; ClearFocus removes
	// the reference. Both are optional.
	FocusActivityKind string `json:"focus_activity_kind"`
	FocusActivityID   *int64 `json:"focus_activity_id"`
	ClearFocus        bool   `json:"clear_focus"`
	// Assessment / Interview record a new round in the SAME transaction as the
	// transition (方案 §6.3), so 切到准备 OA 顺手记一条轮次 cannot half-apply.
	Assessment *AssessmentInput `json:"assessment"`
	Interview  *InterviewInput  `json:"interview"`
}

func sameInt64Ptr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Transition applies a status change, writes an event and updates the snapshot
// atomically. Idempotency keys prevent duplicate events on retried requests.
func (s *Service) Transition(ctx context.Context, ownerID, id int64, in *TransitionInput) (*repository.Row, error) {
	if in.ToStatus == "" {
		return nil, &domain.ValidationError{Code: "to_status_required", Message: "缺少目标状态"}
	}
	if in.ChangeType == "correct" {
		return nil, &domain.ValidationError{
			Code:    "use_correction",
			Message: "「之前选错了」请使用更正流程，以便保留误操作的审计记录",
		}
	}
	switch in.ChangeType {
	case "", domain.ChangeAdvance, domain.ChangeRollback, domain.ChangeReopen:
	default:
		return nil, &domain.ValidationError{Code: "invalid_change_type", Message: "未知的变更类型"}
	}
	if in.IdempotencyKey != "" {
		done, existing, err := s.repo.RunIdempotentGuard(ctx, ownerID, id, in.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if done {
			return existing, nil
		}
	}
	out := &repository.Row{}
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		row, err := s.repo.GetForUpdate(ctx, tx, ownerID, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.Version != in.Version {
			return ErrVersionConflict
		}
		now := time.Now()
		occ := now
		if in.OccurredAt != nil {
			occ = *in.OccurredAt
		} else if in.SubmittedAt != nil && in.ToStatus == domain.StatusApplied {
			// 回填场景：进入「已投递」时只填了实际投递时间、没填发生时间 → 以
			// 投递时间为准，而不是数据库写入的 now（今天补录昨天投递，事件应
			// 发生在昨天）。推进到后续阶段时发生时间留空仍默认现在。
			occ = *in.SubmittedAt
		}

		// Resolve the concrete progress inside the target stage (方案 §6.8):
		// an older client that only sends `status` keeps the existing substatus
		// when the stage does not change, and never carries it across stages.
		toSub := in.ToSubstatus
		if toSub == "" && in.ToStatus == row.Status {
			toSub = row.Substatus
		}
		if toSub != "" && !domain.ValidSubstatus(in.ToStatus, toSub) {
			return &domain.ValidationError{Code: "invalid_substatus", Message: "阶段与子状态不匹配"}
		}

		// Focus reference resolution.
		focusKind, focusID := row.FocusActivityKind, row.FocusActivityID
		if in.ClearFocus {
			focusKind, focusID = "", nil
		}
		if in.FocusActivityKind != "" {
			if !domain.ValidActivityKind(in.FocusActivityKind) {
				return &domain.ValidationError{Code: "invalid_activity_kind", Message: "未知的活动类型"}
			}
			if repository.StageForActivityKind(in.FocusActivityKind) != in.ToStatus {
				return &domain.ValidationError{Code: "activity_stage_mismatch", Message: "活动类型与目标阶段不匹配"}
			}
			focusKind, focusID = in.FocusActivityKind, in.FocusActivityID
			// The named round must actually exist and belong to this application:
			// an arbitrary ID would otherwise be stored as the focus and either
			// 500 the substatus mirror (applySubstatusToRound surfaces pgx.ErrNoRows)
			// or leave a dangling reference the next transition trips over.
			if focusID != nil {
				switch in.FocusActivityKind {
				case domain.ActivityAssessment:
					if _, err := s.acts.GetAssessment(ctx, tx, id, ownerID, *focusID); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return &domain.ValidationError{Code: "activity_not_found", Message: "关注的测评轮次不存在"}
						}
						return err
					}
				case domain.ActivityInterview:
					if _, err := s.acts.GetInterview(ctx, tx, id, ownerID, *focusID); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return &domain.ValidationError{Code: "activity_not_found", Message: "关注的面试轮次不存在"}
						}
						return err
					}
				}
			}
		}

		// A new round may be recorded inline with the transition.
		if in.Assessment != nil {
			if in.ToStatus != domain.StatusAssessment {
				return &domain.ValidationError{Code: "activity_stage_mismatch", Message: "只有 OA / 作业阶段可以新增测评轮次"}
			}
			round, err := s.CreateAssessmentTx(ctx, tx, ownerID, id, in.Assessment)
			if err != nil {
				return err
			}
			focusKind, focusID = domain.ActivityAssessment, &round.ID
			if toSub == "" {
				toSub = domain.SubstatusForActivity(domain.ActivityAssessment, round.Progress, round.Result)
			}
		}
		if in.Interview != nil {
			if in.ToStatus != domain.StatusInterviewing {
				return &domain.ValidationError{Code: "activity_stage_mismatch", Message: "只有面试阶段可以新增面试轮次"}
			}
			round, err := s.CreateInterviewTx(ctx, tx, ownerID, id, in.Interview)
			if err != nil {
				return err
			}
			focusKind, focusID = domain.ActivityInterview, &round.ID
			if toSub == "" {
				toSub = domain.SubstatusForActivity(domain.ActivityInterview, round.Progress, round.Result)
			}
		}

		// Leaving the focused activity's stage drops the reference: a record in
		// 待投递 has no current round.
		if focusKind != "" && repository.StageForActivityKind(focusKind) != in.ToStatus {
			focusKind, focusID = "", nil
		}
		// Derive the substatus from the focused round when the caller did not
		// name one (方案 §6.1: 子状态由选中的活动进度派生). A missing round only
		// means "nothing derivable" (a legacy dangling reference); a real DB
		// error must surface, not be silently read as 没这轮.
		if toSub == "" && focusKind != "" && focusID != nil {
			st, err := s.substatusOfRound(ctx, tx, ownerID, id, focusKind, *focusID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			toSub = st
		}
		// 反向同步（方案 §6.1）：用户直接选了具体进度（例如「已完成 OA」）而没有
		// 新建轮次时，要把关注的那一轮也改成同一事实，否则申请说已完成、轮次还
		// 说准备中，下一次派生又会把它拉回去。
		if in.Assessment == nil && in.Interview == nil && toSub != "" && focusKind != "" && focusID != nil {
			if err := s.applySubstatusToRound(ctx, tx, ownerID, id, focusKind, *focusID, toSub); err != nil {
				// A dangling legacy focus (round deleted before delete cleanup
				// existed) means there is nothing to sync — not a 500.
				if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			}
		}

		focusChanged := focusKind != row.FocusActivityKind || !sameInt64Ptr(focusID, row.FocusActivityID)
		if row.Status == in.ToStatus && row.Substatus == toSub && !focusChanged {
			return &domain.ValidationError{Code: "same_status", Message: "进度未变化"}
		}

		// Skip-ahead (待投递 → 笔试作业) supplies 投递时间 for a stage that is not
		// itself the submission. That time used to vanish into the snapshot
		// column while the event took the write clock, so the trail said 前天 and
		// the timeline said 今天. Give the submission its own row instead.
		backfillApplied := in.SubmittedAt != nil && row.SubmittedAt == nil &&
			in.ToStatus != domain.StatusApplied && !domain.IsPreparing(in.ToStatus)
		hadOffer, err := s.repo.HadOffer(ctx, tx, id)
		if err != nil {
			return err
		}
		wasSubmitted := row.SubmittedAt != nil
		if in.SubmittedAt != nil {
			wasSubmitted = true
		}
		changeType := in.ChangeType
		if changeType == "" {
			changeType = domain.DeriveChangeType(row.Status, in.ToStatus)
		}
		err = domain.ValidateTransition(domain.Transition{
			FromStatus: row.Status, FromSubstatus: row.Substatus,
			ToStatus: in.ToStatus, ToSubstatus: toSub,
			ChangeType: changeType, FocusChanged: focusChanged,
			OccurredAt: occ, Now: now,
			WasSubmitted: wasSubmitted, HadOffer: hadOffer, SkipSubmission: in.NoFormalSubmission,
			Reason: in.Reason, Note: in.Note,
		})
		if err != nil {
			return err
		}

		// derive new snapshot fields from the target status
		newRow := *row
		newRow.Status = in.ToStatus
		newRow.Substatus = toSub
		newRow.FocusActivityKind = focusKind
		newRow.FocusActivityID = focusID
		if in.Reason != "" || row.Status != in.ToStatus {
			newRow.Reason = in.Reason
		}
		if in.SubmittedAt != nil {
			newRow.SubmittedAt = in.SubmittedAt
		}
		if in.FirstResponseAt != nil {
			newRow.FirstResponseAt = in.FirstResponseAt
		}
		// 方案 §4.2: a rollback does NOT erase history. submitted_at and
		// first_response_at stay exactly as they were — the submission and the
		// first reply really happened, even when the process walks back. Only
		// the explicit correction flow may retire them.
		if in.ToStatus == domain.StatusAccepted {
			// record accept time
			if newRow.AcceptedAt == nil {
				at := occ
				newRow.AcceptedAt = &at
			}
			// accepting without a prior offer event: also write a synthesized
			// offer event on the same sequence as history (补录收到 Offer).
			if !hadOffer {
				offerEv := &repository.Event{
					ApplicationID: id, OwnerID: ownerID, EventType: "status_change",
					FromStatus: ptrString(domain.StatusOffer), ToStatus: ptrString(domain.StatusOffer),
					Note: "接受时补录的 Offer 记录", Reason: in.Reason,
					OccurredAt: occ.Add(-time.Minute), ActorID: &ownerID,
				}
				if err := s.repo.InsertEvent(ctx, tx, offerEv); err != nil {
					return err
				}
			}
		}
		if in.ToStatus == domain.StatusRejected && newRow.RejectedAt == nil {
			at := occ
			newRow.RejectedAt = &at
		}
		// NOTE: entering 已投递 / 后续阶段 never fabricates a submitted_at here.
		// 方案 §4.1: 接受 Offer 仍需要真实 Offer 记录或显式补录，不能自动捏造；
		// the missing-submission rule is enforced by ValidateTransition instead.
		if err := s.repo.SetStatus(ctx, tx, &newRow); err != nil {
			return err
		}
		// The synthesized 投递 row goes first so the sequence replay walks
		// from → applied → to_status; every hop is a legal edge.
		from := row.Status
		if backfillApplied {
			if err := s.insertAppliedEvent(ctx, tx, id, ownerID, from, *in.SubmittedAt); err != nil {
				return err
			}
			from = domain.StatusApplied
		}
		ev := &repository.Event{
			ApplicationID: id, OwnerID: ownerID, EventType: "status_change",
			FromStatus: ptrString(from), ToStatus: ptrString(in.ToStatus),
			FromSubstatus: nullStr(row.Substatus), ToSubstatus: nullStr(toSub),
			ActivityKind: nullStr(focusKind), ActivityID: focusID,
			ChangeType: changeType,
			Note:       in.Note, Reason: in.Reason, OccurredAt: occ, ActorID: &ownerID,
		}
		if err := s.repo.InsertEvent(ctx, tx, ev); err != nil {
			return err
		}
		if in.IdempotencyKey != "" {
			if err := s.repo.RecordIdempotency(ctx, tx, ownerID, id, in.IdempotencyKey, in.ToStatus); err != nil {
				return err
			}
		}
		newRow.Version++
		out = &newRow
		return nil
	})
	return out, err
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func ptrString(s string) *string { return &s }

// Events lists the timeline for an application.
func (s *Service) Events(ctx context.Context, ownerID, id int64) ([]*repository.Event, error) {
	evs, err := s.repo.ListEvents(ctx, s.db.Pool(), id, ownerID)
	return evs, err
}

// CorrectionInput rewrites history with an audit trail: the corrected event is
// linked via corrects_event_id and a compensating event is appended.
type CorrectionInput struct {
	EventID   int64
	NewStatus string
	// NewSubstatus optionally also repairs the concrete progress (方案 §6.3:
	// 更正接口扩展可更正的子状态).
	NewSubstatus string
	OccurredAt   time.Time
	Reason       string
	Version      int
}

// Correct validates the resulting timeline then records a correction event.
//
// An omitted OccurredAt must stay omitted: CorrectCurrent reads nil as 「时间
// 没错，只改状态」 and keeps the corrected event's own business time. Passing
// &in.OccurredAt unconditionally handed it the zero Time instead, stamping the
// correction with year 1 — invisible while the replay walked events in
// insertion order, and a wrong current status the moment it walks them in
// business-time order (迁移 00006).
func (s *Service) Correct(ctx context.Context, ownerID, appID int64, in *CorrectionInput) error {
	var occ *time.Time
	if !in.OccurredAt.IsZero() {
		t := in.OccurredAt
		occ = &t
	}
	_, err := s.CorrectCurrent(ctx, ownerID, appID, &CorrectCurrentInput{
		ToStatus: in.NewStatus, ToSubstatus: in.NewSubstatus,
		Reason: in.Reason, OccurredAt: occ, Version: in.Version,
		CorrectedEventID: &in.EventID,
	})
	return err
}

// SetDeleted / Restore / Archive implement trash & archive semantics.
func (s *Service) SoftDelete(ctx context.Context, ownerID, id int64) error {
	return s.setFlag(ctx, ownerID, id, "deleted", true)
}
func (s *Service) Restore(ctx context.Context, ownerID, id int64) error {
	return s.setFlag(ctx, ownerID, id, "deleted", false)
}
func (s *Service) Archive(ctx context.Context, ownerID, id int64, archived bool) error {
	return s.setFlag(ctx, ownerID, id, "archived", archived)
}
func (s *Service) setFlag(ctx context.Context, ownerID, id int64, which string, val bool) error {
	return s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := s.repo.GetForUpdate(ctx, tx, ownerID, id); err != nil {
			return ErrNotFound
		}
		var err error
		if which == "deleted" {
			err = s.repo.SetDeleted(ctx, tx, ownerID, id, val)
		} else {
			err = s.repo.SetArchived(ctx, tx, ownerID, id, val)
		}
		return err
	})
}

// List delegates to the repository with pagination.
func (s *Service) List(ctx context.Context, ownerID int64, o repository.ListOptions) ([]*repository.Row, int64, error) {
	return s.repo.List(ctx, ownerID, o)
}

// Bulk applies shared batch operations (tags/priority/archive) to the given
// rows, skipping records that do not belong to the owner.
func (s *Service) Bulk(ctx context.Context, ownerID int64, ids []int64, addTags []string, priority *string, archive *bool) (int64, error) {
	updated := int64(0)
	err := s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, id := range ids {
			row, err := s.repo.GetForUpdate(ctx, tx, ownerID, id)
			if errors.Is(err, pgx.ErrNoRows) {
				continue // not owned / missing: skip silently
			}
			if err != nil {
				return err
			}
			if len(addTags) > 0 {
				have := map[string]bool{}
				for _, t := range row.Tags {
					have[t] = true
				}
				for _, t := range addTags {
					if !have[t] {
						row.Tags = append(row.Tags, t)
						have[t] = true
					}
				}
			}
			if priority != nil {
				row.Priority = *priority
			}
			if _, err := s.repo.UpdateFields(ctx, tx, row); err != nil {
				return err
			}
			if archive != nil {
				if err := s.repo.SetArchived(ctx, tx, ownerID, id, *archive); err != nil {
					return err
				}
			}
			updated++
		}
		return nil
	})
	return updated, err
}
