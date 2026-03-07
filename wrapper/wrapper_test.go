package wrapper

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestOpen(t *testing.T) {
	db, err := Open("sqlite3", ":memory:", Options{MockDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}
