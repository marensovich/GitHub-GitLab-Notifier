package main

import (
	"GitHub-GitLab-Notifier/internal/bot"
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/server"
	"GitHub-GitLab-Notifier/storage"
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
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
	Version   = "dev-0.0.1"
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

	b, err := bot.NewBot(lang, store, Version)
	if err != nil {
		log.Fatal(err)
	}
	b.Start(ctx)

	notifyUnknown, _ := strconv.ParseBool(os.Getenv("NOTIFY_UNKNOWN_EVENTS"))
	d := dispatcher.NewDispatcher(lang, notifyUnknown)

	srv := server.New(server.Config{
		Addr:          net.JoinHostPort(os.Getenv("SERVER_ADDRESS"), firstNonEmpty(os.Getenv("SERVER_PORT"), "8080")),
		DefaultChatID: chatID,
		DefaultLang:   lang,
		WebhookSecret: os.Getenv("WEBHOOK_SECRET"),
		Version:       Version,
	}, d, store, b)

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
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
