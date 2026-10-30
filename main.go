package main

import (
	"GitHub-GitLab-Notifier/internal/bot"
	"GitHub-GitLab-Notifier/internal/dispatcher"
	"GitHub-GitLab-Notifier/internal/i18n"
	"GitHub-GitLab-Notifier/internal/platform"
	"GitHub-GitLab-Notifier/storage"
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
	Version           = "dev-0.0.1"
	BuildTime         = "unknown"
	b                 *bot.Bot
	dispatcherService *dispatcher.Dispatcher
	chatID            int64
	lang              i18n.Lang
	store             *storage.Store
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

	var dberr error
	store, dberr = storage.Open("data/notifier.db")
	if dberr != nil {
		log.Fatalf("❌ %v", dberr)
	}
	defer store.Close()

	chatIDStr := os.Getenv("CHAT_ID")
	if chatIDStr == "" {
		log.Fatal(i18n.T(lang, i18n.MsgBotChatIdIsNotSet))
	}
	var err error
	chatID, err = strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		log.Fatalf(i18n.T(lang, i18n.MsgInvalidBotChatId), err)
	}

	b, err = bot.NewBot(lang, store)
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
		log.Fatalf(i18n.T(lang, i18n.MsgServerFailed), err)
	}
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, i18n.T(lang, i18n.MsgMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf(i18n.T(lang, i18n.MsgFailedToReadBody), err)
		http.Error(w, i18n.T(lang, i18n.MsgBadRequest), http.StatusBadRequest)
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
		log.Println(i18n.T(lang, i18n.MsgUnknownPlatform))
		http.Error(w, i18n.T(lang, i18n.MsgUnknownPlatform), http.StatusBadRequest)
		return
	}

	log.Print(i18n.Tf(lang, i18n.MsgWebhookReceived, p, eventType))

	message := dispatcherService.Dispatch(p, eventType, payload)

	chats := store.GetAllChats()
	if len(chats) == 0 {
		log.Println(i18n.T(lang, i18n.MsgNoChatsConfigured))
		b.SendMessage(chatID, message)
	} else {
		for _, chat := range chats {
			topicID := store.GetTopicByEvent(chat.ChatID, eventType)
			b.SendMessageToTopic(chat.ChatID, topicID, message)
			log.Print(i18n.Tf(lang, i18n.MsgWebhookNotificationSent, chat.ChatID))
		}
	}

	w.Write([]byte("OK"))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("pong"))
}

func loadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Println(i18n.T(lang, i18n.MsgEnvFileNotFound))
	}
}
