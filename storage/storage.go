package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database connection.
//
// Обёртка над SQLite базой данных.
type Store struct {
	db *sql.DB
}

// ─── Модели ───

const (
	FormatRich    = "rich"
	FormatClassic = "classic"
)

func Open(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Проверяем соединение
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Применяем миграции схемы
	list, err := loadMigrations(embeddedMigrations, "migrations")
	if err == nil {
		err = backupBefore2x(context.Background(), db, path)
	}
	if err == nil {
		err = migrate(context.Background(), db, list)
	}
	if err != nil {
		db.Close()
		return nil, err
	}

	log.Println("✅ Storage initialized (SQLite)")
	return &Store{db: db}, nil
}

// Ping checks that the database answers.
//
// Проверяет, что база данных отвечает.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// Close closes the database.
//
// Закрывает базу данных.
func (s *Store) Close() error {
	return s.db.Close()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func NormalizeRepoURL(raw string) string {
	u := strings.ToLower(strings.TrimSpace(raw))
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return strings.TrimRight(u, "/")
}
