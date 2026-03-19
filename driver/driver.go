// Package driver provides a database/sql driver wrapper that integrates
// with the SQL cache for transparent caching of database queries.
package driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	sqlcache "github.com/officialasishkumar/sql-cache"
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
		mode:       sqlcache.ModeAuto,
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
		mode:       sqlcache.ModeAuto,
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

// QueryContext implements driver.QueryerContext.
// Delegates to the cache which handles both ModeAuto and ModeOffline.
func (c *CachedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c == nil || c.cache == nil {
		return nil, driver.ErrBadConn
	}
	plainArgs := namedValuesToInterfaces(args)
	if c.cache.GetMode() == sqlcache.ModeOffline {
		rows, err := c.cache.QueryContext(ctx, query, plainArgs...)
		if err != nil {
			return nil, err
		}
		return &CachedDriverRows{cached: rows}, nil
	}

	rows, found, err := c.cache.LookupQuery(query, plainArgs...)
	if err != nil {
		return nil, err
	}
	if found {
		return &CachedDriverRows{cached: rows}, nil
	}

	c.cache.NotifyDatabaseHit(query, plainArgs...)
	liveRows, err := c.queryUnderlyingContext(ctx, query, args)
	if err != nil {
		c.cache.CaptureError(query, err, plainArgs...)
		return nil, err
	}

	columns, capturedRows, err := readDriverRows(liveRows)
	if err != nil {
		c.cache.CaptureError(query, err, plainArgs...)
		return nil, err
	}

	c.cache.CaptureQuery(query, columns, capturedRows, plainArgs...)
	return &CachedDriverRows{cached: sqlcache.NewCachedRows(columns, capturedRows)}, nil
}

// ExecContext implements driver.ExecerContext.
// Delegates to the cache which handles both ModeAuto and ModeOffline.
func (c *CachedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c == nil || c.cache == nil {
		return nil, driver.ErrBadConn
	}
	plainArgs := namedValuesToInterfaces(args)
	if c.cache.GetMode() == sqlcache.ModeOffline {
		result, err := c.cache.ExecContext(ctx, query, plainArgs...)
		if err != nil {
			return nil, err
		}
		return &CachedDriverResult{cached: result}, nil
	}

	result, found, err := c.cache.LookupExec(query, plainArgs...)
	if err != nil {
		return nil, err
	}
	if found {
		return &CachedDriverResult{cached: result}, nil
	}

	c.cache.NotifyDatabaseHit(query, plainArgs...)
	liveResult, err := c.execUnderlyingContext(ctx, query, args)
	if err != nil {
		c.cache.CaptureError(query, err, plainArgs...)
		return nil, err
	}

	lastInsertID, _ := liveResult.LastInsertId()
	rowsAffected, _ := liveResult.RowsAffected()
	c.cache.CaptureExec(query, lastInsertID, rowsAffected, plainArgs...)
	return &CachedDriverResult{cached: sqlcache.NewCachedResult(lastInsertID, rowsAffected)}, nil
}

// CachedStmt wraps a driver.Stmt with caching
type CachedStmt struct {
	underlying driver.Stmt
	query      string
	conn       *CachedConn
}

// Close closes the statement
func (s *CachedStmt) Close() error {
	if s == nil || s.underlying == nil {
		return nil
	}
	return s.underlying.Close()
}

// NumInput returns the number of placeholder parameters
func (s *CachedStmt) NumInput() int {
	if s == nil || s.underlying == nil {
		return -1
	}
	return s.underlying.NumInput()
}

// Exec executes a prepared statement
func (s *CachedStmt) Exec(args []driver.Value) (driver.Result, error) {
	if s == nil || s.conn == nil || s.conn.cache == nil {
		return nil, driver.ErrBadConn
	}
	plainArgs := valuesToInterfaces(args)
	if s.conn.cache.GetMode() == sqlcache.ModeOffline {
		result, err := s.conn.cache.Exec(s.query, plainArgs...)
		if err != nil {
			return nil, err
		}
		return &CachedDriverResult{cached: result}, nil
	}

	result, found, err := s.conn.cache.LookupExec(s.query, plainArgs...)
	if err != nil {
		return nil, err
	}
	if found {
		return &CachedDriverResult{cached: result}, nil
	}

	s.conn.cache.NotifyDatabaseHit(s.query, plainArgs...)
	liveResult, err := s.underlying.Exec(args)
	if err != nil {
		s.conn.cache.CaptureError(s.query, err, plainArgs...)
		return nil, err
	}

	lastInsertID, _ := liveResult.LastInsertId()
	rowsAffected, _ := liveResult.RowsAffected()
	s.conn.cache.CaptureExec(s.query, lastInsertID, rowsAffected, plainArgs...)
	return &CachedDriverResult{cached: sqlcache.NewCachedResult(lastInsertID, rowsAffected)}, nil
}

