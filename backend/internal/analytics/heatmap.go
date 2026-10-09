package analytics

import (
	"context"
	"time"

	"offerlog/backend/internal/platform/day"
)

// HeatmapDay is one calendar day that carries at least one submission. Days
// with no submissions are omitted on purpose: zero-filling and streak math are
// pure frontend functions (frontend/src/features/analytics/heatmap.ts), which
// keeps them unit-testable without a database.
type HeatmapDay struct {
	Date  string `json:"date"` // YYYY-MM-DD in the user's timezone
	Count int64  `json:"count"`
}

// Heatmap is the /analytics/heatmap payload.
//
// 口径 (docs/投递热力图方案.md §2) — deliberately NOT the same live set as
// every other analytics query (whereClause drops archived rows):
//
//   - the day is the user's calendar day, derived from applications.submitted_at
//     (a derived column since migration 00006: the first 「已投递」 timeline
//     point, already corrected for 误操作更正 and user nodes), converted with
//     `AT TIME ZONE tz` — a 23:30 Shanghai submission belongs to that day, not
//     to the previous UTC day;
//   - 已删除（回收站）不算 —— 用户已经不认这条记录;
//   - 已归档**算** —— 归档只表示不想再看到，不改变那天确实投过。这一点和
//     「已投递」指标卡（不含归档）不同，前端图下方必须写明;
//   - 没有 submitted_at 但已进入流程的记录（内推 / 猎头免投递）单独计入
//     Undated —— 没有日期就没法放进某一天，但也不能悄悄漏掉.
type Heatmap struct {
	Timezone string       `json:"timezone"`
	From     string       `json:"from"`  // YYYY-MM-DD, inclusive, user timezone
	To       string       `json:"to"`    // YYYY-MM-DD, inclusive, user timezone
	Today    string       `json:"today"` // server-computed 用户时区的今天，前端算连续天数用它，不依赖浏览器时区
	Days     []HeatmapDay `json:"days"`
	Total    int64        `json:"total"`
	Undated  int64        `json:"undated"`
	Years    []int        `json:"years"` // 有投递记录的年份，供年份切换器使用（避免点开空年份）
}

// enteredPipelineSQL: the row reached the employer even without a submission
// fact — it holds a response, or the timeline has a point at 初筛 or later.
// 已拒 counts (a rejection means the employer saw it); 撤回 / 关闭 alone do not.
const enteredPipelineSQL = `first_response_at IS NOT NULL OR EXISTS (SELECT 1 FROM application_stage_points p
	WHERE p.application_id = applications.id
	  AND p.status IN ('screening','assessment','interviewing','offer','accepted','rejected'))`

// Heatmap buckets submissions into user-timezone calendar days in [from, to]
// (inclusive, date-only values at UTC midnight per the day package).
//
// tz must already be SafeLocation-validated by the caller: PostgreSQL's
// `AT TIME ZONE` rejects "" / "Local" and fails the query forever.
//
// No new index is needed: range filters are written against submitted_at
// itself (never `(... AT TIME ZONE tz)::date BETWEEN`), so the existing
// applications_owner_submitted_idx on (owner_id, submitted_at) applies.
func (r *Repo) Heatmap(ctx context.Context, ownerID int64, tz string, from, to time.Time) (*Heatmap, error) {
	q := r.db.Pool()
	h := &Heatmap{
		Timezone: tz,
		From:     day.Format(from),
		To:       day.Format(to),
		Days:     []HeatmapDay{},
		Years:    []int{},
	}

	// fromDay / toDay travel as text so `::date` is session-timezone
	// independent, then a single `date::timestamp AT TIME ZONE tz` turns each
	// user-local day boundary into an instant (calendar/repo.go:176 — the
	// reverse cast would double-shift through the UTC session zone).
	rows, err := q.Query(ctx, `SELECT (submitted_at AT TIME ZONE $2)::date AS d, count(*)
		FROM applications
		WHERE owner_id = $1
		  AND deleted_at IS NULL
		  AND submitted_at IS NOT NULL
		  AND submitted_at >= ($3::date::timestamp AT TIME ZONE $2)
		  AND submitted_at <  (($4::date + 1)::timestamp AT TIME ZONE $2)
		GROUP BY d
		ORDER BY d`, ownerID, tz, h.From, h.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d time.Time
		var n int64
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		h.Days = append(h.Days, HeatmapDay{Date: day.Format(d), Count: n})
		h.Total += n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Years and Undated don't depend on the window, so they share one pass
	// over the owner's rows instead of two extra round trips.
	//
	// Years spans ALL submissions (not just the requested window) so the year
	// switcher can offer every year the user actually has data for.
	//
	// 另有 N 条没有投递日期: no submitted_at, yet the row provably entered the
	// hiring pipeline — a response, or a stage point at 初筛 or later (内推 /
	// 猎头 直接约面). NOT just 「not toApplyFactSQL」: that complement also
	// matches a job closed or withdrawn straight from 收藏 without ever being
	// sent, which the panel would then mislabel as 免投递. Archived rows count
	// here too, matching the day cells.
	var years []int32
	if err := q.QueryRow(ctx, `SELECT
		COALESCE(array_agg(DISTINCT EXTRACT(YEAR FROM (submitted_at AT TIME ZONE $2))::int)
		         FILTER (WHERE submitted_at IS NOT NULL), '{}'),
		count(*) FILTER (WHERE submitted_at IS NULL AND (`+enteredPipelineSQL+`))
		FROM applications
		WHERE owner_id = $1 AND deleted_at IS NULL`, ownerID, tz).Scan(&years, &h.Undated); err != nil {
		return nil, err
	}
	for _, y := range years {
		h.Years = append(h.Years, int(y))
	}

	return h, nil
}
