package repository

import (
	"testing"
	"time"

	"offerlog/backend/internal/applications/domain"
)

// 落点契约：submittedAt 非空 ⇒ 事件里一定有一个业务时间等于它的「已投递」，
// 且最后一格仍然是建档时选的那个阶段（否则回放会把阶段退回去）。
func TestInitialEventsShape(t *testing.T) {
	saved := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	earlier := saved.AddDate(0, 0, -2)
	later := saved.AddDate(0, 0, 2)

	type want struct {
		statuses  []string // to_status，按插入顺序
		appliedAt *time.Time
		lastAt    time.Time
	}
	cases := []struct {
		name      string
		status    string
		submitted *time.Time
		want      want
	}{
		{
			name:   "待投递：只有建档一行",
			status: domain.StatusSaved, submitted: nil,
			want: want{statuses: []string{domain.StatusSaved}, lastAt: saved},
		},
		{
			name:   "当天建档当天投递：建档那一格自己就是投递落点",
			status: domain.StatusApplied, submitted: &saved,
			want: want{statuses: []string{domain.StatusApplied}, appliedAt: &saved, lastAt: saved},
		},
		{
			name:   "补录更早的投递时间：建档降级成待投递，投递单独成行",
			status: domain.StatusApplied, submitted: &earlier,
			want: want{
				statuses:  []string{domain.StatusSaved, domain.StatusApplied},
				appliedAt: &earlier, lastAt: earlier,
			},
		},
		{
			name:   "导入一条已经在面试的记录：投递 + 当前阶段各自成行",
			status: domain.StatusInterviewing, submitted: &earlier,
			want: want{
				statuses:  []string{domain.StatusSaved, domain.StatusApplied, domain.StatusInterviewing},
				appliedAt: &earlier, lastAt: saved,
			},
		},
		{
			name:   "投递时间晚于建档时刻（CSV 填了未来日期）：当前阶段仍排在投递之后",
			status: domain.StatusInterviewing, submitted: &later,
			want: want{
				statuses:  []string{domain.StatusSaved, domain.StatusApplied, domain.StatusInterviewing},
				appliedAt: &later, lastAt: later,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			evs := InitialEvents(7, 42, tc.status, saved, tc.submitted, "导入创建")

			// Assert
			if len(evs) != len(tc.want.statuses) {
				t.Fatalf("events = %d, want %d", len(evs), len(tc.want.statuses))
			}
			var appliedAt *time.Time
			for i, ev := range evs {
				if ev.ToStatus == nil || *ev.ToStatus != tc.want.statuses[i] {
					t.Fatalf("event %d to_status = %v, want %s", i, ev.ToStatus, tc.want.statuses[i])
				}
				if ev.ApplicationID != 7 || ev.OwnerID != 42 {
					t.Errorf("event %d scoped to (%d,%d), want (7,42)", i, ev.ApplicationID, ev.OwnerID)
				}
				if *ev.ToStatus == domain.StatusApplied {
					at := ev.OccurredAt
					appliedAt = &at
				}
			}
			if evs[0].EventType != "created" || evs[0].Note != "导入创建" {
				t.Errorf("first event = %s/%q, want the 建档 row", evs[0].EventType, evs[0].Note)
			}
			if !evs[0].OccurredAt.Equal(saved) {
				t.Errorf("建档 business time = %v, want the saved_at %v", evs[0].OccurredAt, saved)
			}
			switch {
			case tc.want.appliedAt == nil && appliedAt != nil:
				t.Errorf("applied 落点 = %v, want none", appliedAt)
			case tc.want.appliedAt != nil && appliedAt == nil:
				t.Errorf("missing the applied 落点 for submitted_at %v", tc.want.appliedAt)
			case tc.want.appliedAt != nil && !appliedAt.Equal(*tc.want.appliedAt):
				t.Errorf("applied 落点 at %v, want the submitted_at %v", appliedAt, tc.want.appliedAt)
			}
			if last := evs[len(evs)-1]; !last.OccurredAt.Equal(tc.want.lastAt) {
				t.Errorf("last event at %v, want %v", last.OccurredAt, tc.want.lastAt)
			}
		})
	}
}

// 最后一格决定当前状态，所以拿回放跑一遍产出的形状：阶段和投递时间都要对。
func TestInitialEventsReplayKeepsStageAndSubmission(t *testing.T) {
	// Arrange
	saved := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	submitted := saved.AddDate(0, 0, -5)
	evs := InitialEvents(7, 42, domain.StatusInterviewing, saved, &submitted, "导入创建")
	points := make([]*StagePoint, 0, len(evs))
	for _, ev := range evs { // 视图里建档钉最前，其余按业务时间升序
		at := ev.OccurredAt
		points = append(points, &StagePoint{Status: *ev.ToStatus, OccurredAt: &at, Source: "event"})
	}

	// Act
	d := replayStagePoints(points)

	// Assert
	if d.status != domain.StatusInterviewing {
		t.Errorf("status = %s, want interviewing", d.status)
	}
	if d.submitted == nil || !d.submitted.Equal(submitted) {
		t.Errorf("submitted = %v, want %v", d.submitted, submitted)
	}
}
