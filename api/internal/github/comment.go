// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// PreviewCommentParams describes the comment to upsert.
type PreviewCommentParams struct {
	InstallationID int64
	OwnerRepo      string // "owner/repo" — full repository name from the PR event
	PRNumber       int
	ProjectID      string
	URL            string // Preview URL (https://...)
	CommitSHA      string
	Status         string // human-readable status word ("Ready", "Building", "Failed")
}

// PreviewCommentor implements PreviewCommentClient against the GitHub
// REST API using an installation token from AppClient.
type PreviewCommentor struct {
	app    *AppClient
	logger *slog.Logger
}

// NewPreviewCommentor constructs a PreviewCommentor.
func NewPreviewCommentor(app *AppClient, logger *slog.Logger) *PreviewCommentor {
	if logger == nil {
		logger = slog.Default()
	}
	return &PreviewCommentor{app: app, logger: logger}
}

// previewCommentMarker is embedded in every loopruntime-managed comment as the
// first line. The (project, pr_number) tuple lets multiple LoopRuntime projects
// (monorepos) coexist on the same PR without colliding.
func previewCommentMarker(projectID string, prNumber int) string {
	return fmt.Sprintf("<!-- loopruntime-preview:%s:%d -->", projectID, prNumber)
}

// renderPreviewComment composes the markdown body of the comment.
func renderPreviewComment(params PreviewCommentParams) string {
	statusEmoji := "✅"
	switch strings.ToLower(params.Status) {
	case "building", "creating":
		statusEmoji = "⏳"
	case "failed", "cleanupfailed", "cleanup_failed":
		statusEmoji = "❌"
	case "expiring", "deleting":
		statusEmoji = "🧹"
	}

	short := params.CommitSHA
	if len(short) > 7 {
		short = short[:7]
	}

	urlLine := "_Provisioning…_"
	if params.URL != "" {
		urlLine = params.URL
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)

	return fmt.Sprintf(`%s
### Preview Environment

| | |
|---|---|
| **Status** | %s %s |
| **URL** | %s |
| **Commit** | `+"`%s`"+` |
| **Updated** | %s |

---
*Deployed by [LoopRuntime](https://loopruntime.com)*
`,
		previewCommentMarker(params.ProjectID, params.PRNumber),
		statusEmoji, params.Status,
		urlLine,
		short,
		timestamp,
	)
}

// UpsertPreviewComment creates or updates the loopruntime-managed comment on the
// PR. Idempotent across pushes for the same (project, PR).
func (c *PreviewCommentor) UpsertPreviewComment(ctx context.Context, params PreviewCommentParams) error {
	if c.app == nil {
		return fmt.Errorf("github comment: AppClient not configured")
	}
	if params.InstallationID == 0 || params.OwnerRepo == "" || params.PRNumber <= 0 {
		return fmt.Errorf("github comment: missing required params (installation, owner/repo, pr_number)")
	}
	owner, repo, ok := splitOwnerRepo(params.OwnerRepo)
	if !ok {
		return fmt.Errorf("github comment: invalid owner/repo %q", params.OwnerRepo)
	}

	body := renderPreviewComment(params)
	marker := previewCommentMarker(params.ProjectID, params.PRNumber)

	existing, err := c.findExistingComment(ctx, owner, repo, params.PRNumber, marker, params.InstallationID)
	if err != nil {
		// Fall through to create — never make the comment fatal to the
		// preview lifecycle (ADR D5: comment is nice-to-have).
		c.logger.Warn("github comment: list comments failed, will attempt create",
			"error", err, "owner", owner, "repo", repo, "pr", params.PRNumber)
		existing = 0
	}
	if existing > 0 {
		return c.updateComment(ctx, owner, repo, existing, body, params.InstallationID)
	}
	return c.createComment(ctx, owner, repo, params.PRNumber, body, params.InstallationID)
}

// findExistingComment scans recent PR comments for the project marker and
// returns its ID, or 0 if not found. Walks at most 3 pages (300 comments)
// — beyond that, posting a duplicate is acceptable.
func (c *PreviewCommentor) findExistingComment(
	ctx context.Context,
	owner, repo string,
	prNumber int,
	marker string,
	installationID int64,
) (int64, error) {
	const perPage = 100
	for page := 1; page <= 3; page++ {
		url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=%d&page=%d",
			c.app.apiBase(), owner, repo, prNumber, perPage, page)
		resp, err := c.app.doAuthenticated(ctx, installationID, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return 0, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("github comment: list comments status=%d body=%s", resp.StatusCode, string(body))
		}

		var comments []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		if err := json.Unmarshal(body, &comments); err != nil {
			return 0, fmt.Errorf("github comment: parse comments: %w", err)
		}
		for _, com := range comments {
			if strings.Contains(com.Body, marker) {
				return com.ID, nil
			}
		}
		if len(comments) < perPage {
			break
		}
	}
	return 0, nil
}

// createComment posts a fresh comment to the PR.
func (c *PreviewCommentor) createComment(
	ctx context.Context,
	owner, repo string,
	prNumber int,
	body string,
	installationID int64,
) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.app.apiBase(), owner, repo, prNumber)
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp, err := c.app.doAuthenticated(ctx, installationID, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("github comment: create: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github comment: create status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return nil
}

// updateComment replaces the body of an existing comment.
func (c *PreviewCommentor) updateComment(
	ctx context.Context,
	owner, repo string,
	commentID int64,
	body string,
	installationID int64,
) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d", c.app.apiBase(), owner, repo, commentID)
	payload, _ := json.Marshal(map[string]string{"body": body})
	resp, err := c.app.doAuthenticated(ctx, installationID, http.MethodPatch, url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("github comment: update: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github comment: update status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return nil
}

// splitOwnerRepo splits "owner/repo" into its parts. Returns ok=false on
// malformed input.
func splitOwnerRepo(fullName string) (owner, repo string, ok bool) {
	idx := strings.IndexByte(fullName, '/')
	if idx <= 0 || idx == len(fullName)-1 {
		return "", "", false
	}
	return fullName[:idx], fullName[idx+1:], true
}
