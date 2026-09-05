package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"offerlogs/backend/internal/platform/database"
)

// PropertyDef is a custom property definition row (marshaled as the wire DTO).
type PropertyDef struct {
	ID            int64           `json:"id"`
	OwnerID       int64           `json:"-"`
	Name          string          `json:"name"`
	Key           string          `json:"key"`
	DataType      string          `json:"data_type"`
	Options       json.RawMessage `json:"options"`
	Required      bool            `json:"required"`
	Order         int             `json:"order"`
	SchemaVersion int             `json:"schema_version"`
}

// SavedView is one saved row/view config (marshaled as the wire DTO).
type SavedView struct {
	ID            int64           `json:"id"`
	OwnerID       int64           `json:"-"`
	Name          string          `json:"name"`
	Layout        string          `json:"layout"`
	Columns       json.RawMessage `json:"columns"`
	FilterAST     json.RawMessage `json:"filter_ast"`
	Sort          json.RawMessage `json:"sort"`
	GroupBy       json.RawMessage `json:"group_by"`
	IsBuiltin     bool            `json:"is_builtin"`
	SchemaVersion int             `json:"schema_version"`
}

type Repo struct{ db *database.DB }

func New(db *database.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Pool() *database.DB { return r.db }

// -- property definitions --

func (r *Repo) ListProperties(ctx context.Context, ownerID int64) ([]*PropertyDef, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT id, owner_id, name, key, data_type, options, required, "order", schema_version
		FROM property_definitions WHERE owner_id=$1 ORDER BY "order", name`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PropertyDef
	for rows.Next() {
		var p PropertyDef
		if err := rows.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Key, &p.DataType, &p.Options, &p.Required, &p.Order, &p.SchemaVersion); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (r *Repo) GetProperty(ctx context.Context, ownerID, id int64) (*PropertyDef, error) {
	var p PropertyDef
	err := r.db.Pool().QueryRow(ctx, `SELECT id, owner_id, name, key, data_type, options, required, "order", schema_version
		FROM property_definitions WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&p.ID, &p.OwnerID, &p.Name, &p.Key, &p.DataType, &p.Options, &p.Required, &p.Order, &p.SchemaVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

func (r *Repo) CreateProperty(ctx context.Context, p *PropertyDef) error {
	return r.db.Pool().QueryRow(ctx, `INSERT INTO property_definitions(owner_id, name, key, data_type, options, required, "order")
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		p.OwnerID, p.Name, p.Key, p.DataType, p.Options, p.Required, p.Order).Scan(&p.ID)
}

func (r *Repo) UpdateProperty(ctx context.Context, p *PropertyDef) error {
	tag, err := r.db.Pool().Exec(ctx, `UPDATE property_definitions SET name=$1, data_type=$2, options=$3,
		required=$4, "order"=$5, schema_version=schema_version+1, updated_at=now() WHERE id=$6 AND owner_id=$7`,
		p.Name, p.DataType, p.Options, p.Required, p.Order, p.ID, p.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repo) DeleteProperty(ctx context.Context, ownerID, id int64) error {
	_, err := r.db.Pool().Exec(ctx, `DELETE FROM property_definitions WHERE id=$1 AND owner_id=$2`, id, ownerID)
	return err
}

// -- saved views --

func (r *Repo) ListViews(ctx context.Context, ownerID int64) ([]*SavedView, error) {
	rows, err := r.db.Pool().Query(ctx, `SELECT id, owner_id, name, layout, columns, filter_ast, sort, group_by, is_builtin, schema_version
		FROM saved_views WHERE owner_id=$1 ORDER BY is_builtin DESC, id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SavedView
	for rows.Next() {
		var v SavedView
		if err := rows.Scan(&v.ID, &v.OwnerID, &v.Name, &v.Layout, &v.Columns, &v.FilterAST, &v.Sort, &v.GroupBy, &v.IsBuiltin, &v.SchemaVersion); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

func (r *Repo) GetView(ctx context.Context, ownerID, id int64) (*SavedView, error) {
	var v SavedView
	err := r.db.Pool().QueryRow(ctx, `SELECT id, owner_id, name, layout, columns, filter_ast, sort, group_by, is_builtin, schema_version
		FROM saved_views WHERE id=$1 AND owner_id=$2`, id, ownerID).
		Scan(&v.ID, &v.OwnerID, &v.Name, &v.Layout, &v.Columns, &v.FilterAST, &v.Sort, &v.GroupBy, &v.IsBuiltin, &v.SchemaVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

func (r *Repo) CreateView(ctx context.Context, v *SavedView) error {
	if len(v.Columns) == 0 {
		v.Columns = json.RawMessage(`[]`)
	}
	if len(v.FilterAST) == 0 {
		v.FilterAST = json.RawMessage(`[]`)
	}
	if len(v.Sort) == 0 {
		v.Sort = json.RawMessage(`[]`)
	}
	if len(v.GroupBy) == 0 {
		v.GroupBy = json.RawMessage(`{}`)
	}
	return r.db.Pool().QueryRow(ctx, `INSERT INTO saved_views(owner_id, name, layout, columns, filter_ast, sort, group_by, is_builtin)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		v.OwnerID, v.Name, v.Layout, v.Columns, v.FilterAST, v.Sort, v.GroupBy, v.IsBuiltin).Scan(&v.ID)
}

func (r *Repo) UpdateView(ctx context.Context, v *SavedView) error {
	tag, err := r.db.Pool().Exec(ctx, `UPDATE saved_views SET name=$1, layout=$2, columns=$3, filter_ast=$4,
		sort=$5, group_by=$6, schema_version=schema_version+1, updated_at=now() WHERE id=$7 AND owner_id=$8`,
		v.Name, v.Layout, v.Columns, v.FilterAST, v.Sort, v.GroupBy, v.ID, v.OwnerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *Repo) DeleteView(ctx context.Context, ownerID, id int64) error {
	_, err := r.db.Pool().Exec(ctx, `DELETE FROM saved_views WHERE id=$1 AND owner_id=$2 AND is_builtin=false`, id, ownerID)
	return err
}
