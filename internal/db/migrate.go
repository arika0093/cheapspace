package db

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"cheapspace/migrations"
)

func Migrate(db *sql.DB) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}

	if _, err := db.Exec(`
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
`); err != nil {
		return fmt.Errorf("configure sqlite pragmas: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	for _, file := range files {
		applied, err := isApplied(db, file.Name())
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		sqlText, err := fs.ReadFile(migrations.Files, file.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file.Name(), err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", file.Name(), err)
		}

		if _, err := tx.Exec(string(sqlText)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", file.Name(), err)
		}

		if _, err := tx.Exec(`INSERT OR REPLACE INTO schema_migrations(version, applied_at) VALUES(?, ?)`, file.Name(), timestamp(time.Now())); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", file.Name(), err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", file.Name(), err)
		}
	}

	return nil
}

func MigrationStatuses(db *sql.DB) ([]MigrationStatus, error) {
	files, err := migrationFiles()
	if err != nil {
		return nil, err
	}

	statuses := make([]MigrationStatus, 0, len(files))
	for _, file := range files {
		appliedAt, applied, err := migrationAppliedAt(db, file.Name())
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				statuses = append(statuses, MigrationStatus{Version: file.Name()})
				continue
			}
			return nil, err
		}
		statuses = append(statuses, MigrationStatus{
			Version:   file.Name(),
			Applied:   applied,
			AppliedAt: appliedAt,
		})
	}
	return statuses, nil
}

func migrationFiles() ([]fs.DirEntry, error) {
	files, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})
	return files, nil
}

func isApplied(db *sql.DB, version string) (bool, error) {
	_, applied, err := migrationAppliedAt(db, version)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return applied, nil
}

func migrationAppliedAt(db *sql.DB, version string) (*time.Time, bool, error) {
	var appliedAt string
	err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = ?`, version).Scan(&appliedAt)
	if err != nil {
		return nil, false, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, appliedAt)
	if err != nil {
		return nil, false, fmt.Errorf("parse migration timestamp: %w", err)
	}
	return &parsed, true, nil
}
