// Package wrapper provides an easy-to-use wrapper around *sql.DB that
// intercepts queries and provides SQL response caching.
// This allows applications to capture database interactions and serve them
// from cache without needing an actual database connection.
package wrapper

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	sqlcache "github.com/officialasishkumar/sql-cache"
)

// DB wraps a *sql.DB with caching functionality
type DB struct {
	underlying *sql.DB
	cache      *sqlcache.Cache
	mu         sync.RWMutex
	closed     bool
}

// Options configures the cached DB wrapper
type Options struct {
	// MockDir is the directory for mock YAML files
	MockDir string

	// InitialMode is the initial caching mode
	InitialMode sqlcache.Mode

	// SequentialMode - if true, cache entries are consumed in order
	SequentialMode bool

	// OnCapture is called when a query response is captured
	OnCapture func(query string, args []interface{})

	// OnCacheHit is called when a query is served from cache
	OnCacheHit func(query string, args []interface{}, matched bool)

	// OnError is called on errors
	OnError func(err error, context string)
}

// Wrap wraps an existing *sql.DB with caching support
func Wrap(db *sql.DB, opts Options) (*DB, error) {
	cacheOpts := sqlcache.Options{
		MockDir:        opts.MockDir,
		DB:             db,
		OnCapture:      opts.OnCapture,
		OnCacheHit:     opts.OnCacheHit,
		OnError:        opts.OnError,
		SequentialMode: opts.SequentialMode,
	}

	cache, err := sqlcache.New(cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	if opts.InitialMode != 0 {
		cache.SetMode(opts.InitialMode)
	}

	return &DB{
		underlying: db,
		cache:      cache,
	}, nil
}

// NewCachedOnly creates a wrapper for cached-only mode (no database needed)
func NewCachedOnly(opts Options) (*DB, error) {
	cacheOpts := sqlcache.Options{
		MockDir:        opts.MockDir,
		OnCacheHit:     opts.OnCacheHit,
		OnError:        opts.OnError,
		SequentialMode: opts.SequentialMode,
	}

	cache, err := sqlcache.New(cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	cache.SetMode(sqlcache.ModeCached)

	return &DB{
		underlying: nil,
		cache:      cache,
	}, nil
}

// SetMode sets the caching mode
func (db *DB) SetMode(mode sqlcache.Mode) {
	db.cache.SetMode(mode)
}

// GetMode returns the current caching mode
func (db *DB) GetMode() sqlcache.Mode {
	return db.cache.GetMode()
}

// Query executes a query that returns rows
func (db *DB) Query(query string, args ...interface{}) (*Rows, error) {
	return db.QueryContext(context.Background(), query, args...)
}

// QueryContext executes a query with context
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

// QueryRow executes a query that is expected to return at most one row
func (db *DB) QueryRow(query string, args ...interface{}) *Row {
	return db.QueryRowContext(context.Background(), query, args...)
}

// QueryRowContext executes a query with context expecting at most one row
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := db.QueryContext(ctx, query, args...)
	return &Row{rows: rows, err: err}
}

// Exec executes a query without returning any rows
func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.ExecContext(context.Background(), query, args...)
}

// ExecContext executes a query with context
func (db *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if db.isClosed() {
		return nil, sql.ErrConnDone
	}

	return db.cache.ExecContext(ctx, query, args...)
}

// Prepare creates a prepared statement for later queries
func (db *DB) Prepare(query string) (*Stmt, error) {
	return db.PrepareContext(context.Background(), query)
}

// PrepareContext creates a prepared statement with context
func (db *DB) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	if db.isClosed() {
		return nil, sql.ErrConnDone
	}

	var stmt *sql.Stmt
	var err error

	if db.underlying != nil {
		stmt, err = db.underlying.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
	}

	return &Stmt{
		underlying: stmt,
		query:      query,
		db:         db,
	}, nil
}

// Begin starts a transaction
func (db *DB) Begin() (*Tx, error) {
	return db.BeginTx(context.Background(), nil)
}

