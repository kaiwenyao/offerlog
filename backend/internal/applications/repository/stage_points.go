// Stage points: the unified read model behind 「岗位现在处于哪个阶段」.
//
// 迁移 00006 起，阶段落点有两个来源——追加式的状态事件（application_events，
// 含更正覆盖）和用户自己添加的时间线节点（application_milestones.status_effect）。
// 视图 application_stage_points 把它们合成一张表，本文件是它的 Go 侧读取与回放。
//
// 排序就是用户在时间线面板上看到的顺序（前端 mergeTimeline 的同款规则）：
//
//	建档钉最前 → 业务时间升序 → 时间未定的排最后 → 同刻状态事件在前 → id
//
// 于是推导规则只有一条、且看图即可预期：
//
//	当前状态 = 时间线上最后一个阶段落点的状态
//
// 这与旧的 ResyncStatusFromEvents 有一处**有意的语义差异**：旧实现按 sequence
// （录入顺序）回放，因为当时「补录」只能通过状态机、录入顺序才是权威。新模型
// 里用户直接摆弄时间线，排序权威只能是业务时间——否则「把 Offer 的时间改到最早」
// 会让面板显示 Offer 在最前、状态却还停在 Offer，自相矛盾。
package repository

import (
	"context"
	"time"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
	"offerlog/backend/internal/platform/timeutil"
)

// StagePoint is one moment at which the application's stage was asserted.
type StagePoint struct {
	ApplicationID int64
	Status        string
	// Substatus is the concrete progress inside the stage, and only the legacy
	// status events ever assert one — a user node says what happened, never how
	// far along inside a stage. "" therefore means 「这一格没有主张子状态」,
	// which is how adding an event drops a refinement that no longer applies.
	Substatus string
	// OccurredAt is the business time; nil = 时间未定 (a user node whose time the
	// user has not decided yet). Such a point still counts for the CURRENT
	// status — it sorts last, exactly where the timeline draws it — but it can
	// never supply a date (submitted_at, stage history…).
	OccurredAt *time.Time
	// Note is 「这一步为什么发生」: an event's reason, or a user node's note.
	// Feeds applications.reason when the timeline ends on a terminal stage.
	Note   string
	Source string // event | milestone
}

// stagePointCols / stagePointOrder keep the two read paths (one app, many apps)
// on exactly one definition of "timeline order".
const stagePointCols = `application_id, status, COALESCE(substatus, ''), occurred_at, note, source`

const stagePointOrder = `pinned_first DESC, occurred_at ASC NULLS LAST, (source = 'milestone') ASC, source_id ASC`

