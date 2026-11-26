// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// commentRouter dispatches GitHub installation-token, list-comments,
// create-comment, and update-comment paths used by PreviewCommentor.
type commentRouter struct {
	tokenIssued     atomic.Int64
	listCalls       atomic.Int64
	createCalls     atomic.Int64
	updateCalls     atomic.Int64
	existingComment string // when non-empty, list returns one comment containing this body
	updateBody      string
	createBody      string
	listStatus      int // 0 → 200
	createStatus    int // 0 → 201
	updateStatus    int // 0 → 200
}

func (r *commentRouter) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.Contains(req.URL.Path, "/access_tokens"):
			r.tokenIssued.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"2099-01-01T00:00:00Z"}`))

		case strings.Contains(req.URL.Path, "/issues/comments/"):
			// Update existing comment.
			r.updateCalls.Add(1)
			body, _ := io.ReadAll(req.Body)
			_ = req.Body.Close()
			r.updateBody = string(body)
			status := r.updateStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{}`))

		case strings.HasSuffix(req.URL.Path, "/comments"):
			switch req.Method {
			case http.MethodGet:
				r.listCalls.Add(1)
				status := r.listStatus
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				if status != http.StatusOK {
					_, _ = w.Write([]byte(`{"message":"forbidden"}`))
					return
				}
				if r.existingComment != "" {
					payload := []map[string]any{{
						"id":   int64(987654321),
						"body": r.existingComment,
					}}
					_ = json.NewEncoder(w).Encode(payload)
				} else {
					_, _ = w.Write([]byte(`[]`))
				}
			case http.MethodPost:
				r.createCalls.Add(1)
				body, _ := io.ReadAll(req.Body)
				_ = req.Body.Close()
				r.createBody = string(body)
				status := r.createStatus
				if status == 0 {
					status = http.StatusCreated
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{}`))
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found: ` + req.URL.Path + `"}`))
		}
	}
}

func newCommentTestApp(t *testing.T) (*AppClient, *commentRouter, func()) {
	t.Helper()
	router := &commentRouter{}
	srv := httptest.NewServer(router.handler())
	app := newTestAppClient(t, srv)
	app.httpClient = redirectingClient(srv.URL)
	return app, router, srv.Close
}

func newCommentParams() PreviewCommentParams {
	return PreviewCommentParams{
		InstallationID: 9999,
		OwnerRepo:      "acme/api",
		PRNumber:       42,
		ProjectID:      "api",
		URL:            "https://pr42-api.preview.loopruntime.app",
		CommitSHA:      "deadbeef1234",
		Status:         "Ready",
	}
}

// =============================================================================
// Marker rendering
// =============================================================================

func TestRenderPreviewComment_IncludesMarker(t *testing.T) {
	body := renderPreviewComment(newCommentParams())
	expectedMarker := previewCommentMarker("api", 42)
	if !strings.HasPrefix(body, expectedMarker) {
		t.Errorf("body should start with marker %q, got: %q", expectedMarker, body[:min(len(body), 80)])
	}
}

func TestRenderPreviewComment_ContainsURL(t *testing.T) {
	body := renderPreviewComment(newCommentParams())
	if !strings.Contains(body, "https://pr42-api.preview.loopruntime.app") {
		t.Error("comment must contain preview URL")
	}
}

func TestRenderPreviewComment_ShortensCommitSHA(t *testing.T) {
	body := renderPreviewComment(newCommentParams())
	if !strings.Contains(body, "deadbee") {
		t.Errorf("expected commit short SHA in body, got: %s", body)
	}
}

func TestRenderPreviewComment_NoInternalTerms(t *testing.T) {
	// Rule 3 — preview comments are user-facing.
	body := renderPreviewComment(newCommentParams())
	for _, term := range []string{"ArgoCD", "argocd", "Gitea", "MinIO", "Kaniko", "tenant_id", "advisory lock", "kubebuilder"} {
		if strings.Contains(body, term) {
			t.Errorf("Rule 3: comment contains internal term %q", term)
		}
	}
}

func TestPreviewCommentMarker_DistinctPerProject(t *testing.T) {
	// Two projects on the same PR must NOT collide.
	a := previewCommentMarker("api", 42)
	b := previewCommentMarker("web", 42)
	if a == b {
		t.Errorf("same marker for distinct projects: %q == %q", a, b)
	}
}

// =============================================================================
// Upsert flow
// =============================================================================

// extractCommentBody decodes the JSON body and returns the "body" field.
// The transport encodes "<" as "<", so we need to decode before
// substring-checking the marker text.
func extractCommentBody(t *testing.T, raw string) string {
	t.Helper()
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return payload.Body
}

func TestUpsertPreviewComment_FirstPost_CreatesNew(t *testing.T) {
	app, router, cleanup := newCommentTestApp(t)
	defer cleanup()
	c := NewPreviewCommentor(app, nil)

	if err := c.UpsertPreviewComment(context.Background(), newCommentParams()); err != nil {
		t.Fatalf("UpsertPreviewComment: %v", err)
	}
	if router.createCalls.Load() != 1 {
		t.Errorf("expected 1 create call, got %d", router.createCalls.Load())
	}
	if router.updateCalls.Load() != 0 {
		t.Errorf("expected 0 update calls, got %d", router.updateCalls.Load())
	}
	// Body must include the marker.
	body := extractCommentBody(t, router.createBody)
	if !strings.Contains(body, previewCommentMarker("api", 42)) {
		t.Errorf("create body missing marker: %s", body)
	}
}

