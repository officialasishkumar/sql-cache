// Package matcher provides query matching with scoring.
// This provides robust structural matching for SQL queries using
// AST-based algorithms that have been battle-tested.
package matcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"sync"

	"github.com/officialasishkumar/sql-cache/internal/sqlmeta"
	"vitess.io/vitess/go/vt/sqlparser"
)

// MatchResult contains the result of matching two queries
type MatchResult struct {
	// Matched indicates if the queries match (exact or structural)
	Matched bool
	// Score is the match score (higher = better match)
	Score int
	// MatchType describes how the queries matched
	MatchType MatchType
	// Reason provides detail about match/mismatch
	Reason string
}

// MatchType describes the type of match
type MatchType int

const (
	NoMatch MatchType = iota
	ExactMatch
	StructuralMatch
	TypeMatch
	PartialMatch
)

func (m MatchType) String() string {
	switch m {
	case ExactMatch:
		return "exact"
	case StructuralMatch:
		return "structural"
	case TypeMatch:
		return "type"
	case PartialMatch:
		return "partial"
	default:
		return "none"
	}
}

// Matcher provides SQL query matching functionality
type Matcher struct {
	parser         *sqlparser.Parser
	structureCache sync.Map // map[string]string - query -> structure
}

// NewMatcher creates a new matcher instance
func NewMatcher() (*Matcher, error) {
	opts := sqlparser.Options{}
	parser, err := sqlparser.New(opts)
	if err != nil {
		return nil, err
	}
	return &Matcher{parser: parser}, nil
}

// Match compares two SQL queries and returns a detailed match result
func (m *Matcher) Match(expected, actual string) MatchResult {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)

	// Empty queries don't match
	if expected == "" || actual == "" {
		return MatchResult{
			Matched:   false,
			Score:     0,
			MatchType: NoMatch,
			Reason:    "empty query",
		}
	}

	// Exact match (highest score)
	if expected == actual {
		return MatchResult{
			Matched:   true,
			Score:     100,
			MatchType: ExactMatch,
			Reason:    "exact string match",
		}
	}

	// Case-insensitive exact match
	if strings.EqualFold(expected, actual) {
		return MatchResult{
			Matched:   true,
			Score:     95,
			MatchType: ExactMatch,
			Reason:    "case-insensitive exact match",
		}
	}

	// Count placeholders - must match for prepared statements
	expectedPlaceholders := sqlmeta.CountPlaceholders(expected)
	actualPlaceholders := sqlmeta.CountPlaceholders(actual)
	if expectedPlaceholders != actualPlaceholders {
		return MatchResult{
			Matched:   false,
			Score:     0,
			MatchType: NoMatch,
			Reason:    "placeholder count mismatch",
		}
	}

	// Get structural signatures
	expectedSig, errE := m.getStructureCached(expected)
	actualSig, errA := m.getStructureCached(actual)

	if errE != nil || errA != nil {
		// Can't parse - fall back to basic comparison
		return MatchResult{
			Matched:   false,
			Score:     10,
			MatchType: PartialMatch,
			Reason:    "parse error, basic comparison only",
		}
	}

	// Structural match (AST-based approach)
	if expectedSig == actualSig {
		return MatchResult{
			Matched:   true,
			Score:     80,
			MatchType: StructuralMatch,
			Reason:    "canonical query match",
		}
	}

	// Type match only
	expectedType := m.getStatementType(expected)
	actualType := m.getStatementType(actual)
	if expectedType != "" && expectedType == actualType {
		return MatchResult{
			Matched:   false,
			Score:     30,
			MatchType: TypeMatch,
			Reason:    "statement type match only",
		}
	}

	return MatchResult{
		Matched:   false,
		Score:     0,
		MatchType: NoMatch,
		Reason:    "no match",
	}
}

// MatchWithArgs compares queries including their arguments
func (m *Matcher) MatchWithArgs(expectedQuery string, expectedArgs []interface{}, actualQuery string, actualArgs []interface{}) MatchResult {
	// First match the queries
	queryResult := m.Match(expectedQuery, actualQuery)

	// If query doesn't match, return early
	if !queryResult.Matched && queryResult.Score < 30 {
		return queryResult
	}

	// Now match arguments
	argsScore := m.matchArgs(expectedArgs, actualArgs)

	// Combine scores
	totalScore := queryResult.Score
	if len(expectedArgs) > 0 || len(actualArgs) > 0 {
		// Weight: 70% query, 30% args
		totalScore = (queryResult.Score*70 + argsScore*30) / 100
	}

	// Both query and args must match for a definitive match
	argsMatched := argsScore >= 80

	return MatchResult{
		Matched:   queryResult.Matched && argsMatched,
		Score:     totalScore,
		MatchType: queryResult.MatchType,
		Reason:    queryResult.Reason,
	}
}

