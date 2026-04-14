// Package webhook contains Gitea webhook payload models used by the service.
package webhook

import "encoding/json"

// EventPullRequest is the value of X-Gitea-Event / X-Gogs-Event for pull request hooks.
const EventPullRequest = "pull_request"

// PullRequestActions that imply the PR commit set may have changed (see README for edited/label-only events).
const (
	ActionOpened      = "opened"
	ActionReopened    = "reopened"
	ActionSynchronize = "synchronize"
)

// PullRequestPayload is a subset of the Gitea pull_request webhook JSON.
type PullRequestPayload struct {
	Action      string          `json:"action"`
	Number      int             `json:"number"`
	PullRequest PullRequestInfo `json:"pull_request"`
	Repository  RepositoryInfo  `json:"repository"`
	Sender      *UserInfo       `json:"sender"`
}

// PullRequestInfo holds fields needed to fetch commits and render templates.
type PullRequestInfo struct {
	Index int            `json:"index"`
	Title string         `json:"title"`
	Head  PullRequestRef `json:"head"`
	Base  PullRequestRef `json:"base"`
}

// PullRequestRef identifies a branch tip in a webhook.
type PullRequestRef struct {
	Ref string `json:"ref"`
	Sha string `json:"sha"`
}

// RepositoryInfo identifies the repository (full_name preferred).
type RepositoryInfo struct {
	FullName string    `json:"full_name"`
	Name     string    `json:"name"`
	Owner    OwnerInfo `json:"owner"`
}

// OwnerInfo is the repository owner block in webhook JSON.
type OwnerInfo struct {
	Login string `json:"login"`
}

// UserInfo is the sender account in webhook JSON.
type UserInfo struct {
	Login string `json:"login"`
}

// ParsePullRequest unmarshals body as PullRequestPayload.
func ParsePullRequest(body []byte) (*PullRequestPayload, error) {
	var p PullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// OwnerRepo returns "owner/name" using repository.full_name when set, else owner.login + "/" + name.
func (p *PullRequestPayload) OwnerRepo() string {
	if p.Repository.FullName != "" {
		return p.Repository.FullName
	}
	o := p.Repository.Owner.Login
	if o == "" && p.Repository.Name != "" {
		return p.Repository.Name
	}
	if o != "" && p.Repository.Name != "" {
		return o + "/" + p.Repository.Name
	}
	return ""
}

// PRIndex returns the PR number/index for API calls (Gitea uses index in URL; number is usually the same).
func (p *PullRequestPayload) PRIndex() int {
	if p.PullRequest.Index > 0 {
		return p.PullRequest.Index
	}
	if p.Number > 0 {
		return p.Number
	}
	return 0
}
