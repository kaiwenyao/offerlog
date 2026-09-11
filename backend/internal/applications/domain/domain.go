// Package domain holds the core application state rules: the standard status
// set, allowed transitions, terminal states and helpers used by services,
// analytics and the HTTP layer. It must not import web frameworks or storage
// adapters.
package domain

import (
	"fmt"
	"time"
)

// Status keys (2.2). Chinese labels are produced by the UI via a shared
// dictionary; the backend keeps keys stable.
const (
	StatusSaved        = "saved"
	StatusPreparing    = "preparing"
	StatusApplied      = "applied"
	StatusScreening    = "screening"
	StatusAssessment   = "assessment"
	StatusInterviewing = "interviewing"
	StatusOffer        = "offer"
	StatusAccepted     = "accepted"
	StatusRejected     = "rejected"
	StatusWithdrawn    = "withdrawn"
	StatusClosed       = "closed"
)

var AllStatuses = []string{
	StatusSaved, StatusPreparing, StatusApplied, StatusScreening,
	StatusAssessment, StatusInterviewing, StatusOffer, StatusAccepted,
	StatusRejected, StatusWithdrawn, StatusClosed,
}

// Preparing-phase statuses (not yet submitted).
var PreparingStatuses = map[string]bool{StatusSaved: true, StatusPreparing: true}

// In-progress (recruiter-driven) statuses.
var InProgressStatuses = map[string]bool{
	StatusApplied: true, StatusScreening: true, StatusAssessment: true, StatusInterviewing: true,
}

// SkipSubmissionStatuses are the in-progress statuses a record may enter while
// asserting it never went through a formal submission (内推 / 猎头直接约面).
// StatusApplied is deliberately absent: 「已投递」 *is* the claim that a
// submission happened, so a row in that status with a NULL submitted_at would
// read as submitted in the UI while every analytics and reminder query
// (submitted_at IS NOT NULL) treats it as not submitted.
var SkipSubmissionStatuses = map[string]bool{
	StatusScreening: true, StatusAssessment: true, StatusInterviewing: true,
}

// Terminal (ended) statuses.
var TerminalStatuses = map[string]bool{
	StatusAccepted: true, StatusRejected: true, StatusWithdrawn: true, StatusClosed: true,
}

// HasResult groups offer/accepted (positive), rejected and managed endings.
var ResultStatuses = map[string]bool{
	StatusOffer: true, StatusAccepted: true, StatusRejected: true,
	StatusWithdrawn: true, StatusClosed: true,
}

var StatusCategories = map[string]string{
	StatusSaved:        "preparing",
	StatusPreparing:    "preparing",
	StatusApplied:      "in_progress",
	StatusScreening:    "in_progress",
	StatusAssessment:   "in_progress",
	StatusInterviewing: "in_progress",
	StatusOffer:        "decision",
	StatusAccepted:     "ended",
	StatusRejected:     "ended",
	StatusWithdrawn:    "ended",
	StatusClosed:       "ended",
}

// IsTerminal reports whether s is an ended status.
func IsTerminal(s string) bool { return TerminalStatuses[s] }

// IsPreparing reports whether s belongs to the pre-submission phase.
func IsPreparing(s string) bool { return PreparingStatuses[s] }

func ValidStatus(s string) bool {
	_, ok := StatusCategories[s]
	return ok
}

// State keeps the status-related portion of an application that transition
// validation needs.
type State struct {
	Status        string
	Substatus     string
	SubmittedAt   *time.Time
	FirstResponse *time.Time
	AcceptedAt    *time.Time
	RejectedAt    *time.Time
	// HadOffer is true when the record has reached offer in its history
	// (either current status == offer, or an offer event exists).
	HadOffer bool
}

// Transition carries everything needed to validate a requested change.
type Transition struct {
	FromStatus    string
	FromSubstatus string
	ToStatus      string
	ToSubstatus   string
	// ChangeType records WHY the change happened: advance (default),
	// rollback (流程实际退回) or reopen (终态重开). "correct" is not a
	// transition — mistaken records are repaired through the correction API.
	ChangeType string
	// FocusChanged is true when the request changes the focused activity even
	// though status and substatus stay the same, so the change is not a no-op.
	FocusChanged bool
	OccurredAt   time.Time
	Now          time.Time
	WasSubmitted bool // true if a submitted_at already existed
	HadOffer     bool // true when an offer event exists in the application history
	// SkipSubmission records that the user declared this application never went
	// through a formal submission (内推 / 猎头直接约面). It satisfies the
	// in-progress evidence rule WITHOUT inventing a submitted_at, so
	// 投递→回复 analytics keep a truthful empty numerator.
	SkipSubmission bool
	// ReplayHistorical marks a hop REPLAYED from the timeline during a
	// correction re-check rather than a fresh request. The correction replay
	// re-validates structure (allowedTarget, substatus pairs, each event's own
	// reason, Offer evidence) but NOT the submission evidence of hops that
	// were already admitted when they were written: 「未经正式投递」 is a
	// per-request assertion that was never stored on the event row, so there
	// is nothing honest to replay it from — re-asking would make every legal
	// timeline un-correctable.
	ReplayHistorical bool
	Note             string
	Reason           string
}

