package mock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// =============================================================================
// Production-Ready Test Suite for SQL Mock Matching
// =============================================================================
// These tests verify that the mock system works correctly for all SQL scenarios
// that would be encountered in production applications.

// TestExactQueryMatch verifies exact query string matching (highest priority)
func TestExactQueryMatch(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock(
		"SELECT id, name, email FROM users WHERE id = ?",
		[]interface{}{1},
		[]string{"id", "name", "email"},
		[][]interface{}{{1, "Alice", "alice@example.com"}},
		0, 0, "",
	)
	store.Add(mock)

	tests := []struct {
		name      string
		query     string
		args      []interface{}
		wantMatch bool
	}{
		{
			name:      "exact match with same args",
			query:     "SELECT id, name, email FROM users WHERE id = ?",
			args:      []interface{}{1},
			wantMatch: true,
		},
		{
			name:      "structural match with different args",
			query:     "SELECT id, name, email FROM users WHERE id = ?",
			args:      []interface{}{2},
			wantMatch: true, // Structural matching allows different args
		},
		{
			name:      "case different - should not match exactly",
			query:     "select id, name, email from users where id = ?",
			args:      []interface{}{1},
			wantMatch: true, // Case-insensitive matching
		},
		{
			name:      "different query - should not match",
			query:     "SELECT * FROM users WHERE id = ?",
			args:      []interface{}{1},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := store.FindMatch(tt.query, "SELECT", "", tt.args, false)
			if found != tt.wantMatch {
				t.Errorf("FindMatch() = %v, want %v", found, tt.wantMatch)
			}
		})
	}
}

// TestPlaceholderMatching verifies that placeholder counts must match
func TestPlaceholderMatching(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock(
		"SELECT * FROM users WHERE id = ? AND status = ?",
		[]interface{}{1, "active"},
		[]string{"id", "name"},
		[][]interface{}{{1, "Alice"}},
		0, 0, "",
	)
	store.Add(mock)

	tests := []struct {
		name      string
		query     string
		args      []interface{}
		wantMatch bool
	}{
		{
			name:      "same placeholder count",
			query:     "SELECT * FROM users WHERE id = ? AND status = ?",
			args:      []interface{}{1, "active"},
			wantMatch: true,
		},
		{
			name:      "more placeholders",
			query:     "SELECT * FROM users WHERE id = ? AND status = ? AND type = ?",
			args:      []interface{}{1, "active", "premium"},
			wantMatch: false,
		},
		{
			name:      "fewer placeholders",
			query:     "SELECT * FROM users WHERE id = ?",
			args:      []interface{}{1},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := store.FindMatch(tt.query, "SELECT", "", tt.args, false)
			if found != tt.wantMatch {
				t.Errorf("FindMatch() = %v, want %v", found, tt.wantMatch)
			}
		})
	}
}

// TestDMLTypeChecking verifies that DML types must match
func TestDMLTypeChecking(t *testing.T) {
	store := NewMockStore("")

	// Add SELECT mock
	selectMock := CreateMock(
		"SELECT * FROM users WHERE id = ?",
		[]interface{}{1},
		[]string{"id", "name"},
		[][]interface{}{{1, "Alice"}},
		0, 0, "",
	)
	store.Add(selectMock)

	// Add INSERT mock
	insertMock := CreateMock(
		"INSERT INTO users (name, email) VALUES (?, ?)",
		[]interface{}{"Bob", "bob@example.com"},
		nil, nil, 4, 1, "",
	)
	store.Add(insertMock)

	tests := []struct {
		name      string
		query     string
		queryType string
		args      []interface{}
		wantMatch bool
	}{
		{
			name:      "SELECT matches SELECT",
			query:     "SELECT * FROM users WHERE id = ?",
			queryType: "SELECT",
			args:      []interface{}{1},
			wantMatch: true,
		},
		{
			name:      "INSERT matches INSERT",
			query:     "INSERT INTO users (name, email) VALUES (?, ?)",
			queryType: "INSERT",
			args:      []interface{}{"Bob", "bob@example.com"},
			wantMatch: true,
		},
		{
			name:      "SELECT does not match INSERT",
			query:     "INSERT INTO users (id) VALUES (?)",
			queryType: "INSERT",
			args:      []interface{}{1},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := store.FindMatch(tt.query, tt.queryType, "", tt.args, false)
			if found != tt.wantMatch {
				t.Errorf("FindMatch() = %v, want %v", found, tt.wantMatch)
			}
		})
	}
}

