package repository

import (
	"testing"
	"time"
)

// testEvent builds an event row for the pure stage-history builder.
func testEvent(appID int64, typ string, from, to *string, occurred time.Time, corrects *int64) *Event {
	return &Event{ApplicationID: appID, EventType: typ, FromStatus: from, ToStatus: to,
		OccurredAt: occurred, CorrectsEventID: corrects}
}

func ptr(s string) *string { return &s }

func TestBuildStageHistoryRecordsFirstArrivalPerStatus(t *testing.T) {
	loc := time.UTC
	dublin := time.FixedZone("DUB", 60*60)
	_ = dublin

	// created → saved (2026-09-01), applied (09-05), interviewing (09-09),
	// rejected (09-11).
	evs := []*Event{
		testEvent(1, "created", nil, ptr("saved"), time.Date(2026, 9, 1, 12, 0, 0, 0, loc), nil),
		testEvent(1, "status_change", ptr("saved"), ptr("applied"), time.Date(2026, 9, 5, 10, 0, 0, 0, loc), nil),
		testEvent(1, "status_change", ptr("applied"), ptr("interviewing"), time.Date(2026, 9, 9, 9, 30, 0, 0, loc), nil),
		testEvent(1, "status_change", ptr("interviewing"), ptr("rejected"), time.Date(2026, 9, 11, 15, 0, 0, 0, loc), nil),
	}
	got := buildStageHistory(evs, loc, nil)[1]
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

func TestBuildStageHistoryCorrectionReplacesStatus(t *testing.T) {
	loc := time.UTC
	// Events: applied on 09-05 then a correction rewrites that event to
	// screening; applied arrives again on 09-08.
	first := testEvent(1, "status_change", ptr("saved"), ptr("applied"), time.Date(2026, 9, 5, 10, 0, 0, 0, loc), nil)
	corr := testEvent(1, "correction", ptr("applied"), ptr("screening"), time.Date(2026, 9, 6, 0, 0, 0, 0, loc), &first.ID)
	_ = corr
	first.ID = 11
	// A status-only fix: the correction carries the SAME occurred_at as the
	// event it corrects (what the 纠正 dialog sends when only the status is
	// changed), so the arrival day must not move.
	corr2 := testEvent(1, "correction", ptr("applied"), ptr("screening"), time.Date(2026, 9, 5, 10, 0, 0, 0, loc), &first.ID)
	appliedAgain := testEvent(1, "status_change", ptr("screening"), ptr("applied"), time.Date(2026, 9, 8, 12, 0, 0, 0, loc), nil)
	got := buildStageHistory([]*Event{first, corr2, appliedAgain}, loc, nil)[1]
	if got["applied"] != "2026-09-08" {
		t.Errorf("corrected first applied should be dropped; applied = %q, want 2026-09-08", got["applied"])
	}
	if got["screening"] != "2026-09-05" {
		t.Errorf("screening should keep the corrected event's day; got %q, want 2026-09-05", got["screening"])
	}
}

func TestBuildStageHistoryUserTimezoneBucket(t *testing.T) {
	// A late-evening UTC instant is the NEXT calendar day in UTC+8.
	loc := time.FixedZone("CST", 8*60*60)
	evs := []*Event{
		testEvent(1, "status_change", ptr("saved"), ptr("applied"), time.Date(2026, 9, 5, 17, 30, 0, 0, time.UTC), nil),
	}
	got := buildStageHistory(evs, loc, nil)[1]
	if got["applied"] != "2026-09-06" {
		t.Errorf("applied day in UTC+8 = %q, want 2026-09-06", got["applied"])
	}
}

func TestBuildStageHistoryNoEventsYieldsEmpty(t *testing.T) {
	got := buildStageHistory(nil, time.UTC, nil)
	if len(got) != 0 {
		t.Errorf("empty input produced %d apps", len(got))
	}
}

// The user-entered 投递时间 wins for the applied stage: a legacy row whose
// applied event still carries the DB "now" day must render the backfilled
// submission day instead.
func TestBuildStageHistorySubmittedAtOverridesApplied(t *testing.T) {
	loc := time.UTC
	// Record created today (2026-09-08), applied event recorded today as well
	// (occurred_at defaulted to the write clock), but the user backfilled
	// submitted_at = 2026-09-06.
	evs := []*Event{
		testEvent(1, "created", nil, ptr("saved"), time.Date(2026, 9, 8, 9, 0, 0, 0, loc), nil),
		testEvent(1, "status_change", ptr("saved"), ptr("applied"), time.Date(2026, 9, 8, 9, 5, 0, 0, loc), nil),
	}
	sub := time.Date(2026, 9, 6, 14, 0, 0, 0, loc)
	got := buildStageHistory(evs, loc, map[int64]*time.Time{1: &sub})[1]
	if got["applied"] != "2026-09-06" {
		t.Errorf("applied = %q, want backfilled 2026-09-06", got["applied"])
	}
	if got["saved"] != "2026-09-08" {
		t.Errorf("saved = %q, want 2026-09-08", got["saved"])
	}

	// A nil map must keep the event-derived day (callers without snapshots).
	got = buildStageHistory(evs, loc, nil)[1]
	if got["applied"] != "2026-09-08" {
		t.Errorf("applied without override = %q, want 2026-09-08", got["applied"])
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

// A correction exists to repair a wrong time, so the replay must adopt the
// correction's occurred_at — otherwise the timeline shows the fix while the
// stage rail and submitted_at keep the mistyped day.
func TestBuildStageHistoryCorrectionReplacesTime(t *testing.T) {
	loc := time.UTC
	orig := testEvent(1, "status_change", ptr("saved"), ptr("applied"), time.Date(2026, 9, 8, 10, 0, 0, 0, loc), nil)
	orig.ID = 21
	corr := testEvent(1, "correction", ptr("applied"), ptr("applied"), time.Date(2026, 9, 6, 10, 0, 0, 0, loc), &orig.ID)
	got := buildStageHistory([]*Event{orig, corr}, loc, nil)[1]
	if got["applied"] != "2026-09-06" {
		t.Errorf("applied = %q, want the corrected day 2026-09-06", got["applied"])
	}
}
