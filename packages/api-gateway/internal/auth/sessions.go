package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SessionCookieName = "dominion_session"
const DefaultSessionTTL = 24 * time.Hour

var (
	ErrSessionInvalid = errors.New("session invalid")
	ErrSessionExpired = errors.New("session expired")
	ErrSessionRevoked = errors.New("session revoked")
	ErrUserInactive   = errors.New("user inactive")
)

type SessionStore struct {
	pool      *pgxpool.Pool
	jwtSecret []byte
}

func NewSessionStore(pool *pgxpool.Pool, jwtSecret []byte) *SessionStore {
	return &SessionStore{pool: pool, jwtSecret: jwtSecret}
}

type sessionClaims struct {
	SID string `json:"sid"`
	UID string `json:"uid"`
	jwt.RegisteredClaims
}

type SessionInfo struct {
	SessionID uuid.UUID
	UserID    uuid.UUID
	Email     string
	Active    bool
}

// Create inserts a session row and returns a signed JWT cookie value.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID, ttl time.Duration, ua, ip string) (string, uuid.UUID, error) {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	expires := time.Now().Add(ttl)

	var sid uuid.UUID
	err := s.pool.QueryRow(ctx, `
        INSERT INTO sessions (user_id, expires_at, user_agent, ip)
        VALUES ($1, $2, $3, NULLIF($4, '')::inet)
        RETURNING id
    `, userID, expires, ua, ip).Scan(&sid)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("insert session: %w", err)
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, sessionClaims{
		SID: sid.String(),
		UID: userID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expires),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "dominion",
			Subject:   userID.String(),
		},
	})
	signed, err := tok.SignedString(s.jwtSecret)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("sign jwt: %w", err)
	}
	return signed, sid, nil
}

// Verify validates a JWT cookie value and checks the DB for revocation and
// user status. Returns the session info on success.
func (s *SessionStore) Verify(ctx context.Context, raw string) (*SessionInfo, error) {
	var claims sessionClaims
	tok, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil || !tok.Valid {
		return nil, ErrSessionInvalid
	}
	sid, err := uuid.Parse(claims.SID)
	if err != nil {
		return nil, ErrSessionInvalid
	}

	var info SessionInfo
	var revokedAt *time.Time
	var expiresAt time.Time
	err = s.pool.QueryRow(ctx, `
        SELECT s.id, s.user_id, s.expires_at, s.revoked_at, u.email, u.active
        FROM sessions s
        JOIN users u ON u.id = s.user_id
        WHERE s.id = $1
    `, sid).Scan(&info.SessionID, &info.UserID, &expiresAt, &revokedAt, &info.Email, &info.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if revokedAt != nil {
		return nil, ErrSessionRevoked
	}
	if time.Now().After(expiresAt) {
		return nil, ErrSessionExpired
	}
	if !info.Active {
		return nil, ErrUserInactive
	}
	return &info, nil
}

// RevokeByID marks a single session revoked.
func (s *SessionStore) RevokeByID(ctx context.Context, sid uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		sid)
	return err
}

// RevokeAllForUser marks every active session for a user revoked. Called by
// SCIM deactivation and by the Phase 8 one-revoke primitive.
func (s *SessionStore) RevokeAllForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
