package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"slices"
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
	MsgUnknownCmd                  = "unknown_command"
	MsgBotRegistered               = "bot_registered"
	MsgBotStarted                  = "bot_started"
	MsgNewMessage                  = "new_message"
	MsgSendFailed                  = "send_failed"
	MsgProxyEnabled                = "proxy_enabled"
	MsgDirectConnection            = "direct_connection"
	MsgBotStartFailed              = "bot_start_failed"
	MsgConfigureChatNotAdmin       = "configure_chat_not_admin"
	MsgConfigureChatSuccess        = "configure_chat_success"
	MsgConfigureChatHowToUse       = "configure_chat_how_to_use"
	MsgTopicCreateFailed           = "topic_create_failed"
	MsgCfgTopicsQuestion           = "cfg_topics_question"
	MsgCfgTopicsYes                = "cfg_topics_yes"
	MsgCfgTopicsNo                 = "cfg_topics_no"
	MsgCfgFormatQuestion           = "cfg_format_question"
	MsgCfgFormatRich               = "cfg_format_rich"
	MsgCfgFormatClassic            = "cfg_format_classic"
	MsgCfgTopicsOn                 = "cfg_topics_on"
	MsgCfgTopicsOff                = "cfg_topics_off"
	MsgCfgFormatRichShort          = "cfg_format_rich_short"
	MsgCfgFormatClassicShort       = "cfg_format_classic_short"
	MsgCfgSummary                  = "cfg_summary"
	MsgCfgNotAdmin                 = "cfg_not_admin"
	MsgCfgAnonymous                = "cfg_anonymous"
	MsgCfgFailed                   = "cfg_failed"
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
	MsgConnectNeedsSecret          = "connect_needs_secret"
	MsgConnectAdminOnly            = "connect_admin_only"
	MsgWebhookReceived             = "webhook_received"
	MsgNoChatsConfigured           = "no_chats_configured"
	MsgWebhookNotificationSent     = "webhook_notification_sent"
	MsgSetCommandsFailed           = "set_commands_failed"
	MsgVersion                     = "version"
	MsgFailedToSaveRepo            = "failed_to_save_repo"
	MsgConnectSecret               = "connect_secret"
	MsgDisconnectUsage             = "disconnect_usage"
	MsgDisconnectNotFound          = "disconnect_not_found"
	MsgDisconnectSuccess           = "disconnect_success"
	MsgStatusHeader                = "status_header"
	MsgStatusSettings              = "status_settings"
	MsgStatusRichOff               = "status_rich_off"
	MsgStatusYes                   = "status_yes"
	MsgStatusNo                    = "status_no"
	MsgStatusTopicsHeader          = "status_topics_header"
	MsgStatusTopicLine             = "status_topic_line"
	MsgStatusNoTopics              = "status_no_topics"
	MsgStatusReposHeader           = "status_repos_header"
	MsgStatusRepoLine              = "status_repo_line"
	MsgStatusNoRepos               = "status_no_repos"
	MsgStatusVersion               = "status_version"
	MsgLangUsage                   = "lang_usage"
	MsgLangInvalid                 = "lang_invalid"
	MsgLangChanged                 = "lang_changed"

	// Push
	MsgPushNew                = "push_new"
	MsgPushForce              = "push_force"
	MsgPushCreated            = "push_created"
	MsgPushDeleted            = "push_deleted"
	MsgFailedToParsePushEvent = "failed_to_parse_push_event"

	// Star
	MsgStarCreated            = "star_created"
	MsgStarDeleted            = "star_deleted"
	MsgFailedToParseStarEvent = "failed_to_parse_star_event"

	// Generic event handler messages
	MsgFailedToParseEvent = "failed_to_parse_event"
	MsgItemConfidential   = "item_confidential"
	MsgFieldRepo          = "field_repo"
	MsgSectionMore        = "section_more"

	// Ping / fork / tags
	MsgPing       = "ping"
	MsgFork       = "fork"
	MsgTagCreated = "tag_created"
	MsgTagDeleted = "tag_deleted"

	// Pull / merge requests
	MsgPROpened          = "pr_opened"
	MsgPROpenedDraft     = "pr_opened_draft"
	MsgPRClosed          = "pr_closed"
	MsgPRMerged          = "pr_merged"
	MsgPRReopened        = "pr_reopened"
	MsgPRReady           = "pr_ready"
	MsgPRAssigned        = "pr_assigned"
	MsgPRReviewRequested = "pr_review_requested"
	MsgMRAssigned        = "mr_assigned"
	MsgMRReviewRequested = "mr_review_requested"
	MsgIssueAssigned     = "issue_assigned"

	// Personal DMs: why the DM came
	MsgPersonalReview   = "personal_review"
	MsgPersonalAssigned = "personal_assigned"
	MsgPersonalMention  = "personal_mention"
	MsgPersonalPipeline = "personal_pipeline"

	// /link, /unlink, /me
	MsgLinkUsage              = "link_usage"
	MsgLinkPrivateOnly        = "link_private_only"
	MsgLinkBadToken           = "link_bad_token"
	MsgLinkFailed             = "link_failed"
	MsgLinkNeedHost           = "link_need_host"
	MsgLinkPickHost           = "link_pick_host"
	MsgLinkNoDM               = "link_no_dm"
	MsgLinkDone               = "link_done"
	MsgUnlinkUsage            = "unlink_usage"
	MsgUnlinkDone             = "unlink_done"
	MsgUnlinkNone             = "unlink_none"
	MsgMeNone                 = "me_none"
	MsgMeHeader               = "me_header"
	MsgMeLine                 = "me_line"
	MsgMeFooter               = "me_footer"
	MsgMeNotYours             = "me_not_yours"
	MsgMeBtnReview            = "me_btn_review"
	MsgMeBtnAssigned          = "me_btn_assigned"
	MsgMeBtnMention           = "me_btn_mention"
	MsgMeBtnPipeline          = "me_btn_pipeline"
	MsgReviewApproved         = "review_approved"
	MsgReviewChangesRequested = "review_changes_requested"
	MsgReviewCommented        = "review_commented"
	MsgMROpened               = "mr_opened"
	MsgMROpenedDraft          = "mr_opened_draft"
	MsgMRClosed               = "mr_closed"
	MsgMRMerged               = "mr_merged"
	MsgMRReopened             = "mr_reopened"
	MsgMRApproved             = "mr_approved"

	// Issues and comments
	MsgIssueOpened           = "issue_opened"
	MsgIssueClosed           = "issue_closed"
	MsgIssueClosedNotPlanned = "issue_closed_not_planned"
	MsgIssueReopened         = "issue_reopened"
	MsgCommentIssue          = "comment_issue"
	MsgCommentPR             = "comment_pr"
	MsgCommentMR             = "comment_mr"
	MsgCommentCommit         = "comment_commit"

	// Releases
	MsgReleasePublished  = "release_published"
	MsgReleasePrerelease = "release_prerelease"

	// CI/CD
	MsgCISuccess   = "ci_success"
	MsgCIFailed    = "ci_failed"
	MsgCICancelled = "ci_cancelled"

	// Notification layout: field labels, sections and buttons
	MsgPingNote       = "ping_note"
	MsgFieldBranch    = "field_branch"
	MsgFieldBranches  = "field_branches"
	MsgFieldChanges   = "field_changes"
	MsgChangesTail    = "changes_tail"
	MsgFieldCommits   = "field_commits"
	MsgFieldEmail     = "field_email"
	MsgFieldTag       = "field_tag"
	MsgFieldCommit    = "field_commit"
	MsgFieldDuration  = "field_duration"
	MsgFieldStars     = "field_stars"
	MsgFieldForks     = "field_forks"
	MsgFieldLabels    = "field_labels"
	MsgSectionCommits = "section_commits"
	MsgSectionFiles   = "section_files"
	MsgBtnCompare     = "btn_compare"
	MsgBtnOpenPR      = "btn_open_pr"
	MsgBtnOpenMR      = "btn_open_mr"
	MsgBtnFiles       = "btn_files"
	MsgBtnOpenReview  = "btn_open_review"
	MsgBtnOpenIssue   = "btn_open_issue"
	MsgBtnOpenComment = "btn_open_comment"
	MsgBtnOpenRelease = "btn_open_release"
	MsgBtnOpenRun     = "btn_open_run"
	MsgBtnOpenRepo    = "btn_open_repo"
	MsgBtnOpenFork    = "btn_open_fork"
	MsgBtnOpenTag     = "btn_open_tag"
	MsgBtnStargazers  = "btn_stargazers"

	// Dispatcher
	MsgDispatcherInit  = "dispatcher_init"
	MsgUnknownEventLog = "unknown_event_log"
	MsgNoHandlerLog    = "no_handler_log"
	MsgErrorEventLog   = "error_event_log"
	MsgUnknownEvent    = "unknown_event"
	MsgErrorEvent      = "error_event"

	// Server
	MsgServerStarted       = "server_started"
	MsgServerFailed        = "server_failed"
	MsgShuttingDown        = "shutting_down"
	MsgPanelStarted        = "panel_started"
	MsgPanelFailed         = "panel_failed"
	MsgWebhookUnauthorized = "webhook_unauthorized"
	MsgWebhookDuplicate    = "webhook_duplicate"
	MsgDeliveryLogFailed   = "delivery_log_failed"
	MsgInvalidRetention    = "invalid_retention"
	MsgMetricsStarted      = "metrics_started"
	MsgMetricsFailed       = "metrics_failed"
	MsgMethodNotAllowed    = "method_not_allowed"
	MsgFailedToReadBody    = "failed_to_read_body"
	MsgBadRequest          = "bad_request"
	MsgUnknownPlatform     = "unknown_platform"
	MsgEnvFileNotFound     = "env_file_not_found"
	MsgBotTokenIsNotSet    = "bot_token_is_not_set"
	MsgBotChatIdIsNotSet   = "bot_chat_id_is_not_set"
	MsgInvalidBotChatId    = "invalid_bot_chat_id"
	MsgFailedToCreateBot   = "failed_to_create_bot"
	MsgFailedToGetBotInfo  = "failed_to_get_bot_info"
	MsgInvalidProxyUrl     = "invalid_proxy_url"
)

