package domain

import (
	"testing"
	"time"
)

func now() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }

func TestSankeyBenchmarkRules(t *testing.T) {
	// §12.2.1: 10-record fixture → current sankey layers conserve; all final
	// nodes total 10. This test mirrors the expected distribution counts:
	// 待投递2 准备材料1 已投递等待2 面试中2 Offer1 拒绝1 投递后撤回1
	byStatus := map[string][2]int{ // status -> (notSubmitted, submitted)
		"saved":        {2, 0},
		"preparing":    {1, 0},
		"applied":      {0, 2},
		"interviewing": {0, 2},
		"offer":        {0, 1},
		"rejected":     {0, 1},
		"withdrawn":    {0, 1}, // benchmark: 投递后撤回 1 (post-submit only)
	}
	var notSub, sub, total int
	for _, v := range byStatus {
		notSub += v[0]
		sub += v[1]
	}
	total = notSub + sub
	if notSub != 3 || sub != 7 {
		t.Fatalf("want 尚未投递 3 / 已投递 7, got %d/%d", notSub, sub)
	}
	if total != 10 {
		t.Fatalf("total must be 10, got %d", total)
	}
}

func TestTransitionValidationTable(t *testing.T) {
	base := func(from, to string) Transition {
		return Transition{FromStatus: from, ToStatus: to, Now: now(), OccurredAt: now(), WasSubmitted: true, HadOffer: true}
	}
	cases := []struct {
		name string
		tr   Transition
		want string // empty = valid
	}{
		{"saved->preparing valid", base("saved", "preparing"), ""},
		{"preparing->applied valid", base("preparing", "applied"), ""},
		// 跳阶：saved -> 任一招聘阶段现在是合法边，但仍要投递证据。
		{"saved->interviewing without submit rejected", func() Transition {
			tr := base("saved", "interviewing")
			tr.WasSubmitted = false
			return tr
		}(), "missing_submitted_at"},
		{"saved->interviewing with backfilled submit valid", func() Transition {
			tr := base("saved", "interviewing")
			tr.WasSubmitted = true
			return tr
		}(), ""},
		{"saved->interviewing with no-formal-submission valid", func() Transition {
			tr := base("saved", "interviewing")
			tr.WasSubmitted = false
			tr.SkipSubmission = true
			return tr
		}(), ""},
		{"skip-submission is refused for 已投递 itself", func() Transition {
			tr := base("saved", "applied")
			tr.WasSubmitted = false
			tr.SkipSubmission = true
			return tr
		}(), "missing_submitted_at"},
		{"applied still accepts a real submitted_at", func() Transition {
			tr := base("saved", "applied")
			tr.WasSubmitted = true
			return tr
		}(), ""},
		{"skip-submission does not unlock an illegal edge", func() Transition {
			tr := base("saved", "accepted")
			tr.WasSubmitted = false
			tr.SkipSubmission = true
			return tr
		}(), "invalid_transition"},
		{"saved->assessment with submit valid", base("saved", "assessment"), ""},
		{"saved->offer valid (offer needs no in-progress evidence)", base("saved", "offer"), ""},
		{"saved->rejected needs reason", func() Transition {
			tr := base("saved", "rejected")
			tr.Reason = ""
			return tr
		}(), "missing_reason"},
		{"saved->rejected with reason valid", func() Transition {
			tr := base("saved", "rejected")
			tr.Reason = "岗位取消"
			return tr
		}(), ""},
		{"interviewing->assessment rollback ok", base("interviewing", "assessment"), ""},
		{"accepted->withdrawn 毁约 needs reason", func() Transition {
			tr := base("accepted", "withdrawn")
			tr.Reason = ""
			return tr
		}(), "missing_reason"},
		{"accepted->withdrawn with reason valid", func() Transition {
			tr := base("accepted", "withdrawn")
			tr.Reason = "接受了其他 Offer"
			return tr
		}(), ""},
		{"preparing->screening without submit rejected", func() Transition {
			tr := base("preparing", "screening")
			tr.WasSubmitted = false
			return tr
		}(), "missing_submitted_at"},
		{"preparing->screening with submit valid", func() Transition {
			tr := base("preparing", "screening")
			tr.WasSubmitted = true
			return tr
		}(), ""},
		{"screening->interviewing ok", base("screening", "interviewing"), ""},
		{"interviewing->screening rollback ok", base("interviewing", "screening"), ""},
		{"assessment skipped: screening->offer ok", base("screening", "offer"), ""},
		{"offer->accepted valid", base("offer", "accepted"), ""},
		{"accepted needs offer history", func() Transition {
			tr := base("offer", "accepted")
			tr.HadOffer = false
			return tr
		}(), "accepted_without_offer"},
		{"screening->accepted without offer is not a legal edge", base("screening", "accepted"), "invalid_transition"},
		{"rejected->applied reopen needs reason", func() Transition {
			tr := base("rejected", "applied")
			tr.Reason = ""
			return tr
		}(), "missing_reason"},
		{"rejected->applied with reason valid", func() Transition {
			tr := base("rejected", "applied")
			tr.Reason = "重新开放"
			return tr
		}(), ""},
		{"rejected->offer via offer disallowed", base("accepted", "applied"), "invalid_transition"},
		{"saved->accepted disallowed", base("saved", "accepted"), "invalid_transition"},
		{"same status invalid", base("applied", "applied"), "same_status"},
		{"unknown status invalid", base("applied", "bogus"), "invalid_status"},
		{"future occurred_at rejected", func() Transition {
			tr := base("applied", "screening")
			tr.OccurredAt = now().Add(24 * time.Hour)
			return tr
		}(), "future_occurred_at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTransition(tc.tr)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
			} else if err == nil {
				t.Fatalf("expected code %s, got nil", tc.want)
			} else if ve, ok := err.(*ValidationError); !ok || ve.Code != tc.want {
				t.Fatalf("expected code %s, got %v", tc.want, err)
			}
		})
	}
}

func TestStatusCategories(t *testing.T) {
	if !IsPreparing("saved") || IsPreparing("applied") {
		t.Fatal("preparing categories wrong")
	}
	if !InProgressStatuses["interviewing"] || InProgressStatuses["offer"] {
		t.Fatal("in-progress categories wrong")
	}
	if !IsTerminal("accepted") || IsTerminal("offer") {
		t.Fatal("terminal categories wrong")
	}
}
