// Package mock provides cache entry storage and retrieval for SQL queries.
// Entries are stored in YAML format for easy inspection and modification.
// The matching logic uses battle-tested AST-based algorithms.
//
// Key features:
// - AST structure-based matching using vitess sqlparser
// - Placeholder count verification for prepared statements
// - DML type checking (SELECT vs INSERT/UPDATE/DELETE)
// - Sequential consumption support with tracking
// - Type-flexible argument matching
// - TTL support for cache expiration
// - Pattern-based invalidation
//
// The cache entry format is designed to be compatible with common mock systems while
// being optimized for higher-level SQL interception (vs wire protocol).
package mock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"vitess.io/vitess/go/vt/sqlparser"
)

// Version is the mock format version
const Version = "sql-cache/v1"

// Mock represents a cached SQL interaction
// This format is designed for SQL query caching.
//
// The structure is optimized for SQL-specific fields.
type Mock struct {
	Version        string         `yaml:"version"`                      // API version (sql-cache/v1)
	Kind           string         `yaml:"kind"`                         // Always "SQL" for this implementation
	Name           string         `yaml:"name"`                         // Unique cache entry identifier
	Spec           MockSpec       `yaml:"spec"`                         // The actual cache data
	CacheEntryInfo CacheEntryInfo `yaml:"CacheEntryInfo,omitempty"`     // Cache entry tracking
	ConnectionID   string         `yaml:"ConnectionId,omitempty"`       // For connection-aware matching
}

// CacheEntryInfo tracks cache entry usage
type CacheEntryInfo struct {
	ID         int   `yaml:"Id,omitempty"`         // Sequence ID
	IsFiltered bool  `yaml:"isFiltered,omitempty"` // Whether filtered during matching
	SortOrder  int64 `yaml:"sortOrder,omitempty"`  // Order in which mock was consumed
}

// MockSpec contains the mock specification
type MockSpec struct {
	Metadata  map[string]string `yaml:"Metadata,omitempty"`              // Metadata like recorded_at, operation, etc.
	Request   RequestSpec       `yaml:"Request"`                         // The SQL request
	Response  ResponseSpec      `yaml:"Response"`                        // The SQL response
	Created   int64             `yaml:"Created"`                         // Unix timestamp when created
	Consumed  bool              `yaml:"consumed,omitempty"`              // For sequential mode (internal use)
	SortOrder int               `yaml:"sort_order,omitempty"`            // Order consumed (internal use)

	// Timestamps for timing-sensitive tests (using Go's time format)
	ReqTimestampMock time.Time `yaml:"ReqTimestampMock,omitempty"` // Request timestamp
	ResTimestampMock time.Time `yaml:"ResTimestampMock,omitempty"` // Response timestamp
}

// RequestSpec represents a SQL request
// Contains all information needed for query matching
type RequestSpec struct {
	Query     string        `yaml:"Query"`               // The SQL query string
	Args      []interface{} `yaml:"Args,omitempty"`      // Query arguments/parameters
	Type      string        `yaml:"Type"`                // SELECT, INSERT, UPDATE, DELETE, etc.
	Tables    []string      `yaml:"Tables,omitempty"`    // Tables referenced in query
	Structure string        `yaml:"Structure,omitempty"` // AST structure signature

	// Additional matching hints (improves match accuracy)
	PlaceholderCount int    `yaml:"PlaceholderCount,omitempty"` // Number of ? placeholders
	IsDML            bool   `yaml:"IsDML,omitempty"`            // Whether this is a DML statement
	QueryHash        string `yaml:"QueryHash,omitempty"`        // SHA256 hash (first 16 chars) for quick exact matching

	// Extended fields for advanced matching
	Database string `yaml:"Database,omitempty"` // Target database name
	Timeout  int64  `yaml:"Timeout,omitempty"`  // Query timeout in milliseconds
}

