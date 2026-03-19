package sqlcache

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/officialasishkumar/sql-cache/matcher"
)

// Query executes a query with caching support.
func (c *Cache) Query(query string, args ...interface{}) (*CachedRows, error) {
	return c.QueryContext(context.Background(), query, args...)
}

// QueryContext executes a query with context and caching support.
func (c *Cache) QueryContext(ctx context.Context, query string, args ...interface{}) (*CachedRows, error) {
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
	case ModeAuto:
		return c.autoQuery(ctx, db, query, args)
	case ModeOffline:
		return c.offlineQuery(query, args)
	default:
		return nil, ErrInvalidMode
	}
}

// Exec executes a non-SELECT statement with caching support.
func (c *Cache) Exec(query string, args ...interface{}) (*CachedResult, error) {
	return c.ExecContext(context.Background(), query, args...)
}

// ExecContext executes a non-SELECT statement with context.
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
	case ModeAuto:
		return c.autoExec(ctx, db, query, args)
	case ModeOffline:
		return c.offlineExec(query, args)
	default:
		return nil, ErrInvalidMode
	}
}

func (c *Cache) autoQuery(ctx context.Context, db *sql.DB, query string, args []interface{}) (*CachedRows, error) {
	cachedRows, found, err := c.LookupQuery(query, args...)
	if found || err != nil {
		return cachedRows, err
	}

	if db == nil {
		return nil, fmt.Errorf("%w: query=%q (no database configured for auto-capture)", ErrCacheMiss, truncateQuery(query))
	}

	c.recordDatabaseHit(query, args)
	c.logDebug("cache miss, forwarding to database: %s", truncateQuery(query))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.CaptureError(query, err, args...)
		return nil, err
	}

	liveRows, err := NewCachedRowsFromSQL(rows)
	if err != nil {
		c.incrementErrors()
		return nil, err
	}

	c.CaptureQuery(query, liveRows.columns, liveRows.rows, args...)
	return liveRows, nil
}

func (c *Cache) autoExec(ctx context.Context, db *sql.DB, query string, args []interface{}) (*CachedResult, error) {
	cachedResult, found, err := c.LookupExec(query, args...)
	if found || err != nil {
		return cachedResult, err
	}

	if db == nil {
		return nil, fmt.Errorf("%w: query=%q (no database configured for auto-capture)", ErrCacheMiss, truncateQuery(query))
	}

	c.recordDatabaseHit(query, args)
	c.logDebug("cache miss, forwarding to database: %s", truncateQuery(query))

	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		c.CaptureError(query, err, args...)
		return nil, err
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()
	c.CaptureExec(query, lastID, affected, args...)
	return NewCachedResult(lastID, affected), nil
}

func (c *Cache) offlineQuery(query string, args []interface{}) (*CachedRows, error) {
	if matcher.IsControlStatement(query) {
		return NewCachedRows([]string{}, [][]interface{}{}), nil
	}

	matched, found, err := c.LookupQuery(query, args...)
	if found || err != nil {
		return matched, err
	}

	return nil, fmt.Errorf("%w: query=%q (offline mode, no database fallback)", ErrCacheMiss, truncateQuery(query))
}

func (c *Cache) offlineExec(query string, args []interface{}) (*CachedResult, error) {
	if matcher.IsControlStatement(query) {
		return NewCachedResult(0, 0), nil
	}

	matched, found, err := c.LookupExec(query, args...)
	if found || err != nil {
		return matched, err
	}

	return nil, fmt.Errorf("%w: query=%q (offline mode, no database fallback)", ErrCacheMiss, truncateQuery(query))
}
