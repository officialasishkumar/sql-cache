# SQL Cache

A Go library for recording and replaying SQL queries using AST-based structural matching. This allows applications to:

1. **Record** SQL queries and responses to YAML mock files
2. **Replay** responses from mocks without needing a database
3. **Test** applications without external dependencies

## Features

- **YAML Recording**: Record SQL interactions to YAML files
- **Structural Matching**: Match queries by AST structure (not just exact strings)
- **Sequential Replay**: Consume mocks in order for predictable tests
- **Multiple Modes**: Passthrough, Record, Replay, ReplayFallback
- **No Database Required**: Run tests without a database connection
- **Robust Error Handling**: Never crashes, handles all edge cases
- **Easy Integration**: Wrap existing `*sql.DB` with minimal code changes

## Installation

```bash
go get github.com/asish/sql-cache
```

## Quick Start

### 1. Record Mode (Against Real Database)

```go
import (
    "database/sql"
    sqlcache "github.com/asish/sql-cache"
    "github.com/asish/sql-cache/wrapper"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    // Open real database
    db, _ := sql.Open("sqlite3", "mydb.sqlite")
    
    // Wrap with caching
    cachedDB, _ := wrapper.Wrap(db, wrapper.Options{
        MockDir:     "./mocks",           // Where to store mock files
        InitialMode: sqlcache.ModeRecord, // Record all queries
    })
    defer cachedDB.Close()
    
    // Use like normal - queries are recorded to ./mocks/mocks.yaml
    rows, _ := cachedDB.Query("SELECT * FROM users WHERE id = ?", 1)
    // ...
}
```

### 2. Replay Mode (No Database Needed)

```go
// Create replay-only wrapper (no database!)
cachedDB, _ := wrapper.NewReplayOnly(wrapper.Options{
    MockDir:          "./mocks",
    SequentialReplay: true, // Each mock used once
})
defer cachedDB.Close()

// Same queries return mocked responses
rows, _ := cachedDB.Query("SELECT * FROM users WHERE id = ?", 1)
```

### 3. Manual Mock Recording (For Unit Tests)

```go
cache, _ := sqlcache.New(sqlcache.Options{
    MockDir: "./test-mocks",
})

// Record mock data manually
cache.Record(
    "SELECT * FROM products WHERE category = ?",
    []string{"id", "name", "price"},  // columns
    [][]interface{}{                   // rows
        {1, "Widget", 9.99},
        {2, "Gadget", 19.99},
    },
    "electronics", // args
)

// Record exec result
cache.RecordExec(
    "INSERT INTO users (name) VALUES (?)",
    100, // lastInsertID
    1,   // rowsAffected
    "NewUser",
)

// Record error response
cache.RecordError(
    "SELECT * FROM nonexistent",
    "table not found",
)

// Use in replay mode
cache.SetMode(sqlcache.ModeReplay)
rows, _ := cache.Query("SELECT * FROM products WHERE category = ?", "electronics")
```

## Modes

| Mode | Description |
|------|-------------|
| `ModePassthrough` | Execute queries against DB, no caching |
| `ModeRecord` | Execute queries and save responses as mocks |
| `ModeReplay` | Return mocked responses, error if not found |
| `ModeReplayFallback` | Return mocked responses, fall back to DB if not found |

## Mock File Format (YAML)

Mocks are stored in `mocks.yaml` using a robust format optimized for structural matching and easy debugging:

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
test_mode_info:                   # Test tracking
    id: 1
    is_filtered: false
    sort_order: 1
---
# Additional mocks separated by ---
```

### Key Fields Explained

| Field | Purpose |
|-------|---------|
| `structure` | AST structure signature for structural matching |
| `placeholder_count` | Number of `?` placeholders - must match for prepared statements |
| `is_dml` | Whether query is DML (SELECT/INSERT/UPDATE/DELETE) |
| `query_hash` | SHA256 hash for fast exact matching |
| `test_mode_info` | Tracks mock usage during test runs |

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

## Sequential Replay

When `SequentialReplay: true`, each mock can only be used once:

```go
cache, _ := sqlcache.New(sqlcache.Options{
    MockDir:          "./mocks",
    SequentialReplay: true,
})

