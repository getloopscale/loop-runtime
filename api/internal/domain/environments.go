// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Environments domain types — moved from routes/environments.go per T1.3
// of deep-review-remediation-plan (closes find_arch_001 partial). The
// k8s/tenant_cr_manager.go used to import routes.EnvironmentEntry; now
// it imports these types from domain/ directly.

package domain

// EnvironmentEntry is the summary representation of an environment.
type EnvironmentEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// M2 v1.2: runtime environments are per-(project, env). Legacy
	// rows produced before the M2 refactor carry empty Project for
	// backward compatibility (ADR D2 — production legacy). Omitempty
	// keeps the wire format backward-compatible for clients that do
	// not yet group by project.
	Project string `json:"project,omitempty"`
	// J-10 v1.1 LIVE catch 2026-06-07: KPI signals consumed by the
	// dashboard EnvironmentKPIRow. All optional (omitempty) — empty
	// means "no release ACTIVE yet"; dashboard renders honest "—".
	LastDeploy    string `json:"last_deploy,omitempty"`     // ISO-8601 timestamp of latest ACTIVE release in env
	LastDeploySha string `json:"last_deploy_sha,omitempty"` // git commit SHA (full)
}

// EnvironmentDetail is the detailed representation of an environment.
type EnvironmentDetail struct {
	Name      string           `json:"name"`
	Status    string           `json:"status"`
	CreatedAt string           `json:"created_at,omitempty"`
	Projects  []EnvProjectInfo `json:"projects,omitempty"`
}

// EnvProjectInfo describes a project deployed in an environment.
type EnvProjectInfo struct {
	Name          string `json:"name"`
	ActiveRelease string `json:"active_release,omitempty"`
	URL           string `json:"url,omitempty"`
}
