package repository

import (
	"testing"
	"time"
)

// testPoint builds a stage point for the pure builders. Corrections are already
// resolved by the application_stage_points view, so these builders never see a
// correction row — that resolution is covered by the integration suite
// (tests/integration/stage_points_view_test.go).
func testPoint(appID int64, status string, occurred *time.Time, source string) *StagePoint {
	return &StagePoint{ApplicationID: appID, Status: status, OccurredAt: occurred, Source: source}
}

func at(y int, m time.Month, d, hh, mm int, loc *time.Location) *time.Time {
	t := time.Date(y, m, d, hh, mm, 0, 0, loc)
	return &t
}

func TestBuildStageHistoryRecordsFirstArrivalPerStatus(t *testing.T) {
	// Arrange: saved (09-01) → applied (09-05) → interviewing (09-09) → rejected (09-11).
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "saved", at(2026, 9, 1, 12, 0, loc), "event"),
		testPoint(1, "applied", at(2026, 9, 5, 10, 0, loc), "event"),
		testPoint(1, "interviewing", at(2026, 9, 9, 9, 30, loc), "milestone"),
		testPoint(1, "rejected", at(2026, 9, 11, 15, 0, loc), "milestone"),
	}

	// Act
	got := buildStageHistoryFromPoints(points, loc, nil)[1]

	// Assert
	want := map[string]string{
		"saved": "2026-09-01", "applied": "2026-09-05",
		"interviewing": "2026-09-09", "rejected": "2026-09-11",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("stage %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestBuildStageHistoryKeepsTheEarliestArrivalWhenAStageRepeats(t *testing.T) {
	// 回退再前进（面试 → OA → 面试）不能把「第一次到达面试」改写成第二次的日期。
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "interviewing", at(2026, 9, 9, 9, 0, loc), "milestone"),
		testPoint(1, "assessment", at(2026, 9, 10, 9, 0, loc), "milestone"),
		testPoint(1, "interviewing", at(2026, 9, 14, 9, 0, loc), "milestone"),
	}

	got := buildStageHistoryFromPoints(points, loc, nil)[1]

	if got["interviewing"] != "2026-09-09" {
		t.Errorf("interviewing = %q, want the first arrival 2026-09-09", got["interviewing"])
	}
}

func TestBuildStageHistoryUserTimezoneBucket(t *testing.T) {
	// A late-evening UTC instant is the NEXT calendar day in UTC+8.
	loc := time.FixedZone("CST", 8*60*60)
	points := []*StagePoint{testPoint(1, "applied", at(2026, 9, 5, 17, 30, time.UTC), "event")}

	got := buildStageHistoryFromPoints(points, loc, nil)[1]

	if got["applied"] != "2026-09-06" {
		t.Errorf("applied day in UTC+8 = %q, want 2026-09-06", got["applied"])
	}
}

func TestBuildStageHistoryNoPointsYieldsEmpty(t *testing.T) {
	if got := buildStageHistoryFromPoints(nil, time.UTC, nil); len(got) != 0 {
		t.Errorf("empty input produced %d apps", len(got))
	}
}

// 时间未定的节点（用户还没决定时间）没有日期可报告，绝不能伪造一个。
func TestBuildStageHistorySkipsPointsWithNoBusinessTime(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "applied", at(2026, 9, 5, 10, 0, loc), "event"),
		testPoint(1, "interviewing", nil, "milestone"),
	}

	got := buildStageHistoryFromPoints(points, loc, nil)[1]

	if _, ok := got["interviewing"]; ok {
		t.Errorf("a timeless node must contribute no arrival day; got %q", got["interviewing"])
	}
	if got["applied"] != "2026-09-05" {
		t.Errorf("applied = %q, want 2026-09-05", got["applied"])
	}
}

// The user-entered 投递时间 wins for the applied stage: a legacy row whose
// applied point still carries the DB "now" day must render the backfilled
// submission day instead.
func TestBuildStageHistorySubmittedAtOverridesApplied(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "saved", at(2026, 9, 8, 9, 0, loc), "event"),
		testPoint(1, "applied", at(2026, 9, 8, 9, 5, loc), "event"),
	}
	sub := time.Date(2026, 9, 6, 14, 0, 0, 0, loc)

	got := buildStageHistoryFromPoints(points, loc, map[int64]*time.Time{1: &sub})[1]

	if got["applied"] != "2026-09-06" {
		t.Errorf("applied = %q, want backfilled 2026-09-06", got["applied"])
	}
	if got["saved"] != "2026-09-08" {
		t.Errorf("saved = %q, want 2026-09-08", got["saved"])
	}

	// A nil map must keep the point-derived day (callers without snapshots).
	got = buildStageHistoryFromPoints(points, loc, nil)[1]
	if got["applied"] != "2026-09-08" {
		t.Errorf("applied without override = %q, want 2026-09-08", got["applied"])
	}
}