// Change types recorded on an event (application_events.change_type).
const (
	ChangeAdvance  = "advance"
	ChangeRollback = "rollback"
	ChangeReopen   = "reopen"
)

// ValidationError is a domain-level violation with a stable code.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// ErrorCode lets the HTTP layer map domain violations to stable codes.
func (e *ValidationError) ErrorCode() string { return e.Code }

func errf(code, format string, args ...any) *ValidationError {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// allowedTarget is the whole permission model (方案 §4.1): order only drives
// recommendations, never permissions.
//
//   - any non-terminal stage may move to any other non-terminal stage (forward,
//     skip or back), which is what makes 「准备材料 → 待投递」 and
//     「Offer → 面试」 legal;
//   - a terminal record reopens into ANY non-terminal stage (reason required);
//   - 已接受 → 已撤回 (毁约) stays available;
//   - → accepted still requires having an Offer (accepting is not a way to skip
//     the pipeline silently);
//   - terminal → terminal is not a transition — a wrong ending is repaired with
//     a correction so the audit trail keeps the mistake.
func allowedTarget(from, to string) bool {
	if from == to {
		return true // substatus / focus-activity change inside one stage
	}
	if to == StatusAccepted {
		return from == StatusOffer
	}
	if from == StatusAccepted && to == StatusWithdrawn {
		return true
	}
	if TerminalStatuses[from] && TerminalStatuses[to] {
		return false
	}
	return true
}

// ReasonRequired mirrors the reason rule the UI must satisfy before it may
// submit: the three managed endings, and reopening an ended record.
func ReasonRequired(from, to string) bool {
	if to == StatusRejected || to == StatusWithdrawn || to == StatusClosed {
		return true
	}
	return TerminalStatuses[from] && !TerminalStatuses[to]
}

// ValidateTransition returns an error when the requested transition is not
// permitted by the state model or misses required context fields.
func ValidateTransition(t Transition) error {
	if !ValidStatus(t.FromStatus) || !ValidStatus(t.ToStatus) {
		return errf("invalid_status", "未知状态")
	}
	if !ValidSubstatus(t.FromStatus, t.FromSubstatus) || !ValidSubstatus(t.ToStatus, t.ToSubstatus) {
		return errf("invalid_substatus", "阶段与子状态不匹配")
	}
	if t.FromStatus == t.ToStatus && t.FromSubstatus == t.ToSubstatus && !t.FocusChanged {
		return errf("same_status", "状态未变化")
	}
	if !allowedTarget(t.FromStatus, t.ToStatus) {
		return errf("invalid_transition", "不允许从 %s 直接变更为 %s", t.FromStatus, t.ToStatus)
	}

	now := t.Now
	if t.OccurredAt.IsZero() {
		t.OccurredAt = now
	}
	if t.OccurredAt.After(now.Add(5 * time.Minute)) {
		return errf("future_occurred_at", "业务发生时间不能晚于当前时间")
	}

	// Entering a recruiter-driven phase requires evidence of submission. Only
	// checked when the stage actually changes: a substatus tweak inside a stage
	// the record already occupies must not re-ask (a 内推 row entered with
	// no_formal_submission legitimately has no submitted_at).
	if !t.ReplayHistorical && t.FromStatus != t.ToStatus && InProgressStatuses[t.ToStatus] && !t.WasSubmitted {
		if !t.SkipSubmission {
			return errf("missing_submitted_at", "进入后续招聘阶段需补充实际投递时间或标记未经过正式投递")
		}
		if !SkipSubmissionStatuses[t.ToStatus] {
			return errf("missing_submitted_at", "「已投递」必须填写实际投递时间；没走正式投递流程请直接选择对应的招聘阶段")
		}
	}

	// accepted requires an offer history or a simultaneous offer event.
	if t.ToStatus == StatusAccepted && !t.HadOffer {
		return errf("accepted_without_offer", "accepted 要求有 Offer 记录或同时补录收到 Offer 事件")
	}
	if ReasonRequired(t.FromStatus, t.ToStatus) && t.Reason == "" {
		return errf("missing_reason", "该状态变更需要填写原因")
	}
	return nil
}
