// Package repository provides SQL adapters for activities: interviews,
// next-action items and notes attached to applications.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/platform/database"
)

type Interview struct {
	ID              int64
	ApplicationID   int64
	OwnerID         int64
	RoundName       string
	Format          string
	ScheduledAt     *time.Time
	Timezone        string
	DurationMinutes *int
	Result          string
	Feedback        string
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Action struct {
	ID            int64
	ApplicationID *int64
	OwnerID       int64
	Title         string
	DueDate       *time.Time
	DueTs         *time.Time
	DoneAt        *time.Time
	RemindMe      bool
	RemindAt      *time.Time
	Priority      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompanyName   string
	Position      string
	Status        string
}

type Note struct {
	ID            int64
	ApplicationID int64
	OwnerID       int64
	ContentMD     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Pool() *database.DB { return r.db }

// -- interviews --

func (r *Repo) ListInterviews(ctx context.Context, appID, ownerID int64) ([]*Interview, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT id, application_id, owner_id, round_name, format,
		scheduled_at, timezone, duration_minutes, result, feedback, notes, created_at, updated_at
		FROM interviews WHERE application_id=$1 AND owner_id=$2 ORDER BY scheduled_at NULLS LAST, id`, appID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Interview
	for rows.Next() {
		var it Interview
		if err := rows.Scan(&it.ID, &it.ApplicationID, &it.OwnerID, &it.RoundName, &it.Format,
			&it.ScheduledAt, &it.Timezone, &it.DurationMinutes, &it.Result, &it.Feedback, &it.Notes,
			&it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &it)
	}
	return out, rows.Err()
}

func (r *Repo) GetInterview(ctx context.Context, appID, ownerID, id int64) (*Interview, error) {
	var it Interview
	err := r.db.Pool().QueryRow(ctx, `SELECT id, application_id, owner_id, round_name, format,
		scheduled_at, timezone, duration_minutes, result, feedback, notes, created_at, updated_at
		FROM interviews WHERE id=$1 AND application_id=$2 AND owner_id=$3`, id, appID, ownerID).
		Scan(&it.ID, &it.ApplicationID, &it.OwnerID, &it.RoundName, &it.Format, &it.ScheduledAt,
			&it.Timezone, &it.DurationMinutes, &it.Result, &it.Feedback, &it.Notes, &it.CreatedAt, &it.UpdatedAt)
	return &it, err
}

func (r *Repo) CreateInterview(ctx context.Context, q database.Querier, it *Interview) error {
	return q.QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format,
		scheduled_at, timezone, duration_minutes, result, feedback, notes)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, created_at`,
		it.ApplicationID, it.OwnerID, it.RoundName, it.Format, it.ScheduledAt, it.Timezone,
		it.DurationMinutes, it.Result, it.Feedback, it.Notes).Scan(&it.ID, &it.CreatedAt)
}

func (r *Repo) UpdateInterview(ctx context.Context, q database.Querier, it *Interview) error {
	tag, err := q.Exec(ctx, `UPDATE interviews SET round_name=$1, format=$2, scheduled_at=$3,
		timezone=$4, duration_minutes=$5, result=$6, feedback=$7, notes=$8, updated_at=now()
		WHERE id=$9 AND application_id=$10 AND owner_id=$11`,
		it.RoundName, it.Format, it.ScheduledAt, it.Timezone, it.DurationMinutes, it.Result,
		it.Feedback, it.Notes, it.ID, it.ApplicationID, it.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repo) DeleteInterview(ctx context.Context, q database.Querier, appID, ownerID, id int64) error {
	tag, err := q.Exec(ctx, `DELETE FROM interviews WHERE id=$1 AND application_id=$2 AND owner_id=$3`, id, appID, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// -- actions --

// ListActions lists action items. When appID is nil it returns the owner's
// actions across all applications joined with their company/position for the
// today dashboard.
func (r *Repo) ListActions(ctx context.Context, appID *int64, ownerID int64, openOnly bool) ([]*Action, error) {
	// The company/position/status enrichment columns are always selected, so
	// the applications join is always present. appID scoping applies to the
	// actions alias; an unscoped list (today dashboard) reads across apps.
	where := "a.owner_id=$1"
	args := []any{ownerID}
	if appID != nil {
		where += " AND a.application_id=$2"
		args = append(args, *appID)
	}
	if openOnly {
		where += " AND a.done_at IS NULL"
	}
	rows, err := r.db.Pool().Query(ctx, `SELECT a.id, a.application_id, a.owner_id, a.title, a.due_date, a.due_ts,
		a.done_at, a.remind_me, a.remind_at, a.priority, a.created_at, a.updated_at,
		COALESCE(ap.company_name,''), COALESCE(ap.position,''), COALESCE(ap.status,'')
		FROM actions a
		LEFT JOIN applications ap ON ap.id = a.application_id AND ap.owner_id = a.owner_id
		WHERE `+where+` ORDER BY COALESCE(a.due_date, a.due_ts) NULLS LAST, a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.ApplicationID, &a.OwnerID, &a.Title, &a.DueDate, &a.DueTs,
			&a.DoneAt, &a.RemindMe, &a.RemindAt, &a.Priority, &a.CreatedAt, &a.UpdatedAt,
			&a.CompanyName, &a.Position, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (r *Repo) GetAction(ctx context.Context, ownerID, id int64) (*Action, error) {
	var a Action
	err := r.db.Pool().QueryRow(ctx, `SELECT id, application_id, owner_id, title, due_date, due_ts,
		done_at, remind_me, remind_at, priority, created_at, updated_at FROM actions WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&a.ID, &a.ApplicationID, &a.OwnerID, &a.Title, &a.DueDate, &a.DueTs, &a.DoneAt,
			&a.RemindMe, &a.RemindAt, &a.Priority, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

func (r *Repo) CreateAction(ctx context.Context, q database.Querier, a *Action) error {
	prio := a.Priority
	if prio == "" {
		prio = "medium"
	}
	return q.QueryRow(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, due_ts,
		done_at, remind_me, remind_at, priority) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`,
		a.ApplicationID, a.OwnerID, a.Title, a.DueDate, a.DueTs, a.DoneAt, a.RemindMe, a.RemindAt, prio).
		Scan(&a.ID, &a.CreatedAt)
}

func (r *Repo) UpdateAction(ctx context.Context, q database.Querier, a *Action) error {
	prio := a.Priority
	if prio == "" {
		prio = "medium"
	}
	tag, err := q.Exec(ctx, `UPDATE actions SET title=$1, due_date=$2, due_ts=$3, done_at=$4,
		remind_me=$5, remind_at=$6, priority=$7, updated_at=now() WHERE id=$8 AND owner_id=$9`,
		a.Title, a.DueDate, a.DueTs, a.DoneAt, a.RemindMe, a.RemindAt, prio, a.ID, a.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repo) DeleteAction(ctx context.Context, ownerID, id int64) error {
	_, err := r.db.Pool().Exec(ctx, `DELETE FROM actions WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

func (r *Repo) MarkActionDone(ctx context.Context, q database.Querier, ownerID, id int64, done bool) error {
	col := "NULL"
	if done {
		col = "now()"
	}
	_, err := q.Exec(ctx, `UPDATE actions SET done_at=`+col+`, updated_at=now() WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

// -- notes --

func (r *Repo) ListNotes(ctx context.Context, appID, ownerID int64) ([]*Note, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT id, application_id, owner_id, content_md, created_at, updated_at
		FROM notes WHERE application_id=$1 AND owner_id=$2 ORDER BY created_at DESC`, appID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.ApplicationID, &n.OwnerID, &n.ContentMD, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

func (r *Repo) CreateNote(ctx context.Context, q database.Querier, n *Note) error {
	return q.QueryRow(ctx, `INSERT INTO notes(application_id, owner_id, content_md) VALUES($1,$2,$3) RETURNING id, created_at, updated_at`,
		n.ApplicationID, n.OwnerID, n.ContentMD).Scan(&n.ID, &n.CreatedAt, &n.UpdatedAt)
}

func (r *Repo) UpdateNote(ctx context.Context, q database.Querier, n *Note) error {
	tag, err := q.Exec(ctx, `UPDATE notes SET content_md=$1, updated_at=now() WHERE id=$2 AND owner_id=$3`,
		n.ContentMD, n.ID, n.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repo) DeleteNote(ctx context.Context, ownerID, id int64) error {
	_, err := r.db.Pool().Exec(ctx, `DELETE FROM notes WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

// AppOwnedBy validates the application belongs to the user.
func (r *Repo) AppOwnedBy(ctx context.Context, q database.Querier, appID, ownerID int64) error {
	var one int
	err := q.QueryRow(ctx, `SELECT 1 FROM applications WHERE id=$1 AND owner_id=$2`, appID, ownerID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

var ErrNotFound = errors.New("not found")

// ScheduleLink carries the per-interview scheduling metadata (meeting url,
// location, contacts, cancellation). Created lazily with an interview when the
// request carries scheduling fields; updated in place thereafter. This keeps
// the interviews table's core columns stable while the dashboard/calendar can
// still exclude cancelled interviews through the join.
type ScheduleLink struct {
	ID               int64  `json:"id"`
	InterviewID      int64  `json:"interview_id"`
	OwnerID          int64  `json:"owner_id"`
	MeetingURL       string `json:"meeting_url"`
	Location         string `json:"location"`
	ContactName      string `json:"contact_name"`
	ContactEmail     string `json:"contact_email"`
	Notes            string `json:"notes"`
	Cancelled        bool   `json:"cancelled"`
	CancelledReason  string `json:"cancelled_reason"`
	OriginalTimezone string `json:"original_timezone"`
}

// CreateScheduleLink inserts scheduling metadata for an interview.
func (r *Repo) CreateScheduleLink(ctx context.Context, q database.Querier, s *ScheduleLink) error {
	return q.QueryRow(ctx, `INSERT INTO schedule_links(interview_id, owner_id, meeting_url, location,
		contact_name, contact_email, notes, cancelled, cancelled_reason, original_timezone)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		s.InterviewID, s.OwnerID, s.MeetingURL, s.Location, s.ContactName, s.ContactEmail,
		s.Notes, s.Cancelled, s.CancelledReason, s.OriginalTimezone).Scan(&s.ID)
}

// GetScheduleLink reads the scheduling metadata for an interview (nil when
// none exists).
func (r *Repo) GetScheduleLink(ctx context.Context, interviewID, ownerID int64) (*ScheduleLink, error) {
	var s ScheduleLink
	err := r.db.Pool().QueryRow(ctx, `SELECT id, interview_id, owner_id, meeting_url, location,
		contact_name, contact_email, notes, cancelled, cancelled_reason, original_timezone
		FROM schedule_links WHERE interview_id=$1 AND owner_id=$2`, interviewID, ownerID).
		Scan(&s.ID, &s.InterviewID, &s.OwnerID, &s.MeetingURL, &s.Location, &s.ContactName,
			&s.ContactEmail, &s.Notes, &s.Cancelled, &s.CancelledReason, &s.OriginalTimezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// UpsertScheduleLink creates-or-updates scheduling metadata.
func (r *Repo) UpsertScheduleLink(ctx context.Context, q database.Querier, s *ScheduleLink) error {
	_, err := q.Exec(ctx, `INSERT INTO schedule_links(interview_id, owner_id, meeting_url, location,
		contact_name, contact_email, notes, cancelled, cancelled_reason, original_timezone)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (interview_id) DO UPDATE SET
			meeting_url=EXCLUDED.meeting_url, location=EXCLUDED.location,
			contact_name=EXCLUDED.contact_name, contact_email=EXCLUDED.contact_email,
			notes=EXCLUDED.notes, cancelled=EXCLUDED.cancelled,
			cancelled_reason=EXCLUDED.cancelled_reason,
			original_timezone=EXCLUDED.original_timezone,
			updated_at=now()`,
		s.InterviewID, s.OwnerID, s.MeetingURL, s.Location, s.ContactName, s.ContactEmail,
		s.Notes, s.Cancelled, s.CancelledReason, s.OriginalTimezone)
	return err
}