// ResponseSpec represents a SQL response
// Contains all possible response types from SQL queries
type ResponseSpec struct {
	// For SELECT queries
	Columns []string        `yaml:"Columns,omitempty"` // Column names
	Rows    [][]interface{} `yaml:"Rows,omitempty"`    // Row data (type-preserved)

	// For INSERT/UPDATE/DELETE
	LastInsertID int64 `yaml:"LastInsertID,omitempty"` // Auto-increment ID from INSERT
	RowsAffected int64 `yaml:"RowsAffected,omitempty"` // Number of rows affected

	// For errors (stores error responses for error handling)
	Error     string `yaml:"Error,omitempty"`     // Error message if query failed
	ErrorCode int    `yaml:"ErrorCode,omitempty"` // MySQL error code (if applicable)

	// Additional response metadata
	RowCount       int   `yaml:"RowCount,omitempty"`       // Number of rows returned
	ExecutionTime  int64 `yaml:"ExecutionTime,omitempty"`  // Time to execute in nanoseconds
	WarningCount   int   `yaml:"WarningCount,omitempty"`   // Number of SQL warnings
}

// MockStore manages mock storage with YAML files
type MockStore struct {
	mu             sync.RWMutex
	mocks          []*Mock          // Ordered list of mocks
	mockIndex      map[string]*Mock // Hash -> Mock for quick lookup
	dir            string           // Directory for mock files
	sortOrder      int              // Counter for replay order
	structureCache sync.Map         // Cache for query structures
	parser         *sqlparser.Parser
	ttl            int64            // TTL in seconds (0 = no TTL)
}

// NewMockStore creates a new mock store
func NewMockStore(dir string) *MockStore {
	opts := sqlparser.Options{}
	parser, _ := sqlparser.New(opts) // Ignore error, parser is optional for degraded mode

	return &MockStore{
		mocks:     make([]*Mock, 0),
		mockIndex: make(map[string]*Mock),
		dir:       dir,
		parser:    parser,
	}
}

// GenerateQueryHash creates a SHA256 hash for quick exact matching (first 16 hex chars)
func GenerateQueryHash(query string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return hex.EncodeToString(h[:8])
}

// CreateMock is a helper to create a properly formatted mock
func CreateMock(query string, args []interface{}, columns []string, rows [][]interface{}, lastInsertID, rowsAffected int64, errMsg string) *Mock {
	now := time.Now()

	// Count placeholders
	placeholderCount := strings.Count(query, "?")

	// Determine query type
	queryType := getQueryTypeFromString(query)

	// Check if DML
	isDML := sqlparser.IsDML(query)

	// Calculate row count
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
				"recorded_at": now.Format(time.RFC3339),
				"operation":   strings.ToLower(queryType),
			},
			Request: RequestSpec{
				Query:            query,
				Args:             args,
				Type:             queryType,
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

// getQueryTypeFromString determines the query type from the SQL string
func getQueryTypeFromString(query string) string {
	q := strings.TrimSpace(strings.ToUpper(query))
	switch {
	case strings.HasPrefix(q, "SELECT"):
		return "SELECT"
	case strings.HasPrefix(q, "INSERT"):
		return "INSERT"
	case strings.HasPrefix(q, "UPDATE"):
		return "UPDATE"
	case strings.HasPrefix(q, "DELETE"):
		return "DELETE"
	case strings.HasPrefix(q, "REPLACE"):
		return "REPLACE"
	case strings.HasPrefix(q, "CALL"):
		return "CALL"
	case strings.HasPrefix(q, "SHOW"):
		return "SHOW"
	case strings.HasPrefix(q, "DESCRIBE"), strings.HasPrefix(q, "DESC"):
		return "DESCRIBE"
	case strings.HasPrefix(q, "EXPLAIN"):
		return "EXPLAIN"
	default:
		return "OTHER"
	}
}

// Add adds a mock to the store
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

	// Generate hash key for quick lookup
	key := s.generateKey(mock)
	
	// Check for duplicate
	if existing, ok := s.mockIndex[key]; ok {
		// Update existing mock
		existing.Spec = mock.Spec
		return nil
	}

	s.mocks = append(s.mocks, mock)
	s.mockIndex[key] = mock

	return nil
}

