package sqlmeta

import (
	"fmt"
	"sort"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

// Metadata contains the parsed metadata needed for cache matching.
type Metadata struct {
	Type             string
	Tables           []string
	Fingerprint      string
	PlaceholderCount int
	IsDML            bool
}

// Analyze parses a query and extracts matching metadata from it.
func Analyze(parser *sqlparser.Parser, query string) (Metadata, error) {
	query = strings.TrimSpace(query)

	meta := Metadata{
		Type:             DetectQueryType(query),
		PlaceholderCount: CountPlaceholders(query),
		IsDML:            sqlparser.IsDML(query),
	}

	if parser == nil {
		return meta, fmt.Errorf("parser not initialized")
	}

	stmt, err := parser.Parse(query)
	if err != nil {
		return meta, fmt.Errorf("failed to parse SQL: %w", err)
	}

	if stmtType := StatementType(stmt); stmtType != "OTHER" {
		meta.Type = stmtType
	}
	meta.Tables = ExtractTables(stmt)

	fingerprint, err := Fingerprint(parser, query)
	if err != nil {
		return meta, err
	}
	meta.Fingerprint = fingerprint

	return meta, nil
}

// Fingerprint returns a canonical query fingerprint that normalizes formatting
// and identifier escaping but preserves the query's semantic values.
func Fingerprint(parser *sqlparser.Parser, query string) (string, error) {
	if parser == nil {
		return "", fmt.Errorf("parser not initialized")
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	if normalized, err := parser.NormalizeAlphabetically(query); err == nil {
		query = normalized
	}

	stmt, err := parser.Parse(query)
	if err != nil {
		return "", fmt.Errorf("failed to parse SQL: %w", err)
	}

	return sqlparser.CanonicalString(stmt), nil
}

// ExtractTables returns the unique table names referenced by a query.
func ExtractTables(stmt sqlparser.Statement) []string {
	tables := make([]string, 0)
	tableSet := make(map[string]struct{})

	_ = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		if tableName, ok := node.(sqlparser.TableName); ok {
			name := tableName.Name.String()
			if name != "" {
				if _, exists := tableSet[name]; !exists {
					tableSet[name] = struct{}{}
					tables = append(tables, name)
				}
			}
		}
		return true, nil
	}, stmt)

	sort.Strings(tables)
	return tables
}

// StatementType returns the normalized SQL statement type for a parsed statement.
func StatementType(stmt sqlparser.Statement) string {
	switch stmt.(type) {
	case *sqlparser.Select:
		return "SELECT"
	case *sqlparser.Insert:
		return "INSERT"
	case *sqlparser.Update:
		return "UPDATE"
	case *sqlparser.Delete:
		return "DELETE"
	case *sqlparser.Begin:
		return "BEGIN"
	case *sqlparser.Commit:
		return "COMMIT"
	case *sqlparser.Rollback:
		return "ROLLBACK"
	case *sqlparser.Use:
		return "USE"
	case *sqlparser.Show:
		return "SHOW"
	case *sqlparser.ExplainStmt:
		return "EXPLAIN"
	case *sqlparser.CallProc:
		return "CALL"
	case *sqlparser.CreateTable:
		return "CREATE_TABLE"
	case *sqlparser.AlterTable:
		return "ALTER_TABLE"
	case *sqlparser.DropTable:
		return "DROP_TABLE"
	case *sqlparser.CreateDatabase:
		return "CREATE_DATABASE"
	case *sqlparser.CreateView:
		return "CREATE_VIEW"
	case *sqlparser.DropView:
		return "DROP_VIEW"
	default:
		return "OTHER"
	}
}

// DetectQueryType is a lightweight prefix-based fallback used when parsing fails.
func DetectQueryType(query string) string {
	q := strings.TrimSpace(strings.ToUpper(query))
	switch {
	case strings.HasPrefix(q, "SELECT"):
		return "SELECT"
	case strings.HasPrefix(q, "INSERT"):
		return "INSERT"
	case strings.HasPrefix(q, "UPDATE"):
		return "UPDATE"
	case strings.HasPrefix(q, "DELETE"):
		return "DELETE"
	case strings.HasPrefix(q, "REPLACE"):
		return "REPLACE"
	case strings.HasPrefix(q, "CALL"):
		return "CALL"
	case strings.HasPrefix(q, "SHOW"):
		return "SHOW"
	case strings.HasPrefix(q, "DESCRIBE"), strings.HasPrefix(q, "DESC"):
		return "DESCRIBE"
	case strings.HasPrefix(q, "EXPLAIN"):
		return "EXPLAIN"
	case strings.HasPrefix(q, "BEGIN"), strings.HasPrefix(q, "START TRANSACTION"):
		return "BEGIN"
	case strings.HasPrefix(q, "COMMIT"):
		return "COMMIT"
	case strings.HasPrefix(q, "ROLLBACK"):
		return "ROLLBACK"
	case strings.HasPrefix(q, "USE"):
		return "USE"
	default:
		return "OTHER"
	}
}

// CountPlaceholders counts bind parameters in SQL across both "?" and PostgreSQL
// numbered placeholders like "$1".
func CountPlaceholders(query string) int {
	questionMarks := 0
	numbered := make(map[string]struct{})

	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(query); i++ {
		ch := query[i]
		next := byte(0)
		if i+1 < len(query) {
			next = query[i+1]
		}

		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
			}
			continue
		case inBlockComment:
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		case inSingleQuote:
			if ch == '\\' && i+1 < len(query) {
				i++
				continue
			}
			if ch == '\'' {
				if next == '\'' {
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		case inDoubleQuote:
			if ch == '\\' && i+1 < len(query) {
				i++
				continue
			}
			if ch == '"' {
				if next == '"' {
					i++
					continue
				}
				inDoubleQuote = false
			}
			continue
		case inBacktick:
			if ch == '`' {
				inBacktick = false
			}
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '`':
			inBacktick = true
		case '#':
			inLineComment = true
		case '-':
			if next == '-' && (i+2 >= len(query) || isWhitespace(query[i+2])) {
				inLineComment = true
				i++
			}
		case '/':
			if next == '*' {
				inBlockComment = true
				i++
			}
		case '?':
			questionMarks++
		case '$':
			j := i + 1
			for j < len(query) && query[j] >= '0' && query[j] <= '9' {
				j++
			}
			if j > i+1 {
				numbered[query[i+1:j]] = struct{}{}
				i = j - 1
			}
		}
	}

	return questionMarks + len(numbered)
}

func isWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}
