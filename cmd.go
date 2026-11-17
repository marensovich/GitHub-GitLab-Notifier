package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/storage"
)

const usageText = `Usage: notifier [command] [flags]

Commands:
  serve            run the bot (the default)
                     --config <file>  config.yaml to use (default ./config.yaml, then /etc/notifier/config.yaml)
  version          print the version
  config check     validate the configuration and exit
                     --config <file>
  migrate-config   convert a 1.x .env file to config.yaml
                     --from <file>  .env to read (default .env)
                     --to <file>    config.yaml to write (default config.yaml)
                     --force        overwrite an existing file
                     --keep-secrets-in-env  write ${TELEGRAM_TOKEN} and ${WEBHOOK_SECRET} instead of the values
  db backup <path> write a copy of the database to path
                     --config <file>
  help             show this help
`

// run executes the command line and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		if len(args) > 0 && (args[0] == "--version" || args[0] == "-version") {
			return cmdVersion(stdout)
		}
		return cmdServe(args, stderr)
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "serve":
		return cmdServe(rest, stderr)
	case "version":
		return cmdVersion(stdout)
	case "config":
		if len(rest) == 0 || rest[0] != "check" {
			fmt.Fprint(stderr, "usage: notifier config check [--config <file>]\n")
			return 2
		}
		return cmdConfigCheck(rest[1:], stdout, stderr)
	case "migrate-config":
		return cmdMigrateConfig(rest, stdout, stderr)
	case "db":
		if len(rest) == 0 || rest[0] != "backup" {
			fmt.Fprint(stderr, "usage: notifier db backup [--config <file>] <path>\n")
			return 2
		}
		return cmdDBBackup(rest[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usageText)
		return 2
	}
}

func cmdVersion(stdout io.Writer) int {
	fmt.Fprintf(stdout, "GitHub-GitLab-Notifier %s (built %s)\n", Version, BuildTime)
	return 0
}

// newFlags returns a flag set that reports errors to stderr.
func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func cmdServe(args []string, stderr io.Writer) int {
	cfg, err := loadServe(args, environ(), func(s string) { fmt.Fprintf(stderr, "⚠️ %s\n", s) })
	if err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	if err := serve(cfg); err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	return 0
}

// loadServe parses the serve flags and loads the configuration. The 1.x
// flags --db and --lang still override storage.path and defaults.language.
func loadServe(args []string, env map[string]string, warn func(string)) (*config.Config, error) {
	fs := newFlags("serve", io.Discard)
	path := fs.String("config", "", "")
	db := fs.String("db", "", "")
	lang := fs.String("lang", "", "")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	cfg, err := loadConfig(*path, env, warn)
	if err != nil {
		return nil, err
	}
	if *db != "" {
		warn("--db is deprecated: set storage.path in config.yaml")
		cfg.Storage.Path = *db
	}
	if *lang != "" {
		warn("--lang is deprecated: set defaults.language in config.yaml")
		if !i18n.Valid(i18n.Lang(*lang)) {
			return nil, fmt.Errorf("--lang: %q is not supported (supported: %v)", *lang, i18n.Supported)
		}
		cfg.Defaults.Language = *lang
	}
	return cfg, nil
}

// envHint is the error for a 1.x .env given where config.yaml is expected.
func envHint(path string) error {
	return fmt.Errorf("config.yaml is required since 2.0; run: notifier migrate-config --from %s --to config.yaml", path)
}

// loadConfig finds and loads config.yaml. When there is none and the
// environment has the 1.x BOT_TOKEN (a Docker container started with
// env_file), the configuration is built from the environment instead.
func loadConfig(flagPath string, env map[string]string, warn func(string)) (*config.Config, error) {
	if flagPath != "" && looksLikeEnvFile(flagPath) {
		return nil, envHint(flagPath)
	}
	path, err := config.Find(flagPath)
	switch {
	case errors.Is(err, config.ErrNotFound) && env["BOT_TOKEN"] != "":
		text, warnings, err := config.FromEnv(env, false)
		if err != nil {
			return nil, fmt.Errorf("config.yaml not found, and the 1.x environment variables are not valid: %w", err)
		}
		for _, w := range warnings {
			warn(w)
		}
		warn("config.yaml not found: using the 1.x environment variables. This will stop working in a later release; " +
			"run: notifier migrate-config --from <your .env> --to config.yaml")
		return config.Parse(text, func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	case errors.Is(err, config.ErrNotFound):
		if _, statErr := os.Stat(".env"); statErr == nil {
			return nil, envHint(".env")
		}
		return nil, fmt.Errorf("config.yaml not found (looked at --config, ./config.yaml and %s)", config.SystemPath)
	case err != nil:
		return nil, err
	}
	return config.Load(path)
}

var envLine = regexp.MustCompile(`^(export\s+)?[A-Za-z_][A-Za-z0-9_]*=`)

// looksLikeEnvFile reports whether path is a 1.x .env: by its name, or by
// a first line that is KEY=VALUE.
func looksLikeEnvFile(path string) bool {
	base := filepath.Base(path)
	if base == ".env" || strings.HasSuffix(base, ".env") {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return envLine.MatchString(line)
	}
	return false
}

// environ returns the process environment as a map.
func environ() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

func cmdConfigCheck(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("config check", stderr)
	path := fs.String("config", "", "config.yaml to check")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*path, environ(), func(s string) { fmt.Fprintf(stderr, "⚠️ %s\n", s) })
	if err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "✅ configuration is valid; messengers: %s\n", strings.Join(cfg.Enabled(), ", "))
	return 0
}

func cmdMigrateConfig(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("migrate-config", stderr)
	from := fs.String("from", ".env", "1.x .env to read")
	to := fs.String("to", "config.yaml", "config.yaml to write")
	force := fs.Bool("force", false, "overwrite an existing file")
	secretsInEnv := fs.Bool("keep-secrets-in-env", false, "reference the secrets as environment variables")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	env, err := config.ReadEnvFile(*from)
	if err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	text, warnings, err := config.FromEnv(env, *secretsInEnv)
	if err != nil {
		fmt.Fprintf(stderr, "❌ %s: %v\n", *from, err)
		return 1
	}
	if _, err := os.Stat(*to); err == nil && !*force {
		fmt.Fprintf(stderr, "❌ %s already exists; pass --force to overwrite it\n", *to)
		return 1
	}
	// The file holds the bot token: only its owner may read it.
	if err := os.WriteFile(*to, text, 0o600); err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	if err := os.Chmod(*to, 0o600); err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	for _, w := range warnings {
		fmt.Fprintf(stderr, "⚠️ %s\n", w)
	}
	fmt.Fprintf(stdout, "✅ wrote %s from %s; check it with: notifier config check --config %s\n", *to, *from, *to)
	return 0
}

func cmdDBBackup(args []string, stdout, stderr io.Writer) int {
	fs := newFlags("db backup", stderr)
	path := fs.String("config", "", "config.yaml to read storage.path from")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, "usage: notifier db backup [--config <file>] <path>\n")
		return 2
	}
	cfg, err := loadConfig(*path, environ(), func(s string) { fmt.Fprintf(stderr, "⚠️ %s\n", s) })
	if err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	if err := storage.BackupFile(cfg.Storage.Path, fs.Arg(0)); err != nil {
		fmt.Fprintf(stderr, "❌ %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "✅ database %s saved to %s\n", cfg.Storage.Path, fs.Arg(0))
	return 0
}
