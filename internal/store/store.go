// Package store provides PostgreSQL access for the URL shortener.
// Both the API and the worker use this package: the API reads and writes
// URLs, the worker writes click events.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	postgresdrv "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	migrations "url-shortener/migrations"

	_ "github.com/lib/pq"
)

// Store wraps a PostgreSQL connection pool.
type Store struct {
	db  *sql.DB
	dsn string
}

// New opens a connection pool to dsn and tunes it for a small service.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	return &Store{db: db, dsn: dsn}, nil
}

// Close closes the underlying connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for direct access (test helpers, ad-hoc queries).
func (s *Store) DB() *sql.DB { return s.db }

// Migrate applies all embedded SQL migrations up to the latest version.
// Migrations live in migrations/ and are embedded via migrations/embed.go
// so the binaries are self-contained: no external migration files needed.
// This replaces the old CREATE TABLE IF NOT EXISTS bootstrap and gives a
// versioned, reversible schema history (golang-migrate).
//
// Migrate opens a SEPARATE *sql.DB for golang-migrate and closes it before
// returning. This is critical: postgresdrv.WithInstance takes ownership of
// the *sql.DB it wraps, and migrate's Close() closes that DB. If we passed
// the store's shared query pool (s.db), m.Close() would close it and every
// subsequent query in the process would fail with "sql: database is closed".
// Using a throwaway connection keeps the store's pool alive after migration.
func (s *Store) Migrate(ctx context.Context) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("open migration source: %w", err)
	}

	migDB, err := sql.Open("postgres", s.dsn)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer migDB.Close()

	pgDrv, err := postgresdrv.WithInstance(migDB, &postgresdrv.Config{})
	if err != nil {
		return fmt.Errorf("init postgres migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", pgDrv)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// InsertURL stores a short code and its original URL.
func (s *Store) InsertURL(ctx context.Context, code, originalURL string) error {
	const q = `INSERT INTO urls (code, original_url) VALUES ($1, $2)`
	_, err := s.db.ExecContext(ctx, q, code, originalURL)
	return err
}

// GetURL returns the original URL for code.
// Returns sql.ErrNoRows if the code does not exist.
func (s *Store) GetURL(ctx context.Context, code string) (string, error) {
	var original string
	const q = `SELECT original_url FROM urls WHERE code = $1`
	err := s.db.QueryRowContext(ctx, q, code).Scan(&original)
	return original, err
}

// InsertClick records that code was accessed at clickedAt.
func (s *Store) InsertClick(ctx context.Context, code string, clickedAt time.Time) error {
	const q = `INSERT INTO clicks (code, clicked_at) VALUES ($1, $2)`
	_, err := s.db.ExecContext(ctx, q, code, clickedAt)
	return err
}

// ClickStats holds aggregated analytics for a short code over a time window.
type ClickStats struct {
	Code  string
	Count int64
}

// CountClicksSince returns the number of clicks for code since the given time.
// It uses the idx_clicks_code_clicked_at composite index added in migration
// 000002 so the query is an index scan, not a full table scan.
func (s *Store) CountClicksSince(ctx context.Context, code string, since time.Time) (ClickStats, error) {
	const q = `SELECT count(*) FROM clicks WHERE code = $1 AND clicked_at >= $2`
	var count int64
	err := s.db.QueryRowContext(ctx, q, code, since).Scan(&count)
	if err != nil {
		return ClickStats{}, fmt.Errorf("count clicks: %w", err)
	}
	return ClickStats{Code: code, Count: count}, nil
}

// LatestClickAt returns the most recent click time for code, or sql.ErrNoRows.
func (s *Store) LatestClickAt(ctx context.Context, code string) (time.Time, error) {
	const q = `SELECT clicked_at FROM clicks WHERE code = $1 ORDER BY clicked_at DESC LIMIT 1`
	var t time.Time
	err := s.db.QueryRowContext(ctx, q, code).Scan(&t)
	return t, err
}
