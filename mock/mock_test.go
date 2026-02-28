package mock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMockStore_FindMatch_ExactQuery(t *testing.T) {
	store := NewMockStore("")

	// Add a mock
	mock := &Mock{
		Version: Version,
		Kind:    "SQL",
		Name:    "test-mock",
		Spec: MockSpec{
			Request: RequestSpec{
				Query: "SELECT * FROM users WHERE id = ?",
				Args:  []interface{}{1},
				Type:  "SELECT",
			},
			Response: ResponseSpec{
				Columns: []string{"id", "name"},
				Rows:    [][]interface{}{{1, "Alice"}},
			},
		},
	}
	store.Add(mock)

	// Test exact match
	found, ok := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{1}, false)
	if !ok {
		t.Fatal("Expected to find a match")
	}
	if found.Spec.Response.Columns[0] != "id" {
		t.Errorf("Expected column 'id', got %s", found.Spec.Response.Columns[0])
	}
}

func TestMockStore_FindMatch_StructuralMatch(t *testing.T) {
	store := NewMockStore("")

	// Add a mock with structure
	structure := "*sqlparser.Select->*sqlparser.AliasedTableExpr->sqlparser.TableName"
	mock := &Mock{
		Version: Version,
		Kind:    "SQL",
		Name:    "test-mock-structure",
		Spec: MockSpec{
			Request: RequestSpec{
				Query:     "SELECT * FROM users WHERE id = ?",
				Args:      []interface{}{1},
				Type:      "SELECT",
				Structure: structure,
			},
			Response: ResponseSpec{
				Columns: []string{"id", "name"},
				Rows:    [][]interface{}{{1, "Alice"}},
			},
		},
	}
	store.Add(mock)

	// Test structural match (different value, same placeholders)
	found, ok := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", structure, []interface{}{2}, false)
	if !ok {
		t.Fatal("Expected to find a structural match")
	}
	if found.Name != "test-mock-structure" {
		t.Errorf("Expected mock name 'test-mock-structure', got %s", found.Name)
	}
}

func TestMockStore_FindMatch_PlaceholderMismatch(t *testing.T) {
	store := NewMockStore("")

	// Add a mock with one placeholder
	mock := &Mock{
		Version: Version,
		Kind:    "SQL",
		Name:    "test-mock",
		Spec: MockSpec{
			Request: RequestSpec{
				Query: "SELECT * FROM users WHERE id = ?",
				Args:  []interface{}{1},
				Type:  "SELECT",
			},
			Response: ResponseSpec{
				Columns: []string{"id", "name"},
				Rows:    [][]interface{}{{1, "Alice"}},
			},
		},
	}
	store.Add(mock)

	// Try to match with different placeholder count - should not match
	_, ok := store.FindMatch("SELECT * FROM users WHERE id = ? AND name = ?", "SELECT", "", []interface{}{1, "Alice"}, false)
	if ok {
		t.Error("Expected no match due to placeholder count mismatch")
	}
}

func TestMockStore_FindMatch_Sequential(t *testing.T) {
	store := NewMockStore("")

	// Add two mocks with different names (they have same query structure)
	for i := 0; i < 2; i++ {
		mock := &Mock{
			Version: Version,
			Kind:    "SQL",
			Name:    "test-mock-" + string(rune('a'+i)), // Different names
			Spec: MockSpec{
				Request: RequestSpec{
					Query: "SELECT * FROM users WHERE id = ?",
					Args:  []interface{}{i + 1}, // Different args
					Type:  "SELECT",
				},
				Response: ResponseSpec{
					Columns: []string{"id", "name"},
					Rows:    [][]interface{}{{i + 1, "User"}},
				},
			},
		}
		store.Add(mock)
	}

	// First call with consumeOnMatch=true
	found1, ok := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{1}, true)
	if !ok {
		t.Fatal("Expected to find first match")
	}

	// Second call should return the second mock
	found2, ok := store.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{2}, true)
	if !ok {
		t.Fatal("Expected to find second match")
	}

	// Results should be different
	if found1.Spec.Response.Rows[0][0] == found2.Spec.Response.Rows[0][0] {
		t.Error("Sequential matching should return different mocks")
	}
}

func TestMockStore_FindMatch_DMLCheck(t *testing.T) {
	store := NewMockStore("")

	// Add a SELECT mock
	mock := &Mock{
		Version: Version,
		Kind:    "SQL",
		Name:    "test-select",
		Spec: MockSpec{
			Request: RequestSpec{
				Query: "SELECT * FROM users",
				Type:  "SELECT",
			},
			Response: ResponseSpec{
				Columns: []string{"id"},
				Rows:    [][]interface{}{{1}},
			},
		},
	}
	store.Add(mock)

	// Try to match with INSERT (different DML type) - should fail
	_, ok := store.FindMatch("INSERT INTO users (name) VALUES (?)", "INSERT", "", []interface{}{"test"}, false)
	if ok {
		t.Error("Expected no match for different DML types")
	}
}

