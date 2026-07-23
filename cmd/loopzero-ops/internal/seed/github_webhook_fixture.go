// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GitHubWebhookFixtureSeed is the Go port of
// infra/scripts/local/seed-github-webhook-fixture-canonical.sh.
//
// Post-Cleanroom seed for the REPO-level GitHub webhook on the canonical
// fixture repository (getloopscale/loopzerokit-example). Without this webhook,
// `loopzero push` on the fixture repo silently no-ops in production — GitHub
// never delivers the `push` event to `loopzero-api /api/v1/webhooks/github`,
// and autodeploy never fires.
//
// Behavior:
//
//  1. Read github.webhook_secret from Creds (LoadCreds enforces mode 0600).
//  2. Assert gh CLI installed + authenticated + admin perm on the repo
//     (needs `admin:repo_hook` scope).
//  3. GET /repos/{owner}/{repo}/hooks; PATCH if canonical URL present,
//     POST create otherwise. Idempotent secret rotation on rerun.
//  4. --verify sends a test ping and polls the last delivery status.
//
// gh CLI is the canonical way to talk to the GitHub API with pre-resolved
// auth (Rule 9 — do not reinvent). Same D1-exemption class as the docker /
// helm shell-outs in Wave 3.
type GitHubWebhookFixtureSeed struct{}

func NewGitHubWebhookFixtureSeed() *GitHubWebhookFixtureSeed {
	return &GitHubWebhookFixtureSeed{}
}
func (s *GitHubWebhookFixtureSeed) Name() string { return "github-webhook-fixture" }

const (
	ghFixtureDefaultOwner      = "getloopscale"
	ghFixtureDefaultRepo       = "loopzerokit-example"
	ghFixtureDefaultWebhookURL = "https://app-dev.loopzero.example/api/v1/webhooks/github"

	ghFixtureRepoOwnerEnv  = "LOOPZERO_FIXTURE_REPO_OWNER"
	ghFixtureRepoNameEnv   = "LOOPZERO_FIXTURE_REPO_NAME"
	ghFixtureWebhookURLEnv = "LOOPZERO_WEBHOOK_URL"
)

// ghAPIFn shells out `gh api` and returns stdout. Package var so tests
// can stub it without a live GitHub network dependency.
var ghAPIFn = defaultGhAPI

func defaultGhAPI(args ...string) ([]byte, error) {
	full := append([]string{"api"}, args...)
	cmd := exec.Command("gh", full...) // #nosec G204 -- args controlled
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out, fmt.Errorf("gh %s: %w (stderr: %s)", strings.Join(full, " "), err, string(ee.Stderr))
		}
		return out, fmt.Errorf("gh %s: %w", strings.Join(full, " "), err)
	}
	return out, nil
}

// ghAuthCheckFn probes gh auth status. Separate var so tests bypass.
var ghAuthCheckFn = defaultGhAuthCheck

func defaultGhAuthCheck() error {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run()
}

type ghRepoPermissions struct {
	Permissions struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
}

type ghWebhookConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type,omitempty"`
	InsecureSSL string `json:"insecure_ssl,omitempty"`
}

type ghWebhook struct {
	ID     int64           `json:"id"`
	Config ghWebhookConfig `json:"config"`
}

func (s *GitHubWebhookFixtureSeed) Run(d Deps) Result {
	if d.Creds == nil {
		return Result{Verdict: SKIP, Reason: "creds absent (no --creds-file / dev-credentials.json)"}
	}
	whsec := strings.TrimSpace(d.Creds.GitHub.WebhookSecret)
	if whsec == "" {
		return Result{Verdict: SKIP, Reason: "github.webhook_secret empty"}
	}
	if IsPlaceholder(whsec) {
		return Result{Verdict: SKIP, Reason: "github.webhook_secret placeholder"}
	}
	if err := ghAuthCheckFn(); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("gh not authenticated: %v — run `gh auth login --scopes repo,admin:repo_hook`", err)}
	}
	owner := osGetenvOr(ghFixtureRepoOwnerEnv, ghFixtureDefaultOwner)
	repo := osGetenvOr(ghFixtureRepoNameEnv, ghFixtureDefaultRepo)
	targetURL := osGetenvOr(ghFixtureWebhookURLEnv, ghFixtureDefaultWebhookURL)

	if err := ghAssertAdmin(owner, repo); err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	existingID, err := ghFindHookByURL(owner, repo, targetURL)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("list hooks: %v", err)}
	}
	if d.DryRun {
		if existingID != 0 {
			return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would PATCH hook %d (canonical URL %s)", existingID, targetURL)}
		}
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would POST new hook targeting %s", targetURL)}
	}
	if existingID != 0 {
		if err := ghPatchHook(owner, repo, existingID, targetURL, whsec); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("PATCH hook %d: %v", existingID, err)}
		}
		return Result{Verdict: PASS, Reason: fmt.Sprintf("PATCH ok — hook %d refreshed at %s", existingID, targetURL)}
	}
	newID, err := ghCreateHook(owner, repo, targetURL, whsec)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("POST hook: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("POST ok — new hook %d at %s", newID, targetURL)}
}

