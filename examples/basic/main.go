// Example: Basic usage of SQL cache
// This demonstrates how to use sql-cache for transparent caching of SQL responses.
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
	fmt.Println("=== Example 1: Auto Cache-Through ===")
	autoCacheThroughExample()

	fmt.Println("\n=== Example 2: Direct Cache API ===")
	directCacheExample()

	fmt.Println("\n=== Example 3: Manual Cache Population ===")
	manualPopulateExample()

	fmt.Println("\n=== Example 4: Offline Mode ===")
	offlineExample()
}

func autoCacheThroughExample() {
	// Open the underlying database connection
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Setup test data
	db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)`)
	db.Exec(`INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')`)

	// Wrap with caching support - ModeAuto is the default
	cachedDB, err := wrapper.Wrap(db, wrapper.Options{
		MockDir:        "./mocks",
		SequentialMode: true,
		OnCacheSave: func(query string, args []interface{}) {
			fmt.Printf("  [SAVED] %s with args %v\n", truncate(query, 50), args)
		},
		OnCacheHit: func(query string, args []interface{}, matched bool) {
			if matched {
				fmt.Printf("  [CACHE HIT] %s\n", truncate(query, 50))
			}
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cachedDB.Close()

	// First query: cache miss → calls real DB → saves to cache → returns
	fmt.Println("First query (cache miss → calls DB → saves):")
	row := cachedDB.QueryRow("SELECT id, name, email FROM users WHERE id = ?", 1)
	var id int
	var name, email string
	if err := row.Scan(&id, &name, &email); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Result: id=%d, name=%s, email=%s\n", id, name, email)

	// Second query: cache hit → returns from cache (no DB call)
	fmt.Println("\nSecond query (cache hit → from cache):")
	row = cachedDB.QueryRow("SELECT id, name, email FROM users WHERE id = ?", 1)
	if err := row.Scan(&id, &name, &email); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Result: id=%d, name=%s, email=%s\n", id, name, email)

	// Print stats
	stats := cachedDB.Stats()
	fmt.Printf("\n  Stats: mocks=%d, hits=%d, misses=%d, saved=%d\n",
		stats.TotalMocks, stats.Hits, stats.Misses, stats.Saved)
}

func directCacheExample() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Setup test data
	db.Exec(`CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT, price REAL)`)
	db.Exec(`INSERT INTO products (name, price) VALUES ('Widget', 9.99)`)
	db.Exec(`INSERT INTO products (name, price) VALUES ('Gadget', 19.99)`)

	cache, err := sqlcache.New(sqlcache.Options{
		MockDir: "./mocks/direct",
		DB:      db,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	// Query through cache - auto fetches from DB on miss, saves, returns
	rows, err := cache.Query("SELECT id, name, price FROM products")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Products (auto-cached):")
	for rows.Next() {
		var id int
		var name string
		var price float64
		if err := rows.Scan(&id, &name, &price); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  id=%d, name=%s, price=%.2f\n", id, name, price)
	}
}

func manualPopulateExample() {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir: "./mocks/manual",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	// Manually populate cache entries
	cache.Populate(
		"SELECT count(*) FROM users",
		[]string{"count"},
		[][]interface{}{{42}},
	)

	cache.Populate(
		"SELECT name FROM users WHERE active = ?",
		[]string{"name"},
		[][]interface{}{{"Alice"}, {"Bob"}, {"Charlie"}},
		true,
	)

	// Populate an exec result
	cache.PopulateExec(
		"INSERT INTO users (name) VALUES (?)",
		100, // lastInsertID
		1,   // rowsAffected
		"NewUser",
	)

	// Switch to offline mode (cache-only, no DB)
	cache.SetMode(sqlcache.ModeOffline)

	// Query count
	rows, _ := cache.Query("SELECT count(*) FROM users")
	if rows.Next() {
		var count int
		rows.Scan(&count)
		fmt.Printf("User count: %d\n", count)
	}

	// Query active users
	rows, _ = cache.Query("SELECT name FROM users WHERE active = ?", true)
	fmt.Println("Active users:")
	for rows.Next() {
		var name string
		rows.Scan(&name)
		fmt.Printf("  - %s\n", name)
	}

	// Execute insert
	result, _ := cache.Exec("INSERT INTO users (name) VALUES (?)", "NewUser")
	lastID, _ := result.LastInsertId()
	affected, _ := result.RowsAffected()
	fmt.Printf("Insert result: lastID=%d, affected=%d\n", lastID, affected)
}

func offlineExample() {
	// Create wrapper without database (offline mode)
	db, err := wrapper.NewOffline(wrapper.Options{
		MockDir:        "./mocks",
		SequentialMode: true,
		OnCacheHit: func(query string, args []interface{}, matched bool) {
			if matched {
				fmt.Printf("  [CACHE HIT] %s\n", truncate(query, 40))
			} else {
				fmt.Printf("  [CACHE MISS] %s\n", truncate(query, 40))
			}
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Populate some entries
	db.Populate(
		"SELECT * FROM config WHERE key = ?",
		[]string{"key", "value"},
		[][]interface{}{{"app_name", "MyApp"}},
		"app_name",
	)

	// Query from cache - no database needed!
	fmt.Println("Querying without database:")
	row := db.QueryRow("SELECT * FROM config WHERE key = ?", "app_name")
	var key, value string
	if err := row.Scan(&key, &value); err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("  Config: %s = %s\n", key, value)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
