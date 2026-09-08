// Package analytics computes dashboard metrics and both sankey projections.
//
// Terminology (plan §5):
//   - current distribution cohort: applications sampled by saved date
//   - submitted cohort: applications with submitted_at in range
//   - denominator-zero → "—"; small samples flagged (n<10)
package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
)

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

// SnapshotRequest selects the analytics window + scope.
type SnapshotRequest struct {
	OwnerID       int64
	From          *time.Time
	To            *time.Time
	Status        string
	Channel       string
	Tags          []string
	Company       string
	Search        string
	SubmittedFrom *time.Time
	SubmittedTo   *time.Time
	Timezone      string
	Now           time.Time
}

func (r *Repo) whereClause(req *SnapshotRequest, extra ...string) (string, []any) {
	clauses := []string{"owner_id = $1", "deleted_at IS NULL"}
	args := []any{req.OwnerID}
	add := func(cond string, val any) {
		args = append(args, val)
		clauses = append(clauses, fmt.Sprintf(cond, len(args)))
	}
	if req.From != nil {
		add("saved_at >= $%d", *req.From)
	}
	if req.To != nil {
		add("saved_at <= $%d", *req.To)
	}
	if req.Status != "" {
		add("status = $%d", req.Status)
	}
	if req.Channel != "" {
		add("channel = $%d", req.Channel)
	}
	if len(req.Tags) > 0 {
		add("tags && $%d", req.Tags)
	}
	if req.Company != "" {
		add("company_name = $%d", req.Company)
	}
	if req.Search != "" {
		args = append(args, "%"+req.Search+"%")
		clauses = append(clauses, fmt.Sprintf("(position ILIKE $%d OR company_name ILIKE $%d)", len(args), len(args)))
	}
	if req.SubmittedFrom != nil {
		add("submitted_at >= $%d", *req.SubmittedFrom)
	}
	if req.SubmittedTo != nil {
		add("submitted_at <= $%d", *req.SubmittedTo)
	}
	clauses = append(clauses, extra...)
	return strings.Join(clauses, " AND "), args
}

type Metrics struct {
	TotalAll       int64            `json:"total_all"`       // not deleted (any status)
	ToApply        int64            `json:"to_apply"`        // saved/preparing
	SubmittedCount int64            `json:"submitted_count"` // distinct submitted_at set (incl ended)
	InProgress     int64            `json:"in_progress"`     // applied/screening/assessment/interviewing
	WithResult     int64            `json:"with_result"`     // offer+accepted+rejected+withdrawn+closed
	ByStatus       map[string]int64 `json:"by_status"`
	ByChannel      []ChannelRow     `json:"by_channel"`
	// Rates carry explicit numerators and a denominator so the UI can show
	// 0% (denom>0, num=0) vs "— / 暂无样本" (denom=0) without guessing, and a
	// channel/row with zero submissions never looks like a missing value.
	ResponseRate     *float64 `json:"response_rate"`  // null when denominator=0
	InterviewRate    *float64 `json:"interview_rate"` // null when denominator=0
	OfferRate        *float64 `json:"offer_rate"`     // null when denominator=0
	Responded        int64    `json:"responded"`      // numerator of response_rate
	ReachedInterview int64    `json:"reached_interview"`
	ReceivedOffer    int64    `json:"received_offer"`
	ResponseMedianH  float64  `json:"response_median_hours"` // hours from submit to first response (median, responded only)
	PendingResponse  int64    `json:"pending_response"`      // submitted, no response yet
	RepliedSample    int64    `json:"replied_sample"`
	Denominator      int64    `json:"denominator"` // cohort size
	SmallSample      bool     `json:"small_sample"`
}

type ChannelRow struct {
	Channel       string   `json:"channel"`
	Submitted     int64    `json:"submitted"`
	Responded     int64    `json:"responded"`
	ResponseRate  *float64 `json:"response_rate"` // null when submitted=0
	InterviewRate *float64 `json:"interview_rate"`
	OfferRate     *float64 `json:"offer_rate"`
}