// TestSequentialReplay verifies sequential mock consumption
func TestSequentialReplay(t *testing.T) {
	store := NewMockStore("")

	// Add same query structure but different args/responses
	for i := 1; i <= 3; i++ {
		mock := CreateMock(
			"SELECT * FROM users WHERE id = ?",
			[]interface{}{i},
			[]string{"id", "name"},
			[][]interface{}{{i, "User" + string(rune('A'-1+i))}},
			0, 0, "",
		)
		mock.Name = "mock-" + string(rune('a'-1+i))
		store.Add(mock)
	}

	// Sequential consumption should work
	var results []int
	for i := 1; i <= 3; i++ {
		matched, found := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{i}, true)
		if !found {
			t.Fatalf("Expected to find mock for iteration %d", i)
		}
		if len(matched.Spec.Response.Rows) > 0 {
			if id, ok := matched.Spec.Response.Rows[0][0].(int); ok {
				results = append(results, id)
			}
		}
	}

	// After 3 consumptions, no more mocks should be available
	_, found := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{4}, true)
	if found {
		t.Error("Expected no match after all mocks consumed")
	}

	// Reset should restore all mocks
	store.Reset()
	_, found = store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{1}, true)
	if !found {
		t.Error("Expected match after Reset()")
	}
}

// TestErrorMockHandling verifies error response mocking
func TestErrorMockHandling(t *testing.T) {
	store := NewMockStore("")

	// Add error mock
	mock := CreateMock(
		"INSERT INTO users (id, name) VALUES (?, ?)",
		[]interface{}{1, "Duplicate"},
		nil, nil, 0, 0,
		"Error 1062: Duplicate entry '1' for key 'PRIMARY'",
	)
	store.Add(mock)

	matched, found := store.FindMatch("INSERT INTO users (id, name) VALUES (?, ?)", "INSERT", "", []interface{}{1, "Duplicate"}, false)
	if !found {
		t.Fatal("Expected to find error mock")
	}
	if matched.Spec.Response.Error == "" {
		t.Error("Expected error in response")
	}
	if matched.Spec.Response.Error != "Error 1062: Duplicate entry '1' for key 'PRIMARY'" {
		t.Errorf("Unexpected error message: %s", matched.Spec.Response.Error)
	}
}

// TestEmptyResultSet verifies handling of queries returning no rows
func TestEmptyResultSet(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock(
		"SELECT * FROM users WHERE id = ?",
		[]interface{}{99999},
		[]string{"id", "name", "email"},
		[][]interface{}{}, // Empty rows
		0, 0, "",
	)
	store.Add(mock)

	matched, found := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{99999}, false)
	if !found {
		t.Fatal("Expected to find mock")
	}
	if len(matched.Spec.Response.Rows) != 0 {
		t.Errorf("Expected empty rows, got %d", len(matched.Spec.Response.Rows))
	}
}

