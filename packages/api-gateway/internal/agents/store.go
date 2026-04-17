package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("agent not found")

type Agent struct {
	ID          uuid.UUID  `json:"id"`
	DisplayName string     `json:"display_name"`
	Thumbprint  string     `json:"thumbprint"`
	OwnerUserID *uuid.UUID `json:"owner_user_id,omitempty"`
	Active      bool       `json:"active"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Insert(ctx context.Context, a *Agent, certPEM string) (*Agent, error) {
	const q = `
        INSERT INTO agents (id, display_name, thumbprint, cert_pem, owner_user_id, active)
        VALUES (COALESCE($1, gen_random_uuid()), $2, $3, $4, $5, TRUE)
        RETURNING id, display_name, thumbprint, owner_user_id, active, created_at, revoked_at
    `
	out := &Agent{}
	err := s.pool.QueryRow(ctx, q, a.ID, a.DisplayName, a.Thumbprint, certPEM, a.OwnerUserID).
		Scan(&out.ID, &out.DisplayName, &out.Thumbprint, &out.OwnerUserID,
			&out.Active, &out.CreatedAt, &out.RevokedAt)
	if err != nil {
		return nil, fmt.Errorf("insert agent: %w", err)
	}
	return out, nil
}

func (s *Store) GetByThumbprint(ctx context.Context, thumbprint string) (*Agent, error) {
	const q = `
        SELECT id, display_name, thumbprint, owner_user_id, active, created_at, revoked_at
        FROM agents WHERE thumbprint = $1
    `
	a := &Agent{}
	err := s.pool.QueryRow(ctx, q, thumbprint).
		Scan(&a.ID, &a.DisplayName, &a.Thumbprint, &a.OwnerUserID,
			&a.Active, &a.CreatedAt, &a.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) Revoke(ctx context.Context, id uuid.UUID) (*Agent, error) {
	const q = `
        UPDATE agents
        SET active = FALSE,
            revoked_at = COALESCE(revoked_at, now()),
            updated_at = now()
        WHERE id = $1
        RETURNING id, display_name, thumbprint, owner_user_id, active, created_at, revoked_at
    `
	a := &Agent{}
	err := s.pool.QueryRow(ctx, q, id).
		Scan(&a.ID, &a.DisplayName, &a.Thumbprint, &a.OwnerUserID,
			&a.Active, &a.CreatedAt, &a.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Agent, error) {
	const q = `
        SELECT id, display_name, thumbprint, owner_user_id, active, created_at, revoked_at
        FROM agents WHERE id = $1
    `
	a := &Agent{}
	err := s.pool.QueryRow(ctx, q, id).
		Scan(&a.ID, &a.DisplayName, &a.Thumbprint, &a.OwnerUserID,
			&a.Active, &a.CreatedAt, &a.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}
