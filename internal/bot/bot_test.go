package bot

import (
	"GitHub-GitLab-Notifier/internal/i18n"
	"slices"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		text    string
		command string
		args    []string
		ok      bool
	}{
		{"/start", "start", nil, true},
		{"/connect https://github.com/a/b", "connect", []string{"https://github.com/a/b"}, true},
		{"/Status@MyGitBot", "status", nil, true},
		{"/status@mygitbot", "status", nil, true},
		{"/status@OtherBot", "", nil, false},
		{"/lang   ru  ", "lang", []string{"ru"}, true},
		{"hello", "", nil, false},
	}
	for _, tt := range tests {
		command, args, ok := parseCommand(tt.text, "MyGitBot")
		if ok != tt.ok || command != tt.command || !slices.Equal(args, tt.args) {
			t.Errorf("parseCommand(%q) = %q %v %v, want %q %v %v",
				tt.text, command, args, ok, tt.command, tt.args, tt.ok)
		}
	}
}

func TestParseRepoURL(t *testing.T) {
	tests := []struct {
		in, url, platform string
		ok                bool
	}{
		{"https://github.com/user/repo", "https://github.com/user/repo", "GitHub", true},
		{"github.com/user/repo.git/", "https://github.com/user/repo", "GitHub", true},
		{"https://gitlab.com/group/sub/project", "https://gitlab.com/group/sub/project", "GitLab", true},
		{"https://git.company.local/team/app", "https://git.company.local/team/app", "GitLab", true},
		{"https://github.com/user", "", "", false},
		{"ftp://github.com/user/repo", "", "", false},
		{"not a url", "", "", false},
	}
	for _, tt := range tests {
		url, platform, ok := parseRepoURL(tt.in)
		if ok != tt.ok || url != tt.url || platform != tt.platform {
			t.Errorf("parseRepoURL(%q) = %q %q %v, want %q %q %v",
				tt.in, url, platform, ok, tt.url, tt.platform, tt.ok)
		}
	}
}

func TestWebhookURL(t *testing.T) {
	tests := []struct {
		domain, https, want string
		ok                  bool
	}{
		{"bot.example.com", "true", "https://bot.example.com/webhook", true},
		{"https://bot.example.com/", "", "https://bot.example.com/webhook", true},
		{"1.2.3.4:8080", "false", "http://1.2.3.4:8080/webhook", true},
		{"", "true", "", false},
	}
	for _, tt := range tests {
		t.Setenv("DOMAIN", tt.domain)
		t.Setenv("USE_HTTPS", tt.https)
		got, ok := webhookURL()
		if got != tt.want || ok != tt.ok {
			t.Errorf("webhookURL(DOMAIN=%q, USE_HTTPS=%q) = %q %v, want %q %v",
				tt.domain, tt.https, got, ok, tt.want, tt.ok)
		}
	}
}

func TestCommandsHaveDescriptions(t *testing.T) {
	if err := i18n.Load("../../locales"); err != nil {
		t.Fatal(err)
	}
	for _, lang := range i18n.Supported {
		for _, name := range commandNames {
			if d := i18n.T(lang, "cmd_"+name); d == "???" || d == "" {
				t.Errorf("%s: missing description for /%s (key cmd_%s)", lang, name, name)
			}
		}
	}
}
