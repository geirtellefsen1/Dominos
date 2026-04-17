package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUserNotFound = errors.New("user not found")

type User struct {
	ID          uuid.UUID `json:"id"`
	ExternalID  string    `json:"external_id,omitempty"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name,omitempty"`
	Active      bool      `json:"active"`
}

type UserStore struct{ pool *pgxpool.Pool }

func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

// UpsertFromOIDC creates or refreshes a user row keyed by external_id.
func (s *UserStore) UpsertFromOIDC(ctx context.Context, externalID, email, displayName string) (*User, error) {
	const q = `
        INSERT INTO users (external_id, email, display_name)
        VALUES ($1, $2, $3)
        ON CONFLICT (external_id) DO UPDATE
          SET email = EXCLUDED.email,
              display_name = EXCLUDED.display_name,
              updated_at = now()
        RETURNING id, external_id, email, display_name, active
    `
	u := &User{}
	var ext, dn *string
	err := s.pool.QueryRow(ctx, q, externalID, email, displayName).
		Scan(&u.ID, &ext, &u.Email, &dn, &u.Active)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	if ext != nil {
		u.ExternalID = *ext
	}
	if dn != nil {
		u.DisplayName = *dn
	}
	return u, nil
}

func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	const q = `SELECT id, COALESCE(external_id,''), email, COALESCE(display_name,''), active FROM users WHERE id = $1`
	u := &User{}
	err := s.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.ExternalID, &u.Email, &u.DisplayName, &u.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}