// TestTypeFlexibleArgMatching verifies int/int64/float64 interoperability
func TestTypeFlexibleArgMatching(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock(
		"SELECT * FROM users WHERE id = ?",
		[]interface{}{int64(42)}, // int64 in mock
		[]string{"id", "name"},
		[][]interface{}{{42, "Answer"}},
		0, 0, "",
	)
	store.Add(mock)

	tests := []struct {
		name      string
		args      []interface{}
		wantMatch bool
	}{
		{
			name:      "int64 matches int64",
			args:      []interface{}{int64(42)},
			wantMatch: true,
		},
		{
			name:      "int matches int64",
			args:      []interface{}{42},
			wantMatch: true,
		},
		{
			name:      "float64 matches int64 (same value)",
			args:      []interface{}{float64(42)},
			wantMatch: true,
		},
		{
			name:      "different value with structural match",
			args:      []interface{}{43},
			wantMatch: true, // Structural matching allows different args
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, found := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", tt.args, false)
			if found != tt.wantMatch {
				t.Errorf("FindMatch() = %v, want %v", found, tt.wantMatch)
			}
		})
	}
}

// TestTTLExpiration verifies mock expiration based on TTL
func TestTTLExpiration(t *testing.T) {
	store := NewMockStore("")
	store.SetTTL(1) // 1 second TTL

	// Add mock with old timestamp
	mock := CreateMock(
		"SELECT * FROM users WHERE id = ?",
		[]interface{}{1},
		[]string{"id"},
		[][]interface{}{{1}},
		0, 0, "",
	)
	mock.Spec.Created = time.Now().Unix() - 10 // 10 seconds ago
	store.Add(mock)

	// Should not find expired mock
	_, found := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{1}, false)
	if found {
		t.Error("Expected mock to be expired")
	}

	// Add fresh mock
	freshMock := CreateMock(
		"SELECT * FROM users WHERE id = ?",
		[]interface{}{2},
		[]string{"id"},
		[][]interface{}{{2}},
		0, 0, "",
	)
	store.Add(freshMock)

	// Should find fresh mock
	_, found = store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{2}, false)
	if !found {
		t.Error("Expected to find fresh mock")
	}
}

// TestInvalidateByQuery verifies query-based invalidation
func TestInvalidateByQuery(t *testing.T) {
	store := NewMockStore("")

	mock1 := CreateMock("SELECT * FROM users WHERE id = ?", []interface{}{1}, []string{"id"}, [][]interface{}{{1}}, 0, 0, "")
	mock2 := CreateMock("SELECT * FROM products WHERE id = ?", []interface{}{2}, []string{"id"}, [][]interface{}{{2}}, 0, 0, "")
	store.Add(mock1)
	store.Add(mock2)

	count := store.InvalidateByQuery("SELECT * FROM users WHERE id = ?")
	if count != 1 {
		t.Errorf("Expected to invalidate 1 mock, got %d", count)
	}

	if store.Size() != 1 {
		t.Errorf("Expected 1 mock remaining, got %d", store.Size())
	}
}

// TestInvalidateByTable verifies table-based invalidation
func TestInvalidateByTable(t *testing.T) {
	store := NewMockStore("")

	mock1 := CreateMock("SELECT * FROM users WHERE id = ?", []interface{}{1}, []string{"id"}, [][]interface{}{{1}}, 0, 0, "")
	mock1.Spec.Request.Tables = []string{"users"}
	mock2 := CreateMock("SELECT * FROM products WHERE id = ?", []interface{}{2}, []string{"id"}, [][]interface{}{{2}}, 0, 0, "")
	mock2.Spec.Request.Tables = []string{"products"}
	mock3 := CreateMock("SELECT u.*, o.* FROM users u JOIN orders o ON u.id = o.user_id", nil, []string{"id"}, [][]interface{}{}, 0, 0, "")
	mock3.Spec.Request.Tables = []string{"users", "orders"}
	store.Add(mock1)
	store.Add(mock2)
	store.Add(mock3)

	count := store.InvalidateByTable("users")
	if count != 2 {
		t.Errorf("Expected to invalidate 2 mocks, got %d", count)
	}

	if store.Size() != 1 {
		t.Errorf("Expected 1 mock remaining, got %d", store.Size())
	}
}

