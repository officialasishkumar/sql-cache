package sqlcache

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

type upperScanner struct {
	value string
}

func (s *upperScanner) Scan(src interface{}) error {
	switch v := src.(type) {
	case nil:
		s.value = ""
		return nil
	case string:
		s.value = strings.ToUpper(v)
		return nil
	case []byte:
		s.value = strings.ToUpper(string(v))
		return nil
	default:
		return fmt.Errorf("unsupported source type %T", src)
	}
}

func TestCachedRowsScanSupportsScannerAndRawBytes(t *testing.T) {
	rows := NewCachedRows(
		[]string{"name", "created_at", "payload"},
		[][]interface{}{{"alice", "2024-01-02T03:04:05Z", []byte("abc")}},
	)

	if !rows.Next() {
		t.Fatal("expected first row")
	}

	var (
		name      upperScanner
		createdAt sql.NullTime
		payload   sql.RawBytes
	)
	if err := rows.Scan(&name, &createdAt, &payload); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if name.value != "ALICE" {
		t.Fatalf("name = %q, want %q", name.value, "ALICE")
	}
	if !createdAt.Valid {
		t.Fatal("expected created_at to be valid")
	}
	if !createdAt.Time.Equal(time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("created_at = %v", createdAt.Time)
	}
	if string(payload) != "abc" {
		t.Fatalf("payload = %q, want %q", string(payload), "abc")
	}
}
