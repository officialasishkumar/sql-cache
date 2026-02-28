// Package parser provides SQL query parsing and normalization using vitess sqlparser.
// It creates structural signatures for SQL queries that allow matching similar queries
// even when they have different literal values.
//
// This implementation uses AST-based structural matching.
package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"vitess.io/vitess/go/vt/sqlparser"
)

// Parser handles SQL query parsing and normalization
type Parser struct {
	parser *sqlparser.Parser
	// Cache for query signatures to avoid repeated parsing
	signatureCache sync.Map
}

// NewParser creates a new SQL parser instance
func NewParser() (*Parser, error) {
	opts := sqlparser.Options{}
	parser, err := sqlparser.New(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create SQL parser: %w", err)
	}
	return &Parser{parser: parser}, nil
}

// QuerySignature represents a normalized SQL query signature
type QuerySignature struct {
	// Original is the original SQL query
	Original string
	// Structure is the AST structure signature
	Structure string
	// Hash is a unique hash of the signature for fast comparison
	Hash string
	// Type is the type of SQL statement (SELECT, INSERT, UPDATE, DELETE, etc.)
	Type string
	// Tables contains the tables referenced in the query
	Tables []string
	// IsDML indicates if this is a DML statement
	IsDML bool
}

// Parse parses a SQL query and returns its signature
func (p *Parser) Parse(sql string) (*QuerySignature, error) {
	sql = strings.TrimSpace(sql)

	// Check cache first
	if cached, ok := p.signatureCache.Load(sql); ok {
		return cached.(*QuerySignature), nil
	}

	stmt, err := p.parser.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL: %w", err)
	}

	sig := &QuerySignature{
		Original: sql,
		IsDML:    sqlparser.IsDML(sql),
	}

	// Get statement type
	sig.Type = getStatementType(stmt)

	// Get tables from the query
	sig.Tables = extractTables(stmt)

	// Get structural signature by walking the AST
	structure, err := getQueryStructure(stmt)
	if err != nil {
		return nil, err
	}
	sig.Structure = structure

	// Create hash from original query + structure
	sig.Hash = createHash(sql + "|" + sig.Structure)

	// Cache the result
	p.signatureCache.Store(sql, sig)

	return sig, nil
}

// Match compares two query signatures and returns a match score
// Returns: exact (bool), structuralMatch (bool), score (int)
func (p *Parser) Match(expected, actual *QuerySignature) (exact bool, structural bool, score int) {
	// Exact match
	if expected.Original == actual.Original {
		return true, true, 100
	}

	// Hash match (same query + structure)
	if expected.Hash == actual.Hash {
		return false, true, 90
	}

	// Structure match (same AST structure but different values)
	if expected.Structure == actual.Structure {
		return false, true, 80
	}

	// Type and tables match
	if expected.Type == actual.Type && tablesMatch(expected.Tables, actual.Tables) {
		return false, true, 60
	}

	// Only type matches
	if expected.Type == actual.Type {
		return false, false, 30
	}

	return false, false, 0
}

// IsDML checks if the SQL is a DML statement
func (p *Parser) IsDML(sql string) bool {
	return sqlparser.IsDML(sql)
}

// getStatementType returns the type of SQL statement
func getStatementType(stmt sqlparser.Statement) string {
	switch stmt.(type) {
	case *sqlparser.Select:
		return "SELECT"
	case *sqlparser.Insert:
		return "INSERT"
	case *sqlparser.Update:
		return "UPDATE"
	case *sqlparser.Delete:
		return "DELETE"
	case *sqlparser.CreateTable:
		return "CREATE_TABLE"
	case *sqlparser.AlterTable:
		return "ALTER_TABLE"
	case *sqlparser.DropTable:
		return "DROP_TABLE"
	case *sqlparser.CreateDatabase:
		return "CREATE_DATABASE"
	case *sqlparser.Begin:
		return "BEGIN"
	case *sqlparser.Commit:
		return "COMMIT"
	case *sqlparser.Rollback:
		return "ROLLBACK"
	default:
		return "OTHER"
	}
}

// extractTables extracts table names from a SQL statement
func extractTables(stmt sqlparser.Statement) []string {
	tables := make([]string, 0)
	tableSet := make(map[string]bool)

	_ = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		switch n := node.(type) {
		case sqlparser.TableName:
			tableName := n.Name.String()
			if tableName != "" && !tableSet[tableName] {
				tableSet[tableName] = true
				tables = append(tables, tableName)
			}
		}
		return true, nil
	}, stmt)

	return tables
}

// getQueryStructure creates a structural signature by walking the AST
// This creates a signature based on the Go types of AST nodes
func getQueryStructure(stmt sqlparser.Statement) (string, error) {
	var structureParts []string

	// Walk the AST and collect the Go type of each grammatical node.
	err := sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		structureParts = append(structureParts, reflect.TypeOf(node).String())
		return true, nil
	}, stmt)

	if err != nil {
		return "", fmt.Errorf("failed to walk AST: %w", err)
	}

	return strings.Join(structureParts, "->"), nil
}

// createHash creates a SHA256 hash of the input
func createHash(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// tablesMatch checks if two table lists match (order independent)
func tablesMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSet := make(map[string]bool)
	for _, t := range a {
		aSet[t] = true
	}
	for _, t := range b {
		if !aSet[t] {
			return false
		}
	}
	return true
}

// GetQueryStructureCached returns the cached structure for a query
func (p *Parser) GetQueryStructureCached(sql string) (string, error) {
	sig, err := p.Parse(sql)
	if err != nil {
		return "", err
	}
	return sig.Structure, nil
}
