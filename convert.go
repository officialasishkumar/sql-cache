package sqlcache

import (
	"database/sql"
	"fmt"
	"time"
)

// CachedResult represents cached result from Exec.
type CachedResult struct {
	lastInsertID int64
	rowsAffected int64
}

// LastInsertId returns the last insert ID.
func (r *CachedResult) LastInsertId() (int64, error) {
	if r == nil {
		return 0, nil
	}
	return r.lastInsertID, nil
}

// RowsAffected returns the number of rows affected.
func (r *CachedResult) RowsAffected() (int64, error) {
	if r == nil {
		return 0, nil
	}
	return r.rowsAffected, nil
}

func convertAssign(dest, src interface{}) error {
	if src == nil {
		return setZeroValue(dest)
	}

	switch d := dest.(type) {
	case *string:
		*d = toString(src)
	case *[]byte:
		*d = toBytes(src)
	case *int:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int(v)
	case *int8:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int8(v)
	case *int16:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int16(v)
	case *int32:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = int32(v)
	case *int64:
		v, err := toInt64(src)
		if err != nil {
			return err
		}
		*d = v
	case *uint:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint(v)
	case *uint8:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint8(v)
	case *uint16:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint16(v)
	case *uint32:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = uint32(v)
	case *uint64:
		v, err := toUint64(src)
		if err != nil {
			return err
		}
		*d = v
	case *float32:
		v, err := toFloat64(src)
		if err != nil {
			return err
		}
		*d = float32(v)
	case *float64:
		v, err := toFloat64(src)
		if err != nil {
			return err
		}
		*d = v
	case *bool:
		*d = toBool(src)
	case *time.Time:
		v, err := toTime(src)
		if err != nil {
			return err
		}
		*d = v
	case *interface{}:
		*d = src
	case *sql.NullString:
		d.Valid = src != nil
		if d.Valid {
			d.String = toString(src)
		}
	case *sql.NullInt64:
		d.Valid = src != nil
		if d.Valid {
			v, _ := toInt64(src)
			d.Int64 = v
		}
	case *sql.NullFloat64:
		d.Valid = src != nil
		if d.Valid {
			v, _ := toFloat64(src)
			d.Float64 = v
		}
	case *sql.NullBool:
		d.Valid = src != nil
		if d.Valid {
			d.Bool = toBool(src)
		}
	default:
		return fmt.Errorf("unsupported destination type: %T", dest)
	}
	return nil
}

func setZeroValue(dest interface{}) error {
	switch d := dest.(type) {
	case *string:
		*d = ""
	case *[]byte:
		*d = nil
	case *int:
		*d = 0
	case *int8:
		*d = 0
	case *int16:
		*d = 0
	case *int32:
		*d = 0
	case *int64:
		*d = 0
	case *uint:
		*d = 0
	case *uint8:
		*d = 0
	case *uint16:
		*d = 0
	case *uint32:
		*d = 0
	case *uint64:
		*d = 0
	case *float32:
		*d = 0
	case *float64:
		*d = 0
	case *bool:
		*d = false
	case *time.Time:
		*d = time.Time{}
	case *interface{}:
		*d = nil
	case *sql.NullString:
		d.Valid = false
		d.String = ""
	case *sql.NullInt64:
		d.Valid = false
		d.Int64 = 0
	case *sql.NullFloat64:
		d.Valid = false
		d.Float64 = 0
	case *sql.NullBool:
		d.Valid = false
		d.Bool = false
	default:
		return fmt.Errorf("unsupported destination type for nil: %T", dest)
	}
	return nil
}

func toString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toBytes(v interface{}) []byte {
	switch s := v.(type) {
	case []byte:
		return s
	case string:
		return []byte(s)
	default:
		return []byte(fmt.Sprintf("%v", v))
	}
}

func toInt64(v interface{}) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint:
		return int64(n), nil
	case uint8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint64:
		return int64(n), nil
	case float32:
		return int64(n), nil
	case float64:
		return int64(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case string:
		var i int64
		_, err := fmt.Sscanf(n, "%d", &i)
		return i, err
	}
	return 0, fmt.Errorf("cannot convert %T to int64", v)
}

func toUint64(v interface{}) (uint64, error) {
	switch n := v.(type) {
	case uint:
		return uint64(n), nil
	case uint8:
		return uint64(n), nil
	case uint16:
		return uint64(n), nil
	case uint32:
		return uint64(n), nil
	case uint64:
		return n, nil
	case int:
		return uint64(n), nil
	case int8:
		return uint64(n), nil
	case int16:
		return uint64(n), nil
	case int32:
		return uint64(n), nil
	case int64:
		return uint64(n), nil
	case float32:
		return uint64(n), nil
	case float64:
		return uint64(n), nil
	}
	return 0, fmt.Errorf("cannot convert %T to uint64", v)
}

func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%f", &f)
		return f, err
	}
	return 0, fmt.Errorf("cannot convert %T to float64", v)
}

func toBool(v interface{}) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int, int8, int16, int32, int64:
		n, _ := toInt64(v)
		return n != 0
	case uint, uint8, uint16, uint32, uint64:
		n, _ := toUint64(v)
		return n != 0
	case string:
		return b == "true" || b == "1" || b == "yes"
	}
	return false
}

func toTime(v interface{}) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case string:
		for _, format := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(format, t); err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse time: %s", t)
	case []byte:
		return toTime(string(t))
	}
	return time.Time{}, fmt.Errorf("cannot convert %T to time.Time", v)
}