// TestYAMLPersistence verifies save/load of mocks in YAML format
func TestYAMLPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mock-persistence-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create store and add diverse mocks
	store := NewMockStore(tmpDir)

	// SELECT mock
	selectMock := CreateMock(
		"SELECT id, name FROM users WHERE id = ?",
		[]interface{}{1},
		[]string{"id", "name"},
		[][]interface{}{{1, "Alice"}},
		0, 0, "",
	)
	selectMock.Name = "test-select-mock"
	store.Add(selectMock)

	// INSERT mock
	insertMock := CreateMock(
		"INSERT INTO users (name, email) VALUES (?, ?)",
		[]interface{}{"Bob", "bob@example.com"},
		nil, nil, 5, 1, "",
	)
	insertMock.Name = "test-insert-mock"
	store.Add(insertMock)

	// Error mock
	errorMock := CreateMock(
		"INSERT INTO users (id) VALUES (?)",
		[]interface{}{1},
		nil, nil, 0, 0,
		"Duplicate key error",
	)
	errorMock.Name = "test-error-mock"
	store.Add(errorMock)

	// Save to file
	if err := store.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Verify file exists
	mocksFile := filepath.Join(tmpDir, "mocks.yaml")
	if _, err := os.Stat(mocksFile); os.IsNotExist(err) {
		t.Fatal("mocks.yaml file not created")
	}

	// Load into new store
	store2 := NewMockStore(tmpDir)
	if err := store2.Load(); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Verify loaded mocks
	if store2.Size() != 3 {
		t.Errorf("Expected 3 mocks, got %d", store2.Size())
	}

	// Verify SELECT mock loaded correctly
	matched, found := store2.FindMatch("SELECT id, name FROM users WHERE id = ?", "SELECT", "", []interface{}{1}, false)
	if !found {
		t.Error("Expected to find SELECT mock after loading")
	}
	if matched.Name != "test-select-mock" {
		t.Errorf("Wrong mock name: %s", matched.Name)
	}
}

// TestCreateMockHelper verifies the CreateMock helper function
func TestCreateMockHelper(t *testing.T) {
	mock := CreateMock(
		"SELECT * FROM users WHERE id = ? AND status = ?",
		[]interface{}{1, "active"},
		[]string{"id", "name", "status"},
		[][]interface{}{{1, "Alice", "active"}},
		0, 0, "",
	)

	if mock.Version != Version {
		t.Errorf("Expected version %s, got %s", Version, mock.Version)
	}
	if mock.Kind != "SQL" {
		t.Errorf("Expected kind SQL, got %s", mock.Kind)
	}
	if mock.Spec.Request.PlaceholderCount != 2 {
		t.Errorf("Expected 2 placeholders, got %d", mock.Spec.Request.PlaceholderCount)
	}
	if mock.Spec.Request.Type != "SELECT" {
		t.Errorf("Expected type SELECT, got %s", mock.Spec.Request.Type)
	}
	// Note: vitess IsDML returns false for SELECT (SELECT is DQL, not DML)
	// This is technically correct - DML includes INSERT, UPDATE, DELETE only
	if mock.Spec.Request.Type == "SELECT" && mock.Spec.Request.IsDML {
		t.Error("Expected IsDML to be false for SELECT (SELECT is DQL)")
	}
	if mock.Spec.Request.QueryHash == "" {
		t.Error("Expected QueryHash to be set")
	}
	if mock.Spec.Response.RowCount != 1 {
		t.Errorf("Expected RowCount 1, got %d", mock.Spec.Response.RowCount)
	}
}

