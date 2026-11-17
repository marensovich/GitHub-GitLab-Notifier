package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func mustParse(t *testing.T, yaml string, vars map[string]string) *Config {
	t.Helper()
	c, err := Parse([]byte(yaml), envOf(vars))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

func parseErr(t *testing.T, yaml string, vars map[string]string) string {
	t.Helper()
	_, err := Parse([]byte(yaml), envOf(vars))
	if err == nil {
		t.Fatal("Parse succeeded, want an error")
	}
	return err.Error()
}

const minimal = "messengers:\n  telegram: { enabled: true, token: abc }\n"

func TestParseExample(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(data, envOf(map[string]string{"TELEGRAM_TOKEN": "x"}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Messengers.Telegram == nil || !c.Messengers.Telegram.Enabled || c.Messengers.Telegram.Token != "x" {
		t.Errorf("telegram = %+v", c.Messengers.Telegram)
	}
	if c.Server.Listen != ":8080" {
		t.Errorf("listen = %q", c.Server.Listen)
	}
}

func TestDefaults(t *testing.T) {
	c := mustParse(t, minimal, nil)
	if c.Server.Listen != ":8080" || c.Storage.Path != "data/notifier.db" || c.Defaults.Language != "en" {
		t.Errorf("defaults = %+v %+v %+v", c.Server, c.Storage, c.Defaults)
	}
	if c.RetentionDays() != 14 || !c.MetricsEnabled() || !c.Messengers.Telegram.Rich() {
		t.Errorf("retention %d metrics %v rich %v", c.RetentionDays(), c.MetricsEnabled(), c.Messengers.Telegram.Rich())
	}
}

func TestExplicitZeroAndFalseKept(t *testing.T) {
	c := mustParse(t, minimal+"storage: { deliveries_retention_days: 0 }\nmetrics: { enabled: false }\n", nil)
	if c.RetentionDays() != 0 || c.MetricsEnabled() {
		t.Errorf("retention %d metrics %v", c.RetentionDays(), c.MetricsEnabled())
	}
}

func TestExpand(t *testing.T) {
	c := mustParse(t, `
server: { public_url: "${URL:-https://def.example}" }
security: { webhook_secret: "$${LITERAL}" }
messengers:
  telegram: { enabled: true, token: "${TOKEN}" }
`, map[string]string{"TOKEN": "t0k"})
	if c.Messengers.Telegram.Token != "t0k" {
		t.Errorf("token = %q", c.Messengers.Telegram.Token)
	}
	if c.Server.PublicURL != "https://def.example" {
		t.Errorf("public_url = %q", c.Server.PublicURL)
	}
	if c.Security.WebhookSecret != "${LITERAL}" {
		t.Errorf("webhook_secret = %q", c.Security.WebhookSecret)
	}
}

func TestEmptyDefault(t *testing.T) {
	c := mustParse(t, minimal+"security: { webhook_secret: \"${WS:-}\" }\n", nil)
	if c.Security.WebhookSecret != "" {
		t.Errorf("webhook_secret = %q", c.Security.WebhookSecret)
	}
}

func TestUnsetVarIsError(t *testing.T) {
	msg := parseErr(t, "messengers:\n  telegram: { enabled: true, token: \"${NOPE}\" }\n", nil)
	if !strings.Contains(msg, "messengers.telegram.token") || !strings.Contains(msg, "NOPE") {
		t.Errorf("error = %q", msg)
	}
}

func TestUnknownKeyIsError(t *testing.T) {
	msg := parseErr(t, minimal+"server: { lisen: \":1\" }\n", nil)
	if !strings.Contains(msg, "lisen") {
		t.Errorf("error = %q", msg)
	}
}

func TestNoMessengerEnabled(t *testing.T) {
	msg := parseErr(t, "messengers:\n  telegram: { enabled: false }\n", nil)
	if !strings.Contains(msg, "at least one messenger must be enabled") {
		t.Errorf("error = %q", msg)
	}
}

func TestEnabledTelegramNeedsToken(t *testing.T) {
	msg := parseErr(t, "messengers:\n  telegram: { enabled: true }\n", nil)
	if !strings.Contains(msg, "messengers.telegram.token") {
		t.Errorf("error = %q", msg)
	}
}

func TestBadLanguage(t *testing.T) {
	msg := parseErr(t, minimal+"defaults: { language: de }\n", nil)
	if !strings.Contains(msg, "defaults.language") {
		t.Errorf("error = %q", msg)
	}
}

func TestAdminFormat(t *testing.T) {
	msg := parseErr(t, minimal+"admins: [\"123\"]\n", nil)
	if !strings.Contains(msg, "messenger:id") {
		t.Errorf("error = %q", msg)
	}
}

func TestIsAdmin(t *testing.T) {
	c := mustParse(t, minimal+"admins: [\"telegram:42\", \"discord:7\"]\n", nil)
	if !c.IsAdmin("telegram", "42") || !c.IsAdmin("discord", "7") {
		t.Error("listed admins are not admins")
	}
	if c.IsAdmin("telegram", "7") || c.IsAdmin("telegram", "") {
		t.Error("unlisted user is an admin")
	}
}

func TestMask(t *testing.T) {
	got := Mask(`Post "https://api.telegram.org/bot123:ABC/getMe": timeout`, "123:ABC")
	if strings.Contains(got, "123:ABC") || !strings.Contains(got, "***") {
		t.Errorf("Mask = %q", got)
	}
	if Mask("text", "") != "text" {
		t.Error("empty secret changed the text")
	}
}

func TestFindOrder(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if _, err := Find(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find with nothing = %v, want ErrNotFound", err)
	}
	if err := os.WriteFile("config.yaml", []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, err := Find(""); err != nil || p != "config.yaml" {
		t.Errorf("Find = %q, %v", p, err)
	}
	other := filepath.Join(dir, "other.yaml")
	if err := os.WriteFile(other, []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}
	if p, err := Find(other); err != nil || p != other {
		t.Errorf("Find(flag) = %q, %v", p, err)
	}
	if _, err := Find(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("Find of a missing flag path succeeded")
	}
}

// Docker creates a directory when a bind-mounted config.yaml is missing on
// the host; Find must not pick it.
func TestFindSkipsDirectories(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("config.yaml", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Find(""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Find with a config.yaml directory = %v, want ErrNotFound", err)
	}
}

func TestPublicURLFromEnv(t *testing.T) {
	tests := []struct{ domain, https, want string }{
		{"bot.example.com", "true", "https://bot.example.com"},
		{"https://bot.example.com/", "", "https://bot.example.com"},
		{"1.2.3.4:8080", "false", "http://1.2.3.4:8080"},
		{" bot.example.com ", "yes", "https://bot.example.com"},
		{"", "true", ""},
	}
	for _, tt := range tests {
		if got := PublicURLFromEnv(tt.domain, tt.https); got != tt.want {
			t.Errorf("PublicURLFromEnv(%q, %q) = %q, want %q", tt.domain, tt.https, got, tt.want)
		}
	}
}
