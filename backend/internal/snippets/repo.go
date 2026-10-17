// Package snippets stores the user's reusable text snippets (迁移 00008):
// 实习经历的 role description、自我介绍、Why us…——投递时反复粘贴的那几段。
// Snippets are user-level, never tied to an application; every query is
// owner-scoped.
package snippets

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"offerlog/backend/internal/platform/database"
)

// ErrNotFound means the snippet does not exist or belongs to someone else.
var ErrNotFound = errors.New("snippet not found")

// Snippet is one saved piece of text.
type Snippet struct {
	ID        int64
	OwnerID   int64
	Title     string
	Content   string
	Category  string // free text; "" = 未分类
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

const cols = `id, owner_id, title, content, category, created_at, updated_at`

func scan(row pgx.Row) (*Snippet, error) {
	var s Snippet
	err := row.Scan(&s.ID, &s.OwnerID, &s.Title, &s.Content, &s.Category, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// List returns all of the user's snippets grouped by category (未分类 last),
// most recently edited first within a category.
func (r *Repo) List(ctx context.Context, ownerID int64) ([]*Snippet, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT `+cols+` FROM user_snippets WHERE owner_id=$1
		ORDER BY category = '' ASC, category ASC, updated_at DESC, id DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Snippet{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Get loads one snippet.
func (r *Repo) Get(ctx context.Context, ownerID, id int64) (*Snippet, error) {
	return scan(r.db.Pool().QueryRow(ctx, `SELECT `+cols+` FROM user_snippets WHERE id=$1 AND owner_id=$2`, id, ownerID))
}

// Create inserts a snippet and fills its id and timestamps.
func (r *Repo) Create(ctx context.Context, s *Snippet) error {
	return r.db.Pool().QueryRow(ctx, `INSERT INTO user_snippets(owner_id, title, content, category)
		VALUES($1,$2,$3,$4) RETURNING id, created_at, updated_at`,
		s.OwnerID, s.Title, s.Content, s.Category).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
}

// Update applies a partial edit: nil fields keep their stored value. Doing the
// merge in one UPDATE (COALESCE) avoids a read-merge-write race between two
// tabs editing different fields.
func (r *Repo) Update(ctx context.Context, ownerID, id int64, title, content, category *string) (*Snippet, error) {
	return scan(r.db.Pool().QueryRow(ctx, `UPDATE user_snippets SET
			title = COALESCE($3, title),
			content = COALESCE($4, content),
			category = COALESCE($5, category),
			updated_at = now()
		WHERE id=$1 AND owner_id=$2 RETURNING `+cols, id, ownerID, title, content, category))
}

// Delete removes a snippet.
func (r *Repo) Delete(ctx context.Context, ownerID, id int64) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM user_snippets WHERE id=$1 AND owner_id=$2`, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