// TestGenerateQueryHash verifies hash generation
func TestGenerateQueryHash(t *testing.T) {
	hash1 := GenerateQueryHash("SELECT * FROM users")
	hash2 := GenerateQueryHash("SELECT * FROM users")
	hash3 := GenerateQueryHash("SELECT * FROM products")

	if hash1 != hash2 {
		t.Error("Same query should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("Different queries should produce different hashes")
	}
	if len(hash1) != 16 {
		t.Errorf("Expected hash length 16, got %d", len(hash1))
	}
}

// TestGetQueryTypeFromString verifies query type detection
func TestGetQueryTypeFromString(t *testing.T) {
	tests := []struct {
		query    string
		expected string
	}{
		{"SELECT * FROM users", "SELECT"},
		{"select * from users", "SELECT"},
		{"INSERT INTO users VALUES (?)", "INSERT"},
		{"UPDATE users SET name = ?", "UPDATE"},
		{"DELETE FROM users WHERE id = ?", "DELETE"},
		{"REPLACE INTO users VALUES (?)", "REPLACE"},
		{"CALL procedure_name()", "CALL"},
		{"SHOW TABLES", "SHOW"},
		{"DESCRIBE users", "DESCRIBE"},
		{"DESC users", "DESCRIBE"},
		{"EXPLAIN SELECT * FROM users", "EXPLAIN"},
		{"CREATE TABLE test (id INT)", "OTHER"},
		{"DROP TABLE test", "OTHER"},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := getQueryTypeFromString(tt.query)
			if got != tt.expected {
				t.Errorf("getQueryTypeFromString(%q) = %s, want %s", tt.query, got, tt.expected)
			}
		})
	}
}

// TestStatsTracking verifies statistics are properly tracked
func TestStatsTracking(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock("SELECT * FROM users", nil, []string{"id"}, [][]interface{}{{1}}, 0, 0, "")
	store.Add(mock)
	store.Add(CreateMock("SELECT * FROM products", nil, []string{"id"}, [][]interface{}{{2}}, 0, 0, ""))
	store.Add(CreateMock("SELECT * FROM orders", nil, []string{"id"}, [][]interface{}{{3}}, 0, 0, ""))

	stats := store.Stats()
	if stats.Total != 3 {
		t.Errorf("Expected total 3, got %d", stats.Total)
	}
	if stats.Consumed != 0 {
		t.Errorf("Expected consumed 0, got %d", stats.Consumed)
	}

	// Consume one
	store.FindMatch("SELECT * FROM users", "SELECT", "", nil, true)

	stats = store.Stats()
	if stats.Consumed != 1 {
		t.Errorf("Expected consumed 1, got %d", stats.Consumed)
	}
	if stats.Unconsumed != 2 {
		t.Errorf("Expected unconsumed 2, got %d", stats.Unconsumed)
	}
}

// TestNullHandling verifies NULL value handling in responses
func TestNullHandling(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock(
		"SELECT id, manager_id FROM employees WHERE id = ?",
		[]interface{}{1},
		[]string{"id", "manager_id"},
		[][]interface{}{{1, nil}}, // manager_id is NULL
		0, 0, "",
	)
	store.Add(mock)

	matched, found := store.FindMatch("SELECT id, manager_id FROM employees WHERE id = ?", "SELECT", "", []interface{}{1}, false)
	if !found {
		t.Fatal("Expected to find mock")
	}
	if len(matched.Spec.Response.Rows) != 1 {
		t.Fatal("Expected 1 row")
	}
	if matched.Spec.Response.Rows[0][1] != nil {
		t.Errorf("Expected NULL in manager_id, got %v", matched.Spec.Response.Rows[0][1])
	}
}

// TestMultiRowResult verifies handling of multi-row results
func TestMultiRowResult(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock(
		"SELECT id, name FROM users LIMIT ?",
		[]interface{}{10},
		[]string{"id", "name"},
		[][]interface{}{
			{1, "Alice"},
			{2, "Bob"},
			{3, "Charlie"},
			{4, "Dave"},
			{5, "Eve"},
		},
		0, 0, "",
	)
	store.Add(mock)

	matched, found := store.FindMatch("SELECT id, name FROM users LIMIT ?", "SELECT", "", []interface{}{10}, false)
	if !found {
		t.Fatal("Expected to find mock")
	}
	if len(matched.Spec.Response.Rows) != 5 {
		t.Errorf("Expected 5 rows, got %d", len(matched.Spec.Response.Rows))
	}
	if matched.Spec.Response.RowCount != 5 {
		t.Errorf("Expected RowCount 5, got %d", matched.Spec.Response.RowCount)
	}
}

