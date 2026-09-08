// Package search backs the ⌘K command-palette cross-entity search: one query
// returns a mixed list of applications / companies / files owned by the user,
// so the palette can jump to any of them. Only the current owner's rows are
// ever touched; soft-deleted applications and deleted files are excluded.
package search

import (
	"context"
	"fmt"
	"strings"

	"offerlog/backend/internal/platform/database"
)

// Item is one mixed search result row.
type Item struct {
	Kind  string `json:"kind"` // application | company | file
	ID    any    `json:"id"`   // application/company id is a number; file id is a string (UUID)
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// statusLabel mirrors the frontend status dictionary (analytics.StatusName is
// the same mirror); used only for the human hint text of application rows.
var statusLabel = map[string]string{
	"saved": "待投递", "preparing": "准备材料", "applied": "已投递",
	"screening": "初筛沟通", "assessment": "笔试作业", "interviewing": "面试中",
	"offer": "Offer", "accepted": "已接受", "rejected": "被拒绝",
	"withdrawn": "已撤回", "closed": "岗位关闭",
}

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Pool() *database.DB { return r.db }

// Search returns up to limit mixed results. An empty kw returns the most
// recently touched rows (recently updated applications + newest files); a
// non-empty kw substring-matches companies.name, applications.position /
// applications.company_name / applications.notes, and files.original_name
// (owner-scoped). Applications are ranked first, then companies, then files —
// the same mix the command palette renders.
func (r *Repo) Search(ctx context.Context, ownerID int64, kw string, limit int) ([]*Item, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if strings.TrimSpace(kw) == "" {
		return r.recent(ctx, ownerID, limit)
	}
	pattern := "%" + escapeLike(strings.TrimSpace(kw)) + "%"

	var out []*Item

	// applications: position / company / notes matches.
	appRows, err := r.db.Pool().Query(ctx, `SELECT a.id, a.company_name, a.position, a.status, a.location
		FROM applications a
		WHERE a.owner_id=$1 AND a.deleted_at IS NULL
		  AND (a.position ILIKE $2 OR a.company_name ILIKE $2 OR a.notes ILIKE $2)
		ORDER BY COALESCE(a.updated_at, a.created_at) DESC, a.id DESC
		LIMIT $3`, ownerID, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	for appRows.Next() {
		var id int64
		var company, position, status, location string
		if err := appRows.Scan(&id, &company, &position, &status, &location); err != nil {
			return nil, err
		}
		it := Item{Kind: "application", ID: id, Label: company + " · " + position}
		hint := statusLabel[status]
		if hint == "" {
			hint = status
		}
		if location != "" {
			hint += " · " + location
		}
		it.Hint = hint
		out = append(out, &it)
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}

	if len(out) >= limit {
		return out[:limit], nil
	}
	room := limit - len(out)

	// companies: name matches, hint shows the number of live applications.
	coRows, err := r.db.Pool().Query(ctx, `SELECT c.id, c.name,
			(SELECT count(*) FROM applications a WHERE a.company_id = c.id AND a.owner_id = c.owner_id AND a.deleted_at IS NULL)
		FROM companies c
		WHERE c.owner_id=$1 AND c.name ILIKE $2
		ORDER BY c.updated_at DESC, c.id
		LIMIT $3`, ownerID, pattern, room)
	if err != nil {
		return nil, err
	}
	defer coRows.Close()
	for coRows.Next() {
		var id, n int64
		var name string
		if err := coRows.Scan(&id, &name, &n); err != nil {
			return nil, err
		}
		out = append(out, &Item{Kind: "company", ID: id, Label: name, Hint: fmt.Sprintf("%d 个岗位", n)})
	}
	if err := coRows.Err(); err != nil {
		return nil, err
	}

	if len(out) >= limit {
		return out[:limit], nil
	}
	room = limit - len(out)

	// files: name matches (ready files only — only they can be opened).
	fRows, err := r.db.Pool().Query(ctx, `SELECT f.id, f.original_name, f.size_bytes
		FROM files f
		WHERE f.owner_id=$1 AND f.status='ready' AND f.original_name ILIKE $2
		ORDER BY f.created_at DESC
		LIMIT $3`, ownerID, pattern, room)
	if err != nil {
		return nil, err
	}
	defer fRows.Close()
	for fRows.Next() {
		var id string
		var name string
		var size int64
		if err := fRows.Scan(&id, &name, &size); err != nil {
			return nil, err
		}
		out = append(out, &Item{Kind: "file", ID: id, Label: name, Hint: humanBytes(size)})
	}
	if err := fRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// recent returns the most recently touched rows for an empty query (the
// palette's "最近打开" bucket): applications by last update, then newest
// ready files.
func (r *Repo) recent(ctx context.Context, ownerID int64, limit int) ([]*Item, error) {
	var out []*Item
	appRows, err := r.db.Pool().Query(ctx, `SELECT id, company_name, position, status, location
		FROM applications
		WHERE owner_id=$1 AND deleted_at IS NULL
		ORDER BY COALESCE(updated_at, created_at) DESC, id DESC
		LIMIT $2`, ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	for appRows.Next() {
		var id int64
		var company, position, status, location string
		if err := appRows.Scan(&id, &company, &position, &status, &location); err != nil {
			return nil, err
		}
		hint := statusLabel[status]
		if hint == "" {
			hint = status
		}
		if location != "" {
			hint += " · " + location
		}
		out = append(out, &Item{Kind: "application", ID: id, Label: company + " · " + position, Hint: hint})
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out[:limit], nil
	}
	fRows, err := r.db.Pool().Query(ctx, `SELECT id, original_name, size_bytes FROM files
		WHERE owner_id=$1 AND status='ready'
		ORDER BY created_at DESC
		LIMIT $2`, ownerID, limit-len(out))
	if err != nil {
		return nil, err
	}
	defer fRows.Close()
	for fRows.Next() {
		var id string
		var name string
		var size int64
		if err := fRows.Scan(&id, &name, &size); err != nil {
			return nil, err
		}
		out = append(out, &Item{Kind: "file", ID: id, Label: name, Hint: humanBytes(size)})
	}
	return out, fRows.Err()
}

// escapeLike neutralizes LIKE wildcards in user input.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