// BeginTx starts a transaction with the provided context and options
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	if db.isClosed() {
		return nil, sql.ErrConnDone
	}

	var tx *sql.Tx
	var err error

	if db.underlying != nil {
		tx, err = db.underlying.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
	}

	return &Tx{
		underlying: tx,
		db:         db,
	}, nil
}

// Ping verifies a connection to the database
func (db *DB) Ping() error {
	if db.underlying == nil {
		return nil // No underlying DB in cached-only mode
	}
	return db.underlying.Ping()
}

// PingContext pings with context
func (db *DB) PingContext(ctx context.Context) error {
	if db.underlying == nil {
		return nil
	}
	return db.underlying.PingContext(ctx)
}

// Close closes the database and saves the cache
func (db *DB) Close() error {
	db.mu.Lock()
	if db.closed {
		db.mu.Unlock()
		return nil
	}
	db.closed = true
	db.mu.Unlock()

	// Save mocks first
	if err := db.cache.Close(); err != nil {
		// Log but continue to close underlying
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

// Underlying returns the underlying *sql.DB (may be nil in cached-only mode)
func (db *DB) Underlying() *sql.DB {
	return db.underlying
}

// Cache returns the underlying cache
func (db *DB) Cache() *sqlcache.Cache {
	return db.cache
}

// Invalidate removes a query from the cache
func (db *DB) Invalidate(query string) error {
	// Not applicable with new mock storage
	return nil
}

// Clear clears all cache entries
func (db *DB) Clear() error {
	return db.cache.Clear()
}

// Reset resets cache entry consumption state
func (db *DB) Reset() {
	db.cache.Reset()
}

// Save persists the cache to disk
func (db *DB) Save() error {
	return db.cache.Save()
}

// Load loads the cache from disk
func (db *DB) Load() error {
	return db.cache.Load()
}

// Stats returns cache statistics
func (db *DB) Stats() sqlcache.CacheStats {
	return db.cache.Stats()
}

// Capture manually stores a query with its response in the cache
func (db *DB) Capture(query string, columns []string, rows [][]interface{}, args ...interface{}) error {
	return db.cache.Capture(query, columns, rows, args...)
}

// CaptureExec manually stores an exec result in the cache
func (db *DB) CaptureExec(query string, lastInsertID, rowsAffected int64, args ...interface{}) error {
	return db.cache.CaptureExec(query, lastInsertID, rowsAffected, args...)
}

// CaptureError stores an error response in the cache
func (db *DB) CaptureError(query string, errMsg string, args ...interface{}) error {
	return db.cache.CaptureError(query, errMsg, args...)
}

// =============================================================================
// Rows wraps CachedRows
// =============================================================================

// Rows wraps CachedRows for compatibility with sql.Rows interface
type Rows struct {
	cached *sqlcache.CachedRows
}

// Columns returns column names
func (r *Rows) Columns() ([]string, error) {
	if r == nil || r.cached == nil {
		return nil, sql.ErrNoRows
	}
	return r.cached.Columns(), nil
}

// Next advances to next row
func (r *Rows) Next() bool {
	if r == nil || r.cached == nil {
		return false
	}
	return r.cached.Next()
}

// Scan copies column values into dest
func (r *Rows) Scan(dest ...interface{}) error {
	if r == nil || r.cached == nil {
		return sql.ErrNoRows
	}
	return r.cached.Scan(dest...)
}

// Close closes the rows
func (r *Rows) Close() error {
	if r == nil || r.cached == nil {
		return nil
	}
	return r.cached.Close()
}

// Err returns any error
func (r *Rows) Err() error {
	if r == nil || r.cached == nil {
		return nil
	}
	return r.cached.Err()
}

// =============================================================================
// Row wraps a single row result
// =============================================================================

// Row wraps a single row result
type Row struct {
	rows *Rows
	err  error
}

// Scan copies columns into dest
func (r *Row) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	if r.rows == nil {
		return sql.ErrNoRows
	}
	if !r.rows.Next() {
		err := r.rows.Err()
		if err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return r.rows.Scan(dest...)
}

// Err returns any error
func (r *Row) Err() error {
	return r.err
}

// =============================================================================
// Stmt wraps a prepared statement
// =============================================================================

// Stmt wraps a prepared statement with caching
type Stmt struct {
	underlying *sql.Stmt
	query      string
	db         *DB
}

// Query executes the prepared query
func (s *Stmt) Query(args ...interface{}) (*Rows, error) {
	return s.QueryContext(context.Background(), args...)
}

// QueryContext executes the prepared query with context
func (s *Stmt) QueryContext(ctx context.Context, args ...interface{}) (*Rows, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrConnDone
	}
	return s.db.QueryContext(ctx, s.query, args...)
}

// QueryRow executes expecting one row
func (s *Stmt) QueryRow(args ...interface{}) *Row {
	return s.QueryRowContext(context.Background(), args...)
}

// QueryRowContext executes with context expecting one row
func (s *Stmt) QueryRowContext(ctx context.Context, args ...interface{}) *Row {
	if s == nil || s.db == nil {
		return &Row{err: sql.ErrConnDone}
	}
	return s.db.QueryRowContext(ctx, s.query, args...)
}

// Exec executes the prepared statement
func (s *Stmt) Exec(args ...interface{}) (sql.Result, error) {
	return s.ExecContext(context.Background(), args...)
}

// ExecContext executes with context
func (s *Stmt) ExecContext(ctx context.Context, args ...interface{}) (sql.Result, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrConnDone
	}
	return s.db.ExecContext(ctx, s.query, args...)
}

