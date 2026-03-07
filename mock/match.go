package mock

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/officialasishkumar/sql-cache/internal/sqlmeta"
)

// FindMatch finds a matching cache entry for a request.
func (s *MockStore) FindMatch(query, queryType, structure string, args []interface{}, consumeOnMatch bool) (*Mock, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query = strings.TrimSpace(query)
	var (
		bestMatch *Mock
		bestScore int
	)

	for _, mock := range s.mocks {
		if consumeOnMatch && mock.Spec.Consumed {
			continue
		}
		if s.isExpired(mock) {
			continue
		}

		matched, score := s.matchQuery(mock, query, queryType, structure, args)
		if matched && (bestMatch == nil || score > bestScore) {
			bestMatch = mock
			bestScore = score
		}
	}
	if bestMatch == nil {
		return nil, false
	}
	if consumeOnMatch {
		bestMatch.Spec.Consumed = true
		s.sortOrder++
		bestMatch.Spec.SortOrder = s.sortOrder
	}
	return bestMatch, true
}

func (s *MockStore) matchQuery(mock *Mock, query, queryType, structure string, args []interface{}) (bool, int) {
	req := mock.Spec.Request
	expectedQuery := strings.TrimSpace(req.Query)
	actualQuery := query

	expectedPlaceholders := req.PlaceholderCount
	if expectedPlaceholders == 0 {
		expectedPlaceholders = sqlmeta.CountPlaceholders(expectedQuery)
	}
	actualPlaceholders := sqlmeta.CountPlaceholders(actualQuery)
	if expectedPlaceholders != actualPlaceholders {
		return false, 0
	}
	if !s.matchArgs(req.Args, args) {
		return false, 0
	}
	if expectedQuery == actualQuery {
		return true, 100
	}
	if strings.EqualFold(expectedQuery, actualQuery) {
		return true, 95
	}

	expectedType := req.Type
	if expectedType == "" {
		expectedType = getQueryTypeFromString(expectedQuery)
	}
	if queryType == "" {
		queryType = getQueryTypeFromString(actualQuery)
	}
	if expectedType != "" && queryType != "" && expectedType != queryType {
		return false, 0
	}

	expectedSig := req.Structure
	if expectedSig == "" || expectedQuery != actualQuery {
		expectedSig, _ = s.getQueryStructureCached(expectedQuery)
	}
	actualSig := structure
	if actualSig == "" {
		actualSig, _ = s.getQueryStructureCached(actualQuery)
	}
	if expectedSig != "" && actualSig != "" && expectedSig == actualSig {
		return true, 80
	}
	return false, 0
}

func (s *MockStore) matchArgs(expected, actual []interface{}) bool {
	if len(expected) != len(actual) {
		return false
	}
	if len(expected) == 0 {
		return true
	}
	for i := range expected {
		if !paramValueEqual(expected[i], actual[i]) {
			return false
		}
	}
	return true
}

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

func (s *MockStore) getQueryStructure(sql string) (string, error) {
	if s.parser == nil {
		return "", fmt.Errorf("parser not initialized")
	}
	return sqlmeta.Fingerprint(s.parser, sql)
}

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
	return reflect.DeepEqual(a, b)
}

func (s *MockStore) generateKey(mock *Mock) string {
	return fmt.Sprintf("%s|%s|%s", mock.Spec.Request.Query, mock.Spec.Request.Structure, mock.Name)
}
