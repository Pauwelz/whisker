package chatstore

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
)

// Schema migrations. How to add one:
//  1. Add migrations/NNNN_name.sql with the next free number.
//  2. Write plain SQL. Never edit a file that a release has shipped.
//
// The schema version is PRAGMA user_version: the count of files applied, in file name order.
// Migrate runs each missing file in its own transaction, so a failed step leaves the version unchanged.
// If two branches pick one number, rename one file when you merge.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate brings the chat store to the latest schema.
func Migrate(db *sql.DB) error {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(names) {
		return fmt.Errorf("chat store version %d is newer than this Daemon (%d)", version, len(names))
	}
	for i := version; i < len(names); i++ {
		script, err := migrationFiles.ReadFile("migrations/" + names[i])
		if err != nil {
			return err
		}
		if err := applyStep(db, string(script), i+1); err != nil {
			return fmt.Errorf("migration %s: %w", names[i], err)
		}
	}
	return nil
}

func applyStep(db *sql.DB, script string, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(script); err != nil {
		return err
	}
	// PRAGMA takes no bound parameters; version is our own integer.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}
