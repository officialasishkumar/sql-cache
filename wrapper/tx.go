package wrapper

import (
	"context"
	"database/sql"
	"sync"

	sqlcache "github.com/officialasishkumar/sql-cache"
)

// Tx wraps a transaction with caching.
type Tx struct {
	underlying *sql.Tx
	db         *DB
	done       bool
	mu         sync.Mutex
}

// Query executes a query.
func (tx *Tx) Query(query string, args ...interface{}) (*Rows, error) {
	return tx.QueryContext(context.Background(), query, args...)
}

// QueryContext executes a query with context.
func (tx *Tx) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	if tx.isDone() {
		return nil, sql.ErrTxDone
	}
	if tx == nil || tx.db == nil {
		return nil, sql.ErrConnDone
	}
	if tx.db.GetMode() == sqlcache.ModeOffline || tx.underlying == nil {
		return tx.db.QueryContext(ctx, query, args...)
	}

	rows, err := tx.underlying.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{live: rows}, nil
}

// QueryRow executes expecting one row.
func (tx *Tx) QueryRow(query string, args ...interface{}) *Row {
	return tx.QueryRowContext(context.Background(), query, args...)
}

// QueryRowContext executes with context expecting one row.
func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := tx.QueryContext(ctx, query, args...)
	return &Row{rows: rows, err: err}
}

// Exec executes a query.
func (tx *Tx) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.ExecContext(context.Background(), query, args...)
}

// ExecContext executes with context.
func (tx *Tx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if tx.isDone() {
		return nil, sql.ErrTxDone
	}
	if tx == nil || tx.db == nil {
		return nil, sql.ErrConnDone
	}
	if tx.db.GetMode() == sqlcache.ModeOffline || tx.underlying == nil {
		return tx.db.ExecContext(ctx, query, args...)
	}
	return tx.underlying.ExecContext(ctx, query, args...)
}

// Commit commits the transaction.
func (tx *Tx) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.done {
		return sql.ErrTxDone
	}
	tx.done = true
	if tx.underlying != nil {
		return tx.underlying.Commit()
	}
	return nil
}

// Rollback rolls back the transaction.
func (tx *Tx) Rollback() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.done {
		return sql.ErrTxDone
	}
	tx.done = true
	if tx.underlying != nil {
		return tx.underlying.Rollback()
	}
	return nil
}

func (tx *Tx) isDone() bool {
	if tx == nil {
		return true
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return tx.done
}

// Prepare creates a prepared statement within the transaction.
func (tx *Tx) Prepare(query string) (*Stmt, error) {
	return tx.PrepareContext(context.Background(), query)
}

// PrepareContext creates a prepared statement with context.
func (tx *Tx) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	if tx.isDone() {
		return nil, sql.ErrTxDone
	}
	if tx == nil || tx.db == nil {
		return nil, sql.ErrConnDone
	}
	if tx.db.GetMode() == sqlcache.ModeOffline || tx.underlying == nil {
		return &Stmt{query: query, db: tx.db, tx: tx}, nil
	}

	var (
		stmt *sql.Stmt
		err  error
	)
	if tx.underlying != nil {
		stmt, err = tx.underlying.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
	}
	return &Stmt{underlying: stmt, query: query, db: tx.db, tx: tx}, nil
}