// Record same query 3 times with different results
cache.Record("SELECT value FROM counter", []string{"value"}, [][]interface{}{{1}})
cache.Record("SELECT value FROM counter", []string{"value"}, [][]interface{}{{2}})
cache.Record("SELECT value FROM counter", []string{"value"}, [][]interface{}{{3}})

cache.SetMode(sqlcache.ModeReplay)

// Each query consumes the next mock
rows, _ := cache.Query("SELECT value FROM counter") // Returns 1
rows, _ = cache.Query("SELECT value FROM counter")  // Returns 2
rows, _ = cache.Query("SELECT value FROM counter")  // Returns 3
rows, _ = cache.Query("SELECT value FROM counter")  // Error: no more mocks!
```

## Integration Testing

Set `SQL_CACHE_MODE` environment variable:

```bash
# First run: record against real database
SQL_CACHE_MODE=record go test ./...

# Subsequent runs: replay from mocks (fast, no DB needed)
SQL_CACHE_MODE=replay go test ./...
```

## API Reference

### Cache Options

```go
type Options struct {
    MockDir          string                                    // Directory for mock files
    DB               *sql.DB                                   // Optional DB connection
    SequentialReplay bool                                      // Consume mocks in order
    OnRecord         func(query string, args []interface{})    // Record callback
    OnReplay         func(query string, args []interface{}, matched bool) // Replay callback
    OnError          func(err error, context string)           // Error callback
    Logger           *log.Logger                               // Debug logger
}
```

### Wrapper Options

```go
type Options struct {
    MockDir          string          // Directory for mock files
    InitialMode      sqlcache.Mode   // Starting mode
    SequentialReplay bool            // Consume mocks in order
    OnRecord         func(...)       // Callbacks
    OnReplay         func(...)
    OnError          func(...)
}
```

### Methods

```go
// Cache
cache.SetMode(mode)                    // Set operating mode
cache.Query(query, args...)            // Execute SELECT
cache.Exec(query, args...)             // Execute INSERT/UPDATE/DELETE
cache.Record(query, cols, rows, args)  // Manual mock recording
cache.RecordExec(query, lastID, affected, args)
cache.RecordError(query, errMsg, args)
cache.Save()                           // Persist mocks to disk
cache.Load()                           // Load mocks from disk
cache.Clear()                          // Remove all mocks
cache.Reset()                          // Reset consumed states
cache.Stats()                          // Get statistics

// Cache Invalidation (for production caching)
cache.InvalidateByQuery(query)         // Remove mocks matching exact query
cache.InvalidateByTable(tableName)     // Remove all mocks for a table
cache.InvalidateByPattern(pattern)     // Remove mocks matching pattern (wildcards)
cache.InvalidateAll()                  // Clear all mocks
cache.SetTTL(seconds)                  // Set time-to-live for mocks

// Wrapper
wrapper.Wrap(db, opts)                 // Wrap existing *sql.DB
wrapper.NewReplayOnly(opts)            // Create replay-only wrapper
db.SetMode(mode)                       // Set mode
db.Query/Exec/QueryRow(...)            // Same as sql.DB
db.Record/RecordExec/RecordError(...)  // Manual recording
```

## Cache Invalidation (Production Use)

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

// TTL-based expiration (mocks older than TTL are ignored)
cache.SetTTL(3600) // 1 hour TTL
```

## Examples

See the [examples](./examples) directory:

- **basic**: Simple record/replay usage
- **integration**: Environment-based mode switching
- **testing**: Unit testing without database

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
│   └── mock.go        # YAML mock storage
├── wrapper/
│   └── wrapper.go     # *sql.DB wrapper
├── driver/
│   └── driver.go      # database/sql driver interface
└── examples/
    ├── basic/         # Basic usage examples
    ├── integration/   # Integration patterns
    └── testing/       # Testing examples
```

## Inspiration

This library uses battle-tested AST-based matching algorithms. Key concepts:

- **AST-based structural matching** using Vitess SQL parser
- **Placeholder count validation** - queries must have same number of `?` placeholders
- **DML type checking** - SELECT can only match SELECT, INSERT only matches INSERT, etc.
- **Parameter value matching** - type-flexible comparison (int/int64/float64 interoperability)
- **YAML mock file format** for easy inspection and modification
- **Sequential mock consumption** for predictable test behavior
- **Scoring-based match selection** with definitive and best-effort matching

The matching algorithm has been battle-tested and provides production-grade reliability.

## License

MIT License
