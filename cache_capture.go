package sqlcache

import "errors"

// LookupQuery checks the cache for a query without hitting the database.
func (c *Cache) LookupQuery(query string, args ...interface{}) (*CachedRows, bool, error) {
	matched, found := c.lookupCache(query, args)
	if found {
		c.incrementHits()
		if c.options.OnCacheHit != nil {
			c.options.OnCacheHit(query, args, true)
		}
		if matched.Spec.Response.Error != "" {
			return nil, true, errors.New(matched.Spec.Response.Error)
		}
		return NewCachedRows(matched.Spec.Response.Columns, matched.Spec.Response.Rows), true, nil
	}

	c.incrementMisses()
	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, false)
	}
	return nil, false, nil
}

// LookupExec checks the cache for an exec result without hitting the database.
func (c *Cache) LookupExec(query string, args ...interface{}) (*CachedResult, bool, error) {
	matched, found := c.lookupCache(query, args)
	if found {
		c.incrementHits()
		if c.options.OnCacheHit != nil {
			c.options.OnCacheHit(query, args, true)
		}
		if matched.Spec.Response.Error != "" {
			return nil, true, errors.New(matched.Spec.Response.Error)
		}
		return NewCachedResult(matched.Spec.Response.LastInsertID, matched.Spec.Response.RowsAffected), true, nil
	}

	c.incrementMisses()
	if c.options.OnCacheHit != nil {
		c.options.OnCacheHit(query, args, false)
	}
	return nil, false, nil
}

// CaptureQuery stores a live query response in the cache.
func (c *Cache) CaptureQuery(query string, columns []string, rows [][]interface{}, args ...interface{}) {
	c.saveToCache(query, args, columns, rows, 0, 0, "")
	c.incrementSaved()
	if c.options.OnCacheSave != nil {
		c.options.OnCacheSave(query, args)
	}
}

// CaptureExec stores a live exec response in the cache.
func (c *Cache) CaptureExec(query string, lastInsertID, rowsAffected int64, args ...interface{}) {
	c.saveToCache(query, args, nil, nil, lastInsertID, rowsAffected, "")
	c.incrementSaved()
	if c.options.OnCacheSave != nil {
		c.options.OnCacheSave(query, args)
	}
}

// CaptureError records a live database error when configured to persist it.
func (c *Cache) CaptureError(query string, err error, args ...interface{}) bool {
	if err == nil {
		return false
	}

	c.incrementErrors()
	if !c.shouldCacheDBError(err) {
		return false
	}

	c.saveToCache(query, args, nil, nil, 0, 0, err.Error())
	c.incrementSaved()
	if c.options.OnCacheSave != nil {
		c.options.OnCacheSave(query, args)
	}
	return true
}

// NotifyDatabaseHit records that a live database fallback happened.
func (c *Cache) NotifyDatabaseHit(query string, args ...interface{}) {
	c.recordDatabaseHit(query, args)
}

func (c *Cache) shouldCacheDBError(err error) bool {
	return err != nil && c.options.CacheDBErrors
}
