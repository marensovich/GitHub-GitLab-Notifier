package main

import (
	"GitHub-GitLab-Notifier/internal/commands"
	"GitHub-GitLab-Notifier/internal/config"
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/messenger"
	"GitHub-GitLab-Notifier/internal/messenger/discord"
	"GitHub-GitLab-Notifier/internal/messenger/max"
	"GitHub-GitLab-Notifier/internal/messenger/telegram"
	"GitHub-GitLab-Notifier/internal/metrics"
	"GitHub-GitLab-Notifier/internal/outbox"
	"GitHub-GitLab-Notifier/internal/server"
	"GitHub-GitLab-Notifier/internal/web"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed locales/*.json
var embeddedLocales embed.FS

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// serve runs the bot until SIGINT or SIGTERM.
func serve(cfg *config.Config) error {
	locales, _ := fs.Sub(embeddedLocales, "locales")
	if err := i18n.LoadFS(locales); err != nil {
		return err
	}
	lang := i18n.Lang(cfg.Defaults.Language)

	store, err := storage.Open(cfg.Storage.Path)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Adapters of the enabled messengers.
	ms := map[string]messenger.Messenger{}
	var tg *telegram.Telegram
	if t := cfg.Messengers.Telegram; t != nil && t.Enabled {
		if tg, err = telegram.New(*t, lang, Version); err != nil {
			return err
		}
		ms[telegram.Name] = tg
	}
	var dc *discord.Discord
	routes := map[string]http.Handler{}
	if c := cfg.Messengers.Discord; c != nil && c.Enabled {
		if dc, err = discord.New(*c, lang, Version); err != nil {
			return err
		}
		ms[discord.Name] = dc
		routes[discord.InteractionsPath] = dc
		log.Printf("🔗 Discord: set the Interactions Endpoint URL to %s", strings.TrimRight(cfg.Server.PublicURL, "/")+discord.InteractionsPath)
		log.Printf("🔗 Discord: invite the bot with %s", dc.InviteURL())
	}
	var mx *max.Max
	if c := cfg.Messengers.Max; c != nil && c.Enabled {
		if mx, err = max.New(*c, lang, cfg.Server.PublicURL); err != nil {
			return err
		}
		ms[max.Name] = mx
		if mx.UsesWebhook() {
			routes[max.UpdatesPath] = mx
		} else {
			log.Print("🔗 MAX: server.public_url is not HTTPS on port 443, so updates come by long polling")
		}
	}
	for _, name := range cfg.Enabled() {
		if ms[name] == nil {
			log.Printf("⚠️ %s is enabled in the configuration but not available in this build yet", name)
		}
	}
	if len(ms) == 0 {
		return errors.New("no messenger to run: enable telegram, discord or max in config.yaml")
	}

	core := commands.New(store, cfg, Version, ms)
	if tg != nil {
		go func() {
			if err := tg.Run(ctx, core); err != nil {
				log.Fatal(err)
			}
		}()
	}
	if dc != nil {
		go func() {
			if err := dc.Run(ctx, core); err != nil {
				log.Fatal(err)
			}
		}()
	}
	if mx != nil {
		go func() {
			if err := mx.Run(ctx, core); err != nil {
				log.Fatal(err)
			}
		}()
	}

	d := dispatcher.NewDispatcher(lang, cfg.Defaults.NotifyUnknownEvents)
	m := metrics.New(Version)

	// The webhook server stores messages; the outbox sends and retries them.
	o := outbox.New(store, ms, m, time.Now)
	o.SetThreadMaker(core.MakeThread)
	go o.Run(ctx)

	srv := server.New(server.Config{
		Addr:                cfg.Server.Listen,
		DefaultLang:         lang,
		WebhookSecret:       cfg.Security.WebhookSecret,
		Version:             Version,
		DeliveriesRetention: time.Duration(cfg.RetentionDays()) * 24 * time.Hour,
		Metrics:             m,
		ServeMetrics:        cfg.MetricsEnabled() && cfg.Metrics.Listen == "",
		Checks:              server.Checks(store.Ping, ms),
		Routes:              routes,
	}, d, store, o)
	metricsSrv := startMetrics(lang, m, cfg.MetricsEnabled(), cfg.Metrics.Listen)
	go srv.PruneDeliveries(ctx)

	richAvailable := func() bool { return true }
	if tg != nil {
		richAvailable = tg.RichAvailable
	}
	panel, err := startPanel(lang, cfg, store, richAvailable)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf(i18n.T(lang, i18n.MsgServerFailed), err)
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
	return nil
}

// startPanel starts the local web panel when panel.listen is set. A
// misconfigured address stops the bot; a panel that fails later only logs
// the error, so notifications keep flowing.
func startPanel(lang i18n.Lang, cfg *config.Config, store *storage.Store, richAvailable func() bool) (*web.Server, error) {
	addr := cfg.Panel.Listen
	if addr == "" {
		return nil, nil
	}
	panel, err := web.New(web.Config{
		Addr:            addr,
		AllowRemote:     cfg.Panel.AllowRemote,
		GlobalSecretSet: cfg.Security.WebhookSecret != "",
		Version:         Version,
		StartedAt:       time.Now(),
		RichAvailable:   richAvailable,
		Secrets:         panelSecrets(cfg),
	}, store)
	if err != nil {
		return nil, fmt.Errorf(i18n.T(lang, i18n.MsgPanelFailed), err)
	}

	go func() {
		log.Printf(i18n.T(lang, i18n.MsgPanelStarted), addr)
		if err := panel.ListenAndServe(); err != nil {
			log.Printf(i18n.T(lang, i18n.MsgPanelFailed), err)
		}
	}()
	return panel, nil
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

// panelSecrets are the values the panel masks in error texts it shows.
func panelSecrets(cfg *config.Config) []string {
	secrets := []string{cfg.Security.WebhookSecret}
	m := cfg.Messengers
	if m.Telegram != nil {
		secrets = append(secrets, m.Telegram.Token)
	}
	if m.Discord != nil {
		secrets = append(secrets, m.Discord.Token, m.Discord.PublicKey)
	}
	if m.Max != nil {
		secrets = append(secrets, m.Max.Token)
	}
	return secrets
}
