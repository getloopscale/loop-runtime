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
