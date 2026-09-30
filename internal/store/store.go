// Package store keeps thoughts in SQLite (spec §2, §3): validation of every
// data-model rule, migrations, the full-text index and the Filter compiler.
// Only the daemon opens it.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
	"github.com/NurramoX/thoughts/internal/filter"

	_ "modernc.org/sqlite"
)

// ErrNotFound: an unknown or deleted id (404).
var ErrNotFound = errors.New("thought not found")

// ErrTooLarge: a body over api.MaxBody (413).
var ErrTooLarge = errors.New("body over 10 MB")

// InvalidError: content that breaks a data-model rule (422).
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// StaleError: the Precondition named a Version other than the current one
// (412).
type StaleError struct{ Current int64 }

func (e *StaleError) Error() string {
	return fmt.Sprintf("stale version, current is %d", e.Current)
}

// Sort fields. Desc puts the newest first for the timestamps, and the best
// match first for rank. Title compares under Unicode simple case folding. Ties
// break on id, in the same direction.
const (
	SortUpdated = "updated"
	SortCreated = "created"
	SortTitle   = "title"
	SortRank    = "rank" // only with a ranked text term
)

// Query selects and orders thoughts. The server has already resolved the
// defaults, so Sort and Desc are always set deliberately, and rejected
// sort=rank without a ranked term.
type Query struct {
	Filter filter.Expr // nil matches every thought
	Sort   string
	Desc   bool
	Limit  int // 0 means no limit
	Offset int
}

// Store is the thought store. Every write validates its input (InvalidError,
// ErrTooLarge), checks the Precondition when it is not zero (StaleError;
// Force always passes), and returns the Version after the write. A write that
// changes nothing moves neither version nor updated_at. The server, not the
// store, decides where a Precondition is required.
type Store interface {
	Create(ctx context.Context, req api.CreateRequest) (api.Thought, error)
	Get(ctx context.Context, id int64) (api.Thought, error)
	List(ctx context.Context, q Query) (api.List, error)
	Patch(ctx context.Context, id int64, pre api.Precondition, p api.Patch) (api.Thought, error)
	PutBody(ctx context.Context, id int64, pre api.Precondition, body []byte) (version int64, err error)
	PutTag(ctx context.Context, id int64, pre api.Precondition, tag string) (version int64, err error)
	DeleteTag(ctx context.Context, id int64, pre api.Precondition, tag string) (version int64, err error)
	PutAttribute(ctx context.Context, id int64, pre api.Precondition, key, value string) (version int64, err error)
	DeleteAttribute(ctx context.Context, id int64, pre api.Precondition, key string) (version int64, err error)
	Delete(ctx context.Context, id int64, pre api.Precondition) error
	// Tags, Attributes and AttributeValues return the vocabulary in use,
	// sorted by tag, key or value.
	Tags(ctx context.Context) ([]api.TagCount, error)
	Attributes(ctx context.Context) ([]api.KeyCount, error)
	AttributeValues(ctx context.Context, key string) ([]api.ValueCount, error)
	// Close checkpoints the WAL (TRUNCATE) and closes the database.
	Close() error
}

// Open opens (creating if needed) the database at path and runs the
// migrations. now is the clock for every timestamp the store sets.
func Open(path string, now func() time.Time) (Store, error) {
	// The driver reads everything after the first '?' as parameters.
	if path == "" || strings.ContainsRune(path, '?') {
		return nil, fmt.Errorf("unusable database path %q", path)
	}
	db, err := sql.Open("sqlite", path+"?"+pragmas)
	if err != nil {
		return nil, err
	}
	// One connection: every statement serialises, and the pragmas, set per
	// connection by the driver, always hold.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	scripts, err := migrations()
	if err == nil {
		err = migrate(context.Background(), db, scripts)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteStore{db: db, now: now}, nil
}

// pragmas are applied by the driver to every connection it opens.
const pragmas = "_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=synchronous(FULL)"
