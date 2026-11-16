package bot

import (
	"strconv"

	"GitHub-GitLab-Notifier/storage"
)

// legacyStore keeps the 1.x chat/topic/repo calls of this package working on
// top of the messenger-neutral storage until the commands move to the core
// and this package is removed.
type legacyStore struct{ *storage.Store }

// ChatConfig is a Telegram chat's stored settings.
type ChatConfig struct {
	ChatID    int64
	Language  string
	IsForum   bool
	Format    string
	UseTopics bool
}

// TopicInfo is a forum topic of a chat.
type TopicInfo struct {
	TopicID   int
	ChatID    int64
	Name      string
	EventType string
}

// RepoInfo is a repository subscribed by a chat.
type RepoInfo struct {
	ChatID   int64
	RepoURL  string
	Platform string
	Secret   string
}

const tg = "telegram"

func idText(id int64) string { return strconv.FormatInt(id, 10) }

func (s legacyStore) dest(chatID int64) (*storage.Destination, error) {
	d, err := s.GetDestination(tg, idText(chatID))
	if err == nil && d == nil {
		err = errNotFound
	}
	return d, err
}

type notFound struct{}

func (notFound) Error() string { return "chat not found" }

var errNotFound error = notFound{}

func (s legacyStore) GetChat(chatID int64) (*ChatConfig, error) {
	d, err := s.dest(chatID)
	if err != nil {
		return nil, err
	}
	return &ChatConfig{ChatID: chatID, Language: d.Language, Format: d.Format, UseTopics: d.UseThreads}, nil
}

func (s legacyStore) SaveChat(c ChatConfig) error {
	d, err := s.EnsureDestination(tg, idText(c.ChatID), storage.Destination{Language: c.Language, Format: storage.FormatRich, UseThreads: true})
	if err != nil {
		return err
	}
	if c.Language != "" && c.Language != d.Language {
		return s.SetDestinationLanguage(d.ID, c.Language)
	}
	return nil
}

func (s legacyStore) SetChatSettings(chatID int64, format string, useTopics bool) error {
	d, err := s.dest(chatID)
	if err != nil {
		return err
	}
	return s.SetDestinationSettings(d.ID, format, useTopics)
}

func (s legacyStore) SetChatLanguage(chatID int64, language string, _ bool) error {
	if err := s.SaveChat(ChatConfig{ChatID: chatID, Language: language}); err != nil {
		return err
	}
	return nil
}

func (s legacyStore) GetTopicsByChat(chatID int64) ([]TopicInfo, error) {
	d, err := s.dest(chatID)
	if err != nil {
		return nil, err
	}
	threads, err := s.Threads(d.ID)
	if err != nil {
		return nil, err
	}
	routes, err := s.Routes(d.ID)
	if err != nil {
		return nil, err
	}
	byThread := map[string]string{}
	for event, thread := range routes {
		byThread[thread] = event
	}
	var list []TopicInfo
	for _, t := range threads {
		id, _ := strconv.Atoi(t.ThreadID)
		list = append(list, TopicInfo{TopicID: id, ChatID: chatID, Name: t.Name, EventType: byThread[t.ThreadID]})
	}
	return list, nil
}

func (s legacyStore) SaveTopic(t TopicInfo) error {
	d, err := s.dest(t.ChatID)
	if err != nil {
		return err
	}
	return s.SaveThread(storage.Thread{DestinationID: d.ID, ThreadID: strconv.Itoa(t.TopicID), Name: t.Name}, t.EventType)
}

func (s legacyStore) SetEventTopic(chatID int64, eventType string, topicID int) error {
	d, err := s.dest(chatID)
	if err != nil {
		return err
	}
	threads, err := s.Threads(d.ID)
	if err != nil {
		return err
	}
	name := eventType
	for _, t := range threads {
		if t.ThreadID == strconv.Itoa(topicID) {
			name = t.Name
		}
	}
	return s.SaveThread(storage.Thread{DestinationID: d.ID, ThreadID: strconv.Itoa(topicID), Name: name}, eventType)
}

func (s legacyStore) FindRepo(url string) *RepoInfo {
	r, err := s.Store.FindRepo(url)
	if err != nil || r == nil {
		return nil
	}
	return &RepoInfo{RepoURL: r.URL, Platform: r.Platform, Secret: r.Secret}
}

// SaveRepo creates the repository when needed and subscribes the chat to it.
func (s legacyStore) SaveRepo(r RepoInfo) error {
	d, err := s.dest(r.ChatID)
	if err != nil {
		return err
	}
	repo, err := s.Store.FindRepo(r.RepoURL)
	if err != nil {
		return err
	}
	if repo == nil {
		created, err := s.CreateRepo(r.RepoURL, r.Platform, r.Secret)
		if err != nil {
			return err
		}
		repo = &created
	}
	return s.Subscribe(repo.ID, d.ID)
}

func (s legacyStore) GetReposByChat(chatID int64) ([]RepoInfo, error) {
	d, err := s.dest(chatID)
	if err != nil {
		return nil, nil
	}
	repos, err := s.SubscriptionsOf(d.ID)
	if err != nil {
		return nil, err
	}
	var list []RepoInfo
	for _, r := range repos {
		list = append(list, RepoInfo{ChatID: chatID, RepoURL: r.URL, Platform: r.Platform, Secret: r.Secret})
	}
	return list, nil
}

func (s legacyStore) DeleteRepo(chatID int64, url string) (bool, error) {
	d, err := s.dest(chatID)
	if err != nil {
		return false, nil
	}
	return s.Unsubscribe(d.ID, url)
}
