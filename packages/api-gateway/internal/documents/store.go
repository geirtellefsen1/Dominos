package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound     = errors.New("document not found")
	ErrSchemaNotFound = errors.New("schema not found")
)

// MVPTenantID is the fixed tenant used until v2 multi-tenancy lands.
var MVPTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

type Document struct {
	ID        uuid.UUID       `json:"id"`
	SchemaID  string          `json:"schema_id"`
	TenantID  uuid.UUID       `json:"tenant_id"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"created_at"`
	CreatedBy string          `json:"created_by"`
	UpdatedAt time.Time       `json:"updated_at"`
	UpdatedBy string          `json:"updated_by"`
	DeletedAt *time.Time      `json:"deleted_at,omitempty"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) LoadSchema(ctx context.Context, id string) (json.RawMessage, error) {
	var js json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT json_schema FROM schemas WHERE id = $1`, id).Scan(&js)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSchemaNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load schema: %w", err)
	}
	return js, nil
}

func (s *Store) Create(ctx context.Context, schemaID string, body json.RawMessage, principal string) (*Document, error) {
	const q = `
        INSERT INTO documents (schema_id, tenant_id, body, created_by, updated_by)
        VALUES ($1, $2, $3, $4, $4)
        RETURNING id, schema_id, tenant_id, body, created_at, created_by, updated_at, updated_by, deleted_at
    `
	d := &Document{}
	err := s.pool.QueryRow(ctx, q, schemaID, MVPTenantID, body, principal).Scan(
		&d.ID, &d.SchemaID, &d.TenantID, &d.Body,
		&d.CreatedAt, &d.CreatedBy, &d.UpdatedAt, &d.UpdatedBy, &d.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert document: %w", err)
	}
	return d, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Document, error) {
	const q = `
        SELECT id, schema_id, tenant_id, body, created_at, created_by, updated_at, updated_by, deleted_at
        FROM documents
        WHERE id = $1 AND deleted_at IS NULL
    `
	d := &Document{}
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&d.ID, &d.SchemaID, &d.TenantID, &d.Body,
		&d.CreatedAt, &d.CreatedBy, &d.UpdatedAt, &d.UpdatedBy, &d.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}
	return d, nil
}

type ListParams struct {
	SchemaID string
	Limit    int
	Offset   int
}

func (s *Store) List(ctx context.Context, p ListParams) ([]*Document, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	const q = `
        SELECT id, schema_id, tenant_id, body, created_at, created_by, updated_at, updated_by, deleted_at
        FROM documents
        WHERE deleted_at IS NULL
          AND ($1 = '' OR schema_id = $1)
        ORDER BY created_at DESC
        LIMIT $2 OFFSET $3
    `
	rows, err := s.pool.Query(ctx, q, p.SchemaID, p.Limit, p.Offset)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	var out []*Document
	for rows.Next() {
		d := &Document{}
		if err := rows.Scan(
			&d.ID, &d.SchemaID, &d.TenantID, &d.Body,
			&d.CreatedAt, &d.CreatedBy, &d.UpdatedAt, &d.UpdatedBy, &d.DeletedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
