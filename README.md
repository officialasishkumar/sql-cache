# SQL Cache

`sql-cache` is a Go library that records SQL responses into YAML and replays them on later requests.

The default flow is cache-through:
- cache hit: return the stored response
- cache miss: execute the real query, capture the response, save it to `mocks.yaml`, return it

It also supports a strict offline mode where the real database is never touched.

## Why this exists

This project is built for application-level SQL caching and replay. It works by intercepting calls at the `database/sql` boundary, so it is portable across Linux, macOS, and Windows.

You do not need eBPF or traffic redirection for the normal library workflow.

Use eBPF only if you want a separate zero-instrumentation agent for applications that do not import this library. That is a different product surface and should stay separate from the core package.

## What you get

- cache-through recording in `ModeAuto`
- strict cache-only replay in `ModeOffline`
- deterministic request matching based on query shape and arguments
- YAML-backed mocks that can be inspected and versioned
- `database/sql` wrapper for easy adoption
- explicit logging and callbacks when a real DB fallback happens
- sequential consumption mode for ordered test scenarios
- query invalidation by exact query, table, or pattern

## Installation

```bash
go get github.com/officialasishkumar/sql-cache
```

## Operating modes

| Mode | Behavior |
| --- | --- |
| `ModeAuto` | Look up a mock first. On miss, hit the real DB, capture the result, save it, and return it. |
| `ModeOffline` | Look up a mock only. On miss, return `ErrCacheMiss`. No real DB fallback. |

If you need a hard guarantee that production or tests never touch the database, use `ModeOffline`.

## Quick start

### Wrap an existing `*sql.DB`

```go
package main

import (
    "database/sql"
    "log"
    "os"

    "github.com/officialasishkumar/sql-cache/wrapper"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    db, err := sql.Open("sqlite3", "app.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    cachedDB, err := wrapper.Wrap(db, wrapper.Options{
        MockDir: "./mocks",
        Logger:  log.New(os.Stdout, "sql-cache ", log.LstdFlags),
        OnDatabaseHit: func(query string, args []interface{}) {
            log.Printf("REAL DB HIT: %s args=%v", query, args)
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    defer cachedDB.Close()

    row := cachedDB.QueryRow("SELECT id, name FROM users WHERE id = ?", 1)

    var id int
    var name string
    if err := row.Scan(&id, &name); err != nil {
        log.Fatal(err)
    }
}
```

### Open and wrap in one step

```go
cachedDB, err := wrapper.Open("sqlite3", "app.db", wrapper.Options{
    MockDir: "./mocks",
})
```

### Strict offline mode

```go
cachedDB, err := wrapper.NewOffline(wrapper.Options{
    MockDir: "./mocks",
})
if err != nil {
    log.Fatal(err)
}
defer cachedDB.Close()

cachedDB.SetMode(sqlcache.ModeOffline)

row := cachedDB.QueryRow("SELECT id, name FROM users WHERE id = ?", 1)
```

In offline mode, an uncached query returns `sqlcache.ErrCacheMiss` and the real database is never called.

## Real DB hit visibility

In `ModeAuto`, you can surface every fallback to the real database.

Available controls:
- `OnDatabaseHit func(query string, args []interface{})`
- `Logger *log.Logger`
- `Stats().DatabaseHits`

Example:

```go
cachedDB, err := wrapper.Wrap(db, wrapper.Options{
    MockDir: "./mocks",
    Logger:  log.New(os.Stdout, "", log.LstdFlags),
    OnDatabaseHit: func(query string, args []interface{}) {
        log.Printf("REAL DB HIT: %s args=%v", query, args)
    },
})
```

Behavior:
- `ModeOffline`: no DB hit is possible
- `ModeAuto`: each fallback increments `DatabaseHits`, fires `OnDatabaseHit`, and logs a warning if `Logger` is configured

## Manual population

You can seed the cache without a live database.

```go
cache, err := sqlcache.New(sqlcache.Options{MockDir: "./test-mocks"})
if err != nil {
    log.Fatal(err)
}
defer cache.Close()

cache.Populate(
    "SELECT id, name FROM users WHERE id = ?",
    []string{"id", "name"},
    [][]interface{}{{1, "Alice"}},
    1,
)

cache.PopulateExec(
    "INSERT INTO users (name) VALUES (?)",
    101,
    1,
    "Alice",
)

cache.PopulateError(
    "SELECT * FROM missing_table",
    "table not found",
)

cache.SetMode(sqlcache.ModeOffline)
```

## Matching rules

Matching is deterministic. The cache does not do fuzzy best-effort replay.

A request matches when these checks hold:
- placeholder count matches
- argument values match
- query type matches when available
- exact SQL matches, or canonical SQL fingerprints match

That means this does not happen anymore:
- cached mock for `id = 1`
- replay request for `id = 2`
- accidental match

PostgreSQL-style placeholders such as `$1`, `$2` are also counted correctly.

## Sequential mode

If `SequentialMode` is enabled, each matched entry is consumed once and then skipped on the next lookup. This is useful for ordered test cases or repeated calls that should return different responses.

```go
cache, _ := sqlcache.New(sqlcache.Options{
    MockDir:        "./test-mocks/sequential",
    SequentialMode: true,
})
```

## Cache invalidation

You can invalidate entries when the underlying data changes.

```go
cache.InvalidateByQuery("SELECT id, name FROM users WHERE id = ?")
cache.InvalidateByTable("users")
cache.InvalidateByPattern("SELECT * FROM users*")
cache.InvalidateAll()
cache.SetTTL(3600)
```

## YAML storage

Mocks are stored in `<mock-dir>/mocks.yaml` as YAML documents separated by `---`.

Stored request metadata includes:
- original query text
- argument values
- query type
- referenced tables
- canonical structure fingerprint
- placeholder count

Stored response metadata includes:
- columns and row values for queries
- `LastInsertID` and `RowsAffected` for execs
- captured error text for failed queries

## Package choices

Use the package that fits your integration point.

- `wrapper`: easiest API, mirrors common `database/sql` usage
- `sqlcache`: direct cache object if you want explicit control
- `driver`: lower-level `database/sql/driver` integration

For most applications, start with `wrapper`.

## Examples

Runnable examples are in:
- `examples/basic`
- `examples/testing`
- `examples/integration`

Typical commands:

```bash
go run ./examples/basic
go run ./examples/testing
SQL_CACHE_MODE=auto go run ./examples/integration
SQL_CACHE_MODE=offline go run ./examples/integration
```

## Production notes

- wrapper mode is the portable, production-ready path
- offline mode is the correct choice for hermetic tests and strict no-DB replay
- eBPF is not part of the required runtime path for this library
- if you later build an agent mode, keep it separate from the core library API

## Development

Verification used in this repo:

```bash
go test ./...
```
