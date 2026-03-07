package mock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/officialasishkumar/sql-cache/internal/sqlmeta"
	"vitess.io/vitess/go/vt/sqlparser"
)

// MockStore manages mock storage with YAML files.
type MockStore struct {
	mu             sync.RWMutex
	mocks          []*Mock
	mockIndex      map[string]*Mock
	dir            string
	sortOrder      int
	structureCache sync.Map
	parser         *sqlparser.Parser
	ttl            int64
}

// NewMockStore creates a new mock store.
func NewMockStore(dir string) *MockStore {
	parser, _ := sqlparser.New(sqlparser.Options{})
	return &MockStore{
		mocks:     make([]*Mock, 0),
		mockIndex: make(map[string]*Mock),
		dir:       dir,
		parser:    parser,
	}
}

// GenerateQueryHash creates a SHA256 hash for quick exact matching.
func GenerateQueryHash(query string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return hex.EncodeToString(h[:8])
}

// CreateMock is a helper to create a properly formatted mock.
func CreateMock(query string, args []interface{}, columns []string, rows [][]interface{}, lastInsertID, rowsAffected int64, errMsg string) *Mock {
	now := time.Now()
	fingerprint, tables, placeholderCount, queryType, isDML := analyzeQuery(query)
	rowCount := 0
	if rows != nil {
		rowCount = len(rows)
	}

	return &Mock{
		Version: Version,
		Kind:    "SQL",
		Name:    fmt.Sprintf("mock-%d", now.UnixNano()),
		Spec: MockSpec{
			Metadata: map[string]string{
				"cached_at": now.Format(time.RFC3339),
				"operation": strings.ToLower(queryType),
			},
			Request: RequestSpec{
				Query:            query,
				Args:             args,
				Type:             queryType,
				Tables:           tables,
				Structure:        fingerprint,
				PlaceholderCount: placeholderCount,
				IsDML:            isDML,
				QueryHash:        GenerateQueryHash(query),
			},
			Response: ResponseSpec{
				Columns:      columns,
				Rows:         rows,
				LastInsertID: lastInsertID,
				RowsAffected: rowsAffected,
				Error:        errMsg,
				RowCount:     rowCount,
			},
			Created:          now.Unix(),
			ReqTimestampMock: now,
			ResTimestampMock: now,
		},
	}
}

func analyzeQuery(query string) (fingerprint string, tables []string, placeholderCount int, queryType string, isDML bool) {
	placeholderCount = sqlmeta.CountPlaceholders(query)
	queryType = getQueryTypeFromString(query)
	isDML = sqlparser.IsDML(query)

	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return
	}
	meta, err := sqlmeta.Analyze(parser, query)
	if err != nil {
		return
	}

	fingerprint = meta.Fingerprint
	tables = meta.Tables
	placeholderCount = meta.PlaceholderCount
	queryType = meta.Type
	isDML = meta.IsDML
	return
}

func getQueryTypeFromString(query string) string { return sqlmeta.DetectQueryType(query) }

// Add adds a mock to the store.
func (s *MockStore) Add(mock *Mock) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if mock.Name == "" {
		mock.Name = fmt.Sprintf("mock-%d", time.Now().UnixNano())
	}
	if mock.Version == "" {
		mock.Version = Version
	}
	if mock.Spec.Created == 0 {
		mock.Spec.Created = time.Now().Unix()
	}

	key := s.generateKey(mock)
	if existing, ok := s.mockIndex[key]; ok {
		existing.Spec = mock.Spec
		return nil
	}

	s.mocks = append(s.mocks, mock)
	s.mockIndex[key] = mock
	return nil
}

// Clear removes all mocks.
func (s *MockStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mocks = make([]*Mock, 0)
	s.mockIndex = make(map[string]*Mock)
	s.sortOrder = 0
}

// SetTTL sets a time-to-live for mocks in seconds.
func (s *MockStore) SetTTL(ttlSeconds int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = ttlSeconds
}

// GetTTL returns the current TTL setting.
func (s *MockStore) GetTTL() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ttl
}

func (s *MockStore) isExpired(mock *Mock) bool {
	if s.ttl <= 0 {
		return false
	}
	return time.Now().Unix()-mock.Spec.Created > s.ttl
}

// Reset resets consumed state for all entries.
func (s *MockStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mock := range s.mocks {
		mock.Spec.Consumed = false
		mock.Spec.SortOrder = 0
	}
	s.sortOrder = 0
}

// List returns all mocks.
func (s *MockStore) List() []*Mock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Mock, len(s.mocks))
	copy(result, s.mocks)
	return result
}

// ListUnconsumed returns mocks that haven't been consumed.
func (s *MockStore) ListUnconsumed() []*Mock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Mock, 0)
	for _, mock := range s.mocks {
		if !mock.Spec.Consumed {
			result = append(result, mock)
		}
	}
	return result
}

// ListConsumed returns mocks in consumption order.
func (s *MockStore) ListConsumed() []*Mock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Mock, 0)
	for _, mock := range s.mocks {
		if mock.Spec.Consumed {
			result = append(result, mock)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Spec.SortOrder < result[j].Spec.SortOrder
	})
	return result
}

// Size returns the number of mocks.
func (s *MockStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.mocks)
}

// Stats returns statistics.
func (s *MockStore) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := Stats{Total: len(s.mocks)}
	for _, mock := range s.mocks {
		if mock.Spec.Consumed {
			stats.Consumed++
		} else {
			stats.Unconsumed++
		}
	}
	return stats
}