// 推导规则：当前状态 = 时间线上最后一个阶段落点的状态。
func TestReplayStagePointsTakesTheLastPoint(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "saved", at(2026, 9, 1, 9, 0, loc), "event"),
		testPoint(1, "applied", at(2026, 9, 5, 9, 0, loc), "milestone"),
		testPoint(1, "assessment", at(2026, 9, 7, 9, 0, loc), "milestone"),
		testPoint(1, "interviewing", at(2026, 9, 9, 9, 0, loc), "milestone"),
	}

	d := replayStagePoints(points)

	if d.status != "interviewing" {
		t.Errorf("status = %q, want interviewing", d.status)
	}
	if d.submitted == nil || !d.submitted.Equal(*at(2026, 9, 5, 9, 0, loc)) {
		t.Errorf("submitted = %v, want 2026-09-05T09:00Z", d.submitted)
	}
}

func TestReplayStagePointsWithNoPointsStaysSaved(t *testing.T) {
	d := replayStagePoints(nil)
	if d.status != "saved" {
		t.Errorf("status = %q, want saved", d.status)
	}
	if d.submitted != nil || d.rejected != nil || d.accept != nil {
		t.Errorf("no points must produce no dates; got %v %v %v", d.submitted, d.rejected, d.accept)
	}
}

// 回退出「已投递」再回来，不能改写投递时间：日期取首次到达。
func TestReplayStagePointsKeepsTheFirstSubmissionTime(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "applied", at(2026, 9, 5, 9, 0, loc), "milestone"),
		testPoint(1, "saved", at(2026, 9, 6, 9, 0, loc), "milestone"),
		testPoint(1, "applied", at(2026, 9, 7, 9, 0, loc), "milestone"),
	}

	d := replayStagePoints(points)

	if d.submitted == nil || !d.submitted.Equal(*at(2026, 9, 5, 9, 0, loc)) {
		t.Errorf("submitted = %v, want the first 2026-09-05T09:00Z", d.submitted)
	}
}

// 时间未定的节点决定当前阶段（它在时间线最后），但不能提供投递时间。
func TestReplayStagePointsTimelessPointSetsStageButNoDate(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "saved", at(2026, 9, 1, 9, 0, loc), "event"),
		testPoint(1, "applied", nil, "milestone"),
	}

	d := replayStagePoints(points)

	if d.status != "applied" {
		t.Errorf("status = %q, want applied", d.status)
	}
	if d.submitted != nil {
		t.Errorf("a timeless node must not fabricate 投递时间; got %v", d.submitted)
	}
}

func TestBuildProgressSinceTracksTheLastStageChange(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "saved", at(2026, 9, 1, 9, 0, loc), "event"),
		testPoint(1, "applied", at(2026, 9, 5, 9, 0, loc), "milestone"),
		testPoint(1, "interviewing", at(2026, 9, 9, 9, 0, loc), "milestone"),
	}

	got := buildProgressSinceFromPoints(points, loc)

	if got[1] != "2026-09-09" {
		t.Errorf("progress_since = %q, want 2026-09-09", got[1])
	}
}

// 重复记录同一个阶段（两轮面试各记一个节点）不重启「停留至今」的计时。
func TestBuildProgressSinceIgnoresRepeatsOfTheSameStage(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "interviewing", at(2026, 9, 9, 9, 0, loc), "milestone"),
		testPoint(1, "interviewing", at(2026, 9, 14, 9, 0, loc), "milestone"),
	}

	got := buildProgressSinceFromPoints(points, loc)

	if got[1] != "2026-09-09" {
		t.Errorf("progress_since = %q, want the first entry 2026-09-09", got[1])
	}
}

// 最后一格没有时间时，「进入当前进度的日期」是不详的——不能沿用上一段的日期。
func TestBuildProgressSinceReportsNothingWhenTheCurrentStageHasNoTime(t *testing.T) {
	loc := time.UTC
	points := []*StagePoint{
		testPoint(1, "applied", at(2026, 9, 5, 9, 0, loc), "event"),
		testPoint(1, "interviewing", nil, "milestone"),
	}

	got := buildProgressSinceFromPoints(points, loc)

	if v, ok := got[1]; ok {
		t.Errorf("progress_since = %q, want no entry", v)
	}
}

// A note is unrestricted user text. The idempotency marker is always APPENDED,
// so only a trailing occurrence is bookkeeping — an occurrence inside what the
// user typed must survive, or the API returns something other than what is
// stored.
func TestStripIdempotencyMarkerOnlyRemovesTheGeneratedSuffix(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"generated suffix on an empty note", "|idem:ui-123", ""},
		{"generated suffix after real text", "内推直接进面|idem:ui-123", "内推直接进面"},
		{"no marker at all", "内推直接进面", "内推直接进面"},
		{"marker text typed by the user mid-note", "讨论了 |idem: 这个前缀的设计", "讨论了 |idem: 这个前缀的设计"},
		{"marker text typed by the user at the end", "前缀写作 |idem: 加上 key", "前缀写作 |idem: 加上 key"},
		{"user text plus a real generated suffix", "聊到 |idem: 前缀|idem:ui-123", "聊到 |idem: 前缀"},
		{"only the last suffix is bookkeeping", "a|idem:x1|idem:ui-2", "a|idem:x1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripIdempotencyMarker(tc.in); got != tc.want {
				t.Errorf("StripIdempotencyMarker(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
