package sqlcache

import (
	"context"
	"database/sql"
	"errors"
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
	cached, found := c.lookupCache(query, args)
	if found {
		c.incrementHits()
		if c.options.OnCacheHit != nil {
			c.options.OnCacheHit(query, args, true)
		}
		if cached.Spec.Response.Error != "" {
			return nil, errors.New(cached.Spec.Response.Error)
		}
		return &CachedRows{columns: cached.Spec.Response.Columns, rows: cached.Spec.Response.Rows, rowIndex: -1}, nil
	}

	c.incrementMisses()
	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, false)
	}
	if db == nil {
		return nil, fmt.Errorf("%w: query=%q (no database configured for auto-capture)", ErrCacheMiss, truncateQuery(query))
	}

	c.recordDatabaseHit(query, args)
	c.logDebug("cache miss, forwarding to database: %s", truncateQuery(query))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.incrementErrors()
		c.saveToCache(query, args, nil, nil, 0, 0, err.Error())
		return nil, err
	}

	cachedRows, err := NewCachedRowsFromSQL(rows)
	if err != nil {
		c.incrementErrors()
		return nil, err
	}

	c.saveToCache(query, args, cachedRows.columns, cachedRows.rows, 0, 0, "")
	c.incrementSaved()
	if c.options.OnCacheSave != nil {
		c.options.OnCacheSave(query, args)
	}
	return cachedRows, nil
}

func (c *Cache) autoExec(ctx context.Context, db *sql.DB, query string, args []interface{}) (*CachedResult, error) {
	cached, found := c.lookupCache(query, args)
	if found {
		c.incrementHits()
		if c.options.OnCacheHit != nil {
			c.options.OnCacheHit(query, args, true)
		}
		if cached.Spec.Response.Error != "" {
			return nil, errors.New(cached.Spec.Response.Error)
		}
		return &CachedResult{lastInsertID: cached.Spec.Response.LastInsertID, rowsAffected: cached.Spec.Response.RowsAffected}, nil
	}

	c.incrementMisses()
	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, false)
	}
	if db == nil {
		return nil, fmt.Errorf("%w: query=%q (no database configured for auto-capture)", ErrCacheMiss, truncateQuery(query))
	}

	c.recordDatabaseHit(query, args)
	c.logDebug("cache miss, forwarding to database: %s", truncateQuery(query))

	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		c.incrementErrors()
		c.saveToCache(query, args, nil, nil, 0, 0, err.Error())
		return nil, err
	}

	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()
	c.saveToCache(query, args, nil, nil, lastID, affected, "")
	c.incrementSaved()
	if c.options.OnCacheSave != nil {
		c.options.OnCacheSave(query, args)
	}
	return &CachedResult{lastInsertID: lastID, rowsAffected: affected}, nil
}

func (c *Cache) offlineQuery(query string, args []interface{}) (*CachedRows, error) {
	if matcher.IsControlStatement(query) {
		return &CachedRows{columns: []string{}, rows: [][]interface{}{}, rowIndex: -1}, nil
	}

	matched, found := c.lookupCache(query, args)
	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, found)
	}
	if found {
		c.incrementHits()
		if matched.Spec.Response.Error != "" {
			return nil, errors.New(matched.Spec.Response.Error)
		}
		return &CachedRows{columns: matched.Spec.Response.Columns, rows: matched.Spec.Response.Rows, rowIndex: -1}, nil
	}

	c.incrementMisses()
	return nil, fmt.Errorf("%w: query=%q (offline mode, no database fallback)", ErrCacheMiss, truncateQuery(query))
}

func (c *Cache) offlineExec(query string, args []interface{}) (*CachedResult, error) {
	if matcher.IsControlStatement(query) {
		return &CachedResult{lastInsertID: 0, rowsAffected: 0}, nil
	}

	matched, found := c.lookupCache(query, args)
	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, found)
	}
	if found {
		c.incrementHits()
		if matched.Spec.Response.Error != "" {
			return nil, errors.New(matched.Spec.Response.Error)
		}
		return &CachedResult{lastInsertID: matched.Spec.Response.LastInsertID, rowsAffected: matched.Spec.Response.RowsAffected}, nil
	}

	c.incrementMisses()
	return nil, fmt.Errorf("%w: query=%q (offline mode, no database fallback)", ErrCacheMiss, truncateQuery(query))
}
