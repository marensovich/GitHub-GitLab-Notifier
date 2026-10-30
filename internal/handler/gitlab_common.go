package handler

import "net/url"

// GitLabProject is the project object shared by GitLab webhook payloads.
type GitLabProject struct {
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
}

// GitLabUser is the user object shared by GitLab webhook payloads.
type GitLabUser struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

// gitlabUserURL builds a profile URL on the same GitLab instance as the
// project, so self-hosted GitLab installations get correct links.
func gitlabUserURL(projectURL, username string) string {
	if username == "" {
		return ""
	}
	u, err := url.Parse(projectURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/" + url.PathEscape(username)
}
