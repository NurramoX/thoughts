package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/NurramoX/thoughts/internal/api"
)

// sqliteStore is the Store over one SQLite connection.
type sqliteStore struct {
	db  *sql.DB
	now func() time.Time
}

// stamp is the current time as stored: UTC, milliseconds, api.TimeLayout,
// so that stored timestamps sort lexically.
func (s *sqliteStore) stamp() string {
	return s.now().UTC().Truncate(time.Millisecond).Format(api.TimeLayout)
}

// inTx runs fn in a transaction and commits when it returns nil.
func (s *sqliteStore) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// edit is what one write does to a thought: new values for the thought row, and
// whether anything in the tag or attribute tables changed.
type edit struct {
	title *string // set only when it differs
	body  *string // set only when it differs
	other bool
}

// write runs mutate on thought id inside a transaction, after checking that the
// thought exists and that pre holds. When mutate reports a real change, one
// UPDATE of the thought row applies the new title and body and advances version
// and updated_at; otherwise the row is untouched. It returns the version
// after the write.
func (s *sqliteStore) write(ctx context.Context, tx *sql.Tx, id int64, pre api.Precondition, mutate func(*sql.Tx, current) (edit, error)) (int64, error) {
	cur, err := loadCurrent(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if err := check(pre, cur.version); err != nil {
		return 0, err
	}
	e, err := mutate(tx, cur)
	if err != nil {
		return 0, err
	}
	if e.title == nil && e.body == nil && !e.other {
		return cur.version, nil
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE thought SET
			title = coalesce(?, title),
			body = coalesce(?, body),
			version = version + 1,
			updated_at = ?
		WHERE id = ?`, e.title, e.body, s.stamp(), id)
	if err != nil {
		return 0, err
	}
	return cur.version + 1, nil
}

// current is what a write needs to know about a thought before changing it.
type current struct {
	version int64
	title   string
}

func loadCurrent(ctx context.Context, tx *sql.Tx, id int64) (current, error) {
	var c current
	err := tx.QueryRowContext(ctx, `SELECT version, title FROM thought WHERE id = ?`, id).Scan(&c.version, &c.title)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// check enforces a Precondition against the current version.
func check(pre api.Precondition, version int64) error {
	if pre.IsZero() || pre.Force || pre.Version == version {
		return nil
	}
	return &StaleError{Current: version}
}

func (s *sqliteStore) Close() error {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return errors.Join(err, s.db.Close())
}
