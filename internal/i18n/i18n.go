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

const (
	// Bot
	MsgFailedTopicCreate           = "failed_topic_create"
	MsgFailedSaveTopicToStorage    = "failed_save_topic_to_storage"
	MsgFailedToMapEventToStorage   = "failed_to_map_event_to_storage"
	MsgTopicCreated                = "topic_created"
	MsgFailedToSaveChatConfig      = "failed_to_save_chat_config"
	MsgFailedToCheckBotAdminStatus = "failed_to_check_bot_admin_status"
	MsgFailedToGetTopics           = "failed_to_get_topics"
	MsgConfigureChatSkipped        = "configure_chat_skipped"
	MsgConfigureChatAllExist       = "configure_chat_all_exist"
	MsgFailedToGetExistingTopics   = "failed_to_get_existing_topics"
	MsgTopicAlreadyExists          = "topic_already_exists"
	MsgWelcome                     = "welcome"
	MsgHelp                        = "help"
	MsgConnectDev                  = "connect_dev"
	MsgUnknownCmd                  = "unknown_command"
	MsgBotRegistered               = "bot_registered"
	MsgBotStarted                  = "bot_started"
	MsgNewMessage                  = "new_message"
	MsgSendFailed                  = "send_failed"
	MsgProxyEnabled                = "proxy_enabled"
	MsgDirectConnection            = "direct_connection"
	MsgBotStartFailed              = "bot_start_failed"
	MsgConfigureChatIsNotForum     = "configure_chat_is_not_forum"
	MsgConfigureChatNotAdmin       = "configure_chat_not_admin"
	MsgConfigureChatSuccess        = "configure_chat_success"
	MsgConfigureChatHowToUse       = "configure_chat_how_to_use"
	MsgTopicCreateFailed           = "topic_create_failed"
	MsgConnectUsage                = "connect_usage"
	MsgConnectInvalidURL           = "connect_invalid_url"
	MsgConnectSuccess              = "connect_success"
	MsgConnectURL                  = "connect_url"
	MsgConnectContentType          = "connect_content_type"
	MsgConnectGitHub               = "connect_github"
	MsgConnectGitLab               = "connect_gitlab"
	MsgConnectTest                 = "connect_test"
	MsgConnectFailed               = "connect_failed"
	MsgConnectLogged               = "connect_logged"
	MsgConnectNoDomain             = "connect_no_domain"
	MsgWebhookReceived             = "webhook_received"
	MsgNoChatsConfigured           = "no_chats_configured"
	MsgWebhookNotificationSent     = "webhook_notification_sent"

	// Push
	MsgPushNew                = "push_new"
	MsgPushForce              = "push_force"
	MsgPushCreated            = "push_created"
	MsgPushDeleted            = "push_deleted"
	MsgPushHeader             = "push_header"
	MsgPusher                 = "pusher"
	MsgPusherEmail            = "pusher_email"
	MsgCommitsCount           = "commits_count"
	MsgCommitsForced          = "commits_forced"
	MsgCommitsHeader          = "commits_header"
	MsgCommitsMore            = "commits_more"
	MsgFilesHeader            = "files_header"
	MsgFilesAdded             = "files_added"
	MsgFilesModified          = "files_modified"
	MsgFilesRemoved           = "files_removed"
	MsgViewChanges            = "view_changes"
	MsgFailedToParsePushEvent = "failed_to_parse_push_event"

	// Star
	MsgStarCreated            = "star_created"
	MsgStarDeleted            = "star_deleted"
	MsgStarVerbCreated        = "star_verb_created"
	MsgStarVerbDeleted        = "star_verb_deleted"
	MsgFailedToParseStarEvent = "failed_to_parse_star_event"

	// Dispatcher
	MsgDispatcherInit  = "dispatcher_init"
	MsgUnknownEventLog = "unknown_event_log"
	MsgNoHandlerLog    = "no_handler_log"
	MsgErrorEventLog   = "error_event_log"
	MsgUnknownEvent    = "unknown_event"
	MsgErrorEvent      = "error_event"

	// Server
	MsgServerStarted      = "server_started"
	MsgServerFailed       = "server_failed"
	MsgMethodNotAllowed   = "method_not_allowed"
	MsgFailedToReadBody   = "failed_to_read_body"
	MsgBadRequest         = "bad_request"
	MsgUnknownPlatform    = "unknown_platform"
	MsgEnvFileNotFound    = "env_file_not_found"
	MsgBotTokenIsNotSet   = "bot_token_is_not_set"
	MsgBotChatIdIsNotSet  = "bot_chat_id_is_not_set"
	MsgInvalidBotChatId   = "invalid_bot_chat_id"
	MsgFailedToCreateBot  = "failed_to_create_bot"
	MsgFailedToGetBotInfo = "failed_to_get_bot_info"
	MsgInvalidProxyUrl    = "invalid_proxy_url"
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
