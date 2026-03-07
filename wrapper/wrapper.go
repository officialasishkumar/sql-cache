// Package wrapper provides an easy-to-use wrapper around *sql.DB that intercepts
// queries and provides transparent SQL response caching.
package wrapper

import (
	"database/sql"
	"fmt"
	"log"
	"sync"

	sqlcache "github.com/officialasishkumar/sql-cache"
)

// DB wraps a *sql.DB with caching functionality.
type DB struct {
	underlying *sql.DB
	cache      *sqlcache.Cache
	mu         sync.RWMutex
	closed     bool
}

// Options configures the cached DB wrapper.
type Options struct {
	MockDir        string
	InitialMode    sqlcache.Mode
	SequentialMode bool

	OnCacheSave   func(query string, args []interface{})
	OnCacheHit    func(query string, args []interface{}, matched bool)
	OnDatabaseHit func(query string, args []interface{})
	Logger        *log.Logger
	OnError       func(err error, context string)
}

// Open creates a new sql.DB and wraps it with caching in one step.
func Open(driverName, dsn string, opts Options) (*DB, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}

	wrapped, err := Wrap(db, opts)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return wrapped, nil
}

// Wrap wraps an existing *sql.DB with caching support.
func Wrap(db *sql.DB, opts Options) (*DB, error) {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir:        opts.MockDir,
		DB:             db,
		OnCacheSave:    opts.OnCacheSave,
		OnCacheHit:     opts.OnCacheHit,
		OnDatabaseHit:  opts.OnDatabaseHit,
		Logger:         opts.Logger,
		OnError:        opts.OnError,
		SequentialMode: opts.SequentialMode,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}
	if opts.InitialMode != 0 {
		cache.SetMode(opts.InitialMode)
	}
	return &DB{underlying: db, cache: cache}, nil
}

// NewOffline creates a wrapper for offline mode with no database connection.
func NewOffline(opts Options) (*DB, error) {
	cache, err := sqlcache.New(sqlcache.Options{
		MockDir:        opts.MockDir,
		OnCacheHit:     opts.OnCacheHit,
		OnDatabaseHit:  opts.OnDatabaseHit,
		Logger:         opts.Logger,
		OnError:        opts.OnError,
		SequentialMode: opts.SequentialMode,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	cache.SetMode(sqlcache.ModeOffline)
	return &DB{cache: cache}, nil
}
