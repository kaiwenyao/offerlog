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

// Interview progress values. Progress is the ACTIVITY state (did the round
// happen?), kept separate from Result (did it pass?). Time passing never sets
// either one.
const (
	ProgressAwaitingSchedule = "awaiting_schedule"
	ProgressPreparing        = "preparing"
	ProgressCompleted        = "completed"
	ProgressCancelled        = "cancelled"
)

// Interview results.
const (
	ResultUnknown = "unknown"
	ResultPassed  = "passed"
	ResultFailed  = "failed"
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
	// Progress: "" (legacy/unknown) | awaiting_schedule | preparing | completed | cancelled.
	Progress    string
	Result      string
	InvitedAt   *time.Time
	CompletedAt *time.Time
	// CompletedUnknown records that the round IS finished but the exact time is
	// unknown — never fabricate a precise timestamp.
	CompletedUnknown bool
	Feedback         string
	Notes            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const interviewCols = `id, application_id, owner_id, round_name, format,
	scheduled_at, timezone, duration_minutes, progress, result, invited_at, completed_at,
	completed_unknown, feedback, notes, created_at, updated_at`

func scanInterview(row pgx.Row) (*Interview, error) {
	var it Interview
	err := row.Scan(&it.ID, &it.ApplicationID, &it.OwnerID, &it.RoundName, &it.Format,
		&it.ScheduledAt, &it.Timezone, &it.DurationMinutes, &it.Progress, &it.Result,
		&it.InvitedAt, &it.CompletedAt, &it.CompletedUnknown, &it.Feedback, &it.Notes,
		&it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &it, nil
}

// AssessmentRound is an OA / take-home round attached to an application.
// The four timestamps (invited / planned / due / completed) are stored
// independently and never overwrite each other.
type AssessmentRound struct {
	ID            int64
	ApplicationID int64
	OwnerID       int64
	Kind          string // online_test | take_home | other
	Name          string
	Progress      string // preparing | completed | cancelled
	Result        string // unknown | passed | failed
	InvitedAt     *time.Time
	PlannedAt     *time.Time
	DueAt         *time.Time
	CompletedAt   *time.Time
	// CompletedUnknown: finished, exact time unknown.
	CompletedUnknown bool
	Link             string
	Notes            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const assessmentCols = `id, application_id, owner_id, kind, name, progress, result,
	invited_at, planned_at, due_at, completed_at, completed_unknown, link, notes,
	created_at, updated_at`

func scanAssessment(row pgx.Row) (*AssessmentRound, error) {
	var a AssessmentRound
	err := row.Scan(&a.ID, &a.ApplicationID, &a.OwnerID, &a.Kind, &a.Name, &a.Progress, &a.Result,
		&a.InvitedAt, &a.PlannedAt, &a.DueAt, &a.CompletedAt, &a.CompletedUnknown, &a.Link, &a.Notes,
		&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// -- interviews --

func (r *Repo) ListInterviews(ctx context.Context, appID, ownerID int64) ([]*Interview, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT `+interviewCols+`
		FROM interviews WHERE application_id=$1 AND owner_id=$2 ORDER BY scheduled_at NULLS LAST, id`, appID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Interview
	for rows.Next() {
		it, err := scanInterview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetInterview loads one round through the caller's Querier. Writers pass their
// transaction so the read and the write see the same rows — a pool read next
// to a transactional write is exactly the stale-snapshot bug CorrectCurrent
// had (PR #23 review). Use GetInterviewForUpdate on write paths.
func (r *Repo) GetInterview(ctx context.Context, q database.Querier, appID, ownerID, id int64) (*Interview, error) {
	return scanInterview(q.QueryRow(ctx, `SELECT `+interviewCols+`
		FROM interviews WHERE id=$1 AND application_id=$2 AND owner_id=$3`, id, appID, ownerID))
}

// GetInterviewForUpdate is GetInterview with a row lock, for the paths that
// rewrite the round inside the caller's transaction (complete / reopen /
// PATCH and the application service's round↔substatus sync). Without the
// lock, two concurrent writers both read the old row and the second commit
// silently overwrites the first.
func (r *Repo) GetInterviewForUpdate(ctx context.Context, q database.Querier, appID, ownerID, id int64) (*Interview, error) {
	return scanInterview(q.QueryRow(ctx, `SELECT `+interviewCols+`
		FROM interviews WHERE id=$1 AND application_id=$2 AND owner_id=$3 FOR UPDATE`, id, appID, ownerID))
}

func (r *Repo) CreateInterview(ctx context.Context, q database.Querier, it *Interview) error {
	if it.Progress == "" {
		it.Progress = ProgressAwaitingSchedule
	}
	if it.Result == "" {
		it.Result = ResultUnknown
	}
	return q.QueryRow(ctx, `INSERT INTO interviews(application_id, owner_id, round_name, format,
		scheduled_at, timezone, duration_minutes, progress, result, invited_at, completed_at,
		completed_unknown, feedback, notes)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id, created_at`,
		it.ApplicationID, it.OwnerID, it.RoundName, it.Format, it.ScheduledAt, it.Timezone,
		it.DurationMinutes, it.Progress, it.Result, it.InvitedAt, it.CompletedAt,
		it.CompletedUnknown, it.Feedback, it.Notes).Scan(&it.ID, &it.CreatedAt)
}

func (r *Repo) UpdateInterview(ctx context.Context, q database.Querier, it *Interview) error {
	tag, err := q.Exec(ctx, `UPDATE interviews SET round_name=$1, format=$2, scheduled_at=$3,
		timezone=$4, duration_minutes=$5, progress=$6, result=$7, invited_at=$8, completed_at=$9,
		completed_unknown=$10, feedback=$11, notes=$12, updated_at=now()
		WHERE id=$13 AND application_id=$14 AND owner_id=$15`,
		it.RoundName, it.Format, it.ScheduledAt, it.Timezone, it.DurationMinutes, it.Progress,
		it.Result, it.InvitedAt, it.CompletedAt, it.CompletedUnknown, it.Feedback, it.Notes,
		it.ID, it.ApplicationID, it.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// -- assessment rounds --

func (r *Repo) ListAssessments(ctx context.Context, appID, ownerID int64) ([]*AssessmentRound, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT `+assessmentCols+`
		FROM assessment_rounds WHERE application_id=$1 AND owner_id=$2
		ORDER BY COALESCE(planned_at, due_at, created_at) ASC, id`, appID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AssessmentRound
	for rows.Next() {
		a, err := scanAssessment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repo) GetAssessment(ctx context.Context, q database.Querier, appID, ownerID, id int64) (*AssessmentRound, error) {
	return scanAssessment(q.QueryRow(ctx, `SELECT `+assessmentCols+`
		FROM assessment_rounds WHERE id=$1 AND application_id=$2 AND owner_id=$3`, id, appID, ownerID))
}

// GetAssessmentForUpdate is GetAssessment with a row lock, for the write paths
// that read-merge inside a transaction (PATCH / complete / reopen / the
// substatus mirror). Same rationale as GetInterviewForUpdate: a concurrent
// edit between the read and the write would otherwise be silently overwritten.
func (r *Repo) GetAssessmentForUpdate(ctx context.Context, q database.Querier, appID, ownerID, id int64) (*AssessmentRound, error) {
	return scanAssessment(q.QueryRow(ctx, `SELECT `+assessmentCols+`
		FROM assessment_rounds WHERE id=$1 AND application_id=$2 AND owner_id=$3 FOR UPDATE`, id, appID, ownerID))
}

func (r *Repo) CreateAssessment(ctx context.Context, q database.Querier, a *AssessmentRound) error {
	if a.Kind == "" {
		a.Kind = "online_test"
	}
	if a.Progress == "" {
		a.Progress = ProgressPreparing
	}
	if a.Result == "" {
		a.Result = ResultUnknown
	}
	return q.QueryRow(ctx, `INSERT INTO assessment_rounds(application_id, owner_id, kind, name,
		progress, result, invited_at, planned_at, due_at, completed_at, completed_unknown, link, notes)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id, created_at`,
		a.ApplicationID, a.OwnerID, a.Kind, a.Name, a.Progress, a.Result, a.InvitedAt, a.PlannedAt,
		a.DueAt, a.CompletedAt, a.CompletedUnknown, a.Link, a.Notes).Scan(&a.ID, &a.CreatedAt)
}

func (r *Repo) UpdateAssessment(ctx context.Context, q database.Querier, a *AssessmentRound) error {
	tag, err := q.Exec(ctx, `UPDATE assessment_rounds SET kind=$1, name=$2, progress=$3, result=$4,
		invited_at=$5, planned_at=$6, due_at=$7, completed_at=$8, completed_unknown=$9,
		link=$10, notes=$11, updated_at=now()
		WHERE id=$12 AND application_id=$13 AND owner_id=$14`,
		a.Kind, a.Name, a.Progress, a.Result, a.InvitedAt, a.PlannedAt, a.DueAt, a.CompletedAt,
		a.CompletedUnknown, a.Link, a.Notes, a.ID, a.ApplicationID, a.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repo) DeleteAssessment(ctx context.Context, q database.Querier, appID, ownerID, id int64) error {
	tag, err := q.Exec(ctx, `DELETE FROM assessment_rounds WHERE id=$1 AND application_id=$2 AND owner_id=$3`, id, appID, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	// A deleted round must not dangle as an application's focus: later
	// transitions derive the substatus from the focused round and would fail.
	_, err = q.Exec(ctx, `UPDATE applications SET focus_activity_kind=NULL, focus_activity_id=NULL,
		version=version+1, updated_at=now()
		WHERE owner_id=$1 AND focus_activity_kind=$2 AND focus_activity_id=$3`, ownerID, "assessment", id)
	return err
}

// OpenAssessmentCount counts rounds that are still in progress, used for the
// 「另有 N 项待完成」 hint next to the focused stage.
func (r *Repo) OpenAssessmentCount(ctx context.Context, appID, ownerID int64) (int, error) {
	var n int
	err := r.db.Pool().QueryRow(ctx, `SELECT count(*) FROM assessment_rounds
		WHERE application_id=$1 AND owner_id=$2 AND progress='preparing'`, appID, ownerID).Scan(&n)
	return n, err
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

func (r *Repo) DeleteInterview(ctx context.Context, q database.Querier, appID, ownerID, id int64) error {
	tag, err := q.Exec(ctx, `DELETE FROM interviews WHERE id=$1 AND application_id=$2 AND owner_id=$3`, id, appID, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	// Same dangling-focus cleanup as DeleteAssessment.
	_, err = q.Exec(ctx, `UPDATE applications SET focus_activity_kind=NULL, focus_activity_id=NULL,
		version=version+1, updated_at=now()
		WHERE owner_id=$1 AND focus_activity_kind=$2 AND focus_activity_id=$3`, ownerID, "interview", id)
	return err
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

func (r *Repo) GetNote(ctx context.Context, ownerID, id int64) (*Note, error) {
	var n Note
	err := r.db.Pool().QueryRow(ctx, `SELECT id, application_id, owner_id, content_md, created_at, updated_at
		FROM notes WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&n.ID, &n.ApplicationID, &n.OwnerID, &n.ContentMD, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
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
	// Owner-safe upsert: first try an owner-scoped UPDATE of the existing row;
	// if none matched (no row yet, or the row belongs to someone else) attempt
	// the INSERT. The insert's ON CONFLICT (interview_id) then fires only when
	// another owner already owns the link — DO NOTHING + RowsAffected==0 tells
	// the caller the row is not theirs, so a foreign upsert fails loudly
	// instead of silently overwriting or silently no-op'ing.
	tag, err := q.Exec(ctx, `UPDATE schedule_links SET
		meeting_url=$3, location=$4, contact_name=$5, contact_email=$6, notes=$7,
		cancelled=$8, cancelled_reason=$9, original_timezone=$10, updated_at=now()
		WHERE interview_id=$1 AND owner_id=$2`,
		s.InterviewID, s.OwnerID, s.MeetingURL, s.Location, s.ContactName, s.ContactEmail,
		s.Notes, s.Cancelled, s.CancelledReason, s.OriginalTimezone)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	tag2, err := q.Exec(ctx, `INSERT INTO schedule_links(interview_id, owner_id, meeting_url, location,
		contact_name, contact_email, notes, cancelled, cancelled_reason, original_timezone)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (interview_id) DO NOTHING`,
		s.InterviewID, s.OwnerID, s.MeetingURL, s.Location, s.ContactName, s.ContactEmail,
		s.Notes, s.Cancelled, s.CancelledReason, s.OriginalTimezone)
	if err != nil {
		return err
	}
	if tag2.RowsAffected() == 0 {
		// The interview's schedule link exists but belongs to another owner.
		return ErrNotFound
	}
	return nil
}
