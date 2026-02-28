// Package driver provides a database/sql driver wrapper that integrates
// with the SQL cache for transparent caching of database queries.
package driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"

	sqlcache "github.com/asish/sql-cache"
)

var (
	wrappedDrivers = make(map[string]*CachedDriver)
	mu             sync.RWMutex
)

// CachedDriver wraps a database/sql driver with caching capabilities
type CachedDriver struct {
	underlying driver.Driver
	cache      *sqlcache.Cache
	mode       sqlcache.Mode
}

// WrapDriver wraps an existing driver with caching support
func WrapDriver(name string, d driver.Driver, cache *sqlcache.Cache) *CachedDriver {
	mu.Lock()
	defer mu.Unlock()
	
	wrapped := &CachedDriver{
		underlying: d,
		cache:      cache,
		mode:       sqlcache.ModeReplay,
	}
	wrappedDrivers[name] = wrapped
	
	return wrapped
}

// Register registers a cached driver wrapper with the given name
func Register(name, underlyingDriver string, cache *sqlcache.Cache) error {
	mu.Lock()
	defer mu.Unlock()
	
	// Find the underlying driver
	db, err := sql.Open(underlyingDriver, "")
	if err != nil {
		return fmt.Errorf("failed to get underlying driver: %w", err)
	}
	defer db.Close()
	
	wrapped := &CachedDriver{
		underlying: db.Driver(),
		cache:      cache,
		mode:       sqlcache.ModeReplay,
	}
	wrappedDrivers[name] = wrapped
	
	sql.Register(name, wrapped)
	return nil
}

// Open opens a new connection
func (d *CachedDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := d.underlying.Open(dsn)
	if err != nil {
		return nil, err
	}
	
	return &CachedConn{
		underlying: conn,
		cache:      d.cache,
		driver:     d,
	}, nil
}

// SetMode sets the caching mode for this driver
func (d *CachedDriver) SetMode(mode sqlcache.Mode) {
	d.mode = mode
	d.cache.SetMode(mode)
}

// CachedConn wraps a driver.Conn with caching
type CachedConn struct {
	underlying driver.Conn
	cache      *sqlcache.Cache
	driver     *CachedDriver
}

// Prepare prepares a statement
func (c *CachedConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.underlying.Prepare(query)
	if err != nil {
		return nil, err
	}
	
	return &CachedStmt{
		underlying: stmt,
		query:      query,
		conn:       c,
	}, nil
}

// Close closes the connection
func (c *CachedConn) Close() error {
	return c.underlying.Close()
}

// Begin starts a transaction
func (c *CachedConn) Begin() (driver.Tx, error) {
	tx, err := c.underlying.Begin()
	if err != nil {
		return nil, err
	}
	
	return &CachedTx{
		underlying: tx,
		conn:       c,
	}, nil
}

// QueryContext implements driver.QueryerContext
func (c *CachedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	// Convert args
	plainArgs := make([]any, len(args))
	for i, arg := range args {
		plainArgs[i] = arg.Value
	}
	
	mode := c.driver.mode
	
	switch mode {
	case sqlcache.ModePassthrough:
		if queryer, ok := c.underlying.(driver.QueryerContext); ok {
			return queryer.QueryContext(ctx, query, args)
		}
		return nil, fmt.Errorf("underlying driver does not support QueryContext")
		
	case sqlcache.ModeRecord, sqlcache.ModeReplay, sqlcache.ModeReplayFallback:
		rows, err := c.cache.QueryContext(ctx, query, plainArgs...)
		if err != nil {
			return nil, err
		}
		return &CachedDriverRows{cached: rows}, nil
	}
	
	return nil, fmt.Errorf("unknown mode: %v", mode)
}

// ExecContext implements driver.ExecerContext
func (c *CachedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	plainArgs := make([]any, len(args))
	for i, arg := range args {
		plainArgs[i] = arg.Value
	}
	
	mode := c.driver.mode
	
	switch mode {
	case sqlcache.ModePassthrough:
		if execer, ok := c.underlying.(driver.ExecerContext); ok {
			return execer.ExecContext(ctx, query, args)
		}
		return nil, fmt.Errorf("underlying driver does not support ExecContext")
		
	case sqlcache.ModeRecord, sqlcache.ModeReplay, sqlcache.ModeReplayFallback:
		result, err := c.cache.ExecContext(ctx, query, plainArgs...)
		if err != nil {
			return nil, err
		}
		return &CachedDriverResult{cached: result}, nil
	}
	
	return nil, fmt.Errorf("unknown mode: %v", mode)
}

// CachedStmt wraps a driver.Stmt with caching
type CachedStmt struct {
	underlying driver.Stmt
	query      string
	conn       *CachedConn
}

// Close closes the statement
func (s *CachedStmt) Close() error {
	return s.underlying.Close()
}

// NumInput returns the number of placeholder parameters
func (s *CachedStmt) NumInput() int {
	return s.underlying.NumInput()
}

// Exec executes a prepared statement
func (s *CachedStmt) Exec(args []driver.Value) (driver.Result, error) {
	plainArgs := make([]any, len(args))
	for i, arg := range args {
		plainArgs[i] = arg
	}
	
	result, err := s.conn.cache.Exec(s.query, plainArgs...)
	if err != nil {
		return nil, err
	}
	
	return &CachedDriverResult{cached: result}, nil
}

// Query executes a prepared query
func (s *CachedStmt) Query(args []driver.Value) (driver.Rows, error) {
	plainArgs := make([]any, len(args))
	for i, arg := range args {
		plainArgs[i] = arg
	}
	
	rows, err := s.conn.cache.Query(s.query, plainArgs...)
	if err != nil {
		return nil, err
	}
	
	return &CachedDriverRows{cached: rows}, nil
}

// CachedTx wraps a driver.Tx
type CachedTx struct {
	underlying driver.Tx
	conn       *CachedConn
}

// Commit commits the transaction
func (t *CachedTx) Commit() error {
	return t.underlying.Commit()
}

// Rollback rolls back the transaction
func (t *CachedTx) Rollback() error {
	return t.underlying.Rollback()
}

// CachedDriverRows wraps CachedRows to implement driver.Rows
type CachedDriverRows struct {
	cached *sqlcache.CachedRows
}

// Columns returns the column names
func (r *CachedDriverRows) Columns() []string {
	return r.cached.Columns()
}

// Close closes the rows
func (r *CachedDriverRows) Close() error {
	return r.cached.Close()
}

// Next advances to the next row
func (r *CachedDriverRows) Next(dest []driver.Value) error {
	if !r.cached.Next() {
		return fmt.Errorf("no more rows")
	}
	
	// Scan into dest
	dests := make([]interface{}, len(dest))
	for i := range dest {
		dests[i] = &dest[i]
	}
	
	return r.cached.Scan(dests...)
}

// CachedDriverResult wraps CachedResult to implement driver.Result
type CachedDriverResult struct {
	cached *sqlcache.CachedResult
}

// LastInsertId returns the last insert ID
func (r *CachedDriverResult) LastInsertId() (int64, error) {
	return r.cached.LastInsertId()
}

// RowsAffected returns the number of rows affected
func (r *CachedDriverResult) RowsAffected() (int64, error) {
	return r.cached.RowsAffected()
}

// GetDriver returns a registered cached driver by name
func GetDriver(name string) *CachedDriver {
	mu.RLock()
	defer mu.RUnlock()
	return wrappedDrivers[name]
}
