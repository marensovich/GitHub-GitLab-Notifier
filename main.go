package main

import (
	"GitHub-GitLab-Notifier/internal/bot"
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	Version   = "dev-0.0.1"
	BuildTime = "unknown"
)

var (
	b                 *bot.Bot
	dispatcherService *dispatcher.Dispatcher
	chatID            int64
	lang              i18n.Lang
)

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	langFlag := flag.String("lang", "en", "Language (ru, en)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("GitHub-GitLab-Notifier %s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	lang = i18n.Lang(*langFlag)
	if !i18n.Valid(lang) {
		fmt.Printf("Invalid language: %s. Supported: ru, en\n", *langFlag)
		os.Exit(1)
	}

	if err := i18n.Load("locales"); err != nil {
		log.Fatalf("❌ %v", err)
	}

	loadEnv()

	chatIDStr := os.Getenv("CHAT_ID")
	if chatIDStr == "" {
		log.Fatal("❌ CHAT_ID is not set")
	}
	var err error
	chatID, err = strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		log.Fatalf("❌ Invalid CHAT_ID: %v", err)
	}

	b, err = bot.NewBot(lang)
	if err != nil {
		log.Fatalf("❌ %v", err)
	}
	b.Start()

	dispatcherService = dispatcher.NewDispatcher(lang)

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/webhook", webhookHandler)
	http.HandleFunc("/webhook/health", healthHandler)

	log.Printf(i18n.T(lang, i18n.MsgServerStarted), port, Version)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Failed to read body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	payload := string(body)

	var eventType string
	var p platform.Platform

	githubEvent := r.Header.Get("X-GitHub-Event")
	gitlabEvent := r.Header.Get("X-Gitlab-Event")

	if githubEvent != "" {
		p = platform.GitHub
		eventType = githubEvent
	} else if gitlabEvent != "" {
		p = platform.GitLab
		eventType = gitlabEvent
	} else {
		log.Println(i18n.T(lang, i18n.MsgUnknownEvent))
		http.Error(w, "Unknown platform", http.StatusBadRequest)
		return
	}

	message := dispatcherService.Dispatch(p, eventType, payload)
	b.SendMessage(chatID, message)

	w.Write([]byte("OK"))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

func loadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️ .env file not found")
	}
}
