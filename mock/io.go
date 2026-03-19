package mock

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	loaded := make([]*Mock, 0)
	index := 0

	for {
		var mock Mock
		err := decoder.Decode(&mock)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to decode cache entry %d: %w", index+1, err)
		}
		if mock.Name == "" && mock.Kind == "" && mock.Version == "" && mock.Spec.Request.Query == "" {
			continue
		}

		mock.Spec.Consumed = false
		mock.Spec.SortOrder = 0
		loaded = append(loaded, &mock)
		index++
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.mocks = make([]*Mock, 0, len(loaded))
	s.mockIndex = make(map[string]*Mock, len(loaded))
	for _, mock := range loaded {
		key := s.generateKey(mock)
		s.mocks = append(s.mocks, mock)
		s.mockIndex[key] = mock
	}
	return nil
}