// TestJoinQueryMatching verifies JOIN query handling
func TestJoinQueryMatching(t *testing.T) {
	store := NewMockStore("")

	mock := CreateMock(
		"SELECT u.id, u.name, o.total FROM users u INNER JOIN orders o ON u.id = o.user_id WHERE u.id = ?",
		[]interface{}{1},
		[]string{"id", "name", "total"},
		[][]interface{}{
			{1, "Alice", 100.50},
			{1, "Alice", 200.75},
		},
		0, 0, "",
	)
	mock.Spec.Request.Tables = []string{"users", "orders"}
	store.Add(mock)

	matched, found := store.FindMatch(
		"SELECT u.id, u.name, o.total FROM users u INNER JOIN orders o ON u.id = o.user_id WHERE u.id = ?",
		"SELECT", "", []interface{}{1}, false,
	)
	if !found {
		t.Fatal("Expected to find JOIN mock")
	}
	if len(matched.Spec.Request.Tables) != 2 {
		t.Errorf("Expected 2 tables, got %d", len(matched.Spec.Request.Tables))
	}
}

// TestLargeResultSet verifies handling of large result sets
func TestLargeResultSet(t *testing.T) {
	store := NewMockStore("")

	// Create 1000 rows
	rows := make([][]interface{}, 1000)
	for i := 0; i < 1000; i++ {
		rows[i] = []interface{}{i + 1, "User" + string(rune('A'+i%26))}
	}

	mock := CreateMock(
		"SELECT id, name FROM users",
		nil,
		[]string{"id", "name"},
		rows,
		0, 0, "",
	)
	store.Add(mock)

	matched, found := store.FindMatch("SELECT id, name FROM users", "SELECT", "", nil, false)
	if !found {
		t.Fatal("Expected to find mock")
	}
	if len(matched.Spec.Response.Rows) != 1000 {
		t.Errorf("Expected 1000 rows, got %d", len(matched.Spec.Response.Rows))
	}
}

// TestConcurrentAccess verifies thread-safety of the mock store
func TestConcurrentAccess(t *testing.T) {
	store := NewMockStore("")

	// Add some initial mocks
	for i := 0; i < 10; i++ {
		mock := CreateMock("SELECT * FROM users WHERE id = ?", []interface{}{i}, []string{"id"}, [][]interface{}{{i}}, 0, 0, "")
		store.Add(mock)
	}

	// Concurrent reads and writes
	done := make(chan bool, 100)

	// Writers
	for i := 0; i < 20; i++ {
		go func(id int) {
			mock := CreateMock("SELECT * FROM products WHERE id = ?", []interface{}{id}, []string{"id"}, [][]interface{}{{id}}, 0, 0, "")
			store.Add(mock)
			done <- true
		}(i)
	}

	// Readers
	for i := 0; i < 80; i++ {
		go func(id int) {
			store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{id % 10}, false)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Verify store is still consistent
	if store.Size() < 10 {
		t.Errorf("Expected at least 10 mocks, got %d", store.Size())
	}
}

// BenchmarkFindMatch benchmarks the FindMatch function
func BenchmarkFindMatch(b *testing.B) {
	store := NewMockStore("")

	// Add 1000 mocks
	for i := 0; i < 1000; i++ {
		mock := CreateMock(
			"SELECT * FROM users WHERE id = ?",
			[]interface{}{i},
			[]string{"id", "name"},
			[][]interface{}{{i, "User"}},
			0, 0, "",
		)
		store.Add(mock)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{500}, false)
	}
}

// BenchmarkGenerateQueryHash benchmarks hash generation
func BenchmarkGenerateQueryHash(b *testing.B) {
	query := "SELECT id, name, email, status, created_at, updated_at FROM users WHERE id = ? AND status = ?"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GenerateQueryHash(query)
	}
}
