// Package sqlcache provides a robust SQL query caching layer that intercepts SQL calls,
// stores responses in YAML files, and returns cached responses for matching queries.
//
// The library supports:
// - Capturing SQL queries and responses to YAML cache files
// - Returning cached responses with structural matching
// - Sequential cache consumption for predictable ordering
// - Multiple match levels (exact, structural, type-based)
// - Robust error handling that never crashes
//
// Basic usage:
//
//	cache, _ := sqlcache.New(sqlcache.Options{
//	    MockDir: "./mocks",
//	})
//	defer cache.Close()
//
//	// Capture mode: execute queries and store responses
//	cache.SetMode(sqlcache.ModeCapture)
//	rows, _ := cache.Query("SELECT * FROM users WHERE id = ?", 1)
//
//	// Cached mode: return stored responses
//	cache.SetMode(sqlcache.ModeCached)
//	rows, _ := cache.Query("SELECT * FROM users WHERE id = ?", 1) // Returns from cache
package sqlcache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/officialasishkumar/sql-cache/matcher"
	"github.com/officialasishkumar/sql-cache/mock"
)

// Mode determines how the cache behaves
type Mode int

const (
	// ModePassthrough - execute queries against DB without caching
	ModePassthrough Mode = iota

	// ModeCapture - execute queries and save responses as cache entries
	ModeCapture

	// ModeCached - return cached responses, error if not found
	ModeCached

	// ModeCacheFallback - return cached responses, fall back to DB if not found
	ModeCacheFallback
)

func (m Mode) String() string {
	switch m {
	case ModePassthrough:
		return "passthrough"
	case ModeCapture:
		return "capture"
	case ModeCached:
		return "cached"
	case ModeCacheFallback:
		return "cache-fallback"
	default:
		return "unknown"
	}
}

// Options configures the SQL cache
type Options struct {
	// MockDir is the directory to store mock YAML files
	MockDir string

	// DB is the underlying database connection (optional for pure replay)
	DB *sql.DB

	// OnCapture is called when a query response is captured
	OnCapture func(query string, args []interface{})

	// OnCacheHit is called when a query is served from cache
	OnCacheHit func(query string, args []interface{}, matched bool)

	// OnError is called when an error occurs (for logging)
	OnError func(err error, context string)

	// Logger for debug output (optional)
	Logger *log.Logger

	// SequentialMode - if true, cache entries are consumed in order and can only be used once
	// This enables predictable sequential consumption behavior
	SequentialMode bool
}

// Cache is the main SQL caching interface
type Cache struct {
	mu      sync.RWMutex
	mode    Mode
	options Options
	matcher *matcher.Matcher
	mocks   *mock.MockStore
	db      *sql.DB

	// Statistics
	stats CacheStats
}

// CacheStats contains runtime statistics
type CacheStats struct {
	Mode         string `json:"mode"`
	TotalMocks   int    `json:"total_mocks"`
	Hits         int64  `json:"hits"`
	Misses       int64  `json:"misses"`
	Errors       int64  `json:"errors"`
	Captured     int64  `json:"captured"`
	HitRate      float64 `json:"hit_rate"`
}

// Errors
var (
	ErrMockNotFound   = errors.New("no matching mock found")
	ErrNoDatabase     = errors.New("no database connection configured")
	ErrQueryFailed    = errors.New("query execution failed")
	ErrInvalidMode    = errors.New("invalid mode for this operation")
	ErrParseError     = errors.New("failed to parse query")
)

// New creates a new SQL cache instance
func New(opts Options) (*Cache, error) {
	m, err := matcher.NewMatcher()
	if err != nil {
		// Even if matcher fails, create a degraded cache
		if opts.OnError != nil {
			opts.OnError(err, "creating matcher")
		}
	}

	mockDir := opts.MockDir
	if mockDir == "" {
		mockDir = "./mocks" // Default mock directory
	}

	mockStore := mock.NewMockStore(mockDir)

	c := &Cache{
		mode:    ModePassthrough,
		options: opts,
		matcher: m,
		mocks:   mockStore,
		db:      opts.DB,
	}

	// Try to load existing mocks
	if err := mockStore.Load(); err != nil {
		c.logError(err, "loading mocks")
		// Don't fail - just continue without cached mocks
	}

	return c, nil
}

