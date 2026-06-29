// Package store is the SQLite materialized view of the eventlog. The JSONL
// files under ~/.shiptrace/events/ are the source of truth; this package
// holds derived rows that exist so the CLI and dashboard can run queries
// without rescanning files. If this DB is deleted, the ingester rebuilds it.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

// Store wraps a *sql.DB and exposes typed methods for the operations the rest
// of shiptrace needs. Day 1 covers session start/stop and ship events; day 2
// extends with tool_events.
type Store struct {
	db *sql.DB
}

// dbtx is the read+write surface common to *sql.DB and *sql.Tx, so helper
// functions can run either standalone or inside a transaction. The staleness
// sweep uses this to run its UPDATEs and replan recomputes atomically.
type dbtx interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Open opens (or creates) the SQLite database at path, applies any pending
// migrations, and returns the Store. Idempotent — safe to call against an
// existing DB.
func Open(path string) (*Store, error) {
	// The "?_pragma=" trick lets modernc apply pragmas at connection time so
	// every connection in the pool is consistent (WAL is per-DB but
	// foreign_keys is per-connection). We URL-escape the path so a path
	// containing '?', '#', '&', or '%' can't break out of the file: portion
	// and inject additional pragmas. (PathEscape leaves '/' alone, which
	// modernc.org/sqlite parses correctly.)
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		url.PathEscape(path),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	if err := runMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB exposes the *sql.DB for advanced callers (tests, ad-hoc queries). The
// CLI should prefer the typed methods.
func (s *Store) DB() *sql.DB {
	return s.db
}
