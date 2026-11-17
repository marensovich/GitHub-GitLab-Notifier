package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"GitHub-GitLab-Notifier/storage"
)

// runCmd runs the CLI and returns its exit code and output.
func runCmd(args ...string) (int, string) {
	var out bytes.Buffer
	code := run(args, &out, &out)
	return code, out.String()
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVersionAndHelp(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		if code, out := runCmd(args...); code != 0 || !strings.Contains(out, Version) {
			t.Errorf("%v: %d %q", args, code, out)
		}
	}
	if code, out := runCmd("help"); code != 0 || !strings.Contains(out, "migrate-config") {
		t.Errorf("help: %d %q", code, out)
	}
	if code, out := runCmd("frobnicate"); code != 2 || !strings.Contains(out, "unknown command") {
		t.Errorf("unknown command: %d %q", code, out)
	}
}

func TestEnvConfigFlagGivesHint(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, "x.env", "# 1.x\nBOT_TOKEN=1:abc\n")
	write(t, "renamed.yaml", "BOT_TOKEN=1:abc\n")

	const hint = "config.yaml is required since 2.0; run: notifier migrate-config --from %s --to config.yaml"
	for _, name := range []string{"x.env", "renamed.yaml"} {
		_, err := loadConfig(name, nil, func(string) {})
		if err == nil || err.Error() != strings.Replace(hint, "%s", name, 1) {
			t.Errorf("--config %s: got %v", name, err)
		}
	}

	// No config.yaml, but a .env next to the binary: the same hint.
	write(t, ".env", "BOT_TOKEN=1:abc\n")
	if _, err := loadConfig("", nil, func(string) {}); err == nil || !strings.Contains(err.Error(), "--from .env") {
		t.Errorf("leftover .env: got %v", err)
	}
}

func TestLegacyEnvStartup(t *testing.T) {
	t.Chdir(t.TempDir())
	var warned []string
	env := map[string]string{"BOT_TOKEN": "1:abc", "DB_PATH": "/data/notifier.db", "SERVER_PORT": "9000", "PATH": "/bin"}
	c, err := loadConfig("", env, func(s string) { warned = append(warned, s) })
	if err != nil {
		t.Fatal(err)
	}
	if c.Messengers.Telegram.Token != "1:abc" || c.Storage.Path != "/data/notifier.db" || c.Server.Listen != ":9000" {
		t.Errorf("config from env: %+v", c)
	}
	if len(warned) == 0 || !strings.Contains(strings.Join(warned, "\n"), "migrate-config") {
		t.Errorf("no deprecation warning: %q", warned)
	}

	// Without BOT_TOKEN there is nothing to start from.
	if _, err := loadConfig("", map[string]string{"PATH": "/bin"}, func(string) {}); err == nil {
		t.Error("no config and no BOT_TOKEN: want an error")
	}
}

func TestDeprecatedFlagsOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, "config.yaml", "messengers:\n  telegram: { enabled: true, token: x }\n")
	var warned []string
	c, err := loadServe([]string{"--db", "other.db", "--lang", "ru"}, nil, func(s string) { warned = append(warned, s) })
	if err != nil {
		t.Fatal(err)
	}
	if c.Storage.Path != "other.db" || c.Defaults.Language != "ru" || len(warned) != 2 {
		t.Errorf("path %q lang %q warnings %q", c.Storage.Path, c.Defaults.Language, warned)
	}
	if _, err := loadServe([]string{"--lang", "de"}, nil, func(string) {}); err == nil {
		t.Error("--lang de: want an error")
	}
}

func TestMigrateConfigRefusesOverwrite(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, "notifier.env", "BOT_TOKEN=1:abc\nCHAT_ID=5\n")

	if code, out := runCmd("migrate-config", "--from", "notifier.env", "--to", "config.yaml"); code != 0 {
		t.Fatalf("migrate: %d %s", code, out)
	}
	first, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat("config.yaml"); filepath.Separator == '/' && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	if code, out := runCmd("config", "check", "--config", "config.yaml"); code != 0 {
		t.Errorf("config check of the migrated file: %d %s", code, out)
	}

	write(t, "notifier.env", "BOT_TOKEN=2:xyz\n")
	code, out := runCmd("migrate-config", "--from", "notifier.env", "--to", "config.yaml")
	if code == 0 || !strings.Contains(out, "--force") {
		t.Errorf("overwrite without --force: %d %q", code, out)
	}
	if now, _ := os.ReadFile("config.yaml"); !bytes.Equal(now, first) {
		t.Error("config.yaml was changed without --force")
	}
	if code, out := runCmd("migrate-config", "--from", "notifier.env", "--to", "config.yaml", "--force"); code != 0 {
		t.Errorf("--force: %d %s", code, out)
	}
	if now, _ := os.ReadFile("config.yaml"); !strings.Contains(string(now), "2:xyz") {
		t.Error("--force did not overwrite")
	}
	if strings.Contains(out, "2:xyz") {
		t.Error("the token was printed")
	}
}

func TestConfigCheckReportsErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, "config.yaml", "messengers:\n  telegram: { enabled: true }\n")
	if code, out := runCmd("config", "check", "--config", "config.yaml"); code == 0 || !strings.Contains(out, "messengers.telegram.token") {
		t.Errorf("invalid config: %d %q", code, out)
	}
}

func TestDBBackup(t *testing.T) {
	t.Chdir(t.TempDir())
	write(t, "config.yaml", "storage: { path: live.db }\nmessengers:\n  telegram: { enabled: true, token: x }\n")
	s, err := storage.Open("live.db")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := s.SchemaVersion()
	s.Close()

	if code, out := runCmd("db", "backup", "copy.db"); code != 0 {
		t.Fatalf("db backup: %d %s", code, out)
	}
	b, err := storage.Open("copy.db")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got, _ := b.SchemaVersion(); got != want {
		t.Errorf("backup version = %d, want %d", got, want)
	}
	if code, _ := runCmd("db", "backup"); code != 2 {
		t.Errorf("db backup without a path: exit %d, want 2", code)
	}
}