// SetDB sets the database connection
func (c *Cache) SetDB(db *sql.DB) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.db = db
}

// SetMode sets the caching mode
func (c *Cache) SetMode(mode Mode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = mode

	// Reset cache consumption state when switching to cached mode
	if mode == ModeCached || mode == ModeCacheFallback {
		c.mocks.Reset()
	}
}

// GetMode returns the current caching mode
func (c *Cache) GetMode() Mode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode
}

// Query executes a query with caching support
func (c *Cache) Query(query string, args ...interface{}) (*CachedRows, error) {
	return c.QueryContext(context.Background(), query, args...)
}

// QueryContext executes a query with context and caching support
func (c *Cache) QueryContext(ctx context.Context, query string, args ...interface{}) (*CachedRows, error) {
	c.mu.RLock()
	mode := c.mode
	db := c.db
	c.mu.RUnlock()

	// Handle context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch mode {
	case ModePassthrough:
		return c.executeQuery(ctx, db, query, args)

	case ModeCapture:
		return c.captureQuery(ctx, db, query, args)

	case ModeCached:
		return c.cachedQuery(ctx, nil, query, args, true)

	case ModeCacheFallback:
		return c.cachedQuery(ctx, db, query, args, false)
	}

	return nil, ErrInvalidMode
}

// Exec executes a non-SELECT statement with caching support
func (c *Cache) Exec(query string, args ...interface{}) (*CachedResult, error) {
	return c.ExecContext(context.Background(), query, args...)
}

// ExecContext executes a non-SELECT statement with context
func (c *Cache) ExecContext(ctx context.Context, query string, args ...interface{}) (*CachedResult, error) {
	c.mu.RLock()
	mode := c.mode
	db := c.db
	c.mu.RUnlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch mode {
	case ModePassthrough:
		return c.executeExec(ctx, db, query, args)

	case ModeCapture:
		return c.captureExec(ctx, db, query, args)

	case ModeCached:
		return c.cachedExec(ctx, nil, query, args, true)

	case ModeCacheFallback:
		return c.cachedExec(ctx, db, query, args, false)
	}

	return nil, ErrInvalidMode
}

// =============================================================================
// Capture Mode Implementation
// =============================================================================

func (c *Cache) captureQuery(ctx context.Context, db *sql.DB, query string, args []interface{}) (*CachedRows, error) {
	if db == nil {
		return nil, ErrNoDatabase
	}

	// Execute the actual query
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.incrementErrors()
		// Capture the error response
		c.captureMock(query, args, nil, nil, 0, 0, err.Error())
		return nil, err
	}

	// Convert to cached rows
	cachedRows, err := NewCachedRowsFromSQL(rows)
	if err != nil {
		c.incrementErrors()
		return nil, err
	}

	// Capture the successful response
	c.captureMock(query, args, cachedRows.columns, cachedRows.rows, 0, 0, "")

	c.mu.Lock()
	c.stats.Captured++
	c.mu.Unlock()

	if c.options.OnCapture != nil {
		c.options.OnCapture(query, args)
	}

	return cachedRows, nil
}

func (c *Cache) captureExec(ctx context.Context, db *sql.DB, query string, args []interface{}) (*CachedResult, error) {
	if db == nil {
		return nil, ErrNoDatabase
	}

	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		c.incrementErrors()
		// Capture error
		c.captureMock(query, args, nil, nil, 0, 0, err.Error())
		return nil, err
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()

	// Capture success
	c.captureMock(query, args, nil, nil, lastID, affected, "")

	c.mu.Lock()
	c.stats.Captured++
	c.mu.Unlock()

	if c.options.OnCapture != nil {
		c.options.OnCapture(query, args)
	}

	return &CachedResult{lastInsertID: lastID, rowsAffected: affected}, nil
}

