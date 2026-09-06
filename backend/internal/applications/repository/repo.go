// Package repo provides SQL adapters for the applications module.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/platform/database"
)

// Row is the full applications row.
type Row struct {
	ID                    int64
	OwnerID               int64
	CompanyID             int64
	CompanyName           string
	Position              string
	JobURL                string
	JDSnapshot            string
	Location              string
	RemotePolicy          string
	EmploymentType        string
	SalaryMin             *int64
	SalaryMax             *int64
	SalaryCurrency        string
	Channel               string
	Status                string
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
	ID              int64
	ApplicationID   int64
	OwnerID         int64
	Sequence        int
	EventType       string
	FromStatus      *string
	ToStatus        *string
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
	channel, status, priority, tags, custom_values, notes, saved_at, submitted_at,
	first_response_at, deadline, accepted_at, rejected_at, reason, next_action,
	next_action_due_at, next_action_due_ts, version, archived_at, deleted_at,
	previous_application_id, created_at, updated_at`

func scanRow(row pgx.Row) (*Row, error) {
	var r Row
	err := row.Scan(&r.ID, &r.OwnerID, &r.CompanyID, &r.CompanyName, &r.Position, &r.JobURL,
		&r.JDSnapshot, &r.Location, &r.RemotePolicy, &r.EmploymentType, &r.SalaryMin, &r.SalaryMax,
		&r.SalaryCurrency, &r.Channel, &r.Status, &r.Priority, &r.Tags, &r.CustomValues, &r.Notes,
		&r.SavedAt, &r.SubmittedAt, &r.FirstResponseAt, &r.Deadline, &r.AcceptedAt, &r.RejectedAt,
		&r.Reason, &r.NextAction, &r.NextActionDueAt, &r.NextActionDueTs, &r.Version,
		&r.ArchivedAt, &r.DeletedAt, &r.PreviousApplicationID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
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
	var id int64
	err := q.QueryRow(ctx, `INSERT INTO applications(owner_id, company_id, company_name, position, job_url,
		jd_snapshot, location, remote_policy, employment_type, salary_min, salary_max, salary_currency,
		channel, status, priority, tags, custom_values, notes, saved_at, submitted_at, first_response_at,
		deadline, reason, next_action, next_action_due_at, next_action_due_ts, previous_application_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
		RETURNING id`,
		a.OwnerID, a.CompanyID, a.CompanyName, a.Position, a.JobURL, a.JDSnapshot, a.Location,
		a.RemotePolicy, a.EmploymentType, a.SalaryMin, a.SalaryMax, a.SalaryCurrency, a.Channel,
		a.Status, a.Priority, a.Tags, a.CustomValues, a.Notes, a.SavedAt, a.SubmittedAt,
		a.FirstResponseAt, a.Deadline, a.Reason, a.NextAction, a.NextActionDueAt, a.NextActionDueTs,
		a.PreviousApplicationID).Scan(&id)
	return id, err
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

// SetStatus updates status-related columns within a transition.
func (r *Repo) SetStatus(ctx context.Context, q database.Querier, a *Row) error {
	_, err := q.Exec(ctx, `UPDATE applications SET status=$1, submitted_at=COALESCE($2::timestamptz, submitted_at),
		first_response_at=COALESCE($3::timestamptz, first_response_at), saved_at=COALESCE($4::timestamptz, saved_at),
		accepted_at=$5::timestamptz, rejected_at=$6::timestamptz, reason=$7, version = version + 1, updated_at = now()
		WHERE id=$8 AND owner_id=$9 AND version=$10`,
		a.Status, a.SubmittedAt, a.FirstResponseAt, a.SavedAt, a.AcceptedAt, a.RejectedAt,
		a.Reason, a.ID, a.OwnerID, a.Version)
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
	return q.QueryRow(ctx, `INSERT INTO application_events(application_id, owner_id, sequence, event_type,
		from_status, to_status, note, reason, occurred_at, corrects_event_id, actor_id)
		VALUES ($1, $2, (SELECT COALESCE(MAX(sequence),0)+1 FROM application_events WHERE application_id = $1), $3, $4::text, $5::text, $6, $7, $8, $9, $10)
		RETURNING sequence`,
		e.ApplicationID, e.OwnerID, e.EventType, from, to, e.Note, e.Reason,
		e.OccurredAt, e.CorrectsEventID, e.ActorID).Scan(&e.Sequence)
}

func (r *Repo) ListEvents(ctx context.Context, q database.Querier, appID, ownerID int64) ([]*Event, error) {
	rows, err := q.Query(ctx, `SELECT id, application_id, sequence, event_type, from_status, to_status,
		note, reason, occurred_at, recorded_at, corrects_event_id, actor_id
		FROM application_events WHERE application_id=$1 AND owner_id=$2
		ORDER BY occurred_at ASC, sequence ASC`, appID, ownerID)
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

func (r *Repo) GetEventByID(ctx context.Context, q database.Querier, appID, ownerID, eventID int64) (*Event, error) {
	var e Event
	err := q.QueryRow(ctx, `SELECT id, application_id, sequence, event_type, from_status, to_status,
		note, reason, occurred_at, recorded_at, corrects_event_id, actor_id
		FROM application_events WHERE id=$1 AND application_id=$2 AND owner_id=$3`,
		eventID, appID, ownerID).Scan(&e.ID, &e.ApplicationID, &e.Sequence, &e.EventType,
		&e.FromStatus, &e.ToStatus, &e.Note, &e.Reason, &e.OccurredAt, &e.RecordedAt,
		&e.CorrectsEventID, &e.ActorID)
	if err != nil {
		return nil, err
	}
	return &e, nil
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
