package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// firstV2Migration is the first migration of 2.0 (0004_messengers.sql),
// which rewrites the 1.x tables.
const firstV2Migration = 4

// Backup writes a consistent copy of the database to path, which must not
// exist yet.
//
// Сохраняет копию базы в path.
func (s *Store) Backup(path string) error {
	return backupTo(context.Background(), s.db, path)
}

// BackupFile copies the database at src to dst without migrating it, so a
// backup taken before an update matches the old version.
//
// Копирует базу src в dst, не применяя миграции.
func BackupFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(src)+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	return backupTo(context.Background(), db, dst)
}

func backupTo(ctx context.Context, db *sql.DB, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return fmt.Errorf("back up the database to %s: %w", path, err)
	}
	return nil
}

// backupBefore2x copies a 1.x database to <path>.1x-backup once, before
// the 2.0 migrations change it, so a downgrade to 1.x has a database to
// go back to. New and already migrated databases are left alone.
func backupBefore2x(ctx context.Context, db *sql.DB, path string) error {
	version := 0
	versioned, err := tableExists(ctx, db, "schema_version")
	if err != nil {
		return err
	}
	if versioned {
		if version, err = schemaVersion(ctx, db); err != nil {
			return err
		}
	}
	if version == 0 {
		// 1.0.x recorded no version; its tables mean version 1.
		legacy, err := tableExists(ctx, db, "chats")
		if err != nil || !legacy {
			return err
		}
		version = 1
	}
	if version >= firstV2Migration {
		return nil
	}
	backup := path + ".1x-backup"
	if _, err := os.Stat(backup); err == nil {
		return nil
	}
	if err := backupTo(ctx, db, backup); err != nil {
		return err
	}
	log.Printf("💾 1.x database saved to %s before the 2.0 migrations", backup)
	return nil
}
