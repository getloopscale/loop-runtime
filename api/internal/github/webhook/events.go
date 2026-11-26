// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package webhook

import "strings"

// PushEvent represents a GitHub push webhook event.
type PushEvent struct {
	Ref          string     `json:"ref"`     // "refs/heads/main"
	Before       string     `json:"before"`  // previous commit SHA
	After        string     `json:"after"`   // new commit SHA (deploy target)
	Created      bool       `json:"created"` // true if branch was just created
	Deleted      bool       `json:"deleted"` // true if branch was deleted
	Forced       bool       `json:"forced"`  // true if force push
	Repository   Repository `json:"repository"`
	Sender       User       `json:"sender"`
	HeadCommit   *Commit    `json:"head_commit"`
	Commits      []Commit   `json:"commits"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// Branch returns the branch name from the ref (strips "refs/heads/").
func (e PushEvent) Branch() string {
	return strings.TrimPrefix(e.Ref, "refs/heads/")
}

// IsDefaultBranch returns true if the push is to the repo's default branch.
func (e PushEvent) IsDefaultBranch() bool {
	return e.Branch() == e.Repository.DefaultBranch
}

// IsTag returns true if the ref is a tag (not a branch).
func (e PushEvent) IsTag() bool {
	return strings.HasPrefix(e.Ref, "refs/tags/")
}

// PullRequestEvent represents a GitHub pull_request webhook event.
type PullRequestEvent struct {
	Action       string      `json:"action"` // opened, synchronize, closed, reopened, etc.
	Number       int         `json:"number"`
	PullRequest  PullRequest `json:"pull_request"`
	Repository   Repository  `json:"repository"`
	Sender       User        `json:"sender"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// InstallationEvent represents a GitHub installation webhook event.
type InstallationEvent struct {
	Action       string       `json:"action"` // created, deleted, suspend, unsuspend
	Installation Installation `json:"installation"`
	Repositories []Repository `json:"repositories"`
	Sender       User         `json:"sender"`
}

// --- Shared types ---

// Repository represents a GitHub repository in webhook payloads.
type Repository struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`      // "my-app"
	FullName      string `json:"full_name"` // "owner/my-app"
	Owner         User   `json:"owner"`
	DefaultBranch string `json:"default_branch"` // "main"
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
}

// User represents a GitHub user in webhook payloads.
type User struct {
	Login     string `json:"login"`
	ID        int64  `json:"id"`
	AvatarURL string `json:"avatar_url"`
	Type      string `json:"type"` // "User" or "Organization"
}

// Commit represents a git commit in webhook payloads.
type Commit struct {
	ID        string `json:"id"` // full SHA
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Author    struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Username string `json:"username"`
	} `json:"author"`
	URL      string   `json:"url"`
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}

// PullRequest represents a GitHub pull request in webhook payloads.
type PullRequest struct {
	ID       int64  `json:"id"`
	Number   int    `json:"number"`
	State    string `json:"state"` // "open", "closed"
	Title    string `json:"title"`
	Body     string `json:"body"`
	Merged   bool   `json:"merged"`
	MergedAt string `json:"merged_at"`
	HTMLURL  string `json:"html_url"`
	Head     struct {
		Ref string `json:"ref"` // branch name
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"` // target branch (e.g., "main")
	} `json:"base"`
	User User `json:"user"`
}

// Installation represents a GitHub App installation.
type Installation struct {
	ID      int64 `json:"id"`
	Account User  `json:"account"` // user or org that installed
	AppID   int64 `json:"app_id"`
}
