package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Migrate applies every .sql file in the embedded migrations directory
// in lexical order, and records each one in a schema_migrations table
// so subsequent boots skip files that have already been applied.
//
// Before Sprint 2 #15 there was no ledger — every file re-ran on
// every boot. The existing migrations were written idempotently
// (CREATE ... IF NOT EXISTS, INSERT ... ON CONFLICT DO NOTHING) so
// nothing broke, but the moment anyone added a non-idempotent
// statement (ALTER TABLE ADD COLUMN, a one-shot UPDATE with a
// backfill default, a constraint creation) the re-run model would
// break production on the second deploy.
//
// Acquire a Postgres advisory lock while applying so two gateways
// starting at the same time don't race on the ledger.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	// Advisory lock — arbitrary int64, same value everywhere so every
	// gateway in a cluster serialises on it during Migrate().
	const advisoryKey int64 = 8112025_001

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for migrate: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryKey); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryKey)
	}()

	// Bootstrap the ledger itself. The ledger table is not in a .sql
	// file because we need it before we can read the ledger. Safe to
	// run on every boot.
	if _, err := conn.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            name        TEXT PRIMARY KEY,
            checksum    TEXT NOT NULL,
            applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
        )
    `); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, name := range names {
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		sum := checksum(body)

		var existing string
		err = conn.QueryRow(ctx,
			`SELECT checksum FROM schema_migrations WHERE name = $1`, name,
		).Scan(&existing)
		switch {
		case err == nil:
			if existing != sum {
				// A migration's bytes changed after it was applied.
				// Refuse to continue — someone edited a file that has
				// already run on this DB, which makes the ledger a
				// liar. Correct fix: revert the edit and add a new
				// migration file.
				return fmt.Errorf(
					"migration %s has been modified since it was applied (ledger=%s new=%s); "+
						"do not edit applied migrations — add a new one instead",
					name, existing, sum)
			}
			slog.Info("skipping already-applied migration", "name", name)
			continue
		case errors.Is(err, pgx.ErrNoRows):
			// New migration — apply it.
		default:
			return fmt.Errorf("ledger lookup for %s: %w", name, err)
		}

		slog.Info("applying migration", "name", name)
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := conn.Exec(ctx,
			`INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)`,
			name, sum,
		); err != nil {
			return fmt.Errorf("record %s in ledger: %w", name, err)
		}
	}
	return nil
}

func checksum(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}