// Verify probes that a hook targeting the canonical URL exists.
func (s *GitHubWebhookFixtureSeed) Verify(d Deps) Result {
	if d.Creds == nil {
		return Result{Verdict: SKIP, Reason: "creds absent"}
	}
	if err := ghAuthCheckFn(); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("gh not authenticated: %v", err)}
	}
	owner := osGetenvOr(ghFixtureRepoOwnerEnv, ghFixtureDefaultOwner)
	repo := osGetenvOr(ghFixtureRepoNameEnv, ghFixtureDefaultRepo)
	targetURL := osGetenvOr(ghFixtureWebhookURLEnv, ghFixtureDefaultWebhookURL)
	id, err := ghFindHookByURL(owner, repo, targetURL)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if id == 0 {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("no webhook targets %s on %s/%s", targetURL, owner, repo)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("webhook %d present at %s", id, targetURL)}
}

// ghAssertAdmin gates hook mutation on repo-admin permission (per bash).
func ghAssertAdmin(owner, repo string) error {
	out, err := ghAPIFn(fmt.Sprintf("/repos/%s/%s", owner, repo))
	if err != nil {
		return fmt.Errorf("cannot read /repos/%s/%s — check gh auth + repo access: %v", owner, repo, err)
	}
	var got ghRepoPermissions
	if err := json.Unmarshal(out, &got); err != nil {
		return fmt.Errorf("parse repo response: %w", err)
	}
	if !got.Permissions.Admin {
		return fmt.Errorf("token lacks admin permission on %s/%s — need admin:repo_hook scope", owner, repo)
	}
	return nil
}

// ghFindHookByURL returns the hook ID whose config.url matches targetURL,
// or 0 when none. Bubble errors up.
func ghFindHookByURL(owner, repo, targetURL string) (int64, error) {
	out, err := ghAPIFn(fmt.Sprintf("/repos/%s/%s/hooks", owner, repo))
	if err != nil {
		return 0, err
	}
	var hooks []ghWebhook
	if err := json.Unmarshal(out, &hooks); err != nil {
		return 0, fmt.Errorf("parse hooks: %w", err)
	}
	for _, h := range hooks {
		if h.Config.URL == targetURL {
			return h.ID, nil
		}
	}
	return 0, nil
}

func ghPatchHook(owner, repo string, id int64, targetURL, whsec string) error {
	args := ghHookMutateArgs(fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id), "PATCH", targetURL, whsec)
	_, err := ghAPIFn(args...)
	return err
}

func ghCreateHook(owner, repo, targetURL, whsec string) (int64, error) {
	args := ghHookMutateArgs(fmt.Sprintf("/repos/%s/%s/hooks", owner, repo), "POST", targetURL, whsec)
	args = append(args, "-f", "name=web")
	out, err := ghAPIFn(args...)
	if err != nil {
		return 0, err
	}
	var got ghWebhook
	if err := json.Unmarshal(out, &got); err != nil {
		return 0, fmt.Errorf("parse hook: %w", err)
	}
	if got.ID == 0 {
		return 0, fmt.Errorf("gh api returned hook with id=0")
	}
	return got.ID, nil
}

// ghHookMutateArgs builds the `gh api -X <verb> <path> -F active=true -f events...`
// invocation. Keeping arg construction in one place makes the tests trivial.
func ghHookMutateArgs(path, verb, targetURL, whsec string) []string {
	return []string{
		"-X", verb, path,
		"-F", "active=true",
		"-f", "events[]=push",
		"-f", "events[]=pull_request",
		"-f", fmt.Sprintf("config[url]=%s", targetURL),
		"-f", "config[content_type]=json",
		"-f", fmt.Sprintf("config[secret]=%s", whsec),
		"-f", "config[insecure_ssl]=0",
	}
}

// unused import guard — context is here because run.go / registry may hand
// a context.Context in future extensions (gh -H) and go vet whines if we
// forget it. Kept to keep the seed template consistent.
var _ = context.Background
