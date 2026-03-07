package sqlcache

import (
	"fmt"
	"time"

	"github.com/officialasishkumar/sql-cache/internal/sqlmeta"
	"github.com/officialasishkumar/sql-cache/mock"
)

func (c *Cache) lookupCache(query string, args []interface{}) (*mock.Mock, bool) {
	if c.matcher == nil {
		return c.mocks.FindMatch(query, "", "", args, c.options.SequentialMode)
	}

	structure, _ := c.matcher.GetStructure(query)
	queryType := c.getQueryType(query)
	return c.mocks.FindMatch(query, queryType, structure, args, c.options.SequentialMode)
}

func (c *Cache) saveToCache(query string, args []interface{}, columns []string, rows [][]interface{}, lastInsertID, rowsAffected int64, errMsg string) {
	var (
		structure        string
		queryType        string
		tables           []string
		placeholderCount int
		isDML            bool
		queryHash        string
	)

	if c.matcher != nil {
		structure, _ = c.matcher.GetStructure(query)
		queryType = c.matcher.GetType(query)
		tables = c.matcher.GetTables(query)
		isDML = c.matcher.IsDML(query)
		queryHash = c.matcher.GetHash(query)
	} else {
		queryType = sqlmeta.DetectQueryType(query)
	}

	placeholderCount = sqlmeta.CountPlaceholders(query)
	rowCount := 0
	if rows != nil {
		rowCount = len(rows)
	}

	now := time.Now()
	entry := &mock.Mock{
		Version: mock.Version,
		Kind:    "SQL",
		Name:    fmt.Sprintf("mock-%d", now.UnixNano()),
		Spec: mock.MockSpec{
			Metadata: map[string]string{
				"captured_at": now.Format(time.RFC3339),
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

	if err := c.mocks.Add(entry); err != nil {
		c.logError(err, "saving cache entry")
	}
}

// Populate manually stores a cache entry for a query.
func (c *Cache) Populate(query string, columns []string, rows [][]interface{}, args ...interface{}) error {
	c.saveToCache(query, args, columns, rows, 0, 0, "")
	return nil
}

// PopulateExec manually stores a cache entry for an exec query.
func (c *Cache) PopulateExec(query string, lastInsertID, rowsAffected int64, args ...interface{}) error {
	c.saveToCache(query, args, nil, nil, lastInsertID, rowsAffected, "")
	return nil
}

// PopulateError stores an error response for a query.
func (c *Cache) PopulateError(query string, errMsg string, args ...interface{}) error {
	c.saveToCache(query, args, nil, nil, 0, 0, errMsg)
	return nil
}

// Save persists cache entries to disk.
func (c *Cache) Save() error { return c.mocks.Save() }

// Load loads cache entries from disk.
func (c *Cache) Load() error { return c.mocks.Load() }

// Clear removes all cache entries.
func (c *Cache) Clear() error {
	c.mocks.Clear()
	return nil
}

// Reset resets cache entry consumption state.
func (c *Cache) Reset() { c.mocks.Reset() }

// Close saves cache entries and cleans up.
func (c *Cache) Close() error { return c.mocks.Save() }

// InvalidateByQuery removes cache entries matching a specific query.
func (c *Cache) InvalidateByQuery(query string) int { return c.mocks.InvalidateByQuery(query) }

// InvalidateByTable removes cache entries for queries involving a specific table.
func (c *Cache) InvalidateByTable(tableName string) int { return c.mocks.InvalidateByTable(tableName) }

// InvalidateByPattern removes cache entries matching a query pattern.
func (c *Cache) InvalidateByPattern(pattern string) int { return c.mocks.InvalidateByPattern(pattern) }

// InvalidateAll removes all cache entries.
func (c *Cache) InvalidateAll() int {
	count := c.mocks.Size()
	c.mocks.Clear()
	return count
}

// SetTTL sets a time-to-live for cache entries.
func (c *Cache) SetTTL(ttlSeconds int64) { c.mocks.SetTTL(ttlSeconds) }

// Stats returns cache statistics.
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

// MockStats returns detailed cache store statistics.
func (c *Cache) MockStats() mock.Stats { return c.mocks.Stats() }

func (c *Cache) getQueryType(query string) string {
	if c.matcher == nil {
		return sqlmeta.DetectQueryType(query)
	}
	return c.matcher.GetType(query)
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

func (c *Cache) incrementSaved() {
	c.mu.Lock()
	c.stats.Saved++
	c.mu.Unlock()
}

func (c *Cache) recordDatabaseHit(query string, args []interface{}) {
	c.mu.Lock()
	c.stats.DatabaseHits++
	c.mu.Unlock()

	if c.options.OnDatabaseHit != nil {
		c.options.OnDatabaseHit(query, args)
	}
	if c.options.Logger != nil {
		c.options.Logger.Printf("WARN: database fallback for uncached query: %s", truncateQuery(query))
	}
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

func truncateQuery(q string) string {
	if len(q) > 100 {
		return q[:100] + "..."
	}
	return q
}
