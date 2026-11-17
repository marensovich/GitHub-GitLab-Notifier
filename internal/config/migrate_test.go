package config

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// update rewrites testdata/env-1.1.yaml instead of comparing against it:
//
//	go test ./internal/config -run TestFromEnvGolden -update
var update = flag.Bool("update", false, "rewrite golden files")

func noEnv(string) (string, bool) { return "", false }

func TestReadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	text := "# comment\n\nA=1\nexport B=two words\nC=\"quoted # not a comment\"\nD='single'\nE=x # trailing\n  F = spaced \nbroken line\nG=\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"A": "1", "B": "two words", "C": "quoted # not a comment", "D": "single",
		"E": "x", "F": "spaced", "G": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadEnvFile = %v, want %v", got, want)
	}

	if _, err := ReadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err == nil {
		t.Error("missing file: want an error")
	}
}

func TestFromEnvGolden(t *testing.T) {
	env, err := ReadEnvFile("testdata/env-1.1.env")
	if err != nil {
		t.Fatal(err)
	}
	got, warnings, err := FromEnv(env, false)
	if err != nil {
		t.Fatal(err)
	}
	golden := "testdata/env-1.1.yaml"
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("FromEnv output differs from %s (run with -update to accept):\n%s", golden, got)
	}
	joined := strings.Join(warnings, "\n")
	for _, w := range []string{"CHAT_ID", "TELEGRAM_BOT_USERNAME"} {
		if !strings.Contains(joined, w) {
			t.Errorf("warnings %q do not mention %s", warnings, w)
		}
	}
	if !strings.Contains(string(got), "# CHAT_ID=-1001234567890 from 1.x is no longer used: events of unconnected repositories are dropped.") {
		t.Error("the CHAT_ID comment is missing")
	}
}

func TestFromEnvResultLoads(t *testing.T) {
	env, err := ReadEnvFile("testdata/env-1.1.env")
	if err != nil {
		t.Fatal(err)
	}
	text, _, err := FromEnv(env, false)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(text, noEnv)
	if err != nil {
		t.Fatalf("migrated config does not load: %v\n%s", err, text)
	}
	tg := c.Messengers.Telegram
	switch {
	case c.Server.Listen != "0.0.0.0:8443":
		t.Errorf("listen = %q", c.Server.Listen)
	case c.Server.PublicURL != "https://notify.example.com":
		t.Errorf("public_url = %q", c.Server.PublicURL)
	case c.Panel.Listen != "127.0.0.1:8081" || c.Panel.AllowRemote:
		t.Errorf("panel = %+v", c.Panel)
	case !c.MetricsEnabled() || c.Metrics.Listen != "127.0.0.1:9090":
		t.Errorf("metrics = %+v", c.Metrics)
	case c.Storage.Path != "/var/lib/notifier/notifier.db" || c.RetentionDays() != 30:
		t.Errorf("storage = %+v", c.Storage)
	case c.Defaults.Language != "ru" || !c.Defaults.NotifyUnknownEvents:
		t.Errorf("defaults = %+v", c.Defaults)
	case c.Security.WebhookSecret != "pa$$word":
		t.Errorf("webhook_secret = %q", c.Security.WebhookSecret)
	case tg == nil || !tg.Enabled || tg.Token != "123456:ABCdefGHIjklMNOpqrsTUVwxyz":
		t.Errorf("telegram = %+v", tg)
	case tg.Proxy != "socks5://127.0.0.1:1080" || tg.APIURL != "" || tg.Rich():
		t.Errorf("telegram = %+v", tg)
	}

	// With the secrets kept in the environment the file names them.
	text, _, err = FromEnv(env, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "ABCdef") || strings.Contains(string(text), "word") {
		t.Errorf("secrets written inline:\n%s", text)
	}
	vars := map[string]string{"TELEGRAM_TOKEN": "t0k", "WEBHOOK_SECRET": "s"}
	c, err = Parse(text, func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if c.Messengers.Telegram.Token != "t0k" || c.Security.WebhookSecret != "s" {
		t.Errorf("secrets from env: token %q secret %q", c.Messengers.Telegram.Token, c.Security.WebhookSecret)
	}
}

func TestFromEnvDefaultsAndErrors(t *testing.T) {
	text, _, err := FromEnv(map[string]string{"BOT_TOKEN": "x"}, false)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse(text, noEnv)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if c.Server.Listen != ":8080" || c.Storage.Path != "data/notifier.db" || c.RetentionDays() != 14 ||
		c.Defaults.Language != "en" || !c.MetricsEnabled() || !c.Messengers.Telegram.Rich() || c.Panel.Listen != "" {
		t.Errorf("defaults not kept: %+v", c)
	}

	for _, env := range []map[string]string{
		{},                                   // no token
		{"BOT_TOKEN": "x", "BOT_LANG": "de"}, // unsupported language
		{"BOT_TOKEN": "x", "DELIVERIES_RETENTION_DAYS": "-1"},
		{"BOT_TOKEN": "x", "METRICS_ENABLED": "maybe"},
	} {
		if _, _, err := FromEnv(env, false); err == nil {
			t.Errorf("FromEnv(%v): want an error", env)
		}
	}
}