// matchArgs compares two argument slices (paramValueEqual approach)
func (m *Matcher) matchArgs(expected, actual []interface{}) int {
	if len(expected) != len(actual) {
		return 0
	}
	if len(expected) == 0 {
		return 100 // No args to compare
	}

	matchedCount := 0
	for i := range expected {
		if m.valueEqual(expected[i], actual[i]) {
			matchedCount++
		}
	}

	return (matchedCount * 100) / len(expected)
}

// valueEqual compares two values with type flexibility
func (m *Matcher) valueEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

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
		return m.intEqual(int64(av), b)
	case int8:
		return m.intEqual(int64(av), b)
	case int16:
		return m.intEqual(int64(av), b)
	case int32:
		return m.intEqual(int64(av), b)
	case int64:
		return m.intEqual(av, b)
	case uint:
		return m.uintEqual(uint64(av), b)
	case uint8:
		return m.uintEqual(uint64(av), b)
	case uint16:
		return m.uintEqual(uint64(av), b)
	case uint32:
		return m.uintEqual(uint64(av), b)
	case uint64:
		return m.uintEqual(av, b)
	case float32:
		return m.floatEqual(float64(av), b)
	case float64:
		return m.floatEqual(av, b)
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	}

	// Fallback to reflect.DeepEqual
	return reflect.DeepEqual(a, b)
}

func (m *Matcher) intEqual(a int64, b interface{}) bool {
	switch bv := b.(type) {
	case int:
		return a == int64(bv)
	case int8:
		return a == int64(bv)
	case int16:
		return a == int64(bv)
	case int32:
		return a == int64(bv)
	case int64:
		return a == bv
	case float32:
		return float32(a) == bv
	case float64:
		return float64(a) == bv
	}
	return false
}

func (m *Matcher) uintEqual(a uint64, b interface{}) bool {
	switch bv := b.(type) {
	case uint:
		return a == uint64(bv)
	case uint8:
		return a == uint64(bv)
	case uint16:
		return a == uint64(bv)
	case uint32:
		return a == uint64(bv)
	case uint64:
		return a == bv
	case float32:
		return float32(a) == bv
	case float64:
		return float64(a) == bv
	}
	return false
}

func (m *Matcher) floatEqual(a float64, b interface{}) bool {
	switch bv := b.(type) {
	case float32:
		return a == float64(bv)
	case float64:
		return a == bv
	case int:
		return a == float64(bv)
	case int32:
		return a == float64(bv)
	case int64:
		return a == float64(bv)
	case uint32:
		return a == float64(bv)
	case uint64:
		return a == float64(bv)
	}
	return false
}

// getStructureCached returns cached structure for a query
func (m *Matcher) getStructureCached(sql string) (string, error) {
	if v, ok := m.structureCache.Load(sql); ok {
		return v.(string), nil
	}

	sig, err := m.getQueryStructure(sql)
	if err != nil {
		return "", err
	}

	m.structureCache.Store(sql, sig)
	return sig, nil
}

// getQueryStructure returns a canonical SQL fingerprint.
func (m *Matcher) getQueryStructure(sql string) (string, error) {
	return sqlmeta.Fingerprint(m.parser, sql)
}

// getStatementType returns the SQL statement type
func (m *Matcher) getStatementType(sql string) string {
	stmt, err := m.parser.Parse(sql)
	if err != nil {
		return sqlmeta.DetectQueryType(sql)
	}

	if stmtType := sqlmeta.StatementType(stmt); stmtType != "OTHER" {
		return stmtType
	}
	return sqlmeta.DetectQueryType(sql)
}

// GetStructure returns the canonical query fingerprint.
func (m *Matcher) GetStructure(sql string) (string, error) {
	return m.getStructureCached(sql)
}

// GetTables returns the tables referenced by the query.
func (m *Matcher) GetTables(sql string) []string {
	stmt, err := m.parser.Parse(sql)
	if err != nil {
		return nil
	}
	return sqlmeta.ExtractTables(stmt)
}

// GetType returns the normalized SQL statement type.
func (m *Matcher) GetType(sql string) string {
	return m.getStatementType(sql)
}

// IsDML checks if the query is a DML statement
func (m *Matcher) IsDML(sql string) bool {
	return sqlparser.IsDML(sql)
}

// GetHash returns a hash of the query for fast exact matching
func (m *Matcher) GetHash(sql string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(sql)))
	return hex.EncodeToString(h[:8]) // First 8 bytes as hex (16 chars)
}

// IsControlStatement checks if query is a control statement that doesn't need mocking
func IsControlStatement(query string) bool {
	q := strings.TrimSpace(strings.ToUpper(query))
	prefixes := []string{
		"BEGIN", "START TRANSACTION", "COMMIT", "ROLLBACK",
		"SET ", "ALTER ", "CREATE ", "DROP ", "TRUNCATE ",
		"RENAME ", "LOCK TABLES", "UNLOCK TABLES",
		"SAVEPOINT ", "RELEASE SAVEPOINT ", "USE ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(q, prefix) || q == strings.TrimSpace(prefix) {
			return true
		}
	}
	return false
}
