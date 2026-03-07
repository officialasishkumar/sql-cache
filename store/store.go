// Package store provides in-memory storage for SQL query mocks/cache entries.
// It supports storing, retrieving, and matching cached SQL responses.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheEntry represents a cached SQL query and its response
type CacheEntry struct {
	ID        string `json:"id"`
	Query     string `json:"query"`
	Signature string `json:"signature"` // Normalized signature hash
	Structure string `json:"structure"` // AST structure
	Args      []any  `json:"args,omitempty"`

	// Response data
	Columns []string        `json:"columns,omitempty"`
	Rows    [][]interface{} `json:"rows,omitempty"`

	// For non-SELECT queries
	LastInsertID int64 `json:"last_insert_id,omitempty"`
	RowsAffected int64 `json:"rows_affected,omitempty"`

	// Metadata
	CreatedAt time.Time     `json:"created_at"`
	HitCount  int64         `json:"hit_count"`
	LastHit   time.Time     `json:"last_hit,omitempty"`
	TTL       time.Duration `json:"ttl,omitempty"` // 0 means no expiry

	// Error response (if query resulted in error during recording)
	Error string `json:"error,omitempty"`
}

// Store is the interface for cache storage
type Store interface {
	// Put stores a cache entry
	Put(entry *CacheEntry) error

	// Get retrieves a cache entry by signature hash
	Get(signatureHash string) (*CacheEntry, bool)

	// GetByStructure finds entries matching the given structure
	GetByStructure(structure string) []*CacheEntry

	// Delete removes a cache entry
	Delete(signatureHash string) error

	// Clear removes all cache entries
	Clear() error

	// List returns all cache entries
	List() []*CacheEntry

	// Size returns the number of entries
	Size() int

	// Save persists the cache to disk
	Save(path string) error

	// Load loads the cache from disk
	Load(path string) error
}

// MemoryStore is an in-memory implementation of Store
type MemoryStore struct {
	mu sync.RWMutex

	// Primary index: signature hash -> entry
	entries map[string]*CacheEntry

	// Secondary index: structure -> signature hashes
	structureIndex map[string][]string

	// Track insertion order for LRU-like behavior
	order []string

	// Maximum entries (0 = unlimited)
	maxEntries int
}

// NewMemoryStore creates a new in-memory store
func NewMemoryStore(maxEntries int) *MemoryStore {
	return &MemoryStore{
		entries:        make(map[string]*CacheEntry),
		structureIndex: make(map[string][]string),
		order:          make([]string, 0),
		maxEntries:     maxEntries,
	}
}

func (s *MemoryStore) Put(entry *CacheEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if entry exists
	existing, exists := s.entries[entry.Signature]
	if exists {
		// Update existing entry but keep hit stats
		entry.HitCount = existing.HitCount
		entry.LastHit = existing.LastHit
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = existing.CreatedAt
		}
	} else {
		// New entry - check capacity
		if s.maxEntries > 0 && len(s.entries) >= s.maxEntries {
			// Remove oldest entry
			if len(s.order) > 0 {
				oldest := s.order[0]
				s.removeEntryLocked(oldest)
			}
		}
		s.order = append(s.order, entry.Signature)
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	s.entries[entry.Signature] = entry

	// Update structure index
	if entry.Structure != "" {
		s.structureIndex[entry.Structure] = appendUnique(s.structureIndex[entry.Structure], entry.Signature)
	}

	return nil
}

func (s *MemoryStore) Get(signatureHash string) (*CacheEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[signatureHash]
	if !ok {
		return nil, false
	}

	// Check TTL
	if entry.TTL > 0 && time.Since(entry.CreatedAt) > entry.TTL {
		s.removeEntryLocked(signatureHash)
		return nil, false
	}

	// Update hit stats
	entry.HitCount++
	entry.LastHit = time.Now()

	return entry, true
}

func (s *MemoryStore) GetByStructure(structure string) []*CacheEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashes, ok := s.structureIndex[structure]
	if !ok {
		return nil
	}

	result := make([]*CacheEntry, 0, len(hashes))
	for _, hash := range hashes {
		if entry, ok := s.entries[hash]; ok {
			// Check TTL
			if entry.TTL > 0 && time.Since(entry.CreatedAt) > entry.TTL {
				continue
			}
			result = append(result, entry)
		}
	}

	return result
}

func (s *MemoryStore) Delete(signatureHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.removeEntryLocked(signatureHash)
	return nil
}

func (s *MemoryStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = make(map[string]*CacheEntry)
	s.structureIndex = make(map[string][]string)
	s.order = make([]string, 0)

	return nil
}

func (s *MemoryStore) List() []*CacheEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*CacheEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		result = append(result, entry)
	}
	return result
}

func (s *MemoryStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func (s *MemoryStore) Save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

func (s *MemoryStore) Load(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No cache file yet, that's ok
		}
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	var entries map[string]*CacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	// Rebuild the store
	s.entries = entries
	s.structureIndex = make(map[string][]string)
	s.order = make([]string, 0, len(entries))

	for hash, entry := range entries {
		s.order = append(s.order, hash)
		if entry.Structure != "" {
			s.structureIndex[entry.Structure] = appendUnique(s.structureIndex[entry.Structure], hash)
		}
	}

	return nil
}

func (s *MemoryStore) removeEntryLocked(signatureHash string) {
	entry, ok := s.entries[signatureHash]
	if !ok {
		return
	}

	delete(s.entries, signatureHash)

	// Remove from structure index
	if entry.Structure != "" {
		hashes := s.structureIndex[entry.Structure]
		for i, h := range hashes {
			if h == signatureHash {
				s.structureIndex[entry.Structure] = append(hashes[:i], hashes[i+1:]...)
				break
			}
		}
	}

	// Remove from order
	for i, h := range s.order {
		if h == signatureHash {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

// Stats returns statistics about the cache
type Stats struct {
	TotalEntries  int            `json:"total_entries"`
	TotalHits     int64          `json:"total_hits"`
	UniqueQueries int            `json:"unique_queries"`
	OldestEntry   time.Time      `json:"oldest_entry"`
	NewestEntry   time.Time      `json:"newest_entry"`
	ByType        map[string]int `json:"by_type"`
}

func (s *MemoryStore) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := Stats{
		TotalEntries: len(s.entries),
		ByType:       make(map[string]int),
	}

	for _, entry := range s.entries {
		stats.TotalHits += entry.HitCount

		if stats.OldestEntry.IsZero() || entry.CreatedAt.Before(stats.OldestEntry) {
			stats.OldestEntry = entry.CreatedAt
		}
		if entry.CreatedAt.After(stats.NewestEntry) {
			stats.NewestEntry = entry.CreatedAt
		}
	}

	stats.UniqueQueries = len(s.structureIndex)

	return stats
}
