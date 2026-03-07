// Package sqlcache provides a transparent SQL query caching layer that intercepts
// SQL calls, checks a local YAML-based cache, and on cache miss forwards the
// query to the real database, captures the response, and saves it for future use.
package sqlcache

import (
	"database/sql"
	"errors"
	"log"
	"sync"

	"github.com/officialasishkumar/sql-cache/matcher"
	"github.com/officialasishkumar/sql-cache/mock"
)

// Mode determines how the cache behaves.
type Mode int

const (
	// ModeAuto checks the cache first and falls back to the real database on miss.
	ModeAuto Mode = iota

	// ModeOffline serves responses only from the cache.
	ModeOffline
)

func (m Mode) String() string {
	switch m {
	case ModeAuto:
		return "auto"
	case ModeOffline:
		return "offline"
	default:
		return "unknown"
	}
}

// Options configures the SQL cache.
type Options struct {
	MockDir string
	DB      *sql.DB

	OnCacheSave   func(query string, args []interface{})
	OnCacheHit    func(query string, args []interface{}, matched bool)
	OnDatabaseHit func(query string, args []interface{})
	OnError       func(err error, context string)

	Logger         *log.Logger
	SequentialMode bool
}

// Cache is the main SQL caching interface.
type Cache struct {
	mu      sync.RWMutex
	mode    Mode
	options Options
	matcher *matcher.Matcher
	mocks   *mock.MockStore
	db      *sql.DB
	stats   CacheStats
}

// CacheStats contains runtime statistics.
type CacheStats struct {
	Mode         string  `json:"mode"`
	TotalMocks   int     `json:"total_mocks"`
	Hits         int64   `json:"hits"`
	Misses       int64   `json:"misses"`
	DatabaseHits int64   `json:"database_hits"`
	Errors       int64   `json:"errors"`
	Saved        int64   `json:"saved"`
	HitRate      float64 `json:"hit_rate"`
}

var (
	ErrCacheMiss   = errors.New("no matching cache entry found")
	ErrNoDatabase  = errors.New("no database connection configured")
	ErrQueryFailed = errors.New("query execution failed")
	ErrInvalidMode = errors.New("invalid mode for this operation")
	ErrParseError  = errors.New("failed to parse query")
)

// New creates a new SQL cache instance.
func New(opts Options) (*Cache, error) {
	m, err := matcher.NewMatcher()
	if err != nil && opts.OnError != nil {
		opts.OnError(err, "creating matcher")
	}

	mockDir := opts.MockDir
	if mockDir == "" {
		mockDir = "./mocks"
	}

	mockStore := mock.NewMockStore(mockDir)
	c := &Cache{
		mode:    ModeAuto,
		options: opts,
		matcher: m,
		mocks:   mockStore,
		db:      opts.DB,
	}

	if err := mockStore.Load(); err != nil {
		c.logError(err, "loading cache")
	}

	return c, nil
}

// SetDB sets the database connection.
func (c *Cache) SetDB(db *sql.DB) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.db = db
}

// SetMode sets the caching mode.
func (c *Cache) SetMode(mode Mode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = mode
	c.mocks.Reset()
}

// GetMode returns the current caching mode.
func (c *Cache) GetMode() Mode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode
}
