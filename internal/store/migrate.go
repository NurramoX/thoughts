package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

// migrationFS holds the forward-only migrations, applied in file-name order.
// The n-th file (1-based) brings the database to user_version n. Never edit
// or reorder a released migration; add a new file.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

func migrations() ([]string, error) {
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	scripts := make([]string, len(names))
	for i, name := range names {
		b, err := migrationFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		scripts[i] = string(b)
	}
	return scripts, nil
}

// migrate brings the database up to the newest migration, each in its own
// transaction. A database newer than this binary is an error. Every
// migration is assumed to touch thought, so each ends with a full-text
// 'rebuild' inside its transaction.
func migrate(ctx context.Context, db *sql.DB, scripts []string) error {
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(scripts) {
		return fmt.Errorf("database schema version %d is newer than this binary knows (%d)", current, len(scripts))
	}
	for v := current + 1; v <= len(scripts); v++ {
		if err := apply(ctx, db, v, scripts[v-1]); err != nil {
			return fmt.Errorf("migration %d: %w", v, err)
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, version int, script string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, script); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO thought_fts(thought_fts) VALUES ('rebuild')"); err != nil {
		return err
	}
	// PRAGMA takes no parameters; version is an int.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}
