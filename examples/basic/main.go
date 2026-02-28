// Example: Basic usage of SQL cache
// This demonstrates how to use sql-cache for capturing and caching SQL responses.
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
	fmt.Println("=== Example 1: Basic Wrapper Usage ===")
	basicWrapperExample()

	fmt.Println("\n=== Example 2: Direct Cache API ===")
	directCacheExample()

	fmt.Println("\n=== Example 3: Manual Cache Capture ===")
	manualCaptureExample()

	fmt.Println("\n=== Example 4: Cached-Only Mode ===")
	cachedOnlyExample()
}

func basicWrapperExample() {
	// Open the underlying database connection
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Setup test data
	db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)`)
	db.Exec(`INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')`)

	// Wrap with caching support
	cachedDB, err := wrapper.Wrap(db, wrapper.Options{
		MockDir:        "./mocks",
		InitialMode:    sqlcache.ModeCapture, // Capture queries and responses
		SequentialMode: true,                 // Sequential cache consumption
		OnCapture: func(query string, args []interface{}) {
			fmt.Printf("  [CAPTURED] %s with args %v\n", truncate(query, 50), args)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cachedDB.Close()

	// Execute query - this records to mock file
	fmt.Println("First query (record mode - creates mock):")
	row := cachedDB.QueryRow("SELECT id, name, email FROM users WHERE id = ?", 1)
	var id int
	var name, email string
	if err := row.Scan(&id, &name, &email); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Result: id=%d, name=%s, email=%s\n", id, name, email)

	// Switch to cached mode
	cachedDB.SetMode(sqlcache.ModeCached)

	// Same query now returns from cache
	fmt.Println("\nSecond query (cached mode - from cache):")
	row = cachedDB.QueryRow("SELECT id, name, email FROM users WHERE id = ?", 1)
	if err := row.Scan(&id, &name, &email); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Result: id=%d, name=%s, email=%s\n", id, name, email)

	// Print stats
	stats := cachedDB.Stats()
	fmt.Printf("\n  Stats: mocks=%d, hits=%d, misses=%d\n",
		stats.TotalMocks, stats.Hits, stats.Misses)
}

func directCacheExample() {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir: "./mocks/direct",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	// Manually capture cache data
	err = cache.Capture(
		"SELECT * FROM products WHERE category = ?",
		[]string{"id", "name", "price"},
		[][]interface{}{
			{1, "Widget", 9.99},
			{2, "Gadget", 19.99},
		},
		"electronics",
	)
	if err != nil {
		log.Fatal(err)
	}

	// Switch to cached mode
	cache.SetMode(sqlcache.ModeCached)

	// Query from cache
	rows, err := cache.Query("SELECT * FROM products WHERE category = ?", "electronics")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Products from cache:")
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

func manualCaptureExample() {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir: "./mocks/manual",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	// Capture various queries
	cache.Capture(
		"SELECT count(*) FROM users",
		[]string{"count"},
		[][]interface{}{{42}},
	)

	cache.Capture(
		"SELECT name FROM users WHERE active = ?",
		[]string{"name"},
		[][]interface{}{{"Alice"}, {"Bob"}, {"Charlie"}},
		true,
	)

	// Capture an exec result
	cache.CaptureExec(
		"INSERT INTO users (name) VALUES (?)",
		100, // lastInsertID
		1,   // rowsAffected
		"NewUser",
	)

	// Cached mode
	cache.SetMode(sqlcache.ModeCached)

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

func cachedOnlyExample() {
	// Create wrapper without database (cached-only)
	db, err := wrapper.NewCachedOnly(wrapper.Options{
		MockDir:        "./mocks",
		SequentialMode: true,
		OnCacheHit: func(query string, args []interface{}, matched bool) {
			if matched {
				fmt.Printf("  [CACHE HIT] %s -> matched\n", truncate(query, 40))
			} else {
				fmt.Printf("  [CACHE MISS] %s -> NOT FOUND\n", truncate(query, 40))
			}
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// First, let's capture some entries manually
	db.Capture(
		"SELECT * FROM config WHERE key = ?",
		[]string{"key", "value"},
		[][]interface{}{{"app_name", "MyApp"}},
		"app_name",
	)

	// Now query - no database needed!
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
