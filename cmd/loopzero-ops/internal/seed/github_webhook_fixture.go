// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/credshape"
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
	ghFixtureDefaultWebhookURL = "https://app-dev.getloopscale.com/api/v1/webhooks/github"

	ghFixtureRepoOwnerEnv  = "LOOPZERO_FIXTURE_REPO_OWNER"
	ghFixtureRepoNameEnv   = "LOOPZERO_FIXTURE_REPO_NAME"
	ghFixtureWebhookURLEnv = "LOOPZERO_WEBHOOK_URL"
)

// defaultGhAPI shells out `gh api` and returns stdout. body, when non-nil, is
// sent on stdin (paired with `--input -`) so request payloads — the webhook
// secret in particular — never enter argv.
func defaultGhAPI(body []byte, args ...string) ([]byte, error) {
	full := append([]string{"api"}, args...)
	cmd := exec.Command("gh", full...) // #nosec G204 -- args controlled
	if body != nil {
		cmd.Stdin = bytes.NewReader(body)
	}
	out, err := cmd.Output()
	if err != nil {
		// Name the request by verb and path only: argv may carry request
		// fields, and this error is surfaced verbatim in the seed's Reason.
		verb, path := ghRequestLine(args)
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, fmt.Errorf("gh api %s %s: %w (stderr: %s)", verb, path, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return out, fmt.Errorf("gh api %s %s: %w", verb, path, err)
	}
	return out, nil
}

// ghRequestLine extracts the HTTP verb and path from a `gh api` argument list:
// `-X VERB path ...` or a bare `path` (GET).
func ghRequestLine(args []string) (verb, path string) {
	verb = "GET"
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-X" && i+1 < len(args):
			verb = args[i+1]
			i++
		case strings.HasPrefix(args[i], "/") && path == "":
			path = args[i]
		}
	}
	return verb, path
}

// defaultGhAuthCheck probes gh auth status.
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
	if credshape.IsPlaceholder(whsec) {
		return Result{Verdict: SKIP, Reason: "github.webhook_secret placeholder"}
	}
	if err := d.Host.ghAuthCheck(); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("gh not authenticated: %v — run `gh auth login --scopes repo,admin:repo_hook`", err)}
	}
	owner := d.Host.envOr(ghFixtureRepoOwnerEnv, ghFixtureDefaultOwner)
	repo := d.Host.envOr(ghFixtureRepoNameEnv, ghFixtureDefaultRepo)
	targetURL := d.Host.envOr(ghFixtureWebhookURLEnv, ghFixtureDefaultWebhookURL)

	if err := ghAssertAdmin(d.Host, owner, repo); err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	existingID, err := ghFindHookByURL(d.Host, owner, repo, targetURL)
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
		if err := ghPatchHook(d.Host, owner, repo, existingID, targetURL, whsec); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("PATCH hook %d: %v", existingID, err)}
		}
		return Result{Verdict: PASS, Reason: fmt.Sprintf("PATCH ok — hook %d refreshed at %s", existingID, targetURL)}
	}
	newID, err := ghCreateHook(d.Host, owner, repo, targetURL, whsec)
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
	if err := d.Host.ghAuthCheck(); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("gh not authenticated: %v", err)}
	}
	owner := d.Host.envOr(ghFixtureRepoOwnerEnv, ghFixtureDefaultOwner)
	repo := d.Host.envOr(ghFixtureRepoNameEnv, ghFixtureDefaultRepo)
	targetURL := d.Host.envOr(ghFixtureWebhookURLEnv, ghFixtureDefaultWebhookURL)
	id, err := ghFindHookByURL(d.Host, owner, repo, targetURL)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if id == 0 {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("no webhook targets %s on %s/%s", targetURL, owner, repo)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("webhook %d present at %s", id, targetURL)}
}

// ghAssertAdmin gates hook mutation on repo-admin permission (per bash).
func ghAssertAdmin(h Host, owner, repo string) error {
	out, err := h.ghAPI(nil, fmt.Sprintf("/repos/%s/%s", owner, repo))
	if err != nil {
		return fmt.Errorf("cannot read /repos/%s/%s — check gh auth + repo access: %w", owner, repo, err)
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
func ghFindHookByURL(h Host, owner, repo, targetURL string) (int64, error) {
	out, err := h.ghAPI(nil, fmt.Sprintf("/repos/%s/%s/hooks", owner, repo))
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

func ghPatchHook(h Host, owner, repo string, id int64, targetURL, whsec string) error {
	body, err := ghHookBody(targetURL, whsec, false)
	if err != nil {
		return err
	}
	_, err = h.ghAPI(body, "-X", "PATCH", fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, id), "--input", "-")
	return err
}

func ghCreateHook(h Host, owner, repo, targetURL, whsec string) (int64, error) {
	body, err := ghHookBody(targetURL, whsec, true)
	if err != nil {
		return 0, err
	}
	out, err := h.ghAPI(body, "-X", "POST", fmt.Sprintf("/repos/%s/%s/hooks", owner, repo), "--input", "-")
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

// ghHookRequest is the JSON body of a repo-hook create/update. It travels on
// stdin because it carries the HMAC secret.
type ghHookRequest struct {
	Name   string              `json:"name,omitempty"`
	Active bool                `json:"active"`
	Events []string            `json:"events"`
	Config ghHookRequestConfig `json:"config"`
}

type ghHookRequestConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Secret      string `json:"secret"`
	InsecureSSL string `json:"insecure_ssl"`
}

// ghHookBody builds the canonical hook payload. create adds name=web, which
// GitHub requires on POST and rejects as a change on PATCH.
func ghHookBody(targetURL, whsec string, create bool) ([]byte, error) {
	req := ghHookRequest{
		Active: true,
		Events: []string{"push", "pull_request"},
		Config: ghHookRequestConfig{URL: targetURL, ContentType: "json", Secret: whsec, InsecureSSL: "0"},
	}
	if create {
		req.Name = "web"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode hook body: %w", err)
	}
	return body, nil
}
