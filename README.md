# SQL Cache

A Go library for capturing and caching SQL query responses using AST-based structural matching. This allows applications to:

1. **Capture** SQL query responses and store them in YAML cache files
2. **Serve** cached responses without needing a database
3. **Run** applications without external database dependencies

## Features

- **YAML Cache Storage**: Store SQL interactions in YAML files
- **Structural Matching**: Match queries by AST structure (not just exact strings)
- **Sequential Consumption**: Consume cache entries in order for predictable behavior
- **Multiple Modes**: Passthrough, Capture, Cached, CacheFallback
- **No Database Required**: Run without a database connection using cached responses
- **Robust Error Handling**: Never crashes, handles all edge cases
- **Easy Integration**: Wrap existing `*sql.DB` with minimal code changes

## Installation

```bash
go get github.com/officialasishkumar/sql-cache
```

## Quick Start

### 1. Capture Mode (Against Real Database)

```go
import (
    "database/sql"
    sqlcache "github.com/officialasishkumar/sql-cache"
    "github.com/officialasishkumar/sql-cache/wrapper"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    // Open real database
    db, _ := sql.Open("sqlite3", "mydb.sqlite")
    
    // Wrap with caching
    cachedDB, _ := wrapper.Wrap(db, wrapper.Options{
        MockDir:     "./mocks",            // Where to store cache files
        InitialMode: sqlcache.ModeCapture, // Capture all query responses
    })
    defer cachedDB.Close()
    
    // Use like normal - responses are captured to ./mocks/mocks.yaml
    rows, _ := cachedDB.Query("SELECT * FROM users WHERE id = ?", 1)
    // ...
}
```

### 2. Cached Mode (No Database Needed)

```go
// Create cached-only wrapper (no database!)
cachedDB, _ := wrapper.NewCachedOnly(wrapper.Options{
    MockDir:        "./mocks",
    SequentialMode: true, // Each entry used once
})
defer cachedDB.Close()

// Same queries return cached responses
rows, _ := cachedDB.Query("SELECT * FROM users WHERE id = ?", 1)
```

### 3. Manual Cache Capture

```go
cache, _ := sqlcache.New(sqlcache.Options{
    MockDir: "./test-mocks",
})

// Capture query data manually
cache.Capture(
    "SELECT * FROM products WHERE category = ?",
    []string{"id", "name", "price"},  // columns
    [][]interface{}{                   // rows
        {1, "Widget", 9.99},
        {2, "Gadget", 19.99},
    },
    "electronics", // args
)

// Capture exec result
cache.CaptureExec(
    "INSERT INTO users (name) VALUES (?)",
    100, // lastInsertID
    1,   // rowsAffected
    "NewUser",
)

// Capture error response
cache.CaptureError(
    "SELECT * FROM nonexistent",
    "table not found",
)

// Use in cached mode
cache.SetMode(sqlcache.ModeCached)
rows, _ := cache.Query("SELECT * FROM products WHERE category = ?", "electronics")
```

## Modes

| Mode | Description |
|------|-------------|
| `ModePassthrough` | Execute queries against DB, no caching |
| `ModeCapture` | Execute queries and save responses as cache entries |
| `ModeCached` | Return cached responses, error if not found |
| `ModeCacheFallback` | Return cached responses, fall back to DB if not found |

## Cache File Format (YAML)

Cache entries are stored in `mocks.yaml` using a robust format optimized for structural matching and easy debugging:

```yaml
version: sql-cache-v1
kind: SQL
name: mock-select-user-by-id
spec:
    metadata:
        recorded_at: "2026-02-28T14:52:30+05:30"
        operation: "query"
        description: "Fetch user by ID"
    request:
        query: SELECT id, name, email FROM users WHERE id = ?
        args:
            - 1
        type: SELECT
        tables:
            - users
        structure: '*sqlparser.Select->...'
        placeholder_count: 1     # For prepared statement matching
        is_dml: true             # Whether it's a DML statement
        query_hash: "a1b2c3d4"   # Fast exact matching hash
    response:
        columns:
            - id
            - name
            - email
        rows:
            - - 1
              - Alice
              - alice@example.com
        row_count: 1
    created: 1772270550
    req_timestamp: 1772270550000000000
    res_timestamp: 1772270550100000000
CacheEntryInfo:                   # Entry tracking
    id: 1
    is_filtered: false
    sort_order: 1
---
# Additional entries separated by ---
```

### Key Fields Explained

| Field | Purpose |
|-------|---------|
| `structure` | AST structure signature for structural matching |
| `placeholder_count` | Number of `?` placeholders - must match for prepared statements |
| `is_dml` | Whether query is DML (SELECT/INSERT/UPDATE/DELETE) |
| `query_hash` | SHA256 hash for fast exact matching |
| `CacheEntryInfo` | Tracks entry usage during sequential consumption |

## Matching Strategy

The library uses AST-based matching:

1. **Exact Match** (100 points): Query strings are identical
2. **Structural Match** (80 points): Same AST structure (from Vitess SQL parser)
3. **Type Match** (30 points): Same statement type (SELECT, INSERT, etc.)
4. **Argument Match**: Adds bonus points when args match

This means slightly different queries can still match:
```go
// These would match structurally:
"SELECT * FROM users WHERE id = 1"
"SELECT * FROM users WHERE id = 2"  // Different value, same structure
```

## Sequential Consumption