// Counts returns key metrics for the scope (default by saved date cohort).
func (r *Repo) Counts(ctx context.Context, req *SnapshotRequest) (*Metrics, error) {
	where, args := r.whereClause(req)
	q := r.db.Pool()

	m := &Metrics{ByStatus: map[string]int64{}}
	var nAll, nPreparing, nInProgress, nResult int64
	err := q.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status IN ('saved','preparing')),
		count(*) FILTER (WHERE status IN ('applied','screening','assessment','interviewing')),
		count(*) FILTER (WHERE status IN ('offer','accepted','rejected','withdrawn','closed')),
		count(*)
		FROM applications WHERE `+where, args...).Scan(&nPreparing, &nInProgress, &nResult, &nAll)
	if err != nil {
		return nil, err
	}
	m.ToApply = nPreparing
	m.InProgress = nInProgress
	m.WithResult = nResult
	m.TotalAll = nAll

	// by status
	rows, err := q.Query(ctx, `SELECT status, count(*) FROM applications WHERE `+where+` GROUP BY status`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		m.ByStatus[s] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// submitted cohort metrics use submitted_at window; if the request carried
	// a submission window, restrict there, else all submitted.
	cohortWhere, cohortArgs := r.submittedCohortWhere(req)
	var denominator int64
	if err := q.QueryRow(ctx, `SELECT count(DISTINCT id) FROM applications WHERE `+cohortWhere, cohortArgs...).Scan(&denominator); err != nil {
		return nil, err
	}
	m.Denominator = denominator
	if denominator > 0 {
		var resp, interv, offer int64
		if err := q.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE first_response_at IS NOT NULL),
			count(*) FILTER (WHERE EXISTS (SELECT 1 FROM application_events e WHERE e.application_id = applications.id AND e.to_status='interviewing')),
			count(*) FILTER (WHERE EXISTS (SELECT 1 FROM application_events e WHERE e.application_id = applications.id AND e.to_status='offer'))
			FROM applications WHERE `+cohortWhere, cohortArgs...).Scan(&resp, &interv, &offer); err != nil {
			return nil, err
		}
		// Rates are only meaningful over a non-empty cohort. Zero numerators
		// with a positive denominator render as 0%, not as a missing dash.
		rr := float64(resp) / float64(denominator)
		ir := float64(interv) / float64(denominator)
		or := float64(offer) / float64(denominator)
		m.ResponseRate = &rr
		m.InterviewRate = &ir
		m.OfferRate = &or
		m.Responded = resp
		m.ReachedInterview = interv
		m.ReceivedOffer = offer
		m.RepliedSample = resp
		if resp > 0 {
			// median hours: submitted_at → first_response_at among responded
			var med float64
			if err := q.QueryRow(ctx, `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (first_response_at - submitted_at))/3600.0)
				FROM applications WHERE `+cohortWhere+` AND first_response_at IS NOT NULL AND submitted_at IS NOT NULL`, cohortArgs...).Scan(&med); err != nil {
				if err.Error() != "no rows" {
					return nil, err
				}
			}
			m.ResponseMedianH = med
		}
		var pending int64
		if err := q.QueryRow(ctx, `SELECT count(*) FROM applications WHERE `+cohortWhere+` AND first_response_at IS NULL`, cohortArgs...).Scan(&pending); err != nil {
			return nil, err
		}
		m.PendingResponse = pending
		m.SmallSample = denominator < 10
	}
	m.SubmittedCount = denominator
	return m, nil
}

func (r *Repo) submittedCohortWhere(req *SnapshotRequest) (string, []any) {
	clauses := []string{"owner_id = $1", "deleted_at IS NULL", "submitted_at IS NOT NULL"}
	args := []any{req.OwnerID}
	add := func(cond string, val any) {
		args = append(args, val)
		clauses = append(clauses, fmt.Sprintf(cond, len(args)))
	}
	if req.SubmittedFrom != nil {
		add("submitted_at >= $%d", *req.SubmittedFrom)
	}
	if req.SubmittedTo != nil {
		add("submitted_at <= $%d", *req.SubmittedTo)
	}
	if req.Channel != "" {
		add("channel = $%d", req.Channel)
	}
	if len(req.Tags) > 0 {
		add("tags && $%d", req.Tags)
	}
	if req.Company != "" {
		add("company_name = $%d", req.Company)
	}
	return strings.Join(clauses, " AND "), args
}

// ByChannel lists per-channel funnel numbers over the submitted cohort.
func (r *Repo) ByChannel(ctx context.Context, req *SnapshotRequest) ([]ChannelRow, error) {
	where, args := r.submittedCohortWhere(req)
	where += " AND channel <> ''"
	rows, err := r.db.Pool().Query(ctx, `SELECT channel, count(*) FILTER (WHERE first_response_at IS NOT NULL),
		count(*) FILTER (WHERE EXISTS (SELECT 1 FROM application_events e WHERE e.application_id = applications.id AND e.to_status='interviewing')),
		count(*) FILTER (WHERE EXISTS (SELECT 1 FROM application_events e WHERE e.application_id = applications.id AND e.to_status='offer')),
		count(*)
		FROM applications WHERE `+where+` GROUP BY channel ORDER BY count(*) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelRow
	for rows.Next() {
		var c ChannelRow
		var resp, interv, offer int64
		if err := rows.Scan(&c.Channel, &resp, &interv, &offer, &c.Submitted); err != nil {
			return nil, err
		}
		c.Responded = resp
		if c.Submitted > 0 {
			rr := float64(resp) / float64(c.Submitted)
			ir := float64(interv) / float64(c.Submitted)
			or := float64(offer) / float64(c.Submitted)
			c.ResponseRate = &rr
			c.InterviewRate = &ir
			c.OfferRate = &or
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

// StatusName maps keys to Chinese labels (mirror of frontend dict).
var StatusName = map[string]string{
	domain.StatusSaved: "待投递", domain.StatusPreparing: "准备材料", domain.StatusApplied: "已投递",
	domain.StatusScreening: "初筛沟通", domain.StatusAssessment: "笔试作业", domain.StatusInterviewing: "面试中",
	domain.StatusOffer: "收到 Offer", domain.StatusAccepted: "已接受", domain.StatusRejected: "被拒绝",
	domain.StatusWithdrawn: "已撤回", domain.StatusClosed: "岗位关闭",
}

var _ = time.Now

// ScopeMemberIDs returns the application ids in scope for a snapshot request,
// in a stable order. Used to back the drilldown token so chart counts and the
// member list stay consistent.
func (r *Repo) ScopeMemberIDs(ctx context.Context, req *SnapshotRequest) ([]int64, error) {
	where, args := r.whereClause(req)
	rows, err := r.db.Pool().Query(ctx, `SELECT id FROM applications WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
