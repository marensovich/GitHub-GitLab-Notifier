package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// ErrSchemaTooNew means the database was migrated by a newer version of the
// bot than the one running now.
//
// База обновлена более новой версией бота, чем запущенная сейчас.
var ErrSchemaTooNew = errors.New("database schema is newer than this version supports")

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads NNNN_name.sql files from dir, sorted by number.
// Numbers must start at 1 and have no gaps.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var list []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		num, _, ok := strings.Cut(e.Name(), "_")
		version, err := strconv.Atoi(num)
		if !ok || err != nil || version < 1 {
			return nil, fmt.Errorf("migration %q: name must look like 0001_name.sql", e.Name())
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}
		list = append(list, migration{version: version, name: e.Name(), sql: string(body)})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })
	for i, m := range list {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration %q: expected number %04d, migrations must have no gaps or duplicates", m.name, i+1)
		}
	}
	return list, nil
}

// migrate brings the schema up to the newest migration. Each migration runs
// in its own transaction; a failed one is rolled back and stops the start-up.
//
// Применяет все новые миграции, каждую в своей транзакции.
func migrate(ctx context.Context, db *sql.DB, list []migration) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	current, err := schemaVersion(ctx, db)
	if err != nil {
		return err
	}

	// A 1.0.x database: tables exist, but nobody recorded a version.
	if current == 0 && len(list) > 0 {
		legacy, err := tableExists(ctx, db, "chats")
		if err != nil {
			return err
		}
		if legacy {
			if _, err := db.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (1)"); err != nil {
				return fmt.Errorf("mark existing database as version 1: %w", err)
			}
			current = 1
		}
	}

	if current > len(list) {
		return fmt.Errorf("%w: database is at version %d, this build knows up to %d; "+
			"run the newer version again or restore a backup made before the update", ErrSchemaTooNew, current, len(list))
	}

	for _, m := range list[current:] {
		if err := apply(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration %s: %w", m.name, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("migration %s failed, nothing from it was applied: %w", m.name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (?)", m.version); err != nil {
		return fmt.Errorf("migration %s: record version: %w", m.name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migration %s: commit: %w", m.name, err)
	}
	return nil
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return int(v.Int64), nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check table %s: %w", name, err)
	}
	return n > 0, nil
}

// SchemaVersion returns the version of the applied database schema.
//
// Возвращает версию схемы базы данных.
func (s *Store) SchemaVersion() (int, error) {
	return schemaVersion(context.Background(), s.db)
}