func (c *Cache) captureMock(query string, args []interface{}, columns []string, rows [][]interface{}, lastInsertID, rowsAffected int64, errMsg string) {
	// Get query structure for matching
	var structure, queryType string
	var tables []string
	var placeholderCount int
	var isDML bool
	var queryHash string

	if c.matcher != nil {
		structure, _ = c.matcher.GetStructure(query)
		queryType = c.getQueryType(query)
		isDML = c.matcher.IsDML(query)

		// Calculate query hash for fast exact matching
		queryHash = c.matcher.GetHash(query)
	}

	// Count placeholders (critical for prepared statement matching)
	placeholderCount = strings.Count(query, "?")

	// Calculate row count for response metadata
	rowCount := 0
	if rows != nil {
		rowCount = len(rows)
	}

	now := time.Now()
	m := &mock.Mock{
		Version: mock.Version,
		Kind:    "SQL",
		Name:    fmt.Sprintf("mock-%d", now.UnixNano()),
		Spec: mock.MockSpec{
			Metadata: map[string]string{
				"recorded_at": now.Format(time.RFC3339),
			},
			Request: mock.RequestSpec{
				Query:            query,
				Args:             args,
				Type:             queryType,
				Tables:           tables,
				Structure:        structure,
				PlaceholderCount: placeholderCount,
				IsDML:            isDML,
				QueryHash:        queryHash,
			},
			Response: mock.ResponseSpec{
				Columns:      columns,
				Rows:         rows,
				LastInsertID: lastInsertID,
				RowsAffected: rowsAffected,
				Error:        errMsg,
				RowCount:     rowCount,
			},
			Created:          now.Unix(),
			ReqTimestampMock: now,
			ResTimestampMock: now,
		},
	}

	if err := c.mocks.Add(m); err != nil {
		c.logError(err, "adding cache entry")
	}
}

// =============================================================================
// Cached Mode Implementation
// =============================================================================

func (c *Cache) cachedQuery(ctx context.Context, db *sql.DB, query string, args []interface{}, strict bool) (*CachedRows, error) {
	// Check for control statements that don't need mocking
	if matcher.IsControlStatement(query) {
		return &CachedRows{columns: []string{}, rows: [][]interface{}{}, rowIndex: -1}, nil
	}

	// Get query structure for matching
	var structure, queryType string
	if c.matcher != nil {
		structure, _ = c.matcher.GetStructure(query)
		queryType = c.getQueryType(query)
	}

	// Find matching cache entry (with optional consumption for sequential mode)
	matched, found := c.mocks.FindMatch(query, queryType, structure, args, c.options.SequentialMode)

	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, found)
	}

	if found {
		c.incrementHits()

		// Check if cache entry contains an error
		if matched.Spec.Response.Error != "" {
			return nil, errors.New(matched.Spec.Response.Error)
		}

		return &CachedRows{
			columns:  matched.Spec.Response.Columns,
			rows:     matched.Spec.Response.Rows,
			rowIndex: -1,
		}, nil
	}

	// Cache entry not found
	c.incrementMisses()

	if strict {
		return nil, fmt.Errorf("%w: query=%q", ErrMockNotFound, truncateQuery(query))
	}

	// Fallback to database
	if db == nil {
		return nil, ErrMockNotFound
	}

	c.logDebug("falling back to database for: %s", truncateQuery(query))
	return c.executeQuery(ctx, db, query, args)
}

func (c *Cache) cachedExec(ctx context.Context, db *sql.DB, query string, args []interface{}, strict bool) (*CachedResult, error) {
	// Control statements - return success
	if matcher.IsControlStatement(query) {
		return &CachedResult{lastInsertID: 0, rowsAffected: 0}, nil
	}

	var structure, queryType string
	if c.matcher != nil {
		structure, _ = c.matcher.GetStructure(query)
		queryType = c.getQueryType(query)
	}

	matched, found := c.mocks.FindMatch(query, queryType, structure, args, c.options.SequentialMode)

	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, found)
	}

	if found {
		c.incrementHits()

		if matched.Spec.Response.Error != "" {
			return nil, errors.New(matched.Spec.Response.Error)
		}

		return &CachedResult{
			lastInsertID: matched.Spec.Response.LastInsertID,
			rowsAffected: matched.Spec.Response.RowsAffected,
		}, nil
	}

	c.incrementMisses()

	if strict {
		return nil, fmt.Errorf("%w: query=%q", ErrMockNotFound, truncateQuery(query))
	}

	if db == nil {
		return nil, ErrMockNotFound
	}

	c.logDebug("falling back to database for: %s", truncateQuery(query))
	return c.executeExec(ctx, db, query, args)
}

// =============================================================================
// Direct Execution (Passthrough)
// =============================================================================

func (c *Cache) executeQuery(ctx context.Context, db *sql.DB, query string, args []interface{}) (*CachedRows, error) {
	if db == nil {
		return nil, ErrNoDatabase
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.incrementErrors()
		return nil, err
	}

	return NewCachedRowsFromSQL(rows)
}