When `SequentialMode: true`, each cache entry can only be used once:

```go
cache, _ := sqlcache.New(sqlcache.Options{
    MockDir:        "./mocks",
    SequentialMode: true,
})

// Capture same query 3 times with different results
cache.Capture("SELECT value FROM counter", []string{"value"}, [][]interface{}{{1}})
cache.Capture("SELECT value FROM counter", []string{"value"}, [][]interface{}{{2}})
cache.Capture("SELECT value FROM counter", []string{"value"}, [][]interface{}{{3}})

cache.SetMode(sqlcache.ModeCached)

// Each query consumes the next entry
rows, _ := cache.Query("SELECT value FROM counter") // Returns 1
rows, _ = cache.Query("SELECT value FROM counter")  // Returns 2
rows, _ = cache.Query("SELECT value FROM counter")  // Returns 3
rows, _ = cache.Query("SELECT value FROM counter")  // Error: no more entries!
```

## Integration Usage

Set `SQL_CACHE_MODE` environment variable:

```bash
# First run: capture from real database
SQL_CACHE_MODE=capture go run ./...

# Subsequent runs: serve from cache (fast, no DB needed)
SQL_CACHE_MODE=cached go run ./...
```

## API Reference

### Cache Options

```go
type Options struct {
    MockDir        string                                    // Directory for cache files
    DB             *sql.DB                                   // Optional DB connection
    SequentialMode bool                                      // Consume entries in order
    OnCapture      func(query string, args []interface{})    // Capture callback
    OnCacheHit     func(query string, args []interface{}, matched bool) // Cache hit callback
    OnError        func(err error, context string)           // Error callback
    Logger         *log.Logger                               // Debug logger
}
```

### Wrapper Options

```go
type Options struct {
    MockDir        string          // Directory for cache files
    InitialMode    sqlcache.Mode   // Starting mode
    SequentialMode bool            // Consume entries in order
    OnCapture      func(...)       // Callbacks
    OnCacheHit     func(...)
    OnError        func(...)
}
```

### Methods

```go
// Cache
cache.SetMode(mode)                    // Set operating mode
cache.Query(query, args...)            // Execute SELECT
cache.Exec(query, args...)             // Execute INSERT/UPDATE/DELETE
cache.Capture(query, cols, rows, args) // Manual cache capture
cache.CaptureExec(query, lastID, affected, args)
cache.CaptureError(query, errMsg, args)
cache.Save()                           // Persist cache to disk
cache.Load()                           // Load cache from disk
cache.Clear()                          // Remove all entries
cache.Reset()                          // Reset consumed states
cache.Stats()                          // Get statistics

// Cache Invalidation
cache.InvalidateByQuery(query)         // Remove entries matching exact query
cache.InvalidateByTable(tableName)     // Remove all entries for a table
cache.InvalidateByPattern(pattern)     // Remove entries matching pattern (wildcards)
cache.InvalidateAll()                  // Clear all entries
cache.SetTTL(seconds)                  // Set time-to-live for entries

// Wrapper
wrapper.Wrap(db, opts)                 // Wrap existing *sql.DB
wrapper.NewCachedOnly(opts)            // Create cached-only wrapper
db.SetMode(mode)                       // Set mode
db.Query/Exec/QueryRow(...)            // Same as sql.DB
db.Capture/CaptureExec/CaptureError(...)  // Manual capture
```

## Cache Invalidation

For production caching scenarios, you can invalidate cached data:

```go
// Invalidate when data changes
func (s *UserService) UpdateUser(id int, name string) error {
    _, err := s.db.Exec("UPDATE users SET name = ? WHERE id = ?", name, id)
    if err != nil {
        return err
    }
    
    // Invalidate cached queries for this table
    s.cache.InvalidateByTable("users")
    return nil
}

// Pattern-based invalidation
cache.InvalidateByPattern("SELECT * FROM users*")  // Invalidate all user queries

// TTL-based expiration (entries older than TTL are ignored)
cache.SetTTL(3600) // 1 hour TTL
```

## Examples

See the [examples](./examples) directory:

- **basic**: Simple capture/cache usage
- **integration**: Environment-based mode switching
- **testing**: Service usage without database

## Project Structure

```
sql-cache/
├── cache.go           # Main cache interface
├── parser/
│   └── parser.go      # SQL parsing and normalization
├── store/
│   └── store.go       # Cache storage implementation  
├── matcher/
│   └── matcher.go     # AST-based query matching
├── mock/
│   └── mock.go        # YAML cache entry storage
├── wrapper/
│   └── wrapper.go     # *sql.DB wrapper
├── driver/
│   └── driver.go      # database/sql driver interface
└── examples/
    ├── basic/         # Basic usage examples
    ├── integration/   # Integration patterns
    └── testing/       # Service examples
```

## Inspiration

This library uses battle-tested AST-based matching algorithms. Key concepts:

- **AST-based structural matching** using Vitess SQL parser
- **Placeholder count validation** - queries must have same number of `?` placeholders
- **DML type checking** - SELECT can only match SELECT, INSERT only matches INSERT, etc.
- **Parameter value matching** - type-flexible comparison (int/int64/float64 interoperability)
- **YAML cache file format** for easy inspection and modification
- **Sequential entry consumption** for predictable behavior
- **Scoring-based match selection** with definitive and best-effort matching

The matching algorithm has been battle-tested and provides production-grade reliability.

## License

MIT License
