package sqlcache

import (
	"database/sql"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// CachedResult represents cached result from Exec.
type CachedResult struct {
	lastInsertID int64
	rowsAffected int64
}

// NewCachedResult creates an exec result from already-captured values.
func NewCachedResult(lastInsertID, rowsAffected int64) *CachedResult {
	return &CachedResult{
		lastInsertID: lastInsertID,
		rowsAffected: rowsAffected,
	}
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
	if dest == nil {
		return fmt.Errorf("destination is nil")
	}
	if scanner, ok := dest.(sql.Scanner); ok {
		return scanner.Scan(normalizeScannerValue(src))
	}
	if src == nil {
		return setZeroValue(dest)
	}

	switch d := dest.(type) {
	case *string:
		*d = toString(src)
	case *[]byte:
		*d = toBytes(src)
	case *sql.RawBytes:
		*d = sql.RawBytes(toBytes(src))
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
	default:
		return assignReflect(dest, src)
	}
	return nil
}

func setZeroValue(dest interface{}) error {
	switch d := dest.(type) {
	case *string:
		*d = ""
	case *[]byte:
		*d = nil
	case *sql.RawBytes:
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
	default:
		destVal := reflect.ValueOf(dest)
		if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
			return fmt.Errorf("unsupported destination type for nil: %T", dest)
		}
		destVal.Elem().SetZero()
	}
	return nil
}

func assignReflect(dest, src interface{}) error {
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Pointer || destVal.IsNil() {
		return fmt.Errorf("unsupported destination type: %T", dest)
	}

	srcVal := reflect.ValueOf(src)
	if !srcVal.IsValid() {
		destVal.Elem().SetZero()
		return nil
	}

	if srcVal.Type().AssignableTo(destVal.Elem().Type()) {
		destVal.Elem().Set(srcVal)
		return nil
	}
	if srcVal.Type().ConvertibleTo(destVal.Elem().Type()) {
		destVal.Elem().Set(srcVal.Convert(destVal.Elem().Type()))
		return nil
	}
	return fmt.Errorf("unsupported destination type: %T", dest)
}

func normalizeScannerValue(src interface{}) interface{} {
	switch v := src.(type) {
	case nil:
		return nil
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		if v > math.MaxInt64 {
			return strconv.FormatUint(v, 10)
		}
		return int64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case sql.RawBytes:
		return toBytes([]byte(v))
	case []byte:
		if parsed, err := toTime(v); err == nil {
			return parsed
		}
		return toBytes(v)
	case string:
		if parsed, err := toTime(v); err == nil {
			return parsed
		}
		return v
	default:
		return src
	}
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
		cp := make([]byte, len(s))
		copy(cp, s)
		return cp
	case sql.RawBytes:
		cp := make([]byte, len(s))
		copy(cp, s)
		return cp
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
		return strconv.ParseInt(strings.TrimSpace(n), 10, 64)
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
	case string:
		return strconv.ParseUint(strings.TrimSpace(n), 10, 64)
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
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
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
		normalized := strings.TrimSpace(strings.ToLower(b))
		if normalized == "yes" {
			return true
		}
		parsed, err := strconv.ParseBool(normalized)
		return err == nil && parsed
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
