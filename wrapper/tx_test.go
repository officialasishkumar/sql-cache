package wrapper

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestTxBypassesCacheInAutoMode(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, name) VALUES (1, 'Alice')`); err != nil {
		t.Fatalf("insert error = %v", err)
	}

	cachedDB, err := Wrap(db, Options{MockDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}
	defer cachedDB.Close()

	var name string
	if err := cachedDB.QueryRow(`SELECT name FROM users WHERE id = ?`, 1).Scan(&name); err != nil {
		t.Fatalf("initial QueryRow() error = %v", err)
	}
	if name != "Alice" {
		t.Fatalf("initial name = %q, want %q", name, "Alice")
	}

	tx, err := cachedDB.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	if _, err := tx.Exec(`UPDATE users SET name = ? WHERE id = ?`, "Bob", 1); err != nil {
		t.Fatalf("tx.Exec() error = %v", err)
	}

	stmt, err := tx.Prepare(`SELECT name FROM users WHERE id = ?`)
	if err != nil {
		t.Fatalf("tx.Prepare() error = %v", err)
	}
	defer stmt.Close()

	if err := stmt.QueryRow(1).Scan(&name); err != nil {
		t.Fatalf("stmt.QueryRow() error = %v", err)
	}
	if name != "Bob" {
		t.Fatalf("name inside tx = %q, want %q", name, "Bob")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	if err := cachedDB.QueryRow(`SELECT name FROM users WHERE id = ?`, 1).Scan(&name); err != nil {
		t.Fatalf("post-rollback QueryRow() error = %v", err)
	}
	if name != "Alice" {
		t.Fatalf("name after rollback = %q, want %q", name, "Alice")
	}
}