var (
	messages map[Lang]map[string]string
	mu       sync.RWMutex
	loaded   bool
)

// Supported lists all available languages.
var Supported = []Lang{EN, RU}

// Load reads translations from JSON files in a directory on disk.
func Load(dir string) error {
	return LoadFS(os.DirFS(dir))
}

// LoadFS reads translations (<lang>.json) from a file system,
// e.g. locales embedded into the binary.
func LoadFS(fsys fs.FS) error {
	loadedMessages := make(map[Lang]map[string]string, len(Supported))

	for _, lang := range Supported {
		data, err := fs.ReadFile(fsys, string(lang)+".json")
		if err != nil {
			return fmt.Errorf("i18n: %w", err)
		}
		var msgs map[string]string
		if err := json.Unmarshal(data, &msgs); err != nil {
			return fmt.Errorf("i18n: %s.json: %w", lang, err)
		}
		loadedMessages[lang] = msgs
	}

	mu.Lock()
	defer mu.Unlock()
	messages = loadedMessages
	loaded = true
	return nil
}

// Keys returns all translation keys defined for a language.
func Keys(lang Lang) []string {
	mu.RLock()
	defer mu.RUnlock()

	keys := make([]string, 0, len(messages[lang]))
	for k := range messages[lang] {
		keys = append(keys, k)
	}
	return keys
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
	return slices.Contains(Supported, lang)
}
