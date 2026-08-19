// Package store contiene el acceso a datos del panel sobre PostgreSQL.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("recurso no encontrado")
	ErrConflict = errors.New("el recurso ya existe")
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// --- Usuarios --------------------------------------------------------------

const userCols = `id, username, email, password_hash, full_name, role, parent_id,
                  is_active, totp_secret, last_login_at, created_at, updated_at`

func scanUser(row pgx.Row) (*models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.FullName,
		&u.Role, &u.ParentID, &u.IsActive, &u.TOTPSecret, &u.LastLoginAt,
		&u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) CreateUser(ctx context.Context, u *models.User) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, full_name, role, parent_id, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at, updated_at`,
		u.Username, u.Email, u.PasswordHash, u.FullName, u.Role, u.ParentID, u.IsActive,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=$1`, id))
}

func (s *Store) GetUserByLogin(ctx context.Context, login string) (*models.User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userCols+` FROM users WHERE username=$1 OR email=$1`, login))
}

// ListUsers devuelve todos los usuarios (admin) o solo los hijos del reseller.
func (s *Store) ListUsers(ctx context.Context, scopeParent *uuid.UUID) ([]models.User, error) {
	q := `SELECT ` + userCols + ` FROM users`
	args := []any{}
	if scopeParent != nil {
		q += ` WHERE parent_id = $1 OR id = $1`
		args = append(args, *scopeParent)
	}
	q += ` ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, u *models.User) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE users SET email=$2, full_name=$3, role=$4, is_active=$5, updated_at=now()
		WHERE id=$1`, u.ID, u.Email, u.FullName, u.Role, u.IsActive)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, hash)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TouchLogin(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, id)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id uuid.UUID) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// --- Sesiones / refresh tokens ---------------------------------------------

func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, refreshHash,
	userAgent, ip string, ttl time.Duration) error {
	var ipArg any
	if ip != "" {
		ipArg = ip
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_hash, user_agent, ip_address, expires_at)
		VALUES ($1,$2,$3,$4,$5)`,
		userID, refreshHash, userAgent, ipArg, time.Now().Add(ttl))
	return err
}

// ConsumeSession valida un refresh token y lo revoca (rotación de tokens).
func (s *Store) ConsumeSession(ctx context.Context, refreshHash string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		UPDATE sessions SET revoked_at = now()
		WHERE refresh_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING user_id`, refreshHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return userID, err
}

func (s *Store) RevokeUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM sessions WHERE expires_at < now() - interval '7 days'`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}
