package driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	sqlcache "github.com/officialasishkumar/sql-cache"
)

var testDriverCounter uint64

type stubDriver struct {
	mu        sync.Mutex
	queryHits int
}

func (d *stubDriver) Open(string) (driver.Conn, error) {
	return &stubConn{driver: d}, nil
}

func (d *stubDriver) recordQueryHit() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queryHits++
}

func (d *stubDriver) QueryHits() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.queryHits
}

type stubConn struct {
	driver *stubDriver
}

func (c *stubConn) Prepare(query string) (driver.Stmt, error) {
	return &stubStmt{conn: c, query: query}, nil
}

func (c *stubConn) Close() error { return nil }

func (c *stubConn) Begin() (driver.Tx, error) { return stubTx{}, nil }

func (c *stubConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	c.driver.recordQueryHit()
	return newStubRows(), nil
}

type stubStmt struct {
	conn  *stubConn
	query string
}

func (s *stubStmt) Close() error { return nil }

func (s *stubStmt) NumInput() int { return 1 }

func (s *stubStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (s *stubStmt) Query([]driver.Value) (driver.Rows, error) {
	s.conn.driver.recordQueryHit()
	return newStubRows(), nil
}

type stubTx struct{}

func (stubTx) Commit() error   { return nil }
func (stubTx) Rollback() error { return nil }

type stubRows struct {
	index int
}

func newStubRows() *stubRows {
	return &stubRows{}
}

func (r *stubRows) Columns() []string { return []string{"name"} }

func (r *stubRows) Close() error { return nil }

func (r *stubRows) Next(dest []driver.Value) error {
	if r.index > 0 {
		return io.EOF
	}
	dest[0] = "Alice"
	r.index++
	return nil
}

func TestCachedDriverQueryCachesResults(t *testing.T) {
	db, stub, cache := openCachedTestDB(t)
	defer db.Close()
	defer cache.Close()

	var name string
	if err := db.QueryRow(`SELECT name FROM users WHERE id = ?`, 1).Scan(&name); err != nil {
		t.Fatalf("first QueryRow().Scan() error = %v", err)
	}
	if err := db.QueryRow(`SELECT name FROM users WHERE id = ?`, 1).Scan(&name); err != nil {
		t.Fatalf("second QueryRow().Scan() error = %v", err)
	}
	if name != "Alice" {
		t.Fatalf("name = %q, want %q", name, "Alice")
	}
	if stub.QueryHits() != 1 {
		t.Fatalf("query hits = %d, want 1", stub.QueryHits())
	}
}

func TestCachedDriverPreparedQueryCachesResults(t *testing.T) {
	db, stub, cache := openCachedTestDB(t)
	defer db.Close()
	defer cache.Close()

	stmt, err := db.Prepare(`SELECT name FROM users WHERE id = ?`)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer stmt.Close()

	var name string
	if err := stmt.QueryRow(1).Scan(&name); err != nil {
		t.Fatalf("first stmt.QueryRow().Scan() error = %v", err)
	}
	if err := stmt.QueryRow(1).Scan(&name); err != nil {
		t.Fatalf("second stmt.QueryRow().Scan() error = %v", err)
	}
	if name != "Alice" {
		t.Fatalf("name = %q, want %q", name, "Alice")
	}
	if stub.QueryHits() != 1 {
		t.Fatalf("query hits = %d, want 1", stub.QueryHits())
	}
}

func openCachedTestDB(t *testing.T) (*sql.DB, *stubDriver, *sqlcache.Cache) {
	t.Helper()

	cache, err := sqlcache.New(sqlcache.Options{MockDir: t.TempDir()})
	if err != nil {
		t.Fatalf("sqlcache.New() error = %v", err)
	}

	stub := &stubDriver{}
	name := fmt.Sprintf("cached-stub-%d", atomic.AddUint64(&testDriverCounter, 1))
	sql.Register(name, WrapDriver(name, stub, cache))

	db, err := sql.Open(name, "ignored")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	return db, stub, cache
}