// FindMatch finds a matching cache entry for a request (sequential mode support)
// This implements AST-based matching algorithm for SQL queries
func (s *MockStore) FindMatch(query, queryType, structure string, args []interface{}, consumeOnMatch bool) (*Mock, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query = strings.TrimSpace(query)
	
	var bestMatch *Mock
	var bestScore int
	var definitiveMatch bool

	for _, mock := range s.mocks {
		// Skip already consumed entries in sequential mode
		if consumeOnMatch && mock.Spec.Consumed {
			continue
		}

		// Skip expired mocks (TTL check)
		if s.isExpired(mock) {
			continue
		}

		matched, score := s.matchQuery(mock, query, queryType, structure, args)
		
		if matched {
			// Definitive match found - use immediately
			bestMatch = mock
			bestScore = score
			definitiveMatch = true
			break
		}

		if score > bestScore {
			bestScore = score
			bestMatch = mock
		}
	}

	// Minimum score of 30 required for a match
	if bestMatch == nil || (!definitiveMatch && bestScore < 30) {
		return nil, false
	}

	// Mark as consumed for sequential mode
	if consumeOnMatch {
		bestMatch.Spec.Consumed = true
		s.sortOrder++
		bestMatch.Spec.SortOrder = s.sortOrder
	}

	return bestMatch, true
}

// matchQuery implements the exact matching logic for SQL queries
// Returns: (definitiveMatch, score)
func (s *MockStore) matchQuery(mock *Mock, query, queryType, structure string, args []interface{}) (bool, int) {
	req := mock.Spec.Request
	expectedQuery := strings.TrimSpace(req.Query)
	actualQuery := query
	score := 0

	// Step 1: Check placeholder count (critical for prepared statements)
	// This is the first check in the matching logic
	expectedPlaceholders := strings.Count(expectedQuery, "?")
	actualPlaceholders := strings.Count(actualQuery, "?")
	if expectedPlaceholders != actualPlaceholders {
		return false, 0
	}

	// Step 2: Exact query match (highest priority)
	if expectedQuery == actualQuery {
		score += 50
		// Still need to match args for definitive match
		argsMatched := s.matchArgs(req.Args, args)
		if argsMatched {
			return true, score + 50 // Definitive match
		}
		return false, score
	}

	// Step 3: Case-insensitive exact match
	if strings.EqualFold(expectedQuery, actualQuery) {
		score += 45
		argsMatched := s.matchArgs(req.Args, args)
		if argsMatched {
			return true, score + 50
		}
		return false, score
	}

	// Step 4: DML type check (critical guard)
	// Both must be DML or both must be non-DML
	expectedIsDML := sqlparser.IsDML(expectedQuery)
	actualIsDML := sqlparser.IsDML(actualQuery)
	if expectedIsDML != actualIsDML {
		return false, 0
	}

	// Step 5: For non-DML queries, don't attempt structural matching
	if !expectedIsDML || !actualIsDML {
		// Type match only gives partial score
		if req.Type != "" && queryType != "" && req.Type == queryType {
			score += 10
		}
		return false, score
	}

	// Step 6: Structural match (key mechanism for DML queries)
	// Get structures using cache
	expectedSig := req.Structure
	if expectedSig == "" {
		expectedSig, _ = s.getQueryStructureCached(expectedQuery)
	}
	actualSig := structure
	if actualSig == "" {
		actualSig, _ = s.getQueryStructureCached(actualQuery)
	}

	if expectedSig != "" && actualSig != "" && expectedSig == actualSig {
		score += 30
		// Check args for definitive structural match
		argsMatched := s.matchArgs(req.Args, args)
		if argsMatched {
			return true, score + 50 // Definitive structural match
		}
		// Partial structural match
		return false, score + s.calculateArgsScore(req.Args, args)
	}

	// Step 7: Type match (fallback scoring)
	if req.Type != "" && queryType != "" && req.Type == queryType {
		score += 10
	}

	// Add partial args score
	score += s.calculateArgsScore(req.Args, args)

	return false, score
}

