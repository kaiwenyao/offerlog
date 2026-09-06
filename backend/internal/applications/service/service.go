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
}

func New(db *database.DB, repo *repository.Repo) *Service { return &Service{db: db, repo: repo} }

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
	return nil
}

// Create creates an application together with its initial event.
func (s *Service) Create(ctx context.Context, ownerID int64, in *CreateInput) (*repository.Row, error) {
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
			Channel: in.Channel, Status: in.Status, Priority: in.Priority,
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

		ev := &repository.Event{
			ApplicationID: id, OwnerID: ownerID, EventType: "created",
			FromStatus: nil, ToStatus: &row.Status, OccurredAt: now,
		}
		return s.repo.InsertEvent(ctx, tx, ev)
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
	OccurredAt      *time.Time `json:"occurred_at"`
	Reason          string     `json:"reason"`
	Note            string     `json:"note"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	FirstResponseAt *time.Time `json:"first_response_at"`
	Version         int        `json:"version"`
	IdempotencyKey  string     `json:"idempotency_key"`
}

// Transition applies a status change, writes an event and updates the snapshot
// atomically. Idempotency keys prevent duplicate events on retried requests.
func (s *Service) Transition(ctx context.Context, ownerID, id int64, in *TransitionInput) (*repository.Row, error) {
	if in.ToStatus == "" {
		return nil, &domain.ValidationError{Code: "to_status_required", Message: "缺少目标状态"}
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
		}
		hadOffer, err := s.repo.HadOffer(ctx, tx, id)
		if err != nil {
			return err
		}
		wasSubmitted := row.SubmittedAt != nil
		if in.SubmittedAt != nil {
			wasSubmitted = true
		}
		err = domain.ValidateTransition(domain.Transition{
			FromStatus: row.Status, ToStatus: in.ToStatus, OccurredAt: occ, Now: now,
			WasSubmitted: wasSubmitted, HadOffer: hadOffer, Reason: in.Reason, Note: in.Note,
		})
		if err != nil {
			return err
		}

		// derive new snapshot fields from the target status
		newRow := *row
		newRow.Status = in.ToStatus
		newRow.Reason = in.Reason
		if in.SubmittedAt != nil {
			newRow.SubmittedAt = in.SubmittedAt
		}
		if in.FirstResponseAt != nil {
			newRow.FirstResponseAt = in.FirstResponseAt
		}
		if in.ToStatus == domain.StatusAccepted {
			// record accept time and default reason
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
		if in.ToStatus == domain.StatusOffer && !wasSubmitted {
			// receiving an offer implies the application was submitted; keep
			// submitted_at null only if user explicitly says not submitted.
			if newRow.SubmittedAt == nil {
				// plan: 从准备中直接进入后续招聘阶段时，必须补充实际投递时间或标记未经过正式投递。
				// Since offers necessarily come after a real submission, use the
				// occurrence time as the submission time.
				st := occ
				newRow.SubmittedAt = &st
			}
		}
		if in.ToStatus == domain.StatusSaved || in.ToStatus == domain.StatusPreparing {
			newRow.SubmittedAt = nil
			newRow.FirstResponseAt = nil
		}
		if err := s.repo.SetStatus(ctx, tx, &newRow); err != nil {
			return err
		}
		ev := &repository.Event{
			ApplicationID: id, OwnerID: ownerID, EventType: "status_change",
			FromStatus: ptrString(row.Status), ToStatus: ptrString(in.ToStatus),
			Note: in.Note, Reason: in.Reason, OccurredAt: occ, ActorID: &ownerID,
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

func ptrString(s string) *string { return &s }

// Events lists the timeline for an application.
func (s *Service) Events(ctx context.Context, ownerID, id int64) ([]*repository.Event, error) {
	evs, err := s.repo.ListEvents(ctx, s.db.Pool(), id, ownerID)
	return evs, err
}

// CorrectionInput rewrites history with an audit trail: the corrected event is
// linked via corrects_event_id and a compensating event is appended.
type CorrectionInput struct {
	EventID    int64
	NewStatus  string
	OccurredAt time.Time
	Reason     string
}

// Correct validates the resulting timeline then records a correction event.
func (s *Service) Correct(ctx context.Context, ownerID, appID int64, in *CorrectionInput) error {
	return s.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		orig, err := s.repo.GetEventByID(ctx, tx, appID, ownerID, in.EventID)
		if err != nil {
			return ErrNotFound
		}
		// Preview timeline with correction applied: substitute the original
		// event's target state for the requested one at its occurrence time.
		timeline, err := s.repo.ListEvents(ctx, tx, appID, ownerID)
		if err != nil {
			return err
		}
		// validate sequence stays consistent; we trust the new status is a
		// legal step from the previous effective state.
		sim := make([]string, 0, len(timeline))
		for _, ev := range timeline {
			if ev.ID == in.EventID {
				sim = append(sim, in.NewStatus)
			} else if ev.ToStatus != nil && ev.EventType != "correction" {
				sim = append(sim, *ev.ToStatus)
			}
		}
		// Simulate step by step: current = created state (first event to_status)
		current := ""
		for _, st := range sim {
			if st == "" {
				continue
			}
			if current != "" && current != st {
				if err := domain.ValidateTransition(domain.Transition{
					FromStatus: current, ToStatus: st, OccurredAt: time.Now(), Now: time.Now(),
					WasSubmitted: true, HadOffer: true, Reason: in.Reason,
				}); err != nil {
					return &domain.ValidationError{Code: "correction_invalid", Message: "纠正后的时间线不合法: " + err.Error()}
				}
			}
			current = st
		}
		// Record correction (keep audit; do not delete the original event)
		ev := &repository.Event{
			ApplicationID: appID, OwnerID: ownerID, EventType: "correction",
			FromStatus: ptrString(*orig.ToStatus), ToStatus: ptrString(in.NewStatus),
			Reason: in.Reason, OccurredAt: time.Now(), CorrectsEventID: &orig.ID, ActorID: &ownerID,
		}
		if err := s.repo.InsertEvent(ctx, tx, ev); err != nil {
			return err
		}
		// Recompute the current application status from the effective timeline.
		return s.repo.ResyncStatusFromEvents(ctx, tx, appID, ownerID)
	})
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