// Query executes a prepared query
func (s *CachedStmt) Query(args []driver.Value) (driver.Rows, error) {
	if s == nil || s.conn == nil || s.conn.cache == nil {
		return nil, driver.ErrBadConn
	}
	plainArgs := valuesToInterfaces(args)
	if s.conn.cache.GetMode() == sqlcache.ModeOffline {
		rows, err := s.conn.cache.Query(s.query, plainArgs...)
		if err != nil {
			return nil, err
		}
		return &CachedDriverRows{cached: rows}, nil
	}

	rows, found, err := s.conn.cache.LookupQuery(s.query, plainArgs...)
	if err != nil {
		return nil, err
	}
	if found {
		return &CachedDriverRows{cached: rows}, nil
	}

	s.conn.cache.NotifyDatabaseHit(s.query, plainArgs...)
	liveRows, err := s.underlying.Query(args)
	if err != nil {
		s.conn.cache.CaptureError(s.query, err, plainArgs...)
		return nil, err
	}

	columns, capturedRows, err := readDriverRows(liveRows)
	if err != nil {
		s.conn.cache.CaptureError(s.query, err, plainArgs...)
		return nil, err
	}

	s.conn.cache.CaptureQuery(s.query, columns, capturedRows, plainArgs...)
	return &CachedDriverRows{cached: sqlcache.NewCachedRows(columns, capturedRows)}, nil
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
	if r == nil || r.cached == nil {
		return nil
	}
	return r.cached.Columns()
}

// Close closes the rows
func (r *CachedDriverRows) Close() error {
	if r == nil || r.cached == nil {
		return nil
	}
	return r.cached.Close()
}

// Next advances to the next row
func (r *CachedDriverRows) Next(dest []driver.Value) error {
	if r == nil || r.cached == nil {
		return io.EOF
	}
	if !r.cached.Next() {
		return io.EOF
	}

	values := make([]interface{}, len(dest))
	dests := make([]interface{}, len(dest))
	for i := range dest {
		dests[i] = &values[i]
	}

	if err := r.cached.Scan(dests...); err != nil {
		return err
	}
	for i, value := range values {
		driverValue, err := toDriverValue(value)
		if err != nil {
			return err
		}
		dest[i] = driverValue
	}
	return nil
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

func (c *CachedConn) queryUnderlyingContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.underlying.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, query, args)
	}

	stmt, err := c.underlying.Prepare(query)
	if err != nil {
		return nil, err
	}

	rows, err := stmt.Query(namedValuesToValues(args))
	if err != nil {
		_ = stmt.Close()
		return nil, err
	}
	return &stmtBackedRows{Rows: rows, stmt: stmt}, nil
}

func (c *CachedConn) execUnderlyingContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := c.underlying.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, query, args)
	}

	stmt, err := c.underlying.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	return stmt.Exec(namedValuesToValues(args))
}

type stmtBackedRows struct {
	driver.Rows
	stmt driver.Stmt
}

func (r *stmtBackedRows) Close() error {
	var closeErr error
	if r.Rows != nil {
		closeErr = r.Rows.Close()
	}
	if r.stmt != nil {
		if err := r.stmt.Close(); closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func readDriverRows(rows driver.Rows) ([]string, [][]interface{}, error) {
	if rows == nil {
		return nil, nil, nil
	}
	defer rows.Close()

	columns := rows.Columns()
	capturedRows := make([][]interface{}, 0)

	for {
		values := make([]driver.Value, len(columns))
		err := rows.Next(values)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}

		row := make([]interface{}, len(values))
		for i, value := range values {
			row[i] = copyDriverValue(value)
		}
		capturedRows = append(capturedRows, row)
	}

	return columns, capturedRows, nil
}

func copyDriverValue(value driver.Value) interface{} {
	switch v := value.(type) {
	case []byte:
		cp := make([]byte, len(v))
		copy(cp, v)
		return cp
	default:
		return v
	}
}

func toDriverValue(value interface{}) (driver.Value, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return nil, fmt.Errorf("uint value overflows int64: %d", v)
		}
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return nil, fmt.Errorf("uint64 value overflows int64: %d", v)
		}
		return int64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case bool:
		return v, nil
	case string:
		return v, nil
	case []byte:
		cp := make([]byte, len(v))
		copy(cp, v)
		return cp, nil
	case time.Time:
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported driver value type: %T", value)
	}
}

func namedValuesToInterfaces(args []driver.NamedValue) []interface{} {
	plainArgs := make([]interface{}, len(args))
	for i, arg := range args {
		plainArgs[i] = arg.Value
	}
	return plainArgs
}

func namedValuesToValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	return values
}

func valuesToInterfaces(args []driver.Value) []interface{} {
	plainArgs := make([]interface{}, len(args))
	for i, arg := range args {
		plainArgs[i] = arg
	}
	return plainArgs
}
