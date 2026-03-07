package sqlcache

import (
	"database/sql"
	"fmt"
)

// CachedRows represents cached query results.
type CachedRows struct {
	columns  []string
	rows     [][]interface{}
	rowIndex int
	err      error
}

// NewCachedRowsFromSQL creates CachedRows from sql.Rows.
func NewCachedRowsFromSQL(rows *sql.Rows) (*CachedRows, error) {
	if rows == nil {
		return &CachedRows{columns: []string{}, rows: [][]interface{}{}, rowIndex: -1}, nil
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	allRows := make([][]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		rowCopy := make([]interface{}, len(values))
		for i, v := range values {
			rowCopy[i] = copyValue(v)
		}
		allRows = append(allRows, rowCopy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &CachedRows{columns: columns, rows: allRows, rowIndex: -1}, nil
}

func copyValue(v interface{}) interface{} {
	if b, ok := v.([]byte); ok {
		cp := make([]byte, len(b))
		copy(cp, b)
		return cp
	}
	return v
}

// Columns returns the column names.
func (r *CachedRows) Columns() []string {
	if r == nil {
		return nil
	}
	return r.columns
}

// Next advances to the next row.
func (r *CachedRows) Next() bool {
	if r == nil || r.err != nil {
		return false
	}
	r.rowIndex++
	return r.rowIndex < len(r.rows)
}

// Scan copies the current row values into dest.
func (r *CachedRows) Scan(dest ...interface{}) error {
	if r == nil {
		return sql.ErrNoRows
	}
	if r.rowIndex < 0 || r.rowIndex >= len(r.rows) {
		return sql.ErrNoRows
	}

	row := r.rows[r.rowIndex]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: expected %d arguments, got %d", len(row), len(dest))
	}
	for i, v := range row {
		if err := convertAssign(dest[i], v); err != nil {
			return fmt.Errorf("scan column %d: %w", i, err)
		}
	}
	return nil
}

// Close closes the rows.
func (r *CachedRows) Close() error { return nil }

// Err returns any error.
func (r *CachedRows) Err() error {
	if r == nil {
		return nil
	}
	return r.err
}

// All returns all rows.
func (r *CachedRows) All() [][]interface{} {
	if r == nil {
		return nil
	}
	return r.rows
}

// Count returns the number of rows.
func (r *CachedRows) Count() int {
	if r == nil {
		return 0
	}
	return len(r.rows)
}
