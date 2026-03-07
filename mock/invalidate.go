package mock

import "strings"

// InvalidateByQuery removes mocks matching a specific query.
func (s *MockStore) InvalidateByQuery(query string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	query = strings.TrimSpace(query)
	count := 0
	newMocks := make([]*Mock, 0, len(s.mocks))
	for _, mock := range s.mocks {
		if strings.TrimSpace(mock.Spec.Request.Query) == query {
			delete(s.mockIndex, s.generateKey(mock))
			count++
			continue
		}
		newMocks = append(newMocks, mock)
	}
	s.mocks = newMocks
	return count
}

// InvalidateByTable removes mocks for queries involving specific tables.
func (s *MockStore) InvalidateByTable(tableName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	tableName = strings.ToLower(strings.TrimSpace(tableName))
	count := 0
	newMocks := make([]*Mock, 0, len(s.mocks))
	for _, mock := range s.mocks {
		if shouldInvalidateTable(mock, tableName) {
			delete(s.mockIndex, s.generateKey(mock))
			count++
			continue
		}
		newMocks = append(newMocks, mock)
	}
	s.mocks = newMocks
	return count
}

func shouldInvalidateTable(mock *Mock, tableName string) bool {
	for _, table := range mock.Spec.Request.Tables {
		if strings.ToLower(table) == tableName {
			return true
		}
	}

	queryLower := strings.ToLower(mock.Spec.Request.Query)
	for _, pattern := range []string{
		"from " + tableName,
		"from `" + tableName + "`",
		"join " + tableName,
		"join `" + tableName + "`",
		"into " + tableName,
		"into `" + tableName + "`",
		"update " + tableName,
		"update `" + tableName + "`",
	} {
		if strings.Contains(queryLower, pattern) {
			return true
		}
	}
	return false
}

// InvalidateByPattern removes mocks matching a query pattern.
func (s *MockStore) InvalidateByPattern(pattern string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	newMocks := make([]*Mock, 0, len(s.mocks))
	for _, mock := range s.mocks {
		if matchPattern(mock.Spec.Request.Query, pattern) {
			delete(s.mockIndex, s.generateKey(mock))
			count++
			continue
		}
		newMocks = append(newMocks, mock)
	}
	s.mocks = newMocks
	return count
}

func matchPattern(str, pattern string) bool {
	str = strings.ToLower(strings.TrimSpace(str))
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return str == pattern
	}
	if parts[0] != "" && !strings.HasPrefix(str, parts[0]) {
		return false
	}

	pos := len(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		idx := strings.Index(str[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}

	lastPart := parts[len(parts)-1]
	return lastPart == "" || strings.HasSuffix(str, lastPart)
}