// matchArgs checks if all arguments match (parameter matching)
func (s *MockStore) matchArgs(expected, actual []interface{}) bool {
	if len(expected) != len(actual) {
		return false
	}
	if len(expected) == 0 {
		return true // No args to compare
	}
	for i := range expected {
		if !paramValueEqual(expected[i], actual[i]) {
			return false
		}
	}
	return true
}

// calculateArgsScore calculates partial score for args
func (s *MockStore) calculateArgsScore(expected, actual []interface{}) int {
	if len(expected) != len(actual) {
		return 0
	}
	if len(expected) == 0 {
		return 10 // No args to compare
	}
	matchedArgs := 0
	for i := range expected {
		if paramValueEqual(expected[i], actual[i]) {
			matchedArgs++
		}
	}
	return (matchedArgs * 10) / len(expected)
}

// getQueryStructureCached returns cached structure for a query
func (s *MockStore) getQueryStructureCached(sql string) (string, error) {
	if v, ok := s.structureCache.Load(sql); ok {
		return v.(string), nil
	}
	sig, err := s.getQueryStructure(sql)
	if err == nil {
		s.structureCache.Store(sql, sig)
	}
	return sig, err
}

// getQueryStructure creates a structural signature by walking the AST
// This creates a signature based on the Go types of AST nodes
func (s *MockStore) getQueryStructure(sql string) (string, error) {
	if s.parser == nil {
		return "", fmt.Errorf("parser not initialized")
	}

	stmt, err := s.parser.Parse(sql)
	if err != nil {
		return "", fmt.Errorf("failed to parse SQL: %w", err)
	}

	var structureParts []string
	// Walk the AST and collect the Go type of each grammatical node.
	err = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		structureParts = append(structureParts, reflect.TypeOf(node).String())
		return true, nil
	}, stmt)

	if err != nil {
		return "", fmt.Errorf("failed to walk AST: %w", err)
	}

	return strings.Join(structureParts, "->"), nil
}

// paramValueEqual compares two values with type flexibility
// This allows matching between different numeric types (int/int64/float64 interoperability)
func paramValueEqual(a, b interface{}) bool {
	switch av := a.(type) {
	case []byte:
		bv, ok := b.([]byte)
		return ok && bytes.Equal(av, bv)
	case string:
		switch bv := b.(type) {
		case string:
			return av == bv
		case []byte:
			return av == string(bv)
		}
	case int:
		switch bv := b.(type) {
		case int:
			return av == bv
		case int64:
			return int64(av) == bv
		case int32:
			return av == int(bv)
		case float32:
			return float32(av) == bv
		case float64:
			return float64(av) == bv
		}
	case int32:
		switch bv := b.(type) {
		case int32:
			return av == bv
		case int:
			return int(av) == bv
		case int64:
			return int64(av) == bv
		case float32:
			return float32(av) == bv
		case float64:
			return float64(av) == bv
		}
	case int64:
		switch bv := b.(type) {
		case int64:
			return av == bv
		case int:
			return av == int64(bv)
		case int32:
			return av == int64(bv)
		case float32:
			return float32(av) == bv
		case float64:
			return float64(av) == bv
		}
	case uint32:
		switch bv := b.(type) {
		case uint32:
			return av == bv
		case uint64:
			return uint64(av) == bv
		case float32:
			return float32(av) == bv
		case float64:
			return float64(av) == bv
		}
	case uint64:
		switch bv := b.(type) {
		case uint64:
			return av == bv
		case uint32:
			return av == uint64(bv)
		case float32:
			return float32(av) == bv
		case float64:
			return float64(av) == bv
		}
	case float32:
		switch bv := b.(type) {
		case float32:
			return av == bv
		case float64:
			return float64(av) == bv
		case int:
			return av == float32(bv)
		case int32:
			return av == float32(bv)
		case int64:
			return av == float32(bv)
		case uint32:
			return av == float32(bv)
		case uint64:
			return av == float32(bv)
		}
	case float64:
		switch bv := b.(type) {
		case float64:
			return av == bv
		case float32:
			return av == float64(bv)
		case int:
			return av == float64(bv)
		case int32:
			return av == float64(bv)
		case int64:
			return av == float64(bv)
		case uint32:
			return av == float64(bv)
		case uint64:
			return av == float64(bv)
		}
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	}
	// Fallback (rare)
	return reflect.DeepEqual(a, b)
}

