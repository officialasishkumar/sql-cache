package mock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Save saves all cache entries to a single YAML file.
func (s *MockStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dir == "" {
		return fmt.Errorf("no directory configured")
	}
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return fmt.Errorf("failed to create mock directory: %w", err)
	}

	mocksFile := filepath.Join(s.dir, "mocks.yaml")
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

	tempFile := mocksFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp mocks file: %w", err)
	}
	if err := os.Rename(tempFile, mocksFile); err != nil {
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename mocks file: %w", err)
	}
	return nil
}

// Load loads cache entries from disk.
func (s *MockStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == "" {
		return nil
	}

	mocksFile := filepath.Join(s.dir, "mocks.yaml")
	data, err := os.ReadFile(mocksFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read mocks file: %w", err)
	}

	docs := splitYAMLDocuments(data)
	s.mocks = make([]*Mock, 0, len(docs))
	s.mockIndex = make(map[string]*Mock)
	for _, doc := range docs {
		if len(strings.TrimSpace(string(doc))) == 0 {
			continue
		}
		var mock Mock
		if err := yaml.Unmarshal(doc, &mock); err != nil {
			continue
		}
		mock.Spec.Consumed = false
		mock.Spec.SortOrder = 0
		key := s.generateKey(&mock)
		s.mocks = append(s.mocks, &mock)
		s.mockIndex[key] = &mock
	}
	return nil
}

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