func (c *Cache) executeExec(ctx context.Context, db *sql.DB, query string, args []interface{}) (*CachedResult, error) {
	if db == nil {
		return nil, ErrNoDatabase
	}

	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		c.incrementErrors()
		return nil, err
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()

	return &CachedResult{lastInsertID: lastID, rowsAffected: affected}, nil
}

// =============================================================================
// Manual Capture API
// =============================================================================

// Capture manually stores a cache entry for a query
func (c *Cache) Capture(query string, columns []string, rows [][]interface{}, args ...interface{}) error {
	c.captureMock(query, args, columns, rows, 0, 0, "")
	return nil
}

// CaptureExec manually stores a cache entry for an exec query
func (c *Cache) CaptureExec(query string, lastInsertID, rowsAffected int64, args ...interface{}) error {
	c.captureMock(query, args, nil, nil, lastInsertID, rowsAffected, "")
	return nil
}

// CaptureError stores an error response for a query
func (c *Cache) CaptureError(query string, errMsg string, args ...interface{}) error {
	c.captureMock(query, args, nil, nil, 0, 0, errMsg)
	return nil
}

// =============================================================================
// Cache Management
// =============================================================================

// Save persists mocks to disk
func (c *Cache) Save() error {
	return c.mocks.Save()
}

// Load loads mocks from disk
func (c *Cache) Load() error {
	return c.mocks.Load()
}

// Clear removes all mocks
func (c *Cache) Clear() error {
	c.mocks.Clear()
	return nil
}

// Reset resets cache entry consumption state (for re-use)
func (c *Cache) Reset() {
	c.mocks.Reset()
}

// Close saves mocks and cleans up
func (c *Cache) Close() error {
	return c.mocks.Save()
}

// =============================================================================
// Cache Invalidation (for production caching use case)
// =============================================================================

// InvalidateByQuery removes mocks matching a specific query
func (c *Cache) InvalidateByQuery(query string) int {
	return c.mocks.InvalidateByQuery(query)
}

// InvalidateByTable removes mocks for queries involving specific tables
func (c *Cache) InvalidateByTable(tableName string) int {
	return c.mocks.InvalidateByTable(tableName)
}

// InvalidateByPattern removes mocks matching a query pattern (supports wildcards)
func (c *Cache) InvalidateByPattern(pattern string) int {
	return c.mocks.InvalidateByPattern(pattern)
}

// InvalidateAll removes all mocks (cache clear)
func (c *Cache) InvalidateAll() int {
	count := c.mocks.Size()
	c.mocks.Clear()
	return count
}

// SetTTL sets a time-to-live for mocks (entries older than TTL are skipped during matching)
func (c *Cache) SetTTL(ttlSeconds int64) {
	c.mocks.SetTTL(ttlSeconds)
}

// Stats returns cache statistics
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.Mode = c.mode.String()
	stats.TotalMocks = c.mocks.Size()

	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total)
	}

	return stats
}

// MockStats returns detailed mock statistics
func (c *Cache) MockStats() mock.Stats {
	return c.mocks.Stats()
}

// =============================================================================
// Helper Methods
// =============================================================================

func (c *Cache) getQueryType(query string) string {
	if c.matcher == nil {
		return ""
	}

	// Use simple prefix matching for speed
	query = trimAndUpper(query)
	switch {
	case hasPrefix(query, "SELECT"):
		return "SELECT"
	case hasPrefix(query, "INSERT"):
		return "INSERT"
	case hasPrefix(query, "UPDATE"):
		return "UPDATE"
	case hasPrefix(query, "DELETE"):
		return "DELETE"
	default:
		return "OTHER"
	}
}

func (c *Cache) incrementHits() {
	c.mu.Lock()
	c.stats.Hits++
	c.mu.Unlock()
}

func (c *Cache) incrementMisses() {
	c.mu.Lock()
	c.stats.Misses++
	c.mu.Unlock()
}

func (c *Cache) incrementErrors() {
	c.mu.Lock()
	c.stats.Errors++
	c.mu.Unlock()
}

func (c *Cache) logError(err error, context string) {
	if c.options.OnError != nil {
		c.options.OnError(err, context)
	}
	if c.options.Logger != nil {
		c.options.Logger.Printf("ERROR [%s]: %v", context, err)
	}
}

