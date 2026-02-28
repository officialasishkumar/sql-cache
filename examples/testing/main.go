// Example: Unit testing with sql-cache
// This shows how to use sql-cache for unit tests without a database.
package main

import (
	"fmt"
	"log"

	sqlcache "github.com/asish/sql-cache"
	"github.com/asish/sql-cache/wrapper"
)

func main() {
	fmt.Println("=== Unit Testing Example ===")

	// Test 1: Test with mocked data
	fmt.Println("Test 1: User Service with Mocked Database")
	testUserService()

	// Test 2: Test error handling
	fmt.Println("\nTest 2: Error Handling")
	testErrorHandling()

	// Test 3: Test sequential replay
	fmt.Println("\nTest 3: Sequential Replay")
	testSequentialReplay()
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
	// Create a mock database (no real DB needed!)
	db, err := wrapper.NewReplayOnly(wrapper.Options{
		MockDir:          "./test-mocks/user-service",
		SequentialReplay: false, // Allow reusing mocks
	})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Setup mock data
	db.Record(
		"SELECT id, name, email FROM users WHERE id = ?",
		[]string{"id", "name", "email"},
		[][]interface{}{{1, "Alice", "alice@example.com"}},
		1,
	)

	db.Record(
		"SELECT id, name, email FROM users",
		[]string{"id", "name", "email"},
		[][]interface{}{
			{1, "Alice", "alice@example.com"},
			{2, "Bob", "bob@example.com"},
		},
	)

	db.RecordExec(
		"INSERT INTO users (name, email) VALUES (?, ?)",
		3,  // lastInsertID
		1,  // rowsAffected
		"Charlie", "charlie@example.com",
	)

	// Create service with mock DB
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

	// Record an error response
	cache.RecordError(
		"SELECT * FROM nonexistent",
		"table not found: nonexistent",
	)

	cache.SetMode(sqlcache.ModeReplay)

	// Query should return the recorded error
	_, err = cache.Query("SELECT * FROM nonexistent")
	if err != nil && err.Error() == "table not found: nonexistent" {
		fmt.Printf("  PASS: Got expected error: %v\n", err)
	} else {
		fmt.Printf("  FAIL: Unexpected result: %v\n", err)
	}

	// Query for unrecorded query should fail
	_, err = cache.Query("SELECT * FROM another_table")
	if err != nil {
		fmt.Printf("  PASS: Got error for unknown query: %v\n", truncate(err.Error(), 40))
	} else {
		fmt.Printf("  FAIL: Expected error for unknown query\n")
	}
}

func testSequentialReplay() {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir:          "./test-mocks/sequential",
		SequentialReplay: true, // Each mock can only be used once
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cache.Close()

	// Clear any existing mocks from previous runs
	cache.Clear()

	// Record same query multiple times with different results
	cache.Record(
		"SELECT value FROM counter",
		[]string{"value"},
		[][]interface{}{{1}},
	)
	cache.Record(
		"SELECT value FROM counter",
		[]string{"value"},
		[][]interface{}{{2}},
	)
	cache.Record(
		"SELECT value FROM counter",
		[]string{"value"},
		[][]interface{}{{3}},
	)

	cache.SetMode(sqlcache.ModeReplay)

	// Each query consumes the next mock in sequence
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

	// Fourth query should fail (no more mocks)
	_, err = cache.Query("SELECT value FROM counter")
	if err != nil {
		fmt.Printf("  PASS: Fourth query correctly failed (mocks exhausted)\n")
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
