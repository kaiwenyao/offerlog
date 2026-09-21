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
			want:    []string{domain.StatusApplied, domain.StatusAssessment, domain.StatusRejected},
		},
		{
			name:    "投递后直接被拒 has no assessment",
			stages:  []string{domain.StatusApplied, domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusApplied, domain.StatusRejected},
		},
		{
			name:    "仍在笔试 stops at assessment",
			stages:  []string{domain.StatusApplied, domain.StatusAssessment},
			current: domain.StatusAssessment,
			want:    []string{domain.StatusApplied, domain.StatusAssessment},
		},
		{
			name:    "内推直达 OA 不虚构 applied",
			stages:  []string{domain.StatusAssessment},
			current: domain.StatusAssessment,
			want:    []string{domain.StatusAssessment},
		},
		{
			name:    "无过程落点的终态 hangs off submitted",
			stages:  []string{domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusRejected},
		},
		{
			name:    "跳过笔试不补边",
			stages:  []string{domain.StatusApplied, domain.StatusInterviewing, domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusApplied, domain.StatusInterviewing, domain.StatusRejected},
		},
		{
			name:    "回退截在当前 rank",
			stages:  []string{domain.StatusApplied, domain.StatusInterviewing, domain.StatusScreening},
			current: domain.StatusScreening,
			want:    []string{domain.StatusApplied, domain.StatusScreening},
		},
		{
			name:    "连续同状态压缩",
			stages:  []string{domain.StatusApplied, domain.StatusApplied, domain.StatusAssessment, domain.StatusAssessment, domain.StatusRejected},
			current: domain.StatusRejected,
			want:    []string{domain.StatusApplied, domain.StatusAssessment, domain.StatusRejected},
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
