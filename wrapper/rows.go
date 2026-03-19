package wrapper

import (
	"database/sql"

	sqlcache "github.com/officialasishkumar/sql-cache"
)

// Rows wraps CachedRows for compatibility with sql.Rows-like usage.
type Rows struct {
	cached *sqlcache.CachedRows
	live   *sql.Rows
}

// Columns returns column names.
func (r *Rows) Columns() ([]string, error) {
	if r == nil {
		return nil, sql.ErrNoRows
	}
	if r.cached != nil {
		return r.cached.Columns(), nil
	}
	if r.live != nil {
		return r.live.Columns()
	}
	return nil, sql.ErrNoRows
}

// Next advances to next row.
func (r *Rows) Next() bool {
	if r == nil {
		return false
	}
	if r.cached != nil {
		return r.cached.Next()
	}
	if r.live != nil {
		return r.live.Next()
	}
	return false
}

// Scan copies column values into dest.
func (r *Rows) Scan(dest ...interface{}) error {
	if r == nil {
		return sql.ErrNoRows
	}
	if r.cached != nil {
		return r.cached.Scan(dest...)
	}
	if r.live != nil {
		return r.live.Scan(dest...)
	}
	return sql.ErrNoRows
}

// Close closes the rows.
func (r *Rows) Close() error {
	if r == nil {
		return nil
	}
	if r.cached != nil {
		return r.cached.Close()
	}
	if r.live != nil {
		return r.live.Close()
	}
	return nil
}

// Err returns any error.
func (r *Rows) Err() error {
	if r == nil {
		return nil
	}
	if r.cached != nil {
		return r.cached.Err()
	}
	if r.live != nil {
		return r.live.Err()
	}
	return nil
}

// Row wraps a single row result.
type Row struct {
	rows *Rows
	err  error
}

// Scan copies columns into dest.
func (r *Row) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	if r.rows == nil {
		return sql.ErrNoRows
	}
	defer func() { _ = r.rows.Close() }()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return r.rows.Scan(dest...)
}

// Err returns any error.
func (r *Row) Err() error { return r.err }
