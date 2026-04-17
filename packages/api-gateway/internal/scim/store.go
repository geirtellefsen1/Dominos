package scim

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("user not found")

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Row struct {
	ID          uuid.UUID
	ExternalID  string
	Email       string
	DisplayName string
	Active      bool
}

func (s *Store) Create(ctx context.Context, externalID, email, displayName string, active bool) (*Row, error) {
	email = strings.ToLower(email)
	const q = `
        INSERT INTO users (external_id, email, display_name, active)
        VALUES (NULLIF($1,''), $2, $3, $4)
        RETURNING id, COALESCE(external_id,''), email, COALESCE(display_name,''), active
    `
	r := &Row{}
	err := s.pool.QueryRow(ctx, q, externalID, email, displayName, active).
		Scan(&r.ID, &r.ExternalID, &r.Email, &r.DisplayName, &r.Active)
	if err != nil {
		return nil, fmt.Errorf("scim create: %w", err)
	}
	return r, nil
}

func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (*Row, error) {
	const q = `SELECT id, COALESCE(external_id,''), email, COALESCE(display_name,''), active FROM users WHERE id = $1`
	r := &Row{}
	err := s.pool.QueryRow(ctx, q, id).Scan(&r.ID, &r.ExternalID, &r.Email, &r.DisplayName, &r.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) ListByUserName(ctx context.Context, userName string) ([]*Row, error) {
	userName = strings.ToLower(userName)
	const q = `SELECT id, COALESCE(external_id,''), email, COALESCE(display_name,''), active FROM users WHERE email = $1`
	rows, err := s.pool.Query(ctx, q, userName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Row
	for rows.Next() {
		r := &Row{}
		if err := rows.Scan(&r.ID, &r.ExternalID, &r.Email, &r.DisplayName, &r.Active); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SetActive(ctx context.Context, id uuid.UUID, active bool) (*Row, error) {
	const q = `
        UPDATE users
        SET active = $2,
            deactivated_at = CASE WHEN $2 THEN NULL ELSE now() END,
            updated_at = now()
        WHERE id = $1
        RETURNING id, COALESCE(external_id,''), email, COALESCE(display_name,''), active
    `
	r := &Row{}
	err := s.pool.QueryRow(ctx, q, id, active).
		Scan(&r.ID, &r.ExternalID, &r.Email, &r.DisplayName, &r.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) Replace(ctx context.Context, id uuid.UUID, externalID, email, displayName string, active bool) (*Row, error) {
	email = strings.ToLower(email)
	const q = `
        UPDATE users
        SET external_id = NULLIF($2,''),
            email = $3,
            display_name = $4,
            active = $5,
            deactivated_at = CASE WHEN $5 THEN NULL ELSE COALESCE(deactivated_at, now()) END,
            updated_at = now()
        WHERE id = $1
        RETURNING id, COALESCE(external_id,''), email, COALESCE(display_name,''), active
    `
	r := &Row{}
	err := s.pool.QueryRow(ctx, q, id, externalID, email, displayName, active).
		Scan(&r.ID, &r.ExternalID, &r.Email, &r.DisplayName, &r.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}