func (c *Cache) logDebug(format string, args ...interface{}) {
	if c.options.Logger != nil {
		c.options.Logger.Printf("DEBUG: "+format, args...)
	}
}

func trimAndUpper(s string) string {
	// Trim and uppercase for fast prefix matching
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
			s = s[i:]
			break
		}
	}
	if len(s) > 10 {
		s = s[:10]
	}
	// Simple uppercase
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		result[i] = c
	}
	return string(result)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func truncateQuery(q string) string {
	if len(q) > 100 {
		return q[:100] + "..."
	}
	return q
}

// =============================================================================
// CachedRows - Represents cached query results
// =============================================================================

// CachedRows represents cached query results
type CachedRows struct {
	columns  []string
	rows     [][]interface{}
	rowIndex int
	err      error
}

// NewCachedRowsFromSQL creates CachedRows from sql.Rows
func NewCachedRowsFromSQL(rows *sql.Rows) (*CachedRows, error) {
	if rows == nil {
		return &CachedRows{columns: []string{}, rows: [][]interface{}{}, rowIndex: -1}, nil
	}

	defer func() {
		// Always close rows, ignore error
		_ = rows.Close()
	}()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var allRows [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Deep copy values
		rowCopy := make([]interface{}, len(values))
		for i, v := range values {
			rowCopy[i] = copyValue(v)
		}
		allRows = append(allRows, rowCopy)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &CachedRows{
		columns:  columns,
		rows:     allRows,
		rowIndex: -1,
	}, nil
}

// copyValue creates a copy of a value (handles []byte specially)
func copyValue(v interface{}) interface{} {
	if b, ok := v.([]byte); ok {
		cp := make([]byte, len(b))
		copy(cp, b)
		return cp
	}
	return v
}

// Columns returns the column names
func (r *CachedRows) Columns() []string {
	if r == nil {
		return nil
	}
	return r.columns
}

// Next advances to the next row
func (r *CachedRows) Next() bool {
	if r == nil || r.err != nil {
		return false
	}
	r.rowIndex++
	return r.rowIndex < len(r.rows)
}

// Scan copies the current row values into dest
func (r *CachedRows) Scan(dest ...interface{}) error {
	if r == nil {
		return sql.ErrNoRows
	}
	if r.rowIndex < 0 || r.rowIndex >= len(r.rows) {
		return sql.ErrNoRows
	}

	row := r.rows[r.rowIndex]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: expected %d arguments, got %d", len(row), len(dest))
	}

	for i, v := range row {
		if err := convertAssign(dest[i], v); err != nil {
			return fmt.Errorf("scan column %d: %w", i, err)
		}
	}

	return nil
}

// Close closes the rows
func (r *CachedRows) Close() error {
	return nil
}

// Err returns any error
func (r *CachedRows) Err() error {
	if r == nil {
		return nil
	}
	return r.err
}

// All returns all rows
func (r *CachedRows) All() [][]interface{} {
	if r == nil {
		return nil
	}
	return r.rows
}

// Count returns the number of rows
func (r *CachedRows) Count() int {
	if r == nil {
		return 0
	}
	return len(r.rows)
}

// =============================================================================
// CachedResult - Represents cached exec results
// =============================================================================

// CachedResult represents cached result from Exec
type CachedResult struct {
	lastInsertID int64
	rowsAffected int64
}

// LastInsertId returns the last insert ID
func (r *CachedResult) LastInsertId() (int64, error) {
	if r == nil {
		return 0, nil
	}
	return r.lastInsertID, nil
}

// RowsAffected returns the number of rows affected
func (r *CachedResult) RowsAffected() (int64, error) {
	if r == nil {
		return 0, nil
	}
	return r.rowsAffected, nil
}

// =============================================================================
// Type Conversion (robust, handles all SQL types)
// =============================================================================