func TestMockStore_SaveLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mock-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewMockStore(tmpDir)

	// Add mocks
	mock := &Mock{
		Version: Version,
		Kind:    "SQL",
		Name:    "test-save-load",
		Spec: MockSpec{
			Request: RequestSpec{
				Query: "SELECT * FROM users WHERE id = ?",
				Args:  []interface{}{1},
				Type:  "SELECT",
			},
			Response: ResponseSpec{
				Columns: []string{"id", "name"},
				Rows:    [][]interface{}{{1, "Alice"}},
			},
		},
	}
	store.Add(mock)

	// Save
	if err := store.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}

	// Check file exists
	mocksFile := filepath.Join(tmpDir, "mocks.yaml")
	if _, err := os.Stat(mocksFile); os.IsNotExist(err) {
		t.Fatal("mocks.yaml file not created")
	}

	// Create new store and load
	store2 := NewMockStore(tmpDir)
	if err := store2.Load(); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}

	// Verify loaded
	found, ok := store2.FindMatch("SELECT * FROM users WHERE id = ?", "SELECT", "", []interface{}{1}, false)
	if !ok {
		t.Fatal("Expected to find loaded mock")
	}
	if found.Name != "test-save-load" {
		t.Errorf("Expected mock name 'test-save-load', got %s", found.Name)
	}
}

func TestMockStore_InvalidateByQuery(t *testing.T) {
	store := NewMockStore("")

	// Add mocks
	mock1 := &Mock{
		Name: "mock1",
		Spec: MockSpec{
			Request:  RequestSpec{Query: "SELECT * FROM users WHERE id = ?"},
			Response: ResponseSpec{Columns: []string{"id"}},
		},
	}
	mock2 := &Mock{
		Name: "mock2",
		Spec: MockSpec{
			Request:  RequestSpec{Query: "SELECT * FROM products"},
			Response: ResponseSpec{Columns: []string{"id"}},
		},
	}
	store.Add(mock1)
	store.Add(mock2)

	// Invalidate specific query
	count := store.InvalidateByQuery("SELECT * FROM users WHERE id = ?")
	if count != 1 {
		t.Errorf("Expected to invalidate 1 mock, got %d", count)
	}

	// Should only have one mock left
	if store.Size() != 1 {
		t.Errorf("Expected 1 mock remaining, got %d", store.Size())
	}
}

func TestMockStore_InvalidateByTable(t *testing.T) {
	store := NewMockStore("")

	// Add mocks for different tables
	mock1 := &Mock{
		Name: "mock1",
		Spec: MockSpec{
			Request:  RequestSpec{Query: "SELECT * FROM users", Tables: []string{"users"}},
			Response: ResponseSpec{Columns: []string{"id"}},
		},
	}
	mock2 := &Mock{
		Name: "mock2",
		Spec: MockSpec{
			Request:  RequestSpec{Query: "SELECT * FROM products", Tables: []string{"products"}},
			Response: ResponseSpec{Columns: []string{"id"}},
		},
	}
	store.Add(mock1)
	store.Add(mock2)

	// Invalidate by table
	count := store.InvalidateByTable("users")
	if count != 1 {
		t.Errorf("Expected to invalidate 1 mock, got %d", count)
	}

	// Verify correct mock remains
	_, ok := store.FindMatch("SELECT * FROM products", "SELECT", "", nil, false)
	if !ok {
		t.Error("Expected products mock to still exist")
	}
}

func TestMockStore_InvalidateByPattern(t *testing.T) {
	store := NewMockStore("")

	// Add mocks
	mock1 := &Mock{
		Name: "mock1",
		Spec: MockSpec{
			Request:  RequestSpec{Query: "SELECT * FROM users WHERE id = ?"},
			Response: ResponseSpec{Columns: []string{"id"}},
		},
	}
	mock2 := &Mock{
		Name: "mock2",
		Spec: MockSpec{
			Request:  RequestSpec{Query: "SELECT * FROM users WHERE name = ?"},
			Response: ResponseSpec{Columns: []string{"id"}},
		},
	}
	mock3 := &Mock{
		Name: "mock3",
		Spec: MockSpec{
			Request:  RequestSpec{Query: "SELECT * FROM products"},
			Response: ResponseSpec{Columns: []string{"id"}},
		},
	}
	store.Add(mock1)
	store.Add(mock2)
	store.Add(mock3)

	// Invalidate by pattern
	count := store.InvalidateByPattern("SELECT * FROM users*")
	if count != 2 {
		t.Errorf("Expected to invalidate 2 mocks, got %d", count)
	}

	// Should only have products mock left
	if store.Size() != 1 {
		t.Errorf("Expected 1 mock remaining, got %d", store.Size())
	}
}

func TestParamValueEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     interface{}
		expected bool
	}{
		{"nil_nil", nil, nil, true},
		{"nil_value", nil, 1, false},
		{"string_match", "test", "test", true},
		{"string_mismatch", "test", "other", false},
		{"int_match", 42, 42, true},
		{"int_int64", 42, int64(42), true},
		{"int64_float64", int64(42), float64(42), true},
		{"float32_float64", float32(3.14), float64(3.14), false}, // float precision
		{"bool_match", true, true, true},
		{"bool_mismatch", true, false, false},
		{"bytes_match", []byte("test"), []byte("test"), true},
		{"bytes_mismatch", []byte("test"), []byte("other"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := paramValueEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("paramValueEqual(%v, %v) = %v, expected %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		str     string
		pattern string
		match   bool
	}{
		{"SELECT * FROM users", "SELECT * FROM users", true},
		{"SELECT * FROM users WHERE id = 1", "SELECT * FROM users*", true},
		{"SELECT * FROM users", "SELECT * FROM products*", false},
		{"SELECT id, name FROM users", "*FROM users", true},
		{"SELECT * FROM users WHERE id = ?", "SELECT*users*", true},
		{"INSERT INTO users", "SELECT*", false},
	}

	for _, tt := range tests {
		t.Run(tt.str+"_"+tt.pattern, func(t *testing.T) {
			result := matchPattern(tt.str, tt.pattern)
			if result != tt.match {
				t.Errorf("matchPattern(%q, %q) = %v, expected %v", tt.str, tt.pattern, result, tt.match)
			}
		})
	}
}
