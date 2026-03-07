package wrapper

import (
	"context"
	"database/sql"
)

// Stmt wraps a prepared statement with caching.
type Stmt struct {
	underlying *sql.Stmt
	query      string
	db         *DB
}

// Query executes the prepared query.
func (s *Stmt) Query(args ...interface{}) (*Rows, error) {
	return s.QueryContext(context.Background(), args...)
}

// QueryContext executes the prepared query with context.
func (s *Stmt) QueryContext(ctx context.Context, args ...interface{}) (*Rows, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrConnDone
	}
	return s.db.QueryContext(ctx, s.query, args...)
}

// QueryRow executes expecting one row.
func (s *Stmt) QueryRow(args ...interface{}) *Row {
	return s.QueryRowContext(context.Background(), args...)
}

// QueryRowContext executes with context expecting one row.
func (s *Stmt) QueryRowContext(ctx context.Context, args ...interface{}) *Row {
	if s == nil || s.db == nil {
		return &Row{err: sql.ErrConnDone}
	}
	return s.db.QueryRowContext(ctx, s.query, args...)
}

// Exec executes the prepared statement.
func (s *Stmt) Exec(args ...interface{}) (sql.Result, error) {
	return s.ExecContext(context.Background(), args...)
}

// ExecContext executes with context.
func (s *Stmt) ExecContext(ctx context.Context, args ...interface{}) (sql.Result, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrConnDone
	}
	return s.db.ExecContext(ctx, s.query, args...)
}

// Close closes the statement.
func (s *Stmt) Close() error {
	if s == nil || s.underlying == nil {
		return nil
	}
	return s.underlying.Close()
}