// generateKey generates a unique key for a mock
func (s *MockStore) generateKey(mock *Mock) string {
	return fmt.Sprintf("%s|%s|%s", mock.Spec.Request.Query, mock.Spec.Request.Structure, mock.Name)
}

// Save saves all cache entries to YAML files (one combined mocks.yaml file)
// Uses atomic writes for production safety - writes to temp file then renames
func (s *MockStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.dir == "" {
		return fmt.Errorf("no directory configured")
	}

	// Ensure directory exists
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("failed to create mock directory: %w", err)
	}

	// Save all mocks to a single mocks.yaml file
	mocksFile := filepath.Join(s.dir, "mocks.yaml")

	// Create the mocks slice for YAML
	var data []byte
	for i, mock := range s.mocks {
		mockData, err := yaml.Marshal(mock)
		if err != nil {
			return fmt.Errorf("failed to marshal cache entry %s: %w", mock.Name, err)
		}

		if i > 0 {
			data = append(data, []byte("\n---\n")...)
		}
		data = append(data, mockData...)
	}

	// Use atomic write: write to temp file then rename
	// This prevents data corruption if the process crashes during write
	tempFile := mocksFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp mocks file: %w", err)
	}

	// Atomic rename - on POSIX systems this is atomic
	if err := os.Rename(tempFile, mocksFile); err != nil {
		// Clean up temp file on error
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename mocks file: %w", err)
	}

	return nil
}

// Load loads cache entries from YAML files
func (s *MockStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		return nil // No directory configured, nothing to load
	}

	mocksFile := filepath.Join(s.dir, "mocks.yaml")

	data, err := os.ReadFile(mocksFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No mocks file yet
		}
		return fmt.Errorf("failed to read mocks file: %w", err)
	}

	// Parse YAML documents (separated by ---)
	docs := splitYAMLDocuments(data)
	s.mocks = make([]*Mock, 0, len(docs))
	s.mockIndex = make(map[string]*Mock)

	for _, doc := range docs {
		if len(strings.TrimSpace(string(doc))) == 0 {
			continue
		}

		var mock Mock
		if err := yaml.Unmarshal(doc, &mock); err != nil {
			// Log warning but continue
			continue
		}

		// Reset consumed state on load
		mock.Spec.Consumed = false
		mock.Spec.SortOrder = 0

		key := s.generateKey(&mock)
		s.mocks = append(s.mocks, &mock)
		s.mockIndex[key] = &mock
	}

	return nil
}

// splitYAMLDocuments splits YAML data by document separator
func splitYAMLDocuments(data []byte) [][]byte {
	parts := strings.Split(string(data), "\n---")
	result := make([][]byte, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimPrefix(part, "\n")
		if len(strings.TrimSpace(part)) > 0 {
			result = append(result, []byte(part))
		}
	}
	return result
}

// Clear removes all mocks
func (s *MockStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mocks = make([]*Mock, 0)
	s.mockIndex = make(map[string]*Mock)
	s.sortOrder = 0
}

// =============================================================================
// Cache Invalidation Methods
// =============================================================================

// InvalidateByQuery removes mocks matching a specific query
func (s *MockStore) InvalidateByQuery(query string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	query = strings.TrimSpace(query)
	count := 0
	newMocks := make([]*Mock, 0, len(s.mocks))

	for _, mock := range s.mocks {
		if strings.TrimSpace(mock.Spec.Request.Query) == query {
			// Remove from index
			key := s.generateKey(mock)
			delete(s.mockIndex, key)
			count++
		} else {
			newMocks = append(newMocks, mock)
		}
	}

	s.mocks = newMocks
	return count
}

