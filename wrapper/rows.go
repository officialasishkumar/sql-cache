package wrapper

import (
	"database/sql"

	sqlcache "github.com/officialasishkumar/sql-cache"
)

// Rows wraps CachedRows for compatibility with sql.Rows-like usage.
type Rows struct {
	cached *sqlcache.CachedRows
}

// Columns returns column names.
func (r *Rows) Columns() ([]string, error) {
	if r == nil || r.cached == nil {
		return nil, sql.ErrNoRows
	}
	return r.cached.Columns(), nil
}

// Next advances to next row.
func (r *Rows) Next() bool {
	if r == nil || r.cached == nil {
		return false
	}
	return r.cached.Next()
}

// Scan copies column values into dest.
func (r *Rows) Scan(dest ...interface{}) error {
	if r == nil || r.cached == nil {
		return sql.ErrNoRows
	}
	return r.cached.Scan(dest...)
}

// Close closes the rows.
func (r *Rows) Close() error {
	if r == nil || r.cached == nil {
		return nil
	}
	return r.cached.Close()
}

// Err returns any error.
func (r *Rows) Err() error {
	if r == nil || r.cached == nil {
		return nil
	}
	return r.cached.Err()
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
