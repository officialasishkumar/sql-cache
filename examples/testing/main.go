// Example: Using sql-cache in a service
// This shows how to use sql-cache for services without a database.
package main

import (
	"fmt"
	"log"

	sqlcache "github.com/officialasishkumar/sql-cache"
	"github.com/officialasishkumar/sql-cache/wrapper"
)

func main() {
	fmt.Println("=== Service Cache Example ===")

	// Example 1: Service with cached data
	fmt.Println("Example 1: User Service with Cached Database")
	testUserService()

	// Example 2: Error handling
	fmt.Println("\nExample 2: Error Handling")
	testErrorHandling()

	// Example 3: Sequential consumption
	fmt.Println("\nExample 3: Sequential Consumption")
	testSequentialConsumption()
}

// =============================================================================
// Example Service Being Tested
// =============================================================================

type UserService struct {
	db *wrapper.DB
}

type User struct {
	ID    int
	Name  string
	Email string
}

func (s *UserService) GetUser(id int) (*User, error) {
	row := s.db.QueryRow("SELECT id, name, email FROM users WHERE id = ?", id)
	user := &User{}
	if err := row.Scan(&user.ID, &user.Name, &user.Email); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserService) GetAllUsers() ([]*User, error) {
	rows, err := s.db.Query("SELECT id, name, email FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user := &User{}
		if err := rows.Scan(&user.ID, &user.Name, &user.Email); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *UserService) CreateUser(name, email string) (int64, error) {
	result, err := s.db.Exec("INSERT INTO users (name, email) VALUES (?, ?)", name, email)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// =============================================================================
// Tests
// =============================================================================

func testUserService() {
	// Create a cached database (no real DB needed!)
	db, err := wrapper.NewCachedOnly(wrapper.Options{
		MockDir:        "./test-mocks/user-service",
		SequentialMode: false, // Allow reusing cache entries
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Setup cache data
	db.Capture(
		"SELECT id, name, email FROM users WHERE id = ?",
		[]string{"id", "name", "email"},
		[][]interface{}{{1, "Alice", "alice@example.com"}},
		1,
	)

	db.Capture(
		"SELECT id, name, email FROM users",
		[]string{"id", "name", "email"},
		[][]interface{}{
			{1, "Alice", "alice@example.com"},
			{2, "Bob", "bob@example.com"},
		},
	)

	db.CaptureExec(
		"INSERT INTO users (name, email) VALUES (?, ?)",
		3,  // lastInsertID
		1,  // rowsAffected
		"Charlie", "charlie@example.com",
	)

	// Create service with cached DB
	service := &UserService{db: db}

	// Test GetUser
	user, err := service.GetUser(1)
	if err != nil {
		fmt.Printf("  FAIL: GetUser error: %v\n", err)
	} else if user.Name != "Alice" {
		fmt.Printf("  FAIL: Expected Alice, got %s\n", user.Name)
	} else {
		fmt.Printf("  PASS: GetUser - %s <%s>\n", user.Name, user.Email)
	}

	// Test GetAllUsers
	users, err := service.GetAllUsers()
	if err != nil {
		fmt.Printf("  FAIL: GetAllUsers error: %v\n", err)
	} else if len(users) != 2 {
		fmt.Printf("  FAIL: Expected 2 users, got %d\n", len(users))
	} else {
		fmt.Printf("  PASS: GetAllUsers - %d users\n", len(users))
	}

	// Test CreateUser
	id, err := service.CreateUser("Charlie", "charlie@example.com")
	if err != nil {
		fmt.Printf("  FAIL: CreateUser error: %v\n", err)
	} else if id != 3 {
		fmt.Printf("  FAIL: Expected ID 3, got %d\n", id)
	} else {
		fmt.Printf("  PASS: CreateUser - new ID: %d\n", id)
	}
}

func testErrorHandling() {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir: "./test-mocks/errors",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	// Capture an error response
	cache.CaptureError(
		"SELECT * FROM nonexistent",
		"table not found: nonexistent",
	)

	cache.SetMode(sqlcache.ModeCached)

	// Query should return the cached error
	_, err = cache.Query("SELECT * FROM nonexistent")
	if err != nil && err.Error() == "table not found: nonexistent" {
		fmt.Printf("  PASS: Got expected error: %v\n", err)
	} else {
		fmt.Printf("  FAIL: Unexpected result: %v\n", err)
	}

	// Query for uncached query should fail
	_, err = cache.Query("SELECT * FROM another_table")
	if err != nil {
		fmt.Printf("  PASS: Got error for unknown query: %v\n", truncate(err.Error(), 40))
	} else {
		fmt.Printf("  FAIL: Expected error for unknown query\n")
	}
}

func testSequentialConsumption() {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir:        "./test-mocks/sequential",
		SequentialMode: true, // Each cache entry can only be used once
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	// Clear any existing entries from previous runs
	cache.Clear()

	// Capture same query multiple times with different results
	cache.Capture(
		"SELECT value FROM counter",
		[]string{"value"},
		[][]interface{}{{1}},
	)
	cache.Capture(
		"SELECT value FROM counter",
		[]string{"value"},
		[][]interface{}{{2}},
	)
	cache.Capture(
		"SELECT value FROM counter",
		[]string{"value"},
		[][]interface{}{{3}},
	)

	cache.SetMode(sqlcache.ModeCached)

	// Each query consumes the next entry in sequence
	for i := 1; i <= 3; i++ {
		rows, err := cache.Query("SELECT value FROM counter")
		if err != nil {
			fmt.Printf("  FAIL: Query %d error: %v\n", i, err)
			continue
		}
		if rows.Next() {
			var value int
			rows.Scan(&value)
			if value == i {
				fmt.Printf("  PASS: Query %d returned %d\n", i, value)
			} else {
				fmt.Printf("  FAIL: Query %d expected %d, got %d\n", i, i, value)
			}
		}
	}

	// Fourth query should fail (no more entries)
	_, err = cache.Query("SELECT value FROM counter")
	if err != nil {
		fmt.Printf("  PASS: Fourth query correctly failed (cache entries exhausted)\n")
	} else {
		fmt.Printf("  FAIL: Fourth query should have failed\n")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
