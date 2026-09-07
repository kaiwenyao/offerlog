package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	auth "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/platform/database"
)

type SQLUsers struct {
	db *database.DB
}

func NewSQLUsers(db *database.DB) *SQLUsers { return &SQLUsers{db: db} }

func (r *SQLUsers) FindUserByEmail(ctx context.Context, email string) (*auth.UserRow, error) {
	var u auth.UserRow
	err := r.db.Pool().QueryRow(ctx, `SELECT id, email, password_hash, display_name, timezone, locale, is_admin FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Timezone, &u.Locale, &u.IsAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *SQLUsers) FindUserByID(ctx context.Context, id int64) (*auth.UserRow, error) {
	var u auth.UserRow
	err := r.db.Pool().QueryRow(ctx, `SELECT id, email, password_hash, display_name, timezone, locale, is_admin FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Timezone, &u.Locale, &u.IsAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *SQLUsers) CreateUser(ctx context.Context, u *auth.UserRow) error {
	return r.db.Pool().QueryRow(ctx, `INSERT INTO users(email, password_hash, display_name, timezone, locale) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		u.Email, u.PasswordHash, u.DisplayName, u.Timezone, u.Locale).Scan(&u.ID)
}

// UpdateProfile persists the mutable account profile fields (display name and
// timezone) on the users row so /auth/me and the session stay consistent. It
// returns the updated row.
func (r *SQLUsers) UpdateProfile(ctx context.Context, id int64, displayName, timezone string) (*auth.UserRow, error) {
	return r.UpdateProfileTx(ctx, r.db, id, displayName, timezone)
}

// UpdateProfileTx is the transactional variant of UpdateProfile: it runs on the
// given Querier (a pgx.Tx) so callers such as PUT /preferences can persist the
// users profile row and the preferences row atomically.
func (r *SQLUsers) UpdateProfileTx(ctx context.Context, q database.Querier, id int64, displayName, timezone string) (*auth.UserRow, error) {
	var u auth.UserRow
	err := q.QueryRow(ctx, `UPDATE users SET display_name=$2, timezone=$3, updated_at=now()
		WHERE id=$1 RETURNING id, email, password_hash, display_name, timezone, locale, is_admin`,
		id, displayName, timezone).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Timezone, &u.Locale, &u.IsAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNotFound
	}
	return &u, err
}
