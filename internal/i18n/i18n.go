package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type Lang string

const (
	RU Lang = "ru"
	EN Lang = "en"
)

// Message keys — всё что используется в коде.
const (
	// Bot
	MsgWelcome          = "welcome"
	MsgHelp             = "help"
	MsgConnectDev       = "connect_dev"
	MsgUnknownCmd       = "unknown_command"
	MsgBotRegistered    = "bot_registered"
	MsgBotStarted       = "bot_started"
	MsgNewMessage       = "new_message"
	MsgSendFailed       = "send_failed"
	MsgProxyEnabled     = "proxy_enabled"
	MsgDirectConnection = "direct_connection"
	MsgBotStartFailed   = "bot_start_failed"

	// Push
	MsgPushNew       = "push_new"
	MsgPushForce     = "push_force"
	MsgPushCreated   = "push_created"
	MsgPushDeleted   = "push_deleted"
	MsgPushHeader    = "push_header"
	MsgPusher        = "pusher"
	MsgPusherEmail   = "pusher_email"
	MsgCommitsCount  = "commits_count"
	MsgCommitsForced = "commits_forced"
	MsgCommitsHeader = "commits_header"
	MsgCommitsMore   = "commits_more"
	MsgFilesHeader   = "files_header"
	MsgFilesAdded    = "files_added"
	MsgFilesModified = "files_modified"
	MsgFilesRemoved  = "files_removed"
	MsgViewChanges   = "view_changes"

	// Star
	MsgStarCreated     = "star_created"
	MsgStarDeleted     = "star_deleted"
	MsgStarVerbCreated = "star_verb_created"
	MsgStarVerbDeleted = "star_verb_deleted"

	// Dispatcher
	MsgDispatcherInit  = "dispatcher_init"
	MsgUnknownEventLog = "unknown_event_log"
	MsgNoHandlerLog    = "no_handler_log"
	MsgErrorEventLog   = "error_event_log"
	MsgUnknownEvent    = "unknown_event"
	MsgErrorEvent      = "error_event"

	// Server
	MsgServerStarted = "server_started"
)

var (
	messages map[Lang]map[string]string
	mu       sync.RWMutex
	loaded   bool
)

func Load(dir string) error {
	mu.Lock()
	defer mu.Unlock()

	messages = make(map[Lang]map[string]string)

	for _, l := range []struct {
		lang Lang
		file string
	}{
		{RU, "ru.json"},
		{EN, "en.json"},
	} {
		if err := loadFile(dir, l.file, l.lang); err != nil {
			return fmt.Errorf("i18n: %w", err)
		}
	}

	loaded = true
	return nil
}

func loadFile(dir, filename string, lang Lang) error {
	data, err := os.ReadFile(dir + "/" + filename)
	if err != nil {
		return err
	}
	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		return err
	}
	messages[lang] = msgs
	return nil
}

func T(lang Lang, key string) string {
	mu.RLock()
	defer mu.RUnlock()

	if !loaded {
		return "???"
	}

	if msgs, ok := messages[lang]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	if msgs, ok := messages[EN]; ok {
		if msg, ok := msgs[key]; ok {
			return msg
		}
	}
	return "???"
}

func Tf(lang Lang, key string, args ...interface{}) string {
	return fmt.Sprintf(T(lang, key), args...)
}

func Valid(lang Lang) bool {
	return lang == RU || lang == EN
}
