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

	// Years spans ALL submissions (not just the requested window) so the year
	// switcher can offer every year the user actually has data for.
	yrows, err := q.Query(ctx, `SELECT DISTINCT EXTRACT(YEAR FROM (submitted_at AT TIME ZONE $2))::int AS y
		FROM applications
		WHERE owner_id = $1 AND deleted_at IS NULL AND submitted_at IS NOT NULL
		ORDER BY y`, ownerID, tz)
	if err != nil {
		return nil, err
	}
	defer yrows.Close()
	for yrows.Next() {
		var y int
		if err := yrows.Scan(&y); err != nil {
			return nil, err
		}
		h.Years = append(h.Years, y)
	}
	if err := yrows.Err(); err != nil {
		return nil, err
	}

	// 另有 N 条没有投递日期: past 未投递 (the complement of toApplyFactSQL —
	// single source of truth for 「还没投出去」) but with no submitted_at, so
	// they cannot land on a day. Archived rows count here too, matching the
	// day cells.
	if err := q.QueryRow(ctx, `SELECT count(*) FROM applications
		WHERE owner_id = $1 AND deleted_at IS NULL AND submitted_at IS NULL
		  AND NOT `+toApplyFactSQL, ownerID).Scan(&h.Undated); err != nil {
		return nil, err
	}

	return h, nil
}
