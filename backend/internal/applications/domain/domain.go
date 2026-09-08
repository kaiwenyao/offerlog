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
	FromStatus   string
	ToStatus     string
	OccurredAt   time.Time
	Now          time.Time
	WasSubmitted bool // true if a submitted_at already existed
	HadOffer     bool // true when an offer event exists in the application history
	// SkipSubmission records that the user declared this application never went
	// through a formal submission (内推 / 猎头直接约面). It satisfies the
	// in-progress evidence rule WITHOUT inventing a submitted_at, so
	// 投递→回复 analytics keep a truthful empty numerator.
	SkipSubmission bool
	Note           string
	Reason         string
}

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

// --- allowed direct transitions ---

// allowedDirect lists transitions that do not need special-case validation.
var allowedDirect = map[[2]string]bool{
	// 跳阶（用户常见的「面完了才想起来记录」「内推直接约面」）：从准备期可以
	// 直接落到任一招聘阶段，进入 in-progress 时仍需投递证据 —— 补一个实际投递
	// 时间，或显式声明未经过正式投递（Transition.SkipSubmission）。
	{StatusSaved, StatusPreparing}:         true,
	{StatusSaved, StatusApplied}:           true,
	{StatusSaved, StatusScreening}:         true,
	{StatusSaved, StatusAssessment}:        true,
	{StatusSaved, StatusInterviewing}:      true,
	{StatusSaved, StatusOffer}:             true,
	{StatusSaved, StatusRejected}:          true,
	{StatusSaved, StatusWithdrawn}:         true,
	{StatusSaved, StatusClosed}:            true,
	{StatusPreparing, StatusApplied}:       true,
	{StatusPreparing, StatusScreening}:     true,
	{StatusPreparing, StatusAssessment}:    true,
	{StatusPreparing, StatusInterviewing}:  true,
	{StatusPreparing, StatusOffer}:         true,
	{StatusPreparing, StatusRejected}:      true,
	{StatusPreparing, StatusWithdrawn}:     true,
	{StatusPreparing, StatusClosed}:        true,
	{StatusApplied, StatusScreening}:       true,
	{StatusApplied, StatusAssessment}:      true,
	{StatusApplied, StatusInterviewing}:    true,
	{StatusApplied, StatusOffer}:           true,
	{StatusApplied, StatusRejected}:        true,
	{StatusApplied, StatusWithdrawn}:       true,
	{StatusApplied, StatusClosed}:          true,
	{StatusScreening, StatusAssessment}:    true,
	{StatusScreening, StatusInterviewing}:  true,
	{StatusScreening, StatusOffer}:         true,
	{StatusScreening, StatusRejected}:      true,
	{StatusScreening, StatusWithdrawn}:     true,
	{StatusScreening, StatusClosed}:        true,
	{StatusAssessment, StatusScreening}:    true,
	{StatusAssessment, StatusInterviewing}: true,
	{StatusAssessment, StatusOffer}:        true,
	{StatusAssessment, StatusRejected}:     true,
	{StatusAssessment, StatusWithdrawn}:    true,
	{StatusAssessment, StatusClosed}:       true,
	{StatusInterviewing, StatusScreening}:  true,
	{StatusInterviewing, StatusAssessment}: true,
	{StatusInterviewing, StatusOffer}:      true,
	{StatusInterviewing, StatusRejected}:   true,
	{StatusInterviewing, StatusWithdrawn}:  true,
	{StatusInterviewing, StatusClosed}:     true,
	{StatusOffer, StatusAccepted}:          true,
	{StatusOffer, StatusRejected}:          true,
	{StatusOffer, StatusWithdrawn}:         true,
	{StatusOffer, StatusClosed}:            true,
	// terminal reopen (终态重开) back into the pipeline is allowed and must
	// carry a reason (enforced by service layer).
	{StatusAccepted, StatusOffer}: true,
	// 毁约：已接受后又放弃（接了更好的 Offer / 个人原因），需填原因。
	{StatusAccepted, StatusWithdrawn}:     true,
	{StatusRejected, StatusApplied}:       true,
	{StatusRejected, StatusScreening}:     true,
	{StatusRejected, StatusAssessment}:    true,
	{StatusRejected, StatusInterviewing}:  true,
	{StatusRejected, StatusOffer}:         true,
	{StatusRejected, StatusSaved}:         true,
	{StatusRejected, StatusPreparing}:     true,
	{StatusWithdrawn, StatusSaved}:        true,
	{StatusWithdrawn, StatusPreparing}:    true,
	{StatusWithdrawn, StatusApplied}:      true,
	{StatusWithdrawn, StatusScreening}:    true,
	{StatusWithdrawn, StatusAssessment}:   true,
	{StatusWithdrawn, StatusInterviewing}: true,
	{StatusWithdrawn, StatusOffer}:        true,
	{StatusClosed, StatusSaved}:           true,
	{StatusClosed, StatusPreparing}:       true,
	{StatusClosed, StatusApplied}:         true,
	{StatusClosed, StatusScreening}:       true,
	{StatusClosed, StatusAssessment}:      true,
	{StatusClosed, StatusInterviewing}:    true,
	{StatusClosed, StatusOffer}:           true,
}

// ValidateTransition returns an error when the requested transition is not
// permitted by the state model or misses required context fields.
func ValidateTransition(t Transition) error {
	if t.FromStatus == t.ToStatus {
		return errf("same_status", "状态未变化")
	}
	if !ValidStatus(t.FromStatus) || !ValidStatus(t.ToStatus) {
		return errf("invalid_status", "未知状态")
	}
	pair := [2]string{t.FromStatus, t.ToStatus}
	if !allowedDirect[pair] {
		return errf("invalid_transition", "不允许从 %s 直接变更为 %s", t.FromStatus, t.ToStatus)
	}

	now := t.Now
	if t.OccurredAt.IsZero() {
		t.OccurredAt = now
	}
	if t.OccurredAt.After(now.Add(5 * time.Minute)) {
		return errf("future_occurred_at", "业务发生时间不能晚于当前时间")
	}

	// Entering a recruiter-driven phase requires evidence of submission.
	toInProgress := InProgressStatuses[t.ToStatus]
	if toInProgress && !t.WasSubmitted && !t.SkipSubmission {
		return errf("missing_submitted_at", "进入后续招聘阶段需补充实际投递时间或标记未经过正式投递")
	}

	// accepted requires an offer history or a simultaneous offer event.
	if t.ToStatus == StatusAccepted && !t.HadOffer {
		return errf("accepted_without_offer", "accepted 要求有 Offer 记录或同时补录收到 Offer 事件")
	}
	// Terminal reopen (终态重开) and result endings require a reason.
	fromTerminal := TerminalStatuses[t.FromStatus]
	toTerminal := TerminalStatuses[t.ToStatus]
	if (fromTerminal && !toTerminal) || t.ToStatus == StatusRejected || t.ToStatus == StatusWithdrawn || t.ToStatus == StatusClosed {
		if t.Reason == "" {
			return errf("missing_reason", "该状态变更需要填写原因")
		}
	}
	return nil
}
