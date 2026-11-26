// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ports

// CreatePreviewRequest is the JSON payload accepted by the Create handler.
// Field names match the CLI's preview shape and the github.PullRequestEvent
// fields used by the webhook handler.
//
// tenantID is private and never read from the request body — it is injected
// by trusted callers (HTTP handler, GitHub webhook handler) from the
// authenticated context to prevent cross-tenant creations.
type CreatePreviewRequest struct {
	ProjectID      string `json:"project_id"`
	PRNumber       int    `json:"pr_number"`
	Branch         string `json:"branch"`
	CommitSHA      string `json:"commit_sha"`
	RepoFullName   string `json:"repo_full_name,omitempty"`
	InstallationID int64  `json:"installation_id,omitempty"`
	TTLHours       int    `json:"ttl_hours,omitempty"` // 0 = use default (72h)

	// LoopRuntimeYamlApps holds the raw `apps` map parsed from loopzero.yaml at the
	// PR commit. When set, the preview service uses it to populate
	// SubmitBuildRequest.Apps with framework/path/build (fields stripped
	// during BuildAppSpec persistence). Required for the build orchestrator
	// to resolve LanguageStrategy and emit language-aware step names
	// (validate-workspace-node, install-deps-node,
	// dockerfile-lint-user-dockerfile). LIVE catch 2026-06-07 J-23 bug #3.
	//
	// Empty map = no loopzero.yaml or parse failure — preview service falls
	// back to BuildAppsLookup (persisted apps_spec without framework/
	// path/build, works for trivial apps but fails for language-aware
	// build steps).
	//
	// Excluded from JSON (only set by trusted webhook callers).
	LoopRuntimeYamlApps map[string]map[string]interface{} `json:"-"`

	tenantID string // injected from middleware; never deserialized
}

// TenantID returns the resolved tenant ID injected by the trusted caller.
func (req *CreatePreviewRequest) TenantID() string { return req.tenantID }

// SetTenantID allows trusted callers (HTTP handler, webhook handler) to inject
// the resolved tenant before forwarding to the service.
func (req *CreatePreviewRequest) SetTenantID(t string) { req.tenantID = t }
