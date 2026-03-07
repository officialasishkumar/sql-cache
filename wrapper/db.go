package wrapper

import (
	"context"
	"database/sql"

	sqlcache "github.com/officialasishkumar/sql-cache"
)

// SetMode sets the caching mode.
func (db *DB) SetMode(mode sqlcache.Mode) { db.cache.SetMode(mode) }

// GetMode returns the current caching mode.
func (db *DB) GetMode() sqlcache.Mode { return db.cache.GetMode() }

// Query executes a query that returns rows.
func (db *DB) Query(query string, args ...interface{}) (*Rows, error) {
	return db.QueryContext(context.Background(), query, args...)
}

// QueryContext executes a query with context.
func (db *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	if db.isClosed() {
		return nil, sql.ErrConnDone
	}
	cachedRows, err := db.cache.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{cached: cachedRows}, nil
}

// QueryRow executes a query that is expected to return at most one row.
func (db *DB) QueryRow(query string, args ...interface{}) *Row {
	return db.QueryRowContext(context.Background(), query, args...)
}

// QueryRowContext executes a query with context expecting at most one row.
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := db.QueryContext(ctx, query, args...)
	return &Row{rows: rows, err: err}
}

// Exec executes a query without returning any rows.
func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.ExecContext(context.Background(), query, args...)
}

// ExecContext executes a query with context.
func (db *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if db.isClosed() {
		return nil, sql.ErrConnDone
	}
	return db.cache.ExecContext(ctx, query, args...)
}

// Prepare creates a prepared statement for later queries.
func (db *DB) Prepare(query string) (*Stmt, error) {
	return db.PrepareContext(context.Background(), query)
}

// PrepareContext creates a prepared statement with context.
func (db *DB) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	if db.isClosed() {
		return nil, sql.ErrConnDone
	}

	var (
		stmt *sql.Stmt
		err  error
	)
	if db.underlying != nil {
		stmt, err = db.underlying.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
	}

	return &Stmt{underlying: stmt, query: query, db: db}, nil
}

// Begin starts a transaction.
func (db *DB) Begin() (*Tx, error) { return db.BeginTx(context.Background(), nil) }

// BeginTx starts a transaction with the provided context and options.
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	if db.isClosed() {
		return nil, sql.ErrConnDone
	}

	var (
		tx  *sql.Tx
		err error
	)
	if db.underlying != nil {
		tx, err = db.underlying.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
	}

	return &Tx{underlying: tx, db: db}, nil
}

// Ping verifies a connection to the database.
func (db *DB) Ping() error {
	if db.underlying == nil {
		return nil
	}
	return db.underlying.Ping()
}

// PingContext pings with context.
func (db *DB) PingContext(ctx context.Context) error {
	if db.underlying == nil {
		return nil
	}
	return db.underlying.PingContext(ctx)
}

// Close closes the database and saves the cache.
func (db *DB) Close() error {
	db.mu.Lock()
	if db.closed {
		db.mu.Unlock()
		return nil
	}
	db.closed = true
	db.mu.Unlock()

	if err := db.cache.Close(); err != nil {
		if db.underlying != nil {
			_ = db.underlying.Close()
		}
		return err
	}
	if db.underlying != nil {
		return db.underlying.Close()
	}
	return nil
}

func (db *DB) isClosed() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.closed
}

// Underlying returns the underlying *sql.DB.
func (db *DB) Underlying() *sql.DB { return db.underlying }

// Cache returns the underlying cache.
func (db *DB) Cache() *sqlcache.Cache { return db.cache }

// Invalidate removes a query from the cache.
func (db *DB) Invalidate(query string) error {
	db.cache.InvalidateByQuery(query)
	return nil
}

// Clear clears all cache entries.
func (db *DB) Clear() error { return db.cache.Clear() }

// Reset resets cache entry consumption state.
func (db *DB) Reset() { db.cache.Reset() }

// Save persists the cache to disk.
func (db *DB) Save() error { return db.cache.Save() }

// Load loads the cache from disk.
func (db *DB) Load() error { return db.cache.Load() }

// Stats returns cache statistics.
func (db *DB) Stats() sqlcache.CacheStats { return db.cache.Stats() }

// Populate manually stores a query with its response in the cache.
func (db *DB) Populate(query string, columns []string, rows [][]interface{}, args ...interface{}) error {
	return db.cache.Populate(query, columns, rows, args...)
}

// PopulateExec manually stores an exec result in the cache.
func (db *DB) PopulateExec(query string, lastInsertID, rowsAffected int64, args ...interface{}) error {
	return db.cache.PopulateExec(query, lastInsertID, rowsAffected, args...)
}

// PopulateError stores an error response in the cache.
func (db *DB) PopulateError(query string, errMsg string, args ...interface{}) error {
	return db.cache.PopulateError(query, errMsg, args...)
}
