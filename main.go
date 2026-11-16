package main

import (
	"GitHub-GitLab-Notifier/internal/bot"
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/metrics"
	"GitHub-GitLab-Notifier/internal/server"
	"GitHub-GitLab-Notifier/internal/web"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

//go:embed locales/*.json
var embeddedLocales embed.FS

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	langFlag := flag.String("lang", "", "Default language (ru, en). Overrides BOT_LANG")
	envFile := flag.String("config", ".env", "Path to the .env configuration file")
	dbPath := flag.String("db", "", "Path to the SQLite database. Overrides DB_PATH (default data/notifier.db)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("GitHub-GitLab-Notifier %s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	locales, _ := fs.Sub(embeddedLocales, "locales")
	if err := i18n.LoadFS(locales); err != nil {
		log.Fatalf("❌ %v", err)
	}

	envErr := godotenv.Load(*envFile)

	lang := i18n.Lang(firstNonEmpty(*langFlag, os.Getenv("BOT_LANG"), string(i18n.EN)))
	if !i18n.Valid(lang) {
		fmt.Printf("Invalid language: %s. Supported: %v\n", lang, i18n.Supported)
		os.Exit(1)
	}
	if envErr != nil {
		log.Println(i18n.T(lang, i18n.MsgEnvFileNotFound))
	}

	var chatID int64
	if chatIDStr := os.Getenv("CHAT_ID"); chatIDStr != "" {
		var err error
		chatID, err = strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			log.Fatalf(i18n.T(lang, i18n.MsgInvalidBotChatId), err)
		}
	}

	store, err := storage.Open(firstNonEmpty(*dbPath, os.Getenv("DB_PATH"), "data/notifier.db"))
	if err != nil {
		log.Fatalf("❌ %v", err)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	b, err := bot.NewBot(lang, store, Version, richMessages())
	if err != nil {
		log.Fatal(err)
	}
	b.Start(ctx)

	notifyUnknown, _ := strconv.ParseBool(os.Getenv("NOTIFY_UNKNOWN_EVENTS"))
	d := dispatcher.NewDispatcher(lang, notifyUnknown)

	m := metrics.New(Version)
	metricsEnabled, metricsAddr := metricsConfig()

	srv := server.New(server.Config{
		Addr:                net.JoinHostPort(os.Getenv("SERVER_ADDRESS"), firstNonEmpty(os.Getenv("SERVER_PORT"), "8080")),
		DefaultChatID:       chatID,
		DefaultLang:         lang,
		WebhookSecret:       os.Getenv("WEBHOOK_SECRET"),
		Version:             Version,
		DeliveriesRetention: deliveriesRetention(lang),
		Metrics:             m,
		ServeMetrics:        metricsEnabled && metricsAddr == "",
		Checks: map[string]server.Check{
			"db":       store.Ping,
			"telegram": b.CheckTelegram,
		},
	}, d, store, b)
	metricsSrv := startMetrics(lang, m, metricsEnabled, metricsAddr)
	go srv.PruneDeliveries(ctx)

	panel := startPanel(lang, store)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil {
			log.Fatalf(i18n.T(lang, i18n.MsgServerFailed), err)
		}
	case <-ctx.Done():
		log.Println(i18n.T(lang, i18n.MsgShuttingDown))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf(i18n.T(lang, i18n.MsgServerFailed), err)
		}
		if metricsSrv != nil {
			metricsSrv.Shutdown(shutdownCtx)
		}
		if panel != nil {
			if err := panel.Shutdown(shutdownCtx); err != nil {
				log.Printf(i18n.T(lang, i18n.MsgPanelFailed), err)
			}
		}
	}
}

// startPanel starts the local web panel when PANEL_ADDRESS is set.
// A misconfigured address stops the bot; a panel that fails later only
// logs the error, so notifications keep flowing.
func startPanel(lang i18n.Lang, store *storage.Store) *web.Server {
	addr := os.Getenv("PANEL_ADDRESS")
	if addr == "" {
		return nil
	}
	allowRemote, _ := strconv.ParseBool(os.Getenv("PANEL_ALLOW_REMOTE"))

	panel, err := web.New(web.Config{
		Addr:            addr,
		AllowRemote:     allowRemote,
		GlobalSecretSet: os.Getenv("WEBHOOK_SECRET") != "",
		Version:         Version,
		StartedAt:       time.Now(),
	}, store)
	if err != nil {
		log.Fatalf(i18n.T(lang, i18n.MsgPanelFailed), err)
	}

	go func() {
		log.Printf(i18n.T(lang, i18n.MsgPanelStarted), addr)
		if err := panel.ListenAndServe(); err != nil {
			log.Printf(i18n.T(lang, i18n.MsgPanelFailed), err)
		}
	}()
	return panel
}

// metricsConfig reads METRICS_ENABLED (default true) and METRICS_ADDR.
func metricsConfig() (enabled bool, addr string) {
	enabled = true
	if v := os.Getenv("METRICS_ENABLED"); v != "" {
		enabled, _ = strconv.ParseBool(v)
	}
	return enabled, os.Getenv("METRICS_ADDR")
}

// startMetrics serves /metrics on its own address when METRICS_ADDR is set,
// so the metrics need not be public together with the webhook port.
func startMetrics(lang i18n.Lang, m *metrics.Metrics, enabled bool, addr string) *http.Server {
	if !enabled || addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf(i18n.T(lang, i18n.MsgMetricsStarted), addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf(i18n.T(lang, i18n.MsgMetricsFailed), err)
		}
	}()
	return srv
}

// deliveriesRetention reads DELIVERIES_RETENTION_DAYS (default 14, 0 = keep forever).
func deliveriesRetention(lang i18n.Lang) time.Duration {
	raw := os.Getenv("DELIVERIES_RETENTION_DAYS")
	if raw == "" {
		return 14 * 24 * time.Hour
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 {
		log.Fatal(i18n.Tf(lang, i18n.MsgInvalidRetention, raw))
	}
	return time.Duration(days) * 24 * time.Hour
}

// richMessages reports whether notifications may be sent as Telegram rich
// messages (RICH_MESSAGES, on by default). Off sends every chat the classic
// format, whatever it chose.
func richMessages() bool {
	v := os.Getenv("RICH_MESSAGES")
	if v == "" {
		return true
	}
	on, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("⚠️ RICH_MESSAGES=%q is not a boolean, keeping rich messages on", v)
		return true
	}
	return on
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
