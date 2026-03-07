package wrapper

import (
	"bytes"
	"database/sql"
	"errors"
	"log"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	sqlcache "github.com/officialasishkumar/sql-cache"
)

func TestWrap_LogsAndTracksDatabaseHits(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (name) VALUES ('Alice')`); err != nil {
		t.Fatalf("insert error = %v", err)
	}

	var dbHitCount int
	logBuf := &bytes.Buffer{}

	cachedDB, err := Wrap(db, Options{
		MockDir: t.TempDir(),
		Logger:  log.New(logBuf, "", 0),
		OnDatabaseHit: func(query string, args []interface{}) {
			dbHitCount++
		},
	})
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}
	defer cachedDB.Close()

	row := cachedDB.QueryRow("SELECT name FROM users WHERE id = ?", 1)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("first Scan() error = %v", err)
	}

	row = cachedDB.QueryRow("SELECT name FROM users WHERE id = ?", 1)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("second Scan() error = %v", err)
	}

	if dbHitCount != 1 {
		t.Fatalf("dbHitCount = %d, want 1", dbHitCount)
	}

	stats := cachedDB.Stats()
	if stats.DatabaseHits != 1 {
		t.Fatalf("stats.DatabaseHits = %d, want 1", stats.DatabaseHits)
	}

	if !strings.Contains(logBuf.String(), "database fallback for uncached query") {
		t.Fatalf("expected warning log, got %q", logBuf.String())
	}
}

func TestNewOffline_NeverHitsDatabase(t *testing.T) {
	var dbHitCount int
	logBuf := &bytes.Buffer{}

	cachedDB, err := NewOffline(Options{
		MockDir: t.TempDir(),
		Logger:  log.New(logBuf, "", 0),
		OnDatabaseHit: func(query string, args []interface{}) {
			dbHitCount++
		},
	})
	if err != nil {
		t.Fatalf("NewOffline() error = %v", err)
	}
	defer cachedDB.Close()

	if _, err := cachedDB.Query("SELECT name FROM users WHERE id = ?", 1); !errors.Is(err, sqlcache.ErrCacheMiss) {
		t.Fatalf("Query() error = %v, want ErrCacheMiss", err)
	}

	if dbHitCount != 0 {
		t.Fatalf("dbHitCount = %d, want 0", dbHitCount)
	}

	stats := cachedDB.Stats()
	if stats.DatabaseHits != 0 {
		t.Fatalf("stats.DatabaseHits = %d, want 0", stats.DatabaseHits)
	}

	if strings.Contains(logBuf.String(), "database fallback") {
		t.Fatalf("did not expect database fallback log, got %q", logBuf.String())
	}
}