func TestUpsertPreviewComment_ExistingComment_Updates(t *testing.T) {
	app, router, cleanup := newCommentTestApp(t)
	defer cleanup()
	router.existingComment = previewCommentMarker("api", 42) + "\nold body"
	c := NewPreviewCommentor(app, nil)

	if err := c.UpsertPreviewComment(context.Background(), newCommentParams()); err != nil {
		t.Fatalf("UpsertPreviewComment: %v", err)
	}
	if router.updateCalls.Load() != 1 {
		t.Errorf("expected 1 update call, got %d", router.updateCalls.Load())
	}
	if router.createCalls.Load() != 0 {
		t.Errorf("expected 0 create calls, got %d", router.createCalls.Load())
	}
	// Updated body must still include the marker.
	body := extractCommentBody(t, router.updateBody)
	if !strings.Contains(body, previewCommentMarker("api", 42)) {
		t.Errorf("update body missing marker: %s", body)
	}
}

func TestUpsertPreviewComment_DistinctProjectMarker_DoesNotMatch(t *testing.T) {
	app, router, cleanup := newCommentTestApp(t)
	defer cleanup()
	// Existing comment is for project "web" — should not match project "api".
	router.existingComment = previewCommentMarker("web", 42) + "\nweb's body"
	c := NewPreviewCommentor(app, nil)

	if err := c.UpsertPreviewComment(context.Background(), newCommentParams()); err != nil {
		t.Fatalf("UpsertPreviewComment: %v", err)
	}
	if router.createCalls.Load() != 1 {
		t.Errorf("expected 1 create (distinct marker, different project), got %d", router.createCalls.Load())
	}
}

func TestUpsertPreviewComment_ListFails_FallsBackToCreate(t *testing.T) {
	app, router, cleanup := newCommentTestApp(t)
	defer cleanup()
	router.listStatus = http.StatusForbidden
	c := NewPreviewCommentor(app, nil)

	// Should not fail — falls back to create when list fails (graceful).
	if err := c.UpsertPreviewComment(context.Background(), newCommentParams()); err != nil {
		t.Fatalf("UpsertPreviewComment: %v", err)
	}
	if router.createCalls.Load() != 1 {
		t.Errorf("expected fallback create, got %d", router.createCalls.Load())
	}
}

func TestUpsertPreviewComment_CreateFails_ReturnsError(t *testing.T) {
	app, router, cleanup := newCommentTestApp(t)
	defer cleanup()
	router.createStatus = http.StatusUnprocessableEntity
	c := NewPreviewCommentor(app, nil)

	err := c.UpsertPreviewComment(context.Background(), newCommentParams())
	if err == nil {
		t.Fatal("expected error when create fails")
	}
}

// =============================================================================
// Param validation
// =============================================================================

func TestUpsertPreviewComment_RejectsMissingRequiredParams(t *testing.T) {
	app, _, cleanup := newCommentTestApp(t)
	defer cleanup()
	c := NewPreviewCommentor(app, nil)

	cases := []struct {
		name string
		mod  func(*PreviewCommentParams)
	}{
		{"installation 0", func(p *PreviewCommentParams) { p.InstallationID = 0 }},
		{"empty owner/repo", func(p *PreviewCommentParams) { p.OwnerRepo = "" }},
		{"PR number 0", func(p *PreviewCommentParams) { p.PRNumber = 0 }},
		{"PR number negative", func(p *PreviewCommentParams) { p.PRNumber = -1 }},
		{"malformed owner/repo", func(p *PreviewCommentParams) { p.OwnerRepo = "no-slash" }},
		{"trailing slash", func(p *PreviewCommentParams) { p.OwnerRepo = "owner/" }},
	}
	for _, tc := range cases {
		// Go 1.22+: per-iteration loop variable; no shadow needed.
		t.Run(tc.name, func(t *testing.T) {
			p := newCommentParams()
			tc.mod(&p)
			if err := c.UpsertPreviewComment(context.Background(), p); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUpsertPreviewComment_NoAppClient_Errors(t *testing.T) {
	c := NewPreviewCommentor(nil, nil)
	if err := c.UpsertPreviewComment(context.Background(), newCommentParams()); err == nil {
		t.Fatal("expected error when AppClient is nil")
	}
}

// =============================================================================
// splitOwnerRepo
// =============================================================================

func TestSplitOwnerRepo(t *testing.T) {
	cases := []struct {
		input     string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{"acme/api", "acme", "api", true},
		{"some-org/some-repo", "some-org", "some-repo", true},
		{"with-dot/repo.io", "with-dot", "repo.io", true},
		{"no-slash", "", "", false},
		{"", "", "", false},
		{"/leading-slash", "", "", false},
		{"trailing-slash/", "", "", false},
	}
	for _, tc := range cases {
		o, r, ok := splitOwnerRepo(tc.input)
		if ok != tc.wantOK || o != tc.wantOwner || r != tc.wantRepo {
			t.Errorf("splitOwnerRepo(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.input, o, r, ok, tc.wantOwner, tc.wantRepo, tc.wantOK)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