// Close closes the statement
func (s *Stmt) Close() error {
	if s == nil {
		return nil
	}
	if s.underlying != nil {
		return s.underlying.Close()
	}
	return nil
}

// =============================================================================
// Tx wraps a transaction
// =============================================================================

// Tx wraps a transaction with caching
type Tx struct {
	underlying *sql.Tx
	db         *DB
	done       bool
	mu         sync.Mutex
}

// Query executes a query
func (tx *Tx) Query(query string, args ...interface{}) (*Rows, error) {
	return tx.QueryContext(context.Background(), query, args...)
}

// QueryContext executes a query with context
func (tx *Tx) QueryContext(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	if tx.isDone() {
		return nil, sql.ErrTxDone
	}
	return tx.db.QueryContext(ctx, query, args...)
}

// QueryRow executes expecting one row
func (tx *Tx) QueryRow(query string, args ...interface{}) *Row {
	return tx.QueryRowContext(context.Background(), query, args...)
}

// QueryRowContext executes with context expecting one row
func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...interface{}) *Row {
	if tx.isDone() {
		return &Row{err: sql.ErrTxDone}
	}
	return tx.db.QueryRowContext(ctx, query, args...)
}

// Exec executes a query
func (tx *Tx) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.ExecContext(context.Background(), query, args...)
}

// ExecContext executes with context
func (tx *Tx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if tx.isDone() {
		return nil, sql.ErrTxDone
	}
	return tx.db.ExecContext(ctx, query, args...)
}

// Commit commits the transaction
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

// Rollback rolls back the transaction
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
	tx.mu.Lock()
	defer tx.mu.Unlock()
	return tx.done
}

// Prepare creates a prepared statement within the transaction
func (tx *Tx) Prepare(query string) (*Stmt, error) {
	return tx.PrepareContext(context.Background(), query)
}

// PrepareContext creates a prepared statement with context
func (tx *Tx) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	if tx.isDone() {
		return nil, sql.ErrTxDone
	}

	var stmt *sql.Stmt
	var err error

	if tx.underlying != nil {
		stmt, err = tx.underlying.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
	}

	return &Stmt{
		underlying: stmt,
		query:      query,
		db:         tx.db,
	}, nil
}
