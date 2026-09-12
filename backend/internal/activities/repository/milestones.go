// Milestones: user-defined timeline nodes (迁移 00005 / 00006). Unlike the
// append-only application_events audit trail, a milestone is a free-form
// record the user creates, edits and deletes — the whole point is that every
// application does NOT have to follow one canonical stage list.
//
// Since 00006 a milestone also carries the stage it puts the application in
// (StatusEffect). The kind → stage table lives in applications/domain so the
// backend has exactly one copy of it; this package only stores what that table
// resolved to at write time.
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	appdomain "offerlog/backend/internal/applications/domain"
	"offerlog/backend/internal/platform/database"
)

// Milestone kinds. The set is open — arbitrary kinds are stored as-is and
// rendered as custom nodes. These aliases keep the storage layer readable; the
// authoritative list (label + stage effect + picker group) is
// appdomain.MilestoneKinds.
const (
	MilestoneKindOA        = appdomain.MKOA
	MilestoneKindScreen    = appdomain.MKScreen
	MilestoneKindInterview = appdomain.MKInterview
	MilestoneKindOffer     = appdomain.MKOffer
	MilestoneKindPhone     = appdomain.MKPhone
	MilestoneKindCustom    = appdomain.MKCustom
)

// MilestoneDefaultLabel maps a kind to its display name. Unknown kinds fall
// back to the label the user typed, or "自定义节点" when both are empty.
func MilestoneDefaultLabel(kind string) string {
	return appdomain.MilestoneLabelForKind(kind)
}

// Milestone is one user-added timeline node.
type Milestone struct {
	ID            int64
	ApplicationID int64
	OwnerID       int64
	Kind          string
	Label         string
	// StatusEffect is the stage recording this event puts the application in
	// ("" = 只记事，不改阶段). Derived from Kind by the write paths below, never
	// accepted from the client — otherwise a caller could mint any stage while
	// bypassing the kind table.
	StatusEffect string
	// OccurredAt is the business time the user picked; nil = 时间未定
	// (never fabricate a timestamp).
	OccurredAt *time.Time
	Note       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const milestoneCols = `id, application_id, owner_id, kind, label, status_effect, occurred_at, note, created_at, updated_at`

func scanMilestone(row pgx.Row) (*Milestone, error) {
	var m Milestone
	err := row.Scan(&m.ID, &m.ApplicationID, &m.OwnerID, &m.Kind, &m.Label,
		&m.StatusEffect, &m.OccurredAt, &m.Note, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListMilestones returns the application's user-added nodes in business-time
// order. Nodes without a time (时间未定) sort last, by creation order — the
// timeline merges them with status events using the same rule.
func (r *Repo) ListMilestones(ctx context.Context, appID, ownerID int64) ([]*Milestone, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT `+milestoneCols+`
		FROM application_milestones WHERE application_id=$1 AND owner_id=$2
		ORDER BY occurred_at ASC NULLS LAST, id ASC`, appID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Milestone
	for rows.Next() {
		m, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMilestone loads one node through the caller's Querier.
func (r *Repo) GetMilestone(ctx context.Context, q database.Querier, appID, ownerID, id int64) (*Milestone, error) {
	return scanMilestone(q.QueryRow(ctx, `SELECT `+milestoneCols+`
		FROM application_milestones WHERE id=$1 AND application_id=$2 AND owner_id=$3`, id, appID, ownerID))
}

// GetMilestoneForUpdate is GetMilestone with a row lock, for the PATCH merge
// (read-merge-write inside one transaction — same rationale as the interview
// and assessment updaters: a pre-transaction read can race another writer).
func (r *Repo) GetMilestoneForUpdate(ctx context.Context, q database.Querier, appID, ownerID, id int64) (*Milestone, error) {
	return scanMilestone(q.QueryRow(ctx, `SELECT `+milestoneCols+`
		FROM application_milestones WHERE id=$1 AND application_id=$2 AND owner_id=$3 FOR UPDATE`, id, appID, ownerID))
}

// CreateMilestone inserts a node, normalizing empty kind/label.
func (r *Repo) CreateMilestone(ctx context.Context, q database.Querier, m *Milestone) error {
	m.normalize()
	return q.QueryRow(ctx, `INSERT INTO application_milestones(application_id, owner_id, kind, label, status_effect, occurred_at, note)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at, updated_at`,
		m.ApplicationID, m.OwnerID, m.Kind, m.Label, m.StatusEffect, m.OccurredAt, m.Note).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
}

// normalize fills the defaults every write path shares: empty kind → custom,
// empty label → the kind's default name, and the stage effect resolved from the
// (possibly just-defaulted) kind.
func (m *Milestone) normalize() {
	if m.Kind == "" {
		m.Kind = MilestoneKindCustom
	}
	if m.Label == "" {
		m.Label = MilestoneDefaultLabel(m.Kind)
	}
	m.StatusEffect = appdomain.StatusEffectForKind(m.Kind)
}

// UpdateMilestone rewrites a node the caller has re-read under lock.
func (r *Repo) UpdateMilestone(ctx context.Context, q database.Querier, m *Milestone) error {
	// Re-derive the stage effect: changing 自定义事件 → 面试 must move the
	// application, and 面试 → 电话沟通 must stop holding it in 面试中.
	m.normalize()
	tag, err := q.Exec(ctx, `UPDATE application_milestones SET kind=$1, label=$2, status_effect=$3,
		occurred_at=$4, note=$5, updated_at=now() WHERE id=$6 AND application_id=$7 AND owner_id=$8`,
		m.Kind, m.Label, m.StatusEffect, m.OccurredAt, m.Note, m.ID, m.ApplicationID, m.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DeleteMilestone removes a node. User-added nodes are deletable on purpose
// (unlike the append-only status audit trail).
func (r *Repo) DeleteMilestone(ctx context.Context, q database.Querier, appID, ownerID, id int64) error {
	tag, err := q.Exec(ctx, `DELETE FROM application_milestones WHERE id=$1 AND application_id=$2 AND owner_id=$3`,
		id, appID, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
