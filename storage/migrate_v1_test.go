package storage

import (
	"context"
	"path/filepath"
	"testing"
)

// openAtVersion creates a database migrated up to version v only, as an
// older release left it.
func openAtVersion(t *testing.T, v int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	list, err := loadMigrations(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(context.Background(), rawDB(t, path), list[:v]); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustOpen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateFrom11(t *testing.T) {
	path := openAtVersion(t, 3)
	if _, err := rawDB(t, path).Exec(`
INSERT INTO chats (chat_id, language, is_forum, format, use_topics) VALUES
	(-1001, 'ru', 1, 'classic', 0),
	(42, 'en', 0, 'rich', 1);
INSERT INTO topics (topic_id, chat_id, name, event_type) VALUES
	(5, -1001, 'CI/CD', 'pipeline'), (6, -1001, 'Pull Requests', 'pull_request'), (7, -1001, 'Old', '');
INSERT INTO event_mapping (chat_id, event_type, topic_id) VALUES
	(-1001, 'pipeline', 5), (-1001, 'pull_request', 6);
INSERT INTO repos (repo_url, chat_id, platform, secret) VALUES
	('https://github.com/acme/api', -1001, 'GitHub', 's3cret'),
	('https://gitlab.com/acme/web', 42, 'GitLab', '');
`); err != nil {
		t.Fatal(err)
	}

	s := mustOpen(t, path)
	if v, _ := s.SchemaVersion(); v < 4 {
		t.Fatalf("version = %d, want >= 4", v)
	}

	dests, err := s.Destinations()
	if err != nil {
		t.Fatal(err)
	}
	if len(dests) != 2 {
		t.Fatalf("destinations = %+v, want 2", dests)
	}
	forum, err := s.GetDestination("telegram", "-1001")
	if err != nil || forum == nil {
		t.Fatalf("forum destination = %v, %v", forum, err)
	}
	if forum.Language != "ru" || forum.Format != FormatClassic || forum.UseThreads {
		t.Errorf("forum = %+v, want ru, classic, no threads", *forum)
	}
	group, _ := s.GetDestination("telegram", "42")
	if group == nil || group.Language != "en" || group.Format != FormatRich || !group.UseThreads {
		t.Errorf("group = %+v", group)
	}

	threads, err := s.Threads(forum.ID)
	if err != nil || len(threads) != 3 {
		t.Errorf("threads = %+v, %v; want 3", threads, err)
	}
	if got := s.ThreadFor(forum.ID, "pipeline"); got != "5" {
		t.Errorf("pipeline thread = %q, want 5", got)
	}
	if got := s.ThreadFor(forum.ID, "pull_request"); got != "6" {
		t.Errorf("pull_request thread = %q, want 6", got)
	}

	api, err := s.FindRepo("github.com/acme/api")
	if err != nil || api == nil || api.Secret != "s3cret" || api.Platform != "GitHub" {
		t.Fatalf("api repo = %+v, %v", api, err)
	}
	web, _ := s.FindRepo("https://gitlab.com/acme/web")
	if web == nil || web.Secret != "" {
		t.Fatalf("web repo = %+v", web)
	}
	subs, err := s.Subscribers(api.ID)
	if err != nil || len(subs) != 1 || subs[0].ID != forum.ID {
		t.Errorf("api subscribers = %+v, %v", subs, err)
	}
	subs, _ = s.Subscribers(web.ID)
	if len(subs) != 1 || subs[0].ID != group.ID {
		t.Errorf("web subscribers = %+v", subs)
	}

	for _, old := range []string{"chats", "topics", "event_mapping"} {
		if ok, _ := tableExists(context.Background(), s.db, old); ok {
			t.Errorf("table %s still exists", old)
		}
	}
}

func TestMigrateFrom10(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	if _, err := rawDB(t, path).Exec(schema101); err != nil {
		t.Fatal(err)
	}
	s := mustOpen(t, path)

	forum, _ := s.GetDestination("telegram", "-1001")
	if forum == nil || forum.Format != FormatRich || !forum.UseThreads || forum.Language != "ru" {
		t.Fatalf("forum = %+v, want ru, rich, threads (0003 defaults)", forum)
	}
	if got := s.ThreadFor(forum.ID, "pipeline"); got != "5" {
		t.Errorf("pipeline thread = %q, want 5", got)
	}
	repos, _ := s.Repos()
	if len(repos) != 2 {
		t.Errorf("repos = %+v, want 2", repos)
	}
}

func TestMigrateOrphans(t *testing.T) {
	path := openAtVersion(t, 3)
	db := rawDB(t, path)
	// Rows that 1.x could leave behind with foreign keys off.
	if _, err := db.Exec(`
PRAGMA foreign_keys = OFF;
INSERT INTO chats (chat_id, language, is_forum) VALUES (-1002, 'en', 1);
INSERT INTO repos (repo_url, chat_id, platform, secret) VALUES ('https://github.com/acme/orphan', 999, 'GitHub', 'o');
INSERT INTO event_mapping (chat_id, event_type, topic_id) VALUES (-1002, 'release', 77);
`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s := mustOpen(t, path)
	dests, _ := s.Destinations()
	if len(dests) != 1 {
		t.Errorf("destinations = %+v, want only the existing chat", dests)
	}
	orphan, _ := s.FindRepo("github.com/acme/orphan")
	if orphan == nil || orphan.Secret != "o" {
		t.Fatalf("orphan repo = %+v, want kept with its secret", orphan)
	}
	if n, _ := s.SubscriberCount(orphan.ID); n != 0 {
		t.Errorf("orphan subscribers = %d, want 0", n)
	}
	if got := s.ThreadFor(dests[0].ID, "release"); got != "" {
		t.Errorf("dangling route = %q, want dropped", got)
	}
}
