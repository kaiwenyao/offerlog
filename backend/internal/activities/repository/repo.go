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
	// 删除被关注的轮次要做两件收尾，缺一不可：
	//
	//  1. focus 必须清掉：悬挂引用会让下一次同阶段流转派生子状态时踩到不存在的轮次
	//     (GetAssessment 直接 pgx.ErrNoRows)。
	//  2. 由这一轮派生的 substatus 也必须清掉：否则岗位会永远显示「已完成 OA ·
	//     等结果」——详情头部芯片、数据库「状态」列、看板卡片全错，顶部「OA 后等
	//     结果」快捷筛选还会把它捞出来，而这个岗位一轮测评都没有了。取消路径当年
	//     按 PR #23 review 修过（见 SyncFromActivity 注释），删除路径漏了；这个 PR
	//     第一次让删除可达，所以这个洞现在才会被用户看见。
	//
	// 守卫 status=$4（该轮次所属阶段）照 SyncFromActivity 的先例：岗位已经推进到
	// 别的阶段时，substatus 可能是那一阶段派生的，不能因为删掉一个老轮次就清掉。
	// focus 的清理不受这个守卫限制（悬挂引用在哪个阶段都不能留）。
	_, err = q.Exec(ctx, `UPDATE applications SET focus_activity_kind=NULL, focus_activity_id=NULL,
		substatus = CASE WHEN status=$4 THEN '' ELSE substatus END,
		version=version+1, updated_at=now()
		WHERE owner_id=$1 AND focus_activity_kind=$2 AND focus_activity_id=$3`,
		ownerID, "assessment", id, "assessment")
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
	// Same dangling-focus + derived-substatus cleanup as DeleteAssessment:
	// 删掉面试轮次后岗位不能继续显示由它派生的「已完成 · 等反馈」
	// （守卫同为 status = 该轮次所属阶段，即 interviewing）。
	_, err = q.Exec(ctx, `UPDATE applications SET focus_activity_kind=NULL, focus_activity_id=NULL,
		substatus = CASE WHEN status=$4 THEN '' ELSE substatus END,
		version=version+1, updated_at=now()
		WHERE owner_id=$1 AND focus_activity_kind=$2 AND focus_activity_id=$3`,
		ownerID, "interview", id, "interviewing")
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

