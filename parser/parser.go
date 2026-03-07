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
	"strings"
	"sync"

	"github.com/officialasishkumar/sql-cache/internal/sqlmeta"
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
	// Structure is the canonical SQL fingerprint used for matching.
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

	meta, err := sqlmeta.Analyze(p.parser, sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL: %w", err)
	}

	sig := &QuerySignature{
		Original:  sql,
		Structure: meta.Fingerprint,
		Type:      meta.Type,
		Tables:    meta.Tables,
		IsDML:     meta.IsDML,
	}

	// Create hash from the canonical query fingerprint.
	sig.Hash = createHash(sig.Structure)

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

// createHash creates a SHA256 hash of the input
func createHash(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// GetQueryStructureCached returns the cached structure for a query
func (p *Parser) GetQueryStructureCached(sql string) (string, error) {
	sig, err := p.Parse(sql)
	if err != nil {
		return "", err
	}
	return sig.Structure, nil
}
