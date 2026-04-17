// Package graph implements the Microsoft 365 mail connector for Phase 6
// (spec §3.6). It owns the OAuth consent flow, the per-user refresh
// tokens in connector_accounts, a polling loop that pulls /me/messages
// every 60 seconds, and the mapping that turns each Graph message into
// an email.v1 document owned by the user with the user's PA granted
// reader.
package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/cryptokeys"
)

const Provider = "graph"

var ErrAccountNotFound = errors.New("connector account not found")

type Account struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	Provider          string
	ExternalAccountID string
	RefreshToken      string
	LastSyncedAt      *time.Time
	LastError         string
	CreatedAt         time.Time
	RevokedAt         *time.Time
}

type Store struct {
	pool *pgxpool.Pool
	aead *cryptokeys.AESGCM
}

func NewStore(pool *pgxpool.Pool, aead *cryptokeys.AESGCM) *Store {
	return &Store{pool: pool, aead: aead}
}

// Upsert saves (or refreshes) the refresh token for a user's mailbox.
func (s *Store) Upsert(ctx context.Context, userID uuid.UUID, externalID, refreshToken string) (*Account, error) {
	ct, err := s.aead.Seal([]byte(refreshToken))
	if err != nil {
		return nil, fmt.Errorf("seal token: %w", err)
	}
	const q = `
        INSERT INTO connector_accounts (user_id, provider, external_account_id, refresh_token_ciphertext)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (user_id, provider) DO UPDATE
            SET external_account_id      = EXCLUDED.external_account_id,
                refresh_token_ciphertext = EXCLUDED.refresh_token_ciphertext,
                revoked_at               = NULL,
                last_error               = NULL,
                updated_at               = now()
        RETURNING id, user_id, provider, external_account_id, last_synced_at, COALESCE(last_error,''), created_at, revoked_at
    `
	a := &Account{RefreshToken: refreshToken}
	err = s.pool.QueryRow(ctx, q, userID, Provider, externalID, ct).Scan(
		&a.ID, &a.UserID, &a.Provider, &a.ExternalAccountID,
		&a.LastSyncedAt, &a.LastError, &a.CreatedAt, &a.RevokedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert connector: %w", err)
	}
	return a, nil
}

// ListActive returns every non-revoked account for the polling loop.
func (s *Store) ListActive(ctx context.Context) ([]*Account, error) {
	const q = `
        SELECT id, user_id, provider, external_account_id,
               refresh_token_ciphertext,
               last_synced_at, COALESCE(last_error,''), created_at, revoked_at
        FROM connector_accounts
        WHERE revoked_at IS NULL AND provider = $1
    `
	rows, err := s.pool.Query(ctx, q, Provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Account
	for rows.Next() {
		a := &Account{}
		var ct []byte
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.Provider, &a.ExternalAccountID,
			&ct, &a.LastSyncedAt, &a.LastError, &a.CreatedAt, &a.RevokedAt,
		); err != nil {
			return nil, err
		}
		pt, err := s.aead.Open(ct)
		if err != nil {
			return nil, fmt.Errorf("decrypt refresh token for %s: %w", a.ID, err)
		}
		a.RefreshToken = string(pt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetForUser returns the user's graph account or ErrAccountNotFound.
func (s *Store) GetForUser(ctx context.Context, userID uuid.UUID) (*Account, error) {
	const q = `
        SELECT id, user_id, provider, external_account_id,
               last_synced_at, COALESCE(last_error,''), created_at, revoked_at
        FROM connector_accounts
        WHERE user_id = $1 AND provider = $2
    `
	a := &Account{}
	err := s.pool.QueryRow(ctx, q, userID, Provider).Scan(
		&a.ID, &a.UserID, &a.Provider, &a.ExternalAccountID,
		&a.LastSyncedAt, &a.LastError, &a.CreatedAt, &a.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	return a, err
}

// TouchSynced records a successful poll; ClearError / SetError manage
// the visible last_error field.
func (s *Store) TouchSynced(ctx context.Context, id uuid.UUID, when time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE connector_accounts SET last_synced_at = $2, last_error = NULL, updated_at = now()
         WHERE id = $1`,
		id, when)
	return err
}

func (s *Store) SetError(ctx context.Context, id uuid.UUID, msg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE connector_accounts SET last_error = $2, updated_at = now() WHERE id = $1`,
		id, msg)
	return err
}