func scanStagePoints(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}) ([]*StagePoint, error) {
	defer rows.Close()
	var out []*StagePoint
	for rows.Next() {
		var p StagePoint
		if err := rows.Scan(&p.ApplicationID, &p.Status, &p.Substatus, &p.OccurredAt, &p.Note, &p.Source); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ListStagePoints returns one application's stage points in timeline order.
func (r *Repo) ListStagePoints(ctx context.Context, q database.Querier, appID, ownerID int64) ([]*StagePoint, error) {
	rows, err := q.Query(ctx, `SELECT `+stagePointCols+` FROM application_stage_points
		WHERE application_id=$1 AND owner_id=$2 ORDER BY `+stagePointOrder, appID, ownerID)
	if err != nil {
		return nil, err
	}
	return scanStagePoints(rows)
}

// ListStagePointsByApps feeds a whole list page from one round-trip.
func (r *Repo) ListStagePointsByApps(ctx context.Context, ownerID int64, appIDs []int64) ([]*StagePoint, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT `+stagePointCols+` FROM application_stage_points
		WHERE owner_id=$1 AND application_id = ANY($2::bigint[])
		ORDER BY application_id, `+stagePointOrder, ownerID, appIDs)
	if err != nil {
		return nil, err
	}
	return scanStagePoints(rows)
}

// derived is what a replay of one application's stage points produces.
type derived struct {
	status    string
	substatus string
	// reason is carried only by a terminal ending: reopening a record must
	// clear the 「为什么结束」 card rather than leave the old ending's words
	// explaining a live application.
	reason                      string
	submitted, rejected, accept *time.Time
}

// replayStagePoints walks the timeline and reads off the snapshot facts. The
// date facts take the FIRST time each status was reached (a rollback out of
// 已投递 and back must not rewrite 投递时间), and a point with no business time
// supplies no date at all rather than a fabricated one.
func replayStagePoints(points []*StagePoint) derived {
	d := derived{status: domain.StatusSaved}
	for _, p := range points {
		d.status = p.Status
		d.substatus = p.Substatus
		d.reason = ""
		if domain.TerminalStatuses[p.Status] {
			d.reason = p.Note
		}
		if p.OccurredAt == nil {
			continue
		}
		at := *p.OccurredAt
		switch {
		case p.Status == domain.StatusApplied && d.submitted == nil:
			d.submitted = &at
		case p.Status == domain.StatusRejected && d.rejected == nil:
			d.rejected = &at
		case p.Status == domain.StatusAccepted && d.accept == nil:
			d.accept = &at
		}
	}
	return d
}

// RecomputeStatus re-derives the application snapshot from its stage points and
// writes it back. Every path that can change the timeline — adding, editing or
// deleting a user node, and the legacy /transitions + /correct-current
// endpoints — ends here, so there is exactly one place that decides what stage
// a record is in.
//
// substatus follows the last point that asserts one. User nodes never do
// (since 00006 the user no longer picks a substatus — it is mirrored from an
// OA / 面试 round by SyncFromActivity), so a point without one keeps whatever
// the round sync last wrote as long as the record stays in the same stage, and
// drops it when the stage moves. The legacy /transitions and /correct-current
// endpoints still assert substatuses, and those keep winning where they appear.
func (r *Repo) RecomputeStatus(ctx context.Context, q database.Querier, appID, ownerID int64) error {
	points, err := r.ListStagePoints(ctx, q, appID, ownerID)
	if err != nil {
		return err
	}
	d := replayStagePoints(points)

	// A focus reference (and the substatus derived from it) only survives while
	// the record still sits in that activity's stage.
	focusKind := ""
	switch d.status {
	case domain.StatusAssessment:
		focusKind = domain.ActivityAssessment
	case domain.StatusInterviewing:
		focusKind = domain.ActivityInterview
	}
	_, err = q.Exec(ctx, `UPDATE applications SET
		substatus = CASE WHEN $9::text <> '' THEN $9
		                 WHEN status = $1 THEN substatus
		                 ELSE NULL END,
		focus_activity_kind = CASE WHEN $6::text IS NOT NULL AND focus_activity_kind = $6 THEN focus_activity_kind ELSE NULL END,
		focus_activity_id   = CASE WHEN $6::text IS NOT NULL AND focus_activity_kind = $6 THEN focus_activity_id   ELSE NULL END,
		status = $1, submitted_at = $2, rejected_at = $3, accepted_at = $4, reason = $8,
		version = version + 1, updated_at = now()
		WHERE id = $5 AND owner_id = $7`,
		d.status, d.submitted, d.rejected, d.accept, appID, nullIfEmpty(focusKind), ownerID, d.reason, d.substatus)
	return err
}

// buildStageHistoryFromPoints records the earliest calendar day (user zone) at
// which each status was reached, per application. Points with no business time
// contribute nothing: the stage rail would otherwise have to invent a day.
//
// submittedAt carries each application's user-entered 投递时间: when present it
// wins for the applied stage, because the trail must show when the user
// actually submitted rather than when the record happened to be touched.
func buildStageHistoryFromPoints(points []*StagePoint, loc *time.Location, submittedAt map[int64]*time.Time) map[int64]map[string]string {
	first := map[int64]map[string]time.Time{}
	for _, p := range points {
		if p.OccurredAt == nil {
			continue
		}
		if first[p.ApplicationID] == nil {
			first[p.ApplicationID] = map[string]time.Time{}
		}
		if t, seen := first[p.ApplicationID][p.Status]; !seen || p.OccurredAt.Before(t) {
			first[p.ApplicationID][p.Status] = *p.OccurredAt
		}
	}
	for appID, sub := range submittedAt {
		if sub == nil {
			continue
		}
		if first[appID] == nil {
			first[appID] = map[string]time.Time{}
		}
		first[appID][domain.StatusApplied] = *sub
	}
	out := make(map[int64]map[string]string, len(first))
	for appID, stages := range first {
		m := make(map[string]string, len(stages))
		for st, t := range stages {
			m[st] = timeutil.DateOnly(t, loc)
		}
		out[appID] = m
	}
	return out
}

// buildProgressSinceFromPoints keeps the business time of the last point that
// actually MOVED the stage (方案 §5「最近一次进入当前进度的日期」). Re-recording
// the same stage, or a point with no time, does not restart the clock.
func buildProgressSinceFromPoints(points []*StagePoint, loc *time.Location) map[int64]string {
	type state struct {
		status string
		last   time.Time
		seen   bool
	}
	cur := map[int64]*state{}
	var order []int64
	for _, p := range points {
		s := cur[p.ApplicationID]
		if s == nil {
			s = &state{}
			cur[p.ApplicationID] = s
			order = append(order, p.ApplicationID)
		}
		if p.OccurredAt == nil {
			// 时间未定的节点仍然决定当前阶段，但没有日期可以报告；把它记成
			// 「阶段变了、时间不详」而不是沿用上一段的日期。
			if p.Status != s.status {
				s.status, s.seen = p.Status, false
			}
			continue
		}
		if !s.seen || p.Status != s.status {
			s.last, s.seen = *p.OccurredAt, true
		}
		s.status = p.Status
	}
	out := map[int64]string{}
	for _, appID := range order {
		if s := cur[appID]; s.seen {
			out[appID] = timeutil.DateOnly(s.last, loc)
		}
	}
	return out
}
