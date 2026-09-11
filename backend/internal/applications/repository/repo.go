// Package repo provides SQL adapters for the applications module.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
)

// Row is the full applications row.
type Row struct {
	ID             int64
	OwnerID        int64
	CompanyID      int64
	CompanyName    string
	Position       string
	JobURL         string
	JDSnapshot     string
	Location       string
	RemotePolicy   string
	EmploymentType string
	SalaryMin      *int64
	SalaryMax      *int64
	SalaryCurrency string
	Channel        string
	Status         string
	// Substatus refines Status inside a large stage ("" = 未细分).
	Substatus string
	// FocusActivityKind/FocusActivityID point at the activity the user is
	// currently focused on ("assessment" / "interview"); nil = none chosen.
	FocusActivityKind     string
	FocusActivityID       *int64
	Priority              string
	Tags                  []string
	CustomValues          json.RawMessage
	Notes                 string
	SavedAt               *time.Time
	SubmittedAt           *time.Time
	FirstResponseAt       *time.Time
	Deadline              *time.Time
	AcceptedAt            *time.Time
	RejectedAt            *time.Time
	Reason                string
	NextAction            string
	NextActionDueAt       *time.Time
	NextActionDueTs       *time.Time
	Version               int
	ArchivedAt            *time.Time
	DeletedAt             *time.Time
	PreviousApplicationID *int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// Event is an application_events row.
type Event struct {
	ID            int64
	ApplicationID int64
	OwnerID       int64
	Sequence      int
	EventType     string
	FromStatus    *string
	ToStatus      *string
	// Substatus / activity references and the reason the change was made
	// (advance / rollback / reopen). Empty for legacy rows.
	FromSubstatus   *string
	ToSubstatus     *string
	ActivityKind    *string
	ActivityID      *int64
	ChangeType      string
	Note            string
	Reason          string
	OccurredAt      time.Time
	RecordedAt      time.Time
	CorrectsEventID *int64
	ActorID         *int64
}

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Pool() *database.DB { return r.db }

const rowCols = `id, owner_id, company_id, company_name, position, job_url, jd_snapshot,
	location, remote_policy, employment_type, salary_min, salary_max, salary_currency,
	channel, status, substatus, focus_activity_kind, focus_activity_id, priority, tags,
	custom_values, notes, saved_at, submitted_at,
	first_response_at, deadline, accepted_at, rejected_at, reason, next_action,
	next_action_due_at, next_action_due_ts, version, archived_at, deleted_at,
	previous_application_id, created_at, updated_at`

const eventCols = `id, application_id, sequence, event_type, from_status, to_status,
	from_substatus, to_substatus, activity_kind, activity_id, change_type,
	note, reason, occurred_at, recorded_at, corrects_event_id, actor_id`

func scanEvent(row pgx.Row) (*Event, error) {
	var e Event
	if err := row.Scan(&e.ID, &e.ApplicationID, &e.Sequence, &e.EventType, &e.FromStatus,
		&e.ToStatus, &e.FromSubstatus, &e.ToSubstatus, &e.ActivityKind, &e.ActivityID,
		&e.ChangeType, &e.Note, &e.Reason, &e.OccurredAt, &e.RecordedAt,
		&e.CorrectsEventID, &e.ActorID); err != nil {
		return nil, err
	}
	e.Note = StripIdempotencyMarker(e.Note)
	return &e, nil
}

func scanRow(row pgx.Row) (*Row, error) {
	var r Row
	// substatus / focus_activity_kind are nullable (NULL = 未细分 / 没有关注轮次),
	// so they scan through pointers and collapse to the "" the domain uses.
	var substatus, focusKind *string
	err := row.Scan(&r.ID, &r.OwnerID, &r.CompanyID, &r.CompanyName, &r.Position, &r.JobURL,
		&r.JDSnapshot, &r.Location, &r.RemotePolicy, &r.EmploymentType, &r.SalaryMin, &r.SalaryMax,
		&r.SalaryCurrency, &r.Channel, &r.Status, &substatus, &focusKind, &r.FocusActivityID,
		&r.Priority, &r.Tags, &r.CustomValues, &r.Notes,
		&r.SavedAt, &r.SubmittedAt, &r.FirstResponseAt, &r.Deadline, &r.AcceptedAt, &r.RejectedAt,
		&r.Reason, &r.NextAction, &r.NextActionDueAt, &r.NextActionDueTs, &r.Version,
		&r.ArchivedAt, &r.DeletedAt, &r.PreviousApplicationID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if substatus != nil {
		r.Substatus = *substatus
	}
	if focusKind != nil {
		r.FocusActivityKind = *focusKind
	}
	return &r, nil
}

var ErrNoRows = pgx.ErrNoRows

func (r *Repo) GetByID(ctx context.Context, ownerID, id int64, includeDeleted bool) (*Row, error) {
	extra := ""
	if !includeDeleted {
		extra = " AND deleted_at IS NULL"
	}
	row := r.db.Pool().QueryRow(ctx, `SELECT `+rowCols+` FROM applications WHERE id=$1 AND owner_id=$2`+extra, id, ownerID)
	return scanRow(row)
}

func (r *Repo) GetForUpdate(ctx context.Context, q database.Querier, ownerID, id int64) (*Row, error) {
	row := q.QueryRow(ctx, `SELECT `+rowCols+` FROM applications WHERE id=$1 AND owner_id=$2 FOR UPDATE`, id, ownerID)
	return scanRow(row)
}

// ListOptions describes server-side pagination.
type ListOptions struct {
	Page         int
	PageSize     int
	OnlyTrash    bool
	ArchivedOnly bool
	Status       string
	Search       string
	Query        database.Querier // optional tx
}

func (r *Repo) List(ctx context.Context, ownerID int64, o ListOptions) ([]*Row, int64, error) {
	q := o.Query
	if q == nil {
		q = r.db.Pool()
	}
	page, size := o.Page, o.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	where := []string{"owner_id = $1"}
	args := []any{ownerID}
	if !o.OnlyTrash {
		where = append(where, "deleted_at IS NULL")
	}
	if o.OnlyTrash {
		where = append(where, "deleted_at IS NOT NULL")
	}
	if o.ArchivedOnly {
		where = append(where, "archived_at IS NOT NULL")
	}
	if o.Status != "" {
		args = append(args, o.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if o.Search != "" {
		args = append(args, "%"+o.Search+"%")
		n := len(args)
		// Substring search over job title, company, tags and notes (plan §8).
		where = append(where, fmt.Sprintf(`(position ILIKE $%d OR company_name ILIKE $%d OR notes ILIKE $%d OR EXISTS (SELECT 1 FROM unnest(tags) t WHERE t ILIKE $%d))`, n, n, n, n))
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := q.QueryRow(ctx, `SELECT count(*) FROM applications WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * size
	args = append(args, size, offset)
	rows, err := q.Query(ctx, `SELECT `+rowCols+` FROM applications WHERE `+whereSQL+
		` ORDER BY COALESCE(updated_at, created_at) DESC, id DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Row
	for rows.Next() {
		item, err := scanRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// Create inserts an application and returns its id.
func (r *Repo) Create(ctx context.Context, q database.Querier, a *Row) (int64, error) {
	err := q.QueryRow(ctx, `INSERT INTO applications(owner_id, company_id, company_name, position, job_url,
		jd_snapshot, location, remote_policy, employment_type, salary_min, salary_max, salary_currency,
		channel, status, substatus, focus_activity_kind, focus_activity_id, priority, tags, custom_values,
		notes, saved_at, submitted_at, first_response_at,
		deadline, reason, next_action, next_action_due_at, next_action_due_ts, previous_application_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30)
		RETURNING id, created_at, updated_at`,
		a.OwnerID, a.CompanyID, a.CompanyName, a.Position, a.JobURL, a.JDSnapshot, a.Location,
		a.RemotePolicy, a.EmploymentType, a.SalaryMin, a.SalaryMax, a.SalaryCurrency, a.Channel,
		a.Status, a.Substatus, nullIfEmpty(a.FocusActivityKind), a.FocusActivityID,
		a.Priority, a.Tags, a.CustomValues, a.Notes, a.SavedAt, a.SubmittedAt,
		a.FirstResponseAt, a.Deadline, a.Reason, a.NextAction, a.NextActionDueAt, a.NextActionDueTs,
		a.PreviousApplicationID).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	return a.ID, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UpdateFields updates the mutable core fields with optimistic locking.
func (r *Repo) UpdateFields(ctx context.Context, q database.Querier, a *Row) (int64, error) {
	tag, err := q.Exec(ctx, `UPDATE applications SET
		company_id=$1, company_name=$2, position=$3, job_url=$4, jd_snapshot=$5, location=$6,
		remote_policy=$7, employment_type=$8, salary_min=$9, salary_max=$10, salary_currency=$11,
		channel=$12, priority=$13, tags=$14, custom_values=$15, notes=$16, deadline=$17,
		reason=$18, next_action=$19, next_action_due_at=$20, next_action_due_ts=$21,
		version = version + 1, updated_at = now()
		WHERE id=$22 AND owner_id=$23 AND version=$24`,
		a.CompanyID, a.CompanyName, a.Position, a.JobURL, a.JDSnapshot, a.Location, a.RemotePolicy,
		a.EmploymentType, a.SalaryMin, a.SalaryMax, a.SalaryCurrency, a.Channel, a.Priority, a.Tags,
		a.CustomValues, a.Notes, a.Deadline, a.Reason, a.NextAction, a.NextActionDueAt,
		a.NextActionDueTs, a.ID, a.OwnerID, a.Version)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SetStatus updates status/substatus/focus plus the derived snapshot columns
// within a transition.
//
// The caller passes a full row read under FOR UPDATE, so submitted_at /
// first_response_at are written outright: the service decides whether a change
// preserves or clears them. The rule (方案 §4.4) is that a ROLLBACK — a real
// process step backwards like 面试中 → 初筛 — keeps them, because the submission
// and the first reply really happened; only an explicit correction may clear
// them.
func (r *Repo) SetStatus(ctx context.Context, q database.Querier, a *Row) error {
	_, err := q.Exec(ctx, `UPDATE applications SET status=$1, substatus=$2,
		focus_activity_kind=$3, focus_activity_id=$4,
		submitted_at=$5::timestamptz,
		first_response_at=$6::timestamptz, saved_at=COALESCE($7::timestamptz, saved_at),
		accepted_at=$8::timestamptz, rejected_at=$9::timestamptz, reason=$10, version = version + 1, updated_at = now()
		WHERE id=$11 AND owner_id=$12 AND version=$13`,
		a.Status, a.Substatus, nullIfEmpty(a.FocusActivityKind), a.FocusActivityID,
		a.SubmittedAt, a.FirstResponseAt, a.SavedAt, a.AcceptedAt, a.RejectedAt,
		a.Reason, a.ID, a.OwnerID, a.Version)
	return err
}

// SetFocus points the application at a specific activity without changing the
// stage (used when the user picks which round they are working on).
func (r *Repo) SetFocus(ctx context.Context, q database.Querier, ownerID, id int64, kind string, activityID *int64) error {
	_, err := q.Exec(ctx, `UPDATE applications SET focus_activity_kind=$1, focus_activity_id=$2,
		version=version+1, updated_at=now() WHERE id=$3 AND owner_id=$4`,
		nullIfEmpty(kind), activityID, id, ownerID)
	return err
}

// InsertEvent appends an event at the next sequence within the application.
func (r *Repo) InsertEvent(ctx context.Context, q database.Querier, e *Event) error {
	from := any(nil)
	if e.FromStatus != nil {
		from = *e.FromStatus
	}
	to := any(nil)
	if e.ToStatus != nil {
		to = *e.ToStatus
	}
	changeType := e.ChangeType
	if changeType == "" {
		changeType = domain.ChangeAdvance
	}
	return q.QueryRow(ctx, `INSERT INTO application_events(application_id, owner_id, sequence, event_type,
		from_status, to_status, from_substatus, to_substatus, activity_kind, activity_id, change_type,
		note, reason, occurred_at, corrects_event_id, actor_id)
		VALUES ($1, $2, (SELECT COALESCE(MAX(sequence),0)+1 FROM application_events WHERE application_id = $1), $3, $4::text, $5::text, $6::text, $7::text, $8::text, $9, $10, $11, $12, $13, $14, $15)
		RETURNING sequence`,
		e.ApplicationID, e.OwnerID, e.EventType, from, to,
		e.FromSubstatus, e.ToSubstatus, e.ActivityKind, e.ActivityID, changeType,
		e.Note, e.Reason, e.OccurredAt, e.CorrectsEventID, e.ActorID).Scan(&e.Sequence)
}

func (r *Repo) ListEvents(ctx context.Context, q database.Querier, appID, ownerID int64) ([]*Event, error) {
	rows, err := q.Query(ctx, `SELECT `+eventCols+`
		FROM application_events WHERE application_id=$1 AND owner_id=$2
		-- Business-time order, but the 建档 row is pinned first: a backfilled
		-- 投递 carries an EARLIER occurred_at than the creation instant, and a
		-- timeline that opens with 「已投递」 above 「建档」 reads as broken.
		-- Safe for the correction simulation, which re-sorts by sequence itself.
		ORDER BY (event_type = 'created') DESC, occurred_at ASC, sequence ASC`, appID, ownerID)
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

func (r *Repo) GetEventByID(ctx context.Context, q database.Querier, appID, ownerID, eventID int64) (*Event, error) {
	return scanEvent(q.QueryRow(ctx, `SELECT `+eventCols+`
		FROM application_events WHERE id=$1 AND application_id=$2 AND owner_id=$3`,
		eventID, appID, ownerID))
}

// idempotencyMarker is APPENDED to note by RecordIdempotency so the guard query
// can find a replayed request. It is storage bookkeeping, never user content.
//
// Matched only in the shape it is generated in: at the very END of the note and
// with a whitespace-free key. Notes are unrestricted user text, so an occurrence
// inside what someone typed is theirs to keep — an unanchored strip silently
// returned content different from what is stored.
//
// A note ending in a literal "|idem:token" is inherently indistinguishable from
// the generated suffix; moving the key to its own column would settle that for
// good, but the strip has to stay for rows written before this change anyway.
var idempotencySuffix = regexp.MustCompile(`\|idem:[^|\s]*$`)

// StripIdempotencyMarker removes the generated marker from a note before it
// leaves the repository, so a user's own note never renders as
// 「内推直接进面|idem:ui-1788884884229」.
func StripIdempotencyMarker(note string) string {
	return idempotencySuffix.ReplaceAllString(note, "")
}

// SoftDelete / Restore / Archive set visibility flags.
func (r *Repo) SetDeleted(ctx context.Context, q database.Querier, ownerID, id int64, deleted bool) error {
	col := "NULL"
	if deleted {
		col = "now()"
	}
	_, err := q.Exec(ctx, `UPDATE applications SET deleted_at=`+col+`, version=version+1, updated_at=now() WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

func (r *Repo) SetArchived(ctx context.Context, q database.Querier, ownerID, id int64, archived bool) error {
	col := "NULL"
	if archived {
		col = "now()"
	}
	_, err := q.Exec(ctx, `UPDATE applications SET archived_at=`+col+`, updated_at=now() WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

// Company helpers -----------------------------------------------------------

type Company struct {
	ID      int64
	OwnerID int64
	Name    string
	Website string
	Notes   string
}

func (r *Repo) FindCompany(ctx context.Context, ownerID int64, name string) (*Company, error) {
	var c Company
	err := r.db.Pool().QueryRow(ctx, `SELECT id, owner_id, name, website, notes FROM companies WHERE owner_id=$1 AND name=$2`, ownerID, name).
		Scan(&c.ID, &c.OwnerID, &c.Name, &c.Website, &c.Notes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoRows
	}
	return &c, err
}

func (r *Repo) CreateCompany(ctx context.Context, q database.Querier, c *Company) error {
	return q.QueryRow(ctx, `INSERT INTO companies(owner_id, name, website, notes) VALUES($1,$2,$3,$4) RETURNING id`,
		c.OwnerID, c.Name, c.Website, c.Notes).Scan(&c.ID)
}

func (r *Repo) CompanyByID(ctx context.Context, ownerID, id int64) (*Company, error) {
	var c Company
	err := r.db.Pool().QueryRow(ctx, `SELECT id, owner_id, name, website, notes FROM companies WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&c.ID, &c.OwnerID, &c.Name, &c.Website, &c.Notes)
	return &c, err
}

func (r *Repo) ListCompanies(ctx context.Context, ownerID int64) ([]*Company, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT id, owner_id, name, website, notes FROM companies WHERE owner_id=$1 ORDER BY name`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Company
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Name, &c.Website, &c.Notes); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// StageForActivityKind maps an activity kind to the large stage it lives in.
func StageForActivityKind(kind string) string {
	switch kind {
	case domain.ActivityAssessment:
		return domain.StatusAssessment
	case domain.ActivityInterview:
		return domain.StatusInterviewing
	}
	return ""
}

// SyncFromActivity reflects an activity's derived progress into the
// application snapshot (方案 §6.1: one command updates both). It is
// deliberately scoped to applications that are CURRENTLY in the activity's
// stage: finishing an old OA after the record already moved on to 面试 must not
// drag the stage back or relabel it.
//
// 方案 §3.3: the focus is the USER'S choice (列表主标签展示用户选定的关注
// 阶段) — editing a round never steals an existing focus, it only claims the
// slot when none is set. And a cancelled round derives no substatus (""), which
// must CLEAR a substatus previously derived from that same round, not leave the
// application showing 已完成 OA forever (PR #23 review).
func (r *Repo) SyncFromActivity(ctx context.Context, q database.Querier, ownerID, appID int64, kind string, activityID int64, substatus string) error {
	stage := StageForActivityKind(kind)
	if stage == "" {
		return nil
	}
	_, err := q.Exec(ctx, `UPDATE applications SET
			focus_activity_kind = CASE WHEN focus_activity_id IS NULL AND $3::text <> '' THEN $1 ELSE focus_activity_kind END,
			focus_activity_id   = CASE WHEN focus_activity_id IS NULL AND $3::text <> '' THEN $2 ELSE focus_activity_id END,
			substatus = CASE
				WHEN $3::text <> '' THEN $3::text
				WHEN focus_activity_id IS NULL OR focus_activity_id = $2 THEN ''
				ELSE substatus
			END,
			version = version + 1, updated_at = now()
		WHERE id = $4 AND owner_id = $5 AND status = $6`,
		kind, activityID, substatus, appID, ownerID, stage)
	return err
}
