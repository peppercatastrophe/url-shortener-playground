// Package store provides PostgreSQL access for the URL shortener.
// Both the API and the worker use this package: the API reads and writes
// URLs, the worker writes click events.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Store wraps a PostgreSQL connection pool.
type Store struct {
	db *sql.DB
}

// New opens a connection pool to dsn and tunes it for a small service.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Minute)
	return &Store{db: db}, nil
}

// Close closes the underlying connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// InitSchema creates the urls and clicks tables if they do not exist.
func (s *Store) InitSchema() error {
	const q = `
CREATE TABLE IF NOT EXISTS urls (
    code         VARCHAR(16) PRIMARY KEY,
    original_url TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS clicks (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(16) NOT NULL,
    clicked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`
	_, err := s.db.Exec(q)
	return err
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
