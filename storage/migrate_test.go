package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// schema101 is the schema exactly as 1.0.1 created it, before migrations.
const schema101 = `
CREATE TABLE IF NOT EXISTS chats (chat_id INTEGER PRIMARY KEY, language TEXT NOT NULL DEFAULT 'ru', is_forum INTEGER NOT NULL DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS topics (topic_id INTEGER NOT NULL, chat_id INTEGER NOT NULL, name TEXT NOT NULL, event_type TEXT NOT NULL DEFAULT '', created_at DATETIME DEFAULT CURRENT_TIMESTAMP, PRIMARY KEY (chat_id, topic_id), FOREIGN KEY (chat_id) REFERENCES chats(chat_id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS repos (repo_url TEXT PRIMARY KEY, chat_id INTEGER NOT NULL, platform TEXT NOT NULL DEFAULT 'GitHub', secret TEXT NOT NULL DEFAULT '', created_at DATETIME DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY (chat_id) REFERENCES chats(chat_id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS event_mapping (chat_id INTEGER NOT NULL, event_type TEXT NOT NULL, topic_id INTEGER NOT NULL, PRIMARY KEY (chat_id, event_type), FOREIGN KEY (chat_id) REFERENCES chats(chat_id) ON DELETE CASCADE, FOREIGN KEY (chat_id, topic_id) REFERENCES topics(chat_id, topic_id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS idx_topics_chat ON topics(chat_id);
CREATE INDEX IF NOT EXISTS idx_repos_chat ON repos(chat_id);
CREATE INDEX IF NOT EXISTS idx_event_mapping_chat ON event_mapping(chat_id);
INSERT INTO chats (chat_id, language, is_forum) VALUES (-1001, 'ru', 1), (42, 'en', 0);
INSERT INTO topics (topic_id, chat_id, name, event_type) VALUES (5, -1001, 'CI/CD', 'pipeline');
INSERT INTO repos (repo_url, chat_id, platform, secret) VALUES ('https://github.com/acme/api', -1001, 'GitHub', 'x'), ('https://gitlab.com/acme/web', 42, 'GitLab', '');
INSERT INTO event_mapping (chat_id, event_type, topic_id) VALUES (-1001, 'pipeline', 5);
`

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestFreshDatabaseIsMigrated(t *testing.T) {
	s := openTest(t)
	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	list, _ := loadMigrations(embeddedMigrations, "migrations")
	if v != len(list) {
		t.Errorf("version = %d, want %d", v, len(list))
	}
	if err := s.SaveChat(ChatConfig{ChatID: 1, Language: "en"}); err != nil {
		t.Fatalf("schema not usable: %v", err)
	}
}

func TestLegacyDatabaseKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	if _, err := rawDB(t, path).Exec(schema101); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	defer s.Close()

	if v, _ := s.SchemaVersion(); v < 1 {
		t.Errorf("version = %d, want >= 1", v)
	}
	if n := len(s.GetAllChats()); n != 2 {
		t.Errorf("chats = %d, want 2", n)
	}
	if n := len(s.GetAllRepos()); n != 2 {
		t.Errorf("repos = %d, want 2", n)
	}
	if id := s.GetTopicByEvent(-1001, "pipeline"); id != 5 {
		t.Errorf("pipeline topic = %d, want 5", id)
	}
	var marked int
	s.db.QueryRow("SELECT COUNT(*) FROM schema_version WHERE version = 1").Scan(&marked)
	if marked != 1 {
		t.Errorf("version 1 recorded %d times, want once", marked)
	}
}

func TestReopenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.db")
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		s.Close()
	}
}

func TestNewerSchemaRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (999)"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := Open(path); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("want ErrSchemaTooNew, got %v", err)
	} else if !strings.Contains(err.Error(), "999") || !strings.Contains(err.Error(), "backup") {
		t.Errorf("error should name the version and the backup: %v", err)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	fsys := fstest.MapFS{
		"m/0001_a.sql": {Data: []byte("CREATE TABLE a (x INTEGER);")},
		"m/0002_b.sql": {Data: []byte("CREATE TABLE b (x INTEGER); INSERT INTO missing VALUES (1);")},
	}
	list, err := loadMigrations(fsys, "m")
	if err != nil {
		t.Fatal(err)
	}
	db := rawDB(t, filepath.Join(t.TempDir(), "db.db"))
	ctx := context.Background()

	err = migrate(ctx, db, list)
	if err == nil || !strings.Contains(err.Error(), "0002_b.sql") {
		t.Fatalf("want error naming 0002_b.sql, got %v", err)
	}
	if v, _ := schemaVersion(ctx, db); v != 1 {
		t.Errorf("version = %d, want 1", v)
	}
	if ok, _ := tableExists(ctx, db, "a"); !ok {
		t.Error("table a from the good migration is missing")
	}
	if ok, _ := tableExists(ctx, db, "b"); ok {
		t.Error("table b from the failed migration was not rolled back")
	}
}

func TestLoadMigrationsValidates(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"gap":       {"m/0001_a.sql": {}, "m/0003_c.sql": {}},
		"duplicate": {"m/0001_a.sql": {}, "m/0001_b.sql": {}},
		"bad name":  {"m/init.sql": {}},
		"zero":      {"m/0000_a.sql": {}},
	}
	for name, fsys := range cases {
		if _, err := loadMigrations(fsys, "m"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	list, err := loadMigrations(fstest.MapFS{"m/0002_b.sql": {}, "m/0001_a.sql": {}, "m/README.md": {}}, "m")
	if err != nil || len(list) != 2 || list[0].version != 1 {
		t.Errorf("sorted load: %v %+v", err, list)
	}
}
