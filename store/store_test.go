package store

import (
	"os"
	"testing"
	"time"
)

func TestMemoryStore(t *testing.T) {
	s := NewMemoryStore(100)

	// Test Put and Get
	entry := &CacheEntry{
		ID:        "test-1",
		Query:     "SELECT * FROM users",
		Signature: "sig-1",
		Structure: "struct-1",
		Columns:   []string{"id", "name"},
		Rows:      [][]interface{}{{1, "Alice"}},
		CreatedAt: time.Now(),
	}

	err := s.Put(entry)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, ok := s.Get("sig-1")
	if !ok {
		t.Fatal("Get returned false")
	}

	if got.Query != entry.Query {
		t.Errorf("query = %s, want %s", got.Query, entry.Query)
	}

	if got.HitCount != 1 {
		t.Errorf("hit count = %d, want 1", got.HitCount)
	}
}

func TestGetByStructure(t *testing.T) {
	s := NewMemoryStore(100)

	// Add entries with same structure
	for i := 1; i <= 3; i++ {
		s.Put(&CacheEntry{
			ID:        "test-" + string(rune('0'+i)),
			Query:     "SELECT * FROM users WHERE id = ?",
			Signature: "sig-" + string(rune('0'+i)),
			Structure: "same-structure",
		})
	}

	// Add entry with different structure
	s.Put(&CacheEntry{
		ID:        "test-4",
		Query:     "INSERT INTO users",
		Signature: "sig-4",
		Structure: "different-structure",
	})

	entries := s.GetByStructure("same-structure")
	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}
}

func TestDelete(t *testing.T) {
	s := NewMemoryStore(100)

	s.Put(&CacheEntry{
		ID:        "test-1",
		Signature: "sig-1",
		Structure: "struct-1",
	})

	if s.Size() != 1 {
		t.Fatalf("size = %d, want 1", s.Size())
	}

	s.Delete("sig-1")

	if s.Size() != 0 {
		t.Errorf("size = %d, want 0", s.Size())
	}

	_, ok := s.Get("sig-1")
	if ok {
		t.Error("expected Get to return false after delete")
	}
}

func TestClear(t *testing.T) {
	s := NewMemoryStore(100)

	for i := 0; i < 5; i++ {
		s.Put(&CacheEntry{
			ID:        "test-" + string(rune('0'+i)),
			Signature: "sig-" + string(rune('0'+i)),
		})
	}

	if s.Size() != 5 {
		t.Fatalf("size = %d, want 5", s.Size())
	}

	s.Clear()

	if s.Size() != 0 {
		t.Errorf("size = %d after clear, want 0", s.Size())
	}
}

func TestMaxEntries(t *testing.T) {
	s := NewMemoryStore(3)

	for i := 0; i < 5; i++ {
		s.Put(&CacheEntry{
			ID:        "test-" + string(rune('0'+i)),
			Signature: "sig-" + string(rune('0'+i)),
		})
	}

	if s.Size() != 3 {
		t.Errorf("size = %d, want 3 (max)", s.Size())
	}

	// First two should be evicted
	_, ok := s.Get("sig-0")
	if ok {
		t.Error("expected sig-0 to be evicted")
	}

	_, ok = s.Get("sig-1")
	if ok {
		t.Error("expected sig-1 to be evicted")
	}

	// Last three should exist
	_, ok = s.Get("sig-2")
	if !ok {
		t.Error("expected sig-2 to exist")
	}
}

func TestTTL(t *testing.T) {
	s := NewMemoryStore(100)

	s.Put(&CacheEntry{
		ID:        "test-1",
		Signature: "sig-1",
		TTL:       10 * time.Millisecond,
		CreatedAt: time.Now(),
	})

	// Should exist immediately
	_, ok := s.Get("sig-1")
	if !ok {
		t.Error("expected entry to exist")
	}

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	// Should be expired now
	_, ok = s.Get("sig-1")
	if ok {
		t.Error("expected entry to be expired")
	}
}

func TestSaveLoad(t *testing.T) {
	tmpFile := "/tmp/test-cache.json"
	defer os.Remove(tmpFile)

	s1 := NewMemoryStore(100)
	s1.Put(&CacheEntry{
		ID:        "test-1",
		Query:     "SELECT * FROM users",
		Signature: "sig-1",
		Structure: "struct-1",
		Columns:   []string{"id", "name"},
		Rows:      [][]interface{}{{1, "Alice"}},
	})

	err := s1.Save(tmpFile)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	s2 := NewMemoryStore(100)
	err = s2.Load(tmpFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if s2.Size() != 1 {
		t.Errorf("size after load = %d, want 1", s2.Size())
	}

	entry, ok := s2.Get("sig-1")
	if !ok {
		t.Fatal("expected entry to exist after load")
	}

	if entry.Query != "SELECT * FROM users" {
		t.Errorf("query = %s, want 'SELECT * FROM users'", entry.Query)
	}
}

func TestList(t *testing.T) {
	s := NewMemoryStore(100)

	for i := 0; i < 3; i++ {
		s.Put(&CacheEntry{
			ID:        "test-" + string(rune('0'+i)),
			Signature: "sig-" + string(rune('0'+i)),
		})
	}

	list := s.List()
	if len(list) != 3 {
		t.Errorf("list length = %d, want 3", len(list))
	}
}

func TestStats(t *testing.T) {
	s := NewMemoryStore(100)

	s.Put(&CacheEntry{
		ID:        "test-1",
		Signature: "sig-1",
		Structure: "struct-1",
		CreatedAt: time.Now().Add(-time.Hour),
	})

	s.Put(&CacheEntry{
		ID:        "test-2",
		Signature: "sig-2",
		Structure: "struct-2",
		CreatedAt: time.Now(),
	})

	// Generate some hits
	s.Get("sig-1")
	s.Get("sig-1")
	s.Get("sig-2")

	stats := s.Stats()

	if stats.TotalEntries != 2 {
		t.Errorf("total entries = %d, want 2", stats.TotalEntries)
	}

	if stats.TotalHits != 3 {
		t.Errorf("total hits = %d, want 3", stats.TotalHits)
	}

	if stats.UniqueQueries != 2 {
		t.Errorf("unique queries = %d, want 2", stats.UniqueQueries)
	}
}
