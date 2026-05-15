package main

import (
	"GitHub-GitLab-Notifier/internal/bot"
	"GitHub-GitLab-Notifier/internal/dispatcher"
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
	b                 *bot.Bot
	dispatcherService *dispatcher.Dispatcher
	chatID            int64
)

var (
	Version   = "dev-0.0.1"
	BuildTime = "unknown"
)

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("GitHub-GitLab-Notifier %s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	loadEnv()

	// Chat ID для уведомлений
	chatIDStr := os.Getenv("CHAT_ID")
	if chatIDStr == "" {
		log.Fatal("❌ CHAT_ID is not set")
	}
	var err error
	chatID, err = strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		log.Fatalf("❌ Invalid CHAT_ID: %v", err)
	}

	// Бот
	b, err = bot.NewBot()
	if err != nil {
		log.Fatalf("❌ %v", err)
	}
	b.Start()

	// Диспетчер вебхуков
	dispatcherService = dispatcher.NewDispatcher()

	// HTTP-сервер
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/webhook", webhookHandler)
	http.HandleFunc("/webhook/health", healthHandler)

	log.Printf("🚀 Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}

// webhookHandler handles incoming webhooks from GitHub and GitLab.
// Auto-detects platform from headers.
//
// Обрабатывает входящие вебхуки от GitHub и GitLab.
// Автоматически определяет платформу по заголовкам.
func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Читаем тело
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("❌ Failed to read body: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	payload := string(body)
	log.Printf("📥 payload=%s", payload)

	// Определяем платформу
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
		log.Println("❓ Unknown platform")
		http.Error(w, "Unknown platform", http.StatusBadRequest)
		return
	}

	log.Printf("📥 Webhook | platform=%s | event=%s", p, eventType)

	// Диспатчим
	message := dispatcherService.Dispatch(p, eventType, payload)
	b.SendMessage(chatID, message)
	log.Println("✅ Notification sent")

	w.Write([]byte("OK"))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

func loadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️ .env file not found, using system environment variables")
	} else {
		log.Println("✅ .env file loaded")
	}
}
