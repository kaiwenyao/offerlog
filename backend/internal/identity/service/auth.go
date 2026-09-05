package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"offerlogs/backend/internal/platform/database"
)

var (
	ErrEmailTaken     = errors.New("email already registered")
	ErrBadLogin       = errors.New("invalid credentials")
	ErrNoPermission   = errors.New("permission denied")
	ErrSessionExpired = errors.New("session expired")
)

type UserRow struct {
	ID           int64
	Email        string
	PasswordHash string
	DisplayName  string
	Timezone     string
	Locale       string
	IsAdmin      bool
}

// Repo abstracts the SQL rows the identity service needs.
type Repo interface {
	FindUserByEmail(ctx context.Context, email string) (*UserRow, error)
	FindUserByID(ctx context.Context, id int64) (*UserRow, error)
	CreateUser(ctx context.Context, u *UserRow) error
}

type Store struct {
	db      *database.DB
	users   Repo
	secrets *Secrets
}

type Secrets struct {
	sessionHours time.Duration
	csrfSecret   string
}

func NewSecrets(sessionHours int, csrfSecret string) *Secrets {
	if csrfSecret == "" {
		csrfSecret = randomHex(32)
	}
	return &Secrets{sessionHours: time.Duration(sessionHours) * time.Hour, csrfSecret: csrfSecret}
}

func randomHex(n int) string {
	b := make([]byte, n/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func New(db *database.DB, users Repo, sessionHours int, csrfSecret string) *Store {
	return &Store{db: db, users: users, secrets: NewSecrets(sessionHours, csrfSecret)}
}

func (s *Store) Secrets() *Secrets { return s.secrets }

// Session is an opaque cookie value the client stores.
type Session struct {
	Token   string
	CSRF    string
	Expires time.Time
}

const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
)

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t, p uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, uint32(t), uint32(m), uint8(p), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Register is used by the admin CLI to seed the first account (public signup
// is closed in v1).
func (s *Store) Register(ctx context.Context, email, password, displayName, tz string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.users.FindUserByEmail(ctx, email)
	if err == nil {
		return ErrEmailTaken
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	u := &UserRow{Email: email, PasswordHash: hash, DisplayName: displayName, Timezone: tz}
	return s.users.CreateUser(ctx, u)
}

var ErrNotFound = errors.New("not found")

func (s *Store) CreateSession(ctx context.Context, email, password string) (*Session, *UserRow, error) {
	u, err := s.users.FindUserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, nil, ErrBadLogin
	}
	if !verifyPassword(u.PasswordHash, password) {
		return nil, nil, ErrBadLogin
	}
	tok := randomHex(32)
	csrf := randomHex(24)
	th := hashToken(tok)
	exp := time.Now().Add(s.secrets.sessionHours)
	if err := s.insertSession(ctx, u.ID, th, csrf, exp); err != nil {
		return nil, nil, err
	}
	return &Session{Token: tok, CSRF: csrf, Expires: exp}, u, nil
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func sha256Sum(b string) []byte {
	h := sha256.Sum256([]byte(b))
	return h[:]
}

func (s *Store) insertSession(ctx context.Context, userID int64, tokenHash, csrf string, exp time.Time) error {
	_, err := s.db.Pool().Exec(ctx, `INSERT INTO sessions(user_id, token_hash, csrf_token, expires_at) VALUES($1,$2,$3,$4)`, userID, tokenHash, csrf, exp)
	return err
}

// ValidateToken resolves a session cookie token to a user; it also refreshes
// last_seen and rejects expired sessions.
func (s *Store) ValidateToken(ctx context.Context, token string) (*UserRow, error) {
	if token == "" {
		return nil, ErrBadLogin
	}
	th := hashToken(token)
	var u UserRow
	var expires time.Time
	err := s.db.Pool().QueryRow(ctx, `SELECT u.id, u.email, u.password_hash, u.display_name, u.timezone, u.locale, u.is_admin, se.expires_at
		FROM sessions se JOIN users u ON u.id = se.user_id
		WHERE se.token_hash = $1`, th).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Timezone, &u.Locale, &u.IsAdmin, &expires)
	if err != nil {
		return nil, ErrBadLogin
	}
	if time.Now().After(expires) {
		return nil, ErrSessionExpired
	}
	_, _ = s.db.Pool().Exec(ctx, `UPDATE sessions SET last_seen = now() WHERE token_hash = $1`, th)
	return &u, nil
}

func (s *Store) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.Pool().Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hashToken(token))
	return err
}

func (s *Store) ValidateCSRF(ctx context.Context, token, csrf string) bool {
	th := hashToken(token)
	var stored string
	err := s.db.Pool().QueryRow(ctx, `SELECT csrf_token FROM sessions WHERE token_hash = $1`, th).Scan(&stored)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(csrf)) == 1
}
