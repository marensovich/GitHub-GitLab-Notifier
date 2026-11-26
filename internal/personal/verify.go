package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"GitHub-GitLab-Notifier/storage"
)

// ErrBadToken: the platform refused the token (401 or 403).
var ErrBadToken = errors.New("the token was refused")

// Verifier asks GitHub or GitLab whose token it is.
type Verifier struct {
	HTTP *http.Client
	// BaseURL returns the API root for a platform and host; nil means
	// DefaultBaseURL.
	BaseURL func(platform, host string) string
}

// DefaultBaseURL is the API root of github.com, GitHub Enterprise
// (/api/v3) and GitLab (/api/v4).
func DefaultBaseURL(platform, host string) string {
	if platform == "github" {
		if host == "github.com" {
			return "https://api.github.com"
		}
		return "https://" + host + "/api/v3"
	}
	return "https://" + host + "/api/v4"
}

// WhoAmI returns the account the token belongs to. The token is sent in
// this one request and nowhere else: errors name the host and the HTTP
// status only, never the token or the answer's body.
func (v Verifier) WhoAmI(ctx context.Context, platform, host, token string) (storage.Account, error) {
	base := v.BaseURL
	if base == nil {
		base = DefaultBaseURL
	}
	client := v.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base(platform, host)+"/user", nil)
	if err != nil {
		return storage.Account{}, fmt.Errorf("%s: bad host", host)
	}
	if platform == "github" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
	} else {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		// The error repeats the URL and may carry more; keep the host.
		return storage.Account{}, fmt.Errorf("%s: unreachable", host)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return storage.Account{}, ErrBadToken
	case resp.StatusCode != http.StatusOK:
		return storage.Account{}, fmt.Errorf("%s answered %d", host, resp.StatusCode)
	}
	var u struct {
		ID       id     `json:"id"`
		Login    string `json:"login"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&u); err != nil {
		return storage.Account{}, fmt.Errorf("%s answered %d with no account", host, resp.StatusCode)
	}
	login := u.Login
	if platform != "github" {
		login = u.Username
	}
	if u.ID == "" || login == "" {
		return storage.Account{}, fmt.Errorf("%s answered %d with no account", host, resp.StatusCode)
	}
	return storage.Account{Platform: platform, Host: host, AccountID: string(u.ID), Login: login}, nil
}