// InvalidateByTable removes mocks for queries involving specific tables
func (s *MockStore) InvalidateByTable(tableName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	tableName = strings.ToLower(strings.TrimSpace(tableName))
	count := 0
	newMocks := make([]*Mock, 0, len(s.mocks))

	for _, mock := range s.mocks {
		shouldRemove := false

		// Check if table is in the Tables list
		for _, t := range mock.Spec.Request.Tables {
			if strings.ToLower(t) == tableName {
				shouldRemove = true
				break
			}
		}

		// Also check query string for table name (fallback)
		if !shouldRemove {
			queryLower := strings.ToLower(mock.Spec.Request.Query)
			// Simple check for table name in common SQL patterns
			patterns := []string{
				"from " + tableName,
				"from `" + tableName + "`",
				"join " + tableName,
				"join `" + tableName + "`",
				"into " + tableName,
				"into `" + tableName + "`",
				"update " + tableName,
				"update `" + tableName + "`",
			}
			for _, p := range patterns {
				if strings.Contains(queryLower, p) {
					shouldRemove = true
					break
				}
			}
		}

		if shouldRemove {
			key := s.generateKey(mock)
			delete(s.mockIndex, key)
			count++
		} else {
			newMocks = append(newMocks, mock)
		}
	}

	s.mocks = newMocks
	return count
}

// InvalidateByPattern removes mocks matching a query pattern (supports * wildcard)
func (s *MockStore) InvalidateByPattern(pattern string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	newMocks := make([]*Mock, 0, len(s.mocks))

	for _, mock := range s.mocks {
		if matchPattern(mock.Spec.Request.Query, pattern) {
			key := s.generateKey(mock)
			delete(s.mockIndex, key)
			count++
		} else {
			newMocks = append(newMocks, mock)
		}
	}

	s.mocks = newMocks
	return count
}

// matchPattern implements simple wildcard matching
// Supports * for any sequence of characters
func matchPattern(str, pattern string) bool {
	str = strings.ToLower(strings.TrimSpace(str))
	pattern = strings.ToLower(strings.TrimSpace(pattern))

	// Split pattern by *
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		// No wildcard, exact match
		return str == pattern
	}

	// Check if string starts with first part
	if parts[0] != "" && !strings.HasPrefix(str, parts[0]) {
		return false
	}

	pos := len(parts[0])
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if part == "" {
			continue
		}

		idx := strings.Index(str[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}

	// If pattern ends with *, we're good
	// If not, the str should end with the last part
	lastPart := parts[len(parts)-1]
	if lastPart != "" && !strings.HasSuffix(str, lastPart) {
		return false
	}

	return true
}

// SetTTL sets a time-to-live for mocks in seconds (0 = no TTL)
func (s *MockStore) SetTTL(ttlSeconds int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = ttlSeconds
}

// GetTTL returns the current TTL setting
func (s *MockStore) GetTTL() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ttl
}

// isExpired checks if a mock has expired based on TTL
func (s *MockStore) isExpired(mock *Mock) bool {
	if s.ttl <= 0 {
		return false
	}
	return time.Now().Unix()-mock.Spec.Created > s.ttl
}

// Reset resets consumed state for all entries (for sequential re-use)
func (s *MockStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mock := range s.mocks {
		mock.Spec.Consumed = false
		mock.Spec.SortOrder = 0
	}
	s.sortOrder = 0
}

// List returns all mocks
func (s *MockStore) List() []*Mock {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Mock, len(s.mocks))
	copy(result, s.mocks)
	return result
}

// ListUnconsumed returns mocks that haven't been consumed
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

// ListConsumed returns mocks in consumption order
func (s *MockStore) ListConsumed() []*Mock {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Mock, 0)
	for _, mock := range s.mocks {
		if mock.Spec.Consumed {
			result = append(result, mock)
		}
	}

	// Sort by consumption order
	sort.Slice(result, func(i, j int) bool {
		return result[i].Spec.SortOrder < result[j].Spec.SortOrder
	})

	return result
}

// Size returns the number of mocks
func (s *MockStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.mocks)
}

// Stats returns statistics
type Stats struct {
	Total      int `json:"total"`
	Consumed   int `json:"consumed"`
	Unconsumed int `json:"unconsumed"`
}

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