// DeleteAction removes the action row through the caller's Querier.
func (r *Repo) DeleteAction(ctx context.Context, q database.Querier, ownerID, id int64) error {
	_, err := q.Exec(ctx, `DELETE FROM actions WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

// ClearLegacyNextActionForApp removes an application's legacy next_action mirror
// (and its due columns) once that application has no action row left at all.
//
// 与 ClearLegacyNextActionWhenSettled 的分工：那条挂在「完成」路径上（靠 action
// 反查岗位，且只看未完成的待办）；这条挂在「删除」路径上——行删了就查不到它属于
// 哪个岗位，所以由调用方先取 application_id 再传进来。
//
// 为什么删掉最后一条待办**必须**清镜像：首页统一待办的 derived 分支有一个守卫
// ——「有 next_action 且该岗位一条 action 行都没有」（home/repo.go）。删掉唯一的
// action 行，守卫就失效，那条早被独立待办取代的旧 mirror 会以 action_id=null 的
// 形式复活到今日待办清单和侧栏计数里，而且只有「查看」——完不成、也删不掉。
func (r *Repo) ClearLegacyNextActionForApp(ctx context.Context, q database.Querier, ownerID, appID int64) error {
	_, err := q.Exec(ctx, `UPDATE applications ap SET next_action='', next_action_due_at=NULL,
		next_action_due_ts=NULL, version=version+1, updated_at=now()
		WHERE ap.owner_id=$1 AND ap.id=$2
		  AND NOT EXISTS (SELECT 1 FROM actions x WHERE x.application_id=ap.id AND x.owner_id=$1)`,
		ownerID, appID)
	return err
}

// DeleteActionAndSettle deletes an action and retires the application's legacy
// next_action mirror when this was its last action row.
//
// 两条语句必须在同一个事务里：只删行不清镜像正好会留下被复活的幽灵待办，
// 而只清镜像不删行则是另一头的不一致——半个操作就是它本来要防的自相矛盾。
// 删除本身幂等：行已不存在（或不属于本人）时静默当成删掉。
func (r *Repo) DeleteActionAndSettle(ctx context.Context, ownerID, id int64) error {
	return r.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// application_id 是可空列（00001_init.sql）：跨岗位待办没有岗位归属。
		// 必须扫进 *int64 —— 扫进 int64 时 pgx 对 NULL 直接报
		// `cannot scan NULL into *int64`，删除会 500 且行还在，下面那条
		// 「没有镜像可清」的分支永远走不到（死分支）。
		var appID *int64
		err := tx.QueryRow(ctx, `SELECT application_id FROM actions WHERE id=$1 AND owner_id=$2`,
			id, ownerID).Scan(&appID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := r.DeleteAction(ctx, tx, ownerID, id); err != nil {
			return err
		}
		if appID == nil {
			// 跨岗位待办（application_id IS NULL）没有镜像可清。
			return nil
		}
		return r.SyncNextActionMirror(ctx, tx, ownerID, *appID)
	})
}

func (r *Repo) MarkActionDone(ctx context.Context, q database.Querier, ownerID, id int64, done bool) error {
	col := "NULL"
	if done {
		col = "now()"
	}
	_, err := q.Exec(ctx, `UPDATE actions SET done_at=`+col+`, updated_at=now() WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

// SyncNextActionMirror rewrites the application's legacy next_action /
// next_action_due_at snapshot from the earliest still-open action. No open
// action → clear the snapshot. The table 「下一步 / 截止」 columns read this
// snapshot, so postpone / completing one of several todos used to leave a
// stale title and date on the row.
func (r *Repo) SyncNextActionMirror(ctx context.Context, q database.Querier, ownerID, appID int64) error {
	var title string
	var dueDate, dueTs *time.Time
	err := q.QueryRow(ctx, `SELECT title, due_date, due_ts FROM actions
		WHERE application_id=$1 AND owner_id=$2 AND done_at IS NULL
		ORDER BY COALESCE(due_date, due_ts) NULLS LAST, id
		LIMIT 1`, appID, ownerID).Scan(&title, &dueDate, &dueTs)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = q.Exec(ctx, `UPDATE applications SET next_action='', next_action_due_at=NULL,
			next_action_due_ts=NULL, version=version+1, updated_at=now()
			WHERE owner_id=$1 AND id=$2`, ownerID, appID)
		return err
	}
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE applications SET next_action=$3, next_action_due_at=$4,
		next_action_due_ts=$5, version=version+1, updated_at=now()
		WHERE owner_id=$1 AND id=$2`, ownerID, appID, title, dueDate, dueTs)
	return err
}

// ClearLegacyNextActionWhenSettled refreshes the application's next_action
// snapshot after an action is completed: remaining open actions retarget the
// mirror; none left → clear it.
func (r *Repo) ClearLegacyNextActionWhenSettled(ctx context.Context, q database.Querier, ownerID, actionID int64) error {
	var appID *int64
	err := q.QueryRow(ctx, `SELECT application_id FROM actions WHERE id=$1 AND owner_id=$2`,
		actionID, ownerID).Scan(&appID)
	if errors.Is(err, pgx.ErrNoRows) || appID == nil {
		return nil
	}
	if err != nil {
		return err
	}
	return r.SyncNextActionMirror(ctx, q, ownerID, *appID)
}

// MarkActionDoneAndSettle marks the action (un)done and, on completion, clears
// the legacy next_action mirror when no open action is left. Both statements run
// in one transaction: a half-applied pair is exactly the contradiction the mirror
// caused in the first place. Reopening an action deliberately does NOT restore
// the mirror — the standalone action is the truth, the mirror stays retired.
func (r *Repo) MarkActionDoneAndSettle(ctx context.Context, ownerID, id int64, done bool) error {
	return r.db.RunInTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := r.MarkActionDone(ctx, tx, ownerID, id, done); err != nil {
			return err
		}
		if !done {
			return nil
		}
		return r.ClearLegacyNextActionWhenSettled(ctx, tx, ownerID, id)
	})
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

// ListScheduleLinks returns the scheduling metadata for a set of interviews, keyed
// by interview id (interviews without a link are simply absent).
//
// 列表接口需要这个：cancelled 存在 schedule_links 里，一次一条地查是 N+1，而
// 前端要据此显示「已取消」并提供「恢复面试」——不带上就等于取消后无法撤销。
func (r *Repo) ListScheduleLinks(ctx context.Context, ownerID int64, interviewIDs []int64) (map[int64]*ScheduleLink, error) {
	out := map[int64]*ScheduleLink{}
	if len(interviewIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Pool().Query(ctx, `SELECT id, interview_id, owner_id, meeting_url, location,
		contact_name, contact_email, notes, cancelled, cancelled_reason, original_timezone
		FROM schedule_links WHERE owner_id=$1 AND interview_id = ANY($2)`, ownerID, interviewIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s ScheduleLink
		if err := rows.Scan(&s.ID, &s.InterviewID, &s.OwnerID, &s.MeetingURL, &s.Location, &s.ContactName,
			&s.ContactEmail, &s.Notes, &s.Cancelled, &s.CancelledReason, &s.OriginalTimezone); err != nil {
			return nil, err
		}
		copy := s
		out[s.InterviewID] = &copy
	}
	return out, rows.Err()
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
