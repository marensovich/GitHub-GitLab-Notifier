package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// ReadEnvFile reads a 1.x .env file: KEY=VALUE lines, # comments, an
// optional "export " prefix, single or double quotes, and " #" comments
// after unquoted values. Variables are not expanded. Lines without "="
// are skipped.
//
// Читает .env версии 1.x.
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		env[key] = envValue(strings.TrimSpace(value))
	}
	return env, sc.Err()
}

// envValue unquotes a .env value or drops its trailing comment.
func envValue(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.LastIndexByte(v, v[0]); end > 0 {
			inner := v[1:end]
			if v[0] == '"' {
				inner = strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n").Replace(inner)
			}
			return inner
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// FromEnv converts the variables of a 1.x .env to config.yaml text. With
// secretsInEnv the Telegram token and the webhook secret are written as
// ${TELEGRAM_TOKEN} and ${WEBHOOK_SECRET} references instead of their
// values. The warnings name 1.x settings that 2.0 no longer uses.
//
// Переводит .env версии 1.x в config.yaml.
func FromEnv(env map[string]string, secretsInEnv bool) (yamlText []byte, warnings []string, err error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(env[key]); v != "" {
			return v
		}
		return def
	}
	var errs []error
	boolean := func(key string, def bool) bool {
		v := get(key, "")
		if v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q is not true or false", key, v))
		}
		return b
	}

	token := get("BOT_TOKEN", "")
	if token == "" {
		errs = append(errs, errors.New("BOT_TOKEN is not set"))
	}
	retention, err := strconv.Atoi(get("DELIVERIES_RETENTION_DAYS", "14"))
	if err != nil {
		errs = append(errs, fmt.Errorf("DELIVERIES_RETENTION_DAYS=%q is not a number", env["DELIVERIES_RETENTION_DAYS"]))
	}
	proxy := ""
	if boolean("PROXY_ENABLED", false) {
		host, port := get("PROXY_HOST", ""), get("PROXY_PORT", "")
		if host == "" || port == "" {
			warnings = append(warnings, "PROXY_ENABLED is true but PROXY_HOST or PROXY_PORT is empty: no proxy is configured")
		} else {
			scheme := "http"
			switch strings.ToLower(get("PROXY_TYPE", "")) {
			case "socks", "socks5":
				scheme = "socks5"
			}
			proxy = scheme + "://" + net.JoinHostPort(host, port)
		}
	}
	panelRemote := boolean("PANEL_ALLOW_REMOTE", false)
	metricsOn := boolean("METRICS_ENABLED", true)
	unknown := boolean("NOTIFY_UNKNOWN_EVENTS", false)
	rich := boolean("RICH_MESSAGES", true)
	if len(errs) > 0 {
		return nil, nil, errors.Join(errs...)
	}

	tokenValue, secretValue := quote(token), quote(get("WEBHOOK_SECRET", ""))
	if secretsInEnv {
		tokenValue, secretValue = `"${TELEGRAM_TOKEN}"`, `"${WEBHOOK_SECRET:-}"`
	}

	var b strings.Builder
	b.WriteString("# Migrated from a 1.x .env by notifier migrate-config.\n")
	if secretsInEnv {
		b.WriteString("# The Telegram token and the webhook secret are read from the TELEGRAM_TOKEN and\n# WEBHOOK_SECRET environment variables.\n")
	}
	fmt.Fprintf(&b, "server:\n  listen: %s\n  public_url: %s\n",
		quote(net.JoinHostPort(get("SERVER_ADDRESS", ""), get("SERVER_PORT", "8080"))),
		quote(PublicURLFromEnv(env["DOMAIN"], env["USE_HTTPS"])))
	fmt.Fprintf(&b, "panel:\n  listen: %s\n  allow_remote: %t\n", quote(get("PANEL_ADDRESS", "")), panelRemote)
	fmt.Fprintf(&b, "metrics:\n  enabled: %t\n  listen: %s\n", metricsOn, quote(get("METRICS_ADDR", "")))
	fmt.Fprintf(&b, "storage:\n  path: %s\n  deliveries_retention_days: %d\n", quote(get("DB_PATH", "data/notifier.db")), retention)
	fmt.Fprintf(&b, "defaults:\n  language: %s\n  notify_unknown_events: %t\n", quote(get("BOT_LANG", "en")), unknown)
	b.WriteString("# Bot administrators as messenger:user_id, e.g. telegram:123456789. An\n" +
		"# administrator may connect a repository that another chat already connected.\n" +
		"admins: []\n")
	fmt.Fprintf(&b, "security:\n  webhook_secret: %s\n", secretValue)
	fmt.Fprintf(&b, "messengers:\n  telegram:\n    enabled: true\n    token: %s\n    api_url: %s\n    proxy: %s\n    rich_messages: %t\n",
		tokenValue, quote(get("TELEGRAM_API_URL", "")), quote(proxy), rich)

	if id := get("CHAT_ID", ""); id != "" {
		fmt.Fprintf(&b, "\n# CHAT_ID=%s from 1.x is no longer used: events of unconnected repositories are dropped.\n", id)
		warnings = append(warnings, "CHAT_ID is no longer used: events of repositories that no chat connected with /connect are dropped")
	}
	if get("TELEGRAM_BOT_USERNAME", "") != "" {
		warnings = append(warnings, "TELEGRAM_BOT_USERNAME is no longer needed: the bot reads its name from Telegram")
	}

	// The result must load; a placeholder stands for every ${VAR}.
	if _, err := Parse([]byte(b.String()), func(string) (string, bool) { return "x", true }); err != nil {
		return nil, nil, err
	}
	return []byte(b.String()), warnings, nil
}

// quote writes s as a YAML double-quoted string that ${VAR} expansion
// leaves as it is.
func quote(s string) string {
	return strconv.Quote(strings.ReplaceAll(s, "$", "$$"))
}
