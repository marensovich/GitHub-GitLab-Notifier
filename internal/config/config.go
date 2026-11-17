// Package config loads the notifier's config.yaml.
//
// Пакет config читает config.yaml нотифаера.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"GitHub-GitLab-Notifier/internal/i18n"

	"gopkg.in/yaml.v3"
)

// ErrNotFound means no configuration file was found in the lookup order.
var ErrNotFound = errors.New("config.yaml not found")

// SystemPath is the last place Find looks for the configuration.
const SystemPath = "/etc/notifier/config.yaml"

// Messenger names, as used in admins and in the database.
const (
	MessengerTelegram = "telegram"
	MessengerDiscord  = "discord"
	MessengerMax      = "max"
)

// Config is the whole configuration file.
type Config struct {
	Server     Server     `yaml:"server"`
	Panel      Panel      `yaml:"panel"`
	Metrics    Metrics    `yaml:"metrics"`
	Storage    Storage    `yaml:"storage"`
	Defaults   Defaults   `yaml:"defaults"`
	Admins     []string   `yaml:"admins"`
	Security   Security   `yaml:"security"`
	Messengers Messengers `yaml:"messengers"`
}

// Server is the public webhook server.
type Server struct {
	Listen    string `yaml:"listen"`
	PublicURL string `yaml:"public_url"`
}

// Panel is the local read-only web panel; an empty Listen disables it.
type Panel struct {
	Listen      string `yaml:"listen"`
	AllowRemote bool   `yaml:"allow_remote"`
}

// Metrics configures /metrics; an empty Listen serves it on the webhook server.
type Metrics struct {
	Enabled *bool  `yaml:"enabled"`
	Listen  string `yaml:"listen"`
}

// Storage is the SQLite database.
type Storage struct {
	Path                    string `yaml:"path"`
	DeliveriesRetentionDays *int   `yaml:"deliveries_retention_days"`
}

// Defaults apply where a chat has no setting of its own.
type Defaults struct {
	Language            string `yaml:"language"`
	NotifyUnknownEvents bool   `yaml:"notify_unknown_events"`
}

// Security holds installation-wide secrets.
type Security struct {
	WebhookSecret string `yaml:"webhook_secret"`
}

// Messengers holds one section per messenger; a missing section is disabled.
type Messengers struct {
	Telegram *Telegram `yaml:"telegram"`
	Discord  *Discord  `yaml:"discord"`
	Max      *Max      `yaml:"max"`
}

// Telegram configures the Telegram bot.
type Telegram struct {
	Enabled      bool   `yaml:"enabled"`
	Token        string `yaml:"token"`
	APIURL       string `yaml:"api_url"`
	Proxy        string `yaml:"proxy"`
	RichMessages *bool  `yaml:"rich_messages"`
}

// Rich reports whether rich messages are on (default true).
func (t *Telegram) Rich() bool { return t.RichMessages == nil || *t.RichMessages }

// Discord configures the Discord application.
type Discord struct {
	Enabled       bool   `yaml:"enabled"`
	Token         string `yaml:"token"`
	ApplicationID string `yaml:"application_id"`
	PublicKey     string `yaml:"public_key"`
}

// Max configures the MAX bot.
type Max struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`
}

// Load reads, expands and validates the configuration file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data, os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse expands ${VAR} references with env, decodes data strictly (unknown
// keys are errors), applies defaults and validates the result.
func Parse(data []byte, env func(string) (string, bool)) (*Config, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if err := expandNode(&root, "", env); err != nil {
		return nil, err
	}
	// Node.Decode ignores KnownFields, so decode the expanded text strictly.
	expanded, err := yaml.Marshal(&root)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = ":8080"
	}
	if c.Storage.Path == "" {
		c.Storage.Path = "data/notifier.db"
	}
	if c.Defaults.Language == "" {
		c.Defaults.Language = string(i18n.EN)
	}
}

// RetentionDays is how many days the delivery log is kept (0 = forever).
func (c *Config) RetentionDays() int {
	if c.Storage.DeliveriesRetentionDays == nil {
		return 14
	}
	return *c.Storage.DeliveriesRetentionDays
}

// MetricsEnabled reports whether /metrics is served (default true).
func (c *Config) MetricsEnabled() bool { return c.Metrics.Enabled == nil || *c.Metrics.Enabled }

func (c *Config) validate() error {
	var errs []error
	if !i18n.Valid(i18n.Lang(c.Defaults.Language)) {
		errs = append(errs, fmt.Errorf("defaults.language: %q is not supported (supported: %v)", c.Defaults.Language, i18n.Supported))
	}
	if c.RetentionDays() < 0 {
		errs = append(errs, errors.New("storage.deliveries_retention_days: must not be negative"))
	}
	for i, a := range c.Admins {
		m, id, ok := strings.Cut(a, ":")
		if !ok || id == "" || (m != MessengerTelegram && m != MessengerDiscord && m != MessengerMax) {
			errs = append(errs, fmt.Errorf("admins[%d]: %q must look like messenger:id, e.g. telegram:123456789", i, a))
		}
	}
	enabled := 0
	m := c.Messengers
	if m.Telegram != nil && m.Telegram.Enabled {
		enabled++
		if m.Telegram.Token == "" {
			errs = append(errs, errors.New("messengers.telegram.token: required when telegram is enabled"))
		}
	}
	if m.Discord != nil && m.Discord.Enabled {
		enabled++
		for key, v := range map[string]string{"token": m.Discord.Token, "application_id": m.Discord.ApplicationID, "public_key": m.Discord.PublicKey} {
			if v == "" {
				errs = append(errs, fmt.Errorf("messengers.discord.%s: required when discord is enabled", key))
			}
		}
	}
	if m.Max != nil && m.Max.Enabled {
		enabled++
		if m.Max.Token == "" {
			errs = append(errs, errors.New("messengers.max.token: required when max is enabled"))
		}
	}
	if enabled == 0 {
		errs = append(errs, errors.New("messengers: at least one messenger must be enabled"))
	}
	return errors.Join(errs...)
}

// Enabled lists the names of the enabled messengers.
func (c *Config) Enabled() []string {
	var names []string
	m := c.Messengers
	if m.Telegram != nil && m.Telegram.Enabled {
		names = append(names, MessengerTelegram)
	}
	if m.Discord != nil && m.Discord.Enabled {
		names = append(names, MessengerDiscord)
	}
	if m.Max != nil && m.Max.Enabled {
		names = append(names, MessengerMax)
	}
	return names
}

// IsAdmin reports whether the user is a bot administrator listed in admins.
func (c *Config) IsAdmin(messenger, userID string) bool {
	if userID == "" {
		return false
	}
	want := messenger + ":" + userID
	for _, a := range c.Admins {
		if a == want {
			return true
		}
	}
	return false
}

// Find returns the configuration file to use: flagPath when given, then
// ./config.yaml, then SystemPath.
func Find(flagPath string) (string, error) {
	if flagPath != "" {
		if _, err := os.Stat(flagPath); err != nil {
			return "", err
		}
		return flagPath, nil
	}
	for _, p := range []string{"config.yaml", SystemPath} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// Mask replaces every occurrence of a non-empty secret in s with ***.
func Mask(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}

// PublicURLFromEnv builds server.public_url from the 1.x DOMAIN and
// USE_HTTPS variables ("" when DOMAIN is empty). USE_HTTPS defaults to true.
func PublicURLFromEnv(domain, useHTTPS string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimRight(domain, "/")
	if domain == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(useHTTPS)) {
	case "", "true", "1", "yes":
		return "https://" + domain
	}
	return "http://" + domain
}
