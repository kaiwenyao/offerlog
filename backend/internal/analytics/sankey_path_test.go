package analytics

import (
	"reflect"
	"testing"

	"offerlog/backend/internal/applications/domain"
)

func TestCurrentProgressPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		stages  []string
		current string
		want    []string
	}{
		{
			name:    "笔试后被拒 walks through assessment",
			stages:  []string{domain.StatusSaved, domain.StatusApplied, domain.StatusAssessment, domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusAssessment, domain.StatusRejected},
		},
		{
			name:    "投递后直接被拒 hangs off submitted",
			stages:  []string{domain.StatusApplied, domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusRejected},
		},
		{
			name:    "仍在笔试 stops at assessment",
			stages:  []string{domain.StatusApplied, domain.StatusAssessment},
			current: domain.StatusAssessment,
			want:    []string{domain.StatusAssessment},
		},
		{
			name:    "内推直达 OA",
			stages:  []string{domain.StatusAssessment},
			current: domain.StatusAssessment,
			want:    []string{domain.StatusAssessment},
		},
		{
			name:    "无后段的终态 hangs off submitted",
			stages:  []string{domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusRejected},
		},
		{
			name:    "跳过笔试不补边",
			stages:  []string{domain.StatusApplied, domain.StatusInterviewing, domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusInterviewing, domain.StatusRejected},
		},
		{
			name:    "回退截在当前 rank",
			stages:  []string{domain.StatusApplied, domain.StatusInterviewing, domain.StatusScreening},
			current: domain.StatusScreening,
			want:    []string{domain.StatusScreening},
		},
		{
			name:    "仍在已投递 stays on submitted",
			stages:  []string{domain.StatusSaved, domain.StatusApplied},
			current: domain.StatusApplied,
			want:    nil,
		},
		{
			name:    "准备材料回退 stays on submitted",
			stages:  []string{domain.StatusApplied, domain.StatusPreparing},
			current: domain.StatusPreparing,
			want:    nil,
		},
		{
			name:    "连续同状态压缩",
			stages:  []string{domain.StatusApplied, domain.StatusAssessment, domain.StatusAssessment, domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusAssessment, domain.StatusRejected},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := currentProgressPath(tc.stages, tc.current)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("path = %v, want %v", got, tc.want)
			}
		})
	}
}
