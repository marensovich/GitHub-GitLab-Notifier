package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func versionOf(t *testing.T, path string) int {
	t.Helper()
	var v int
	if err := rawDB(t, path).QueryRow("SELECT MAX(version) FROM schema_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenBacksUpBeforeV4(t *testing.T) {
	path := openAtVersion(t, 3)
	if _, err := rawDB(t, path).Exec("INSERT INTO chats (chat_id, language) VALUES (-1001, 'ru')"); err != nil {
		t.Fatal(err)
	}
	mustOpen(t, path)

	backup := path + ".1x-backup"
	if v := versionOf(t, backup); v != 3 {
		t.Fatalf("backup is at version %d, want 3 (taken before the 2.0 migrations)", v)
	}
	var n int
	if err := rawDB(t, backup).QueryRow("SELECT COUNT(*) FROM chats").Scan(&n); err != nil || n != 1 {
		t.Errorf("backup has %d chats (%v), want 1", n, err)
	}

	// The backup is taken once: it is not replaced by a later start.
	info, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	mustOpen(t, path)
	again, err := os.Stat(backup)
	if err != nil || !again.ModTime().Equal(info.ModTime()) || versionOf(t, backup) != 3 {
		t.Error("backup was rewritten by a second start")
	}
}

func TestOpenDoesNotBackUpNewDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.db")
	mustOpen(t, path)
	if _, err := os.Stat(path + ".1x-backup"); !os.IsNotExist(err) {
		t.Errorf("a new database got a 1.x backup: %v", err)
	}
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, filepath.Join(dir, "live.db"))
	want, _ := s.SchemaVersion()

	to := filepath.Join(dir, "copy.db")
	if err := s.Backup(to); err != nil {
		t.Fatal(err)
	}
	if got := versionOf(t, to); got != want {
		t.Errorf("backup version = %d, want %d", got, want)
	}
	if err := s.Backup(to); err == nil {
		t.Error("backup over an existing file: want an error")
	}
}

func TestBackupFileDoesNotMigrate(t *testing.T) {
	path := openAtVersion(t, 3)
	to := filepath.Join(t.TempDir(), "copy.db")
	if err := BackupFile(path, to); err != nil {
		t.Fatal(err)
	}
	if v := versionOf(t, path); v != 3 {
		t.Errorf("source migrated to %d", v)
	}
	if v := versionOf(t, to); v != 3 {
		t.Errorf("copy at version %d, want 3", v)
	}
	if err := BackupFile(filepath.Join(t.TempDir(), "missing.db"), to+"2"); err == nil {
		t.Error("missing source: want an error")
	}
}
