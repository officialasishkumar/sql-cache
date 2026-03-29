# SQL Cache

A Go library that records your SQL query responses into YAML files and replays them later — no database needed.

## How it works

```
Your Go code  →  sql-cache  →  database
                    ↓
               mocks.yaml (saved responses)
```

1. Your code runs a SQL query through sql-cache
2. **First run** — query goes to the real database, response is saved to `mocks.yaml`
3. **Next run** — same query is served from the YAML file, database is never hit

That's it. Your queries get cached automatically.

## Install

```bash
go get github.com/officialasishkumar/sql-cache
```

## Quick example

Copy this into `main.go` and run it. It uses SQLite in-memory so there's nothing to set up.

```go
package main

import (
	"database/sql"
	"fmt"
	"log"

	sqlcache "github.com/officialasishkumar/sql-cache"
	"github.com/officialasishkumar/sql-cache/wrapper"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// 1. Open a normal database connection
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()

	db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	db.Exec(`INSERT INTO users (name) VALUES ('Alice'), ('Bob')`)

	// 2. Wrap it with sql-cache
	cached, _ := wrapper.Wrap(db, wrapper.Options{MockDir: "./mocks"})
	defer cached.Close()

	// 3. Query as usual — first call hits DB and saves to mocks.yaml
	row := cached.QueryRow("SELECT id, name FROM users WHERE id = ?", 1)
	var id int
	var name string
	row.Scan(&id, &name)
	fmt.Printf("From DB: id=%d name=%s\n", id, name)

	// 4. Same query again — served from cache, no DB call
	row = cached.QueryRow("SELECT id, name FROM users WHERE id = ?", 1)
	row.Scan(&id, &name)
	fmt.Printf("From cache: id=%d name=%s\n", id, name)

	// 5. Check stats
	stats := cached.Stats()
	fmt.Printf("Hits: %d, Misses: %d, Saved: %d\n", stats.Hits, stats.Misses, stats.Saved)
}
```

```bash
go run main.go
```

Output:
```
From DB: id=1 name=Alice
From cache: id=1 name=Alice
Hits: 1, Misses: 1, Saved: 1
```

After running, check `./mocks/mocks.yaml` — you'll see the cached response in plain YAML.

## Two modes

| Mode | What happens |
|------|-------------|
| **Auto** (default) | Cache miss → hits real DB → saves response → returns it. Cache hit → returns from cache. |
| **Offline** | Cache miss → returns error. Cache hit → returns from cache. Database is never touched. |

```go
// Auto mode (default)
cached, _ := wrapper.Wrap(db, wrapper.Options{MockDir: "./mocks"})

// Offline mode — no database needed at all
cached, _ := wrapper.NewOffline(wrapper.Options{MockDir: "./mocks"})
```

Offline mode is useful for tests and CI where you don't want a running database.

## Seed the cache without a database

You can manually add entries so the cache works without ever connecting to a real DB.

```go
cache, _ := sqlcache.New(sqlcache.Options{MockDir: "./mocks"})

// Add a SELECT response
cache.Populate(
    "SELECT id, name FROM users WHERE id = ?",
    []string{"id", "name"},           // columns
    [][]interface{}{{1, "Alice"}},     // rows
    1,                                 // query args
)

// Add an INSERT response
cache.PopulateExec(
    "INSERT INTO users (name) VALUES (?)",
    100,       // lastInsertID
    1,         // rowsAffected
    "Charlie", // query args
)

// Switch to offline — all queries served from cache
cache.SetMode(sqlcache.ModeOffline)
```

## Query matching

Matching is deterministic, not fuzzy. A cached response is returned only when:

- Query text matches (exact or structurally equivalent via AST fingerprint)
- Argument values match exactly
- Placeholder count matches
- Query type matches (SELECT, INSERT, etc.)

This means a cached response for `WHERE id = 1` will **not** accidentally match a request for `WHERE id = 2`.

## Cache invalidation

```go
cache.InvalidateByQuery("SELECT * FROM users WHERE id = ?")  // specific query
cache.InvalidateByTable("users")                              // all queries touching a table
cache.InvalidateByPattern("SELECT * FROM users*")             // regex pattern
cache.InvalidateAll()                                         // everything
cache.SetTTL(3600)                                            // expire after 1 hour
```

## Which package to use

| Package | When to use |
|---------|------------|
| `wrapper` | **Start here.** Drop-in replacement for `*sql.DB` with caching. |
| `sqlcache` | Direct cache API when you want full control. |
| `driver` | Low-level `database/sql/driver` integration. |

## More examples

```bash
go run ./examples/basic         # cache-through, direct API, manual population, offline mode
go run ./examples/testing       # test-oriented usage
go run ./examples/integration   # auto vs offline mode comparison
```

## Run tests

```bash
go test ./...
```