// convertAssign converts a value to the destination type
func convertAssign(dest, src interface{}) error {
	if src == nil {
		// Handle nil source - set destination to zero value
		return setZeroValue(dest)
	}

	switch d := dest.(type) {
	case *string:
		*d = toString(src)
	case *[]byte:
		*d = toBytes(src)
	case *int:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int(v)
	case *int8:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int8(v)
	case *int16:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int16(v)
	case *int32:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int32(v)
	case *int64:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = v
	case *uint:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint(v)
	case *uint8:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint8(v)
	case *uint16:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint16(v)
	case *uint32:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint32(v)
	case *uint64:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = v
	case *float32:
		v, err := toFloat64(src)
		if err != nil {
			return err
		}
		*d = float32(v)
	case *float64:
		v, err := toFloat64(src)
		if err != nil {
			return err
		}
		*d = v
	case *bool:
		*d = toBool(src)
	case *time.Time:
		v, err := toTime(src)
		if err != nil {
			return err
		}
		*d = v
	case *interface{}:
		*d = src
	case *sql.NullString:
		d.Valid = src != nil
		if d.Valid {
			d.String = toString(src)
		}
	case *sql.NullInt64:
		d.Valid = src != nil
		if d.Valid {
			v, _ := toInt64(src)
			d.Int64 = v
		}
	case *sql.NullFloat64:
		d.Valid = src != nil
		if d.Valid {
			v, _ := toFloat64(src)
			d.Float64 = v
		}
	case *sql.NullBool:
		d.Valid = src != nil
		if d.Valid {
			d.Bool = toBool(src)
		}
	default:
		return fmt.Errorf("unsupported destination type: %T", dest)
	}
	return nil
}

func setZeroValue(dest interface{}) error {
	switch d := dest.(type) {
	case *string:
		*d = ""
	case *[]byte:
		*d = nil
	case *int:
		*d = 0
	case *int8:
		*d = 0
	case *int16:
		*d = 0
	case *int32:
		*d = 0
	case *int64:
		*d = 0
	case *uint:
		*d = 0
	case *uint8:
		*d = 0
	case *uint16:
		*d = 0
	case *uint32:
		*d = 0
	case *uint64:
		*d = 0
	case *float32:
		*d = 0
	case *float64:
		*d = 0
	case *bool:
		*d = false
	case *time.Time:
		*d = time.Time{}
	case *interface{}:
		*d = nil
	case *sql.NullString:
		d.Valid = false
		d.String = ""
	case *sql.NullInt64:
		d.Valid = false
		d.Int64 = 0
	case *sql.NullFloat64:
		d.Valid = false
		d.Float64 = 0
	case *sql.NullBool:
		d.Valid = false
		d.Bool = false
	default:
		return fmt.Errorf("unsupported destination type for nil: %T", dest)
	}
	return nil
}

func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toBytes(v interface{}) []byte {
	switch s := v.(type) {
	case []byte:
		return s
	case string:
		return []byte(s)
	default:
		return []byte(fmt.Sprintf("%v", v))
	}
}

func toInt64(v interface{}) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint:
		return int64(n), nil
	case uint8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case float32:
		return int64(n), nil
	case float64:
		return int64(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case string:
		// Try parsing as number
		var i int64
		_, err := fmt.Sscanf(n, "%d", &i)
		return i, err
	}
	return 0, fmt.Errorf("cannot convert %T to int64", v)
}

func toUint64(v interface{}) (uint64, error) {
	switch n := v.(type) {
	case uint:
		return uint64(n), nil
	case uint8:
		return uint64(n), nil
	case uint16:
		return uint64(n), nil
	case uint32:
		return uint64(n), nil
	case uint64:
		return n, nil
	case int:
		return uint64(n), nil
	case int8:
		return uint64(n), nil
	case int16:
		return uint64(n), nil
	case int32:
		return uint64(n), nil
	case int64:
		return uint64(n), nil
	case float32:
		return uint64(n), nil
	case float64:
		return uint64(n), nil
	}
	return 0, fmt.Errorf("cannot convert %T to uint64", v)
}

func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%f", &f)
		return f, err
	}
	return 0, fmt.Errorf("cannot convert %T to float64", v)
}

func toBool(v interface{}) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int, int8, int16, int32, int64:
		n, _ := toInt64(v)
		return n != 0
	case uint, uint8, uint16, uint32, uint64:
		n, _ := toUint64(v)
		return n != 0
	case string:
		return b == "true" || b == "1" || b == "yes"
	}
	return false
}

func toTime(v interface{}) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		// Try common formats
		formats := []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if parsed, err := time.Parse(f, t); err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse time: %s", t)
	case []byte:
		return toTime(string(t))
	}
	return time.Time{}, fmt.Errorf("cannot convert %T to time.Time", v)
}
