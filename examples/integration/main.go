// Example: Integration test with sql-cache
// This shows how to use sql-cache in integration tests.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	sqlcache "github.com/asish/sql-cache"
	"github.com/asish/sql-cache/wrapper"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("=== Integration Test Example ===")

	// Check if we should record or replay
	mode := os.Getenv("SQL_CACHE_MODE")
	if mode == "" {
		mode = "record"
	}

	fmt.Printf("Mode: %s\n\n", mode)

	switch mode {
	case "record":
		runRecordMode()
	case "replay":
		runReplayMode()
	default:
		log.Fatalf("Unknown mode: %s", mode)
	}
}

func runRecordMode() {
	fmt.Println("Running in RECORD mode - executing against real database")

	// Real database connection
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Setup schema
	setupDatabase(db)

	// Wrap with caching
	cachedDB, err := wrapper.Wrap(db, wrapper.Options{
		MockDir:          "./test-mocks",
		InitialMode:      sqlcache.ModeRecord,
		SequentialReplay: true,
		OnRecord: func(query string, args []interface{}) {
			fmt.Printf("  Recording: %s\n", truncate(query, 60))
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cachedDB.Close()

	// Run business logic
	runBusinessLogic(cachedDB)

	fmt.Println("\nMocks saved to ./test-mocks/mocks.yaml")
	fmt.Println("Run with SQL_CACHE_MODE=replay to use recorded mocks")
}

func runReplayMode() {
	fmt.Println("Running in REPLAY mode - using recorded mocks (no database)")

	// Create replay-only wrapper
	cachedDB, err := wrapper.NewReplayOnly(wrapper.Options{
		MockDir:          "./test-mocks",
		SequentialReplay: true,
		OnReplay: func(query string, args []interface{}, matched bool) {
			status := "✓"
			if !matched {
				status = "✗"
			}
			fmt.Printf("  [%s] %s\n", status, truncate(query, 60))
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cachedDB.Close()

	// Run same business logic - but now without database!
	runBusinessLogic(cachedDB)

	// Print stats
	stats := cachedDB.Stats()
	fmt.Printf("\nReplay Stats: hits=%d, misses=%d, hit_rate=%.1f%%\n",
		stats.Hits, stats.Misses, stats.HitRate*100)
}

func setupDatabase(db *sql.DB) {
	queries := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT, active BOOLEAN)`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, total DECIMAL, status TEXT)`,
		`INSERT INTO users (name, email, active) VALUES ('Alice', 'alice@example.com', 1)`,
		`INSERT INTO users (name, email, active) VALUES ('Bob', 'bob@example.com', 1)`,
		`INSERT INTO users (name, email, active) VALUES ('Charlie', 'charlie@example.com', 0)`,
		`INSERT INTO orders (user_id, total, status) VALUES (1, 100.00, 'completed')`,
		`INSERT INTO orders (user_id, total, status) VALUES (1, 50.00, 'pending')`,
		`INSERT INTO orders (user_id, total, status) VALUES (2, 75.00, 'completed')`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			log.Printf("Warning: %v", err)
		}
	}
}

func runBusinessLogic(db *wrapper.DB) {
	fmt.Println("\nExecuting business logic:")

	// Get active users
	fmt.Println("\n1. Get active users count:")
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM users WHERE active = ?", true)
	if err := row.Scan(&count); err != nil {
		fmt.Printf("   Error: %v\n", err)
	} else {
		fmt.Printf("   Active users: %d\n", count)
	}

	// Get user details
	fmt.Println("\n2. Get user by ID:")
	var name, email string
	row = db.QueryRow("SELECT name, email FROM users WHERE id = ?", 1)
	if err := row.Scan(&name, &email); err != nil {
		fmt.Printf("   Error: %v\n", err)
	} else {
		fmt.Printf("   User: %s <%s>\n", name, email)
	}

	// Get orders for user
	fmt.Println("\n3. Get orders for user:")
	rows, err := db.Query("SELECT id, total, status FROM orders WHERE user_id = ?", 1)
	if err != nil {
		fmt.Printf("   Error: %v\n", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var id int
			var total float64
			var status string
			if err := rows.Scan(&id, &total, &status); err != nil {
				fmt.Printf("   Error: %v\n", err)
			} else {
				fmt.Printf("   Order #%d: $%.2f (%s)\n", id, total, status)
			}
		}
	}

	// Get order totals
	fmt.Println("\n4. Get total revenue:")
	var total float64
	row = db.QueryRow("SELECT COALESCE(SUM(total), 0) FROM orders WHERE status = ?", "completed")
	if err := row.Scan(&total); err != nil {
		fmt.Printf("   Error: %v\n", err)
	} else {
		fmt.Printf("   Total completed: $%.2f\n", total)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
