// Package mock provides cache entry storage and retrieval for SQL queries.
package mock

import "time"

// Version is the mock format version.
const Version = "sql-cache/v1"

// Mock represents a cached SQL interaction.
type Mock struct {
	Version        string         `yaml:"version"`
	Kind           string         `yaml:"kind"`
	Name           string         `yaml:"name"`
	Spec           MockSpec       `yaml:"spec"`
	CacheEntryInfo CacheEntryInfo `yaml:"CacheEntryInfo,omitempty"`
	ConnectionID   string         `yaml:"ConnectionId,omitempty"`
}

// CacheEntryInfo tracks cache entry usage.
type CacheEntryInfo struct {
	ID         int   `yaml:"Id,omitempty"`
	IsFiltered bool  `yaml:"isFiltered,omitempty"`
	SortOrder  int64 `yaml:"sortOrder,omitempty"`
}

// MockSpec contains the mock specification.
type MockSpec struct {
	Metadata  map[string]string `yaml:"Metadata,omitempty"`
	Request   RequestSpec       `yaml:"Request"`
	Response  ResponseSpec      `yaml:"Response"`
	Created   int64             `yaml:"Created"`
	Consumed  bool              `yaml:"consumed,omitempty"`
	SortOrder int               `yaml:"sort_order,omitempty"`

	ReqTimestampMock time.Time `yaml:"ReqTimestampMock,omitempty"`
	ResTimestampMock time.Time `yaml:"ResTimestampMock,omitempty"`
}

// RequestSpec represents a SQL request.
type RequestSpec struct {
	Query     string        `yaml:"Query"`
	Args      []interface{} `yaml:"Args,omitempty"`
	Type      string        `yaml:"Type"`
	Tables    []string      `yaml:"Tables,omitempty"`
	Structure string        `yaml:"Structure,omitempty"`

	PlaceholderCount int    `yaml:"PlaceholderCount,omitempty"`
	IsDML            bool   `yaml:"IsDML,omitempty"`
	QueryHash        string `yaml:"QueryHash,omitempty"`
	Database         string `yaml:"Database,omitempty"`
	Timeout          int64  `yaml:"Timeout,omitempty"`
}

// ResponseSpec represents a SQL response.
type ResponseSpec struct {
	Columns []string        `yaml:"Columns,omitempty"`
	Rows    [][]interface{} `yaml:"Rows,omitempty"`

	LastInsertID  int64  `yaml:"LastInsertID,omitempty"`
	RowsAffected  int64  `yaml:"RowsAffected,omitempty"`
	Error         string `yaml:"Error,omitempty"`
	ErrorCode     int    `yaml:"ErrorCode,omitempty"`
	RowCount      int    `yaml:"RowCount,omitempty"`
	ExecutionTime int64  `yaml:"ExecutionTime,omitempty"`
	WarningCount  int    `yaml:"WarningCount,omitempty"`
}

// Stats returns store statistics.
type Stats struct {
	Total      int `json:"total"`
	Consumed   int `json:"consumed"`
	Unconsumed int `json:"unconsumed"`
}
