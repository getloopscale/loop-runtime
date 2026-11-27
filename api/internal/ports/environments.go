// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Environments domain types — moved from routes/environments.go per T1.3
// of deep-review-remediation-plan (closes find_arch_001 partial). The
// k8s/tenant_cr_manager.go used to import routes.EnvironmentEntry; now
// it imports these types from domain/ directly.

package ports

import "fmt"

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
	// The ready replicas the readiness poller last observed, and when. Absent
	// for an environment it never observed; a pointer, so that zero is answered.
	ReplicasReady   *int   `json:"replicas_ready,omitempty"`
	ReplicasReadyAt string `json:"replicas_ready_at,omitempty"` // RFC 3339, UTC
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

// ErrAmbiguousEnvironment is returned when a project-less environment lookup
// matches the same name in more than one project (issue #139).
//
// It lives in `domain` rather than beside the Kubernetes adapter that raises
// it because both sides need it: the adapter constructs it, and the HTTP layer
// maps it to a 400. Putting it in the adapter would force `routes` to import
// `k8s` — an interface layer depending on infrastructure, which is the
// dependency inversion `rules/architecture.md` forbids.
//
// It is deliberately an ERROR and not a "pick the first one". The caller asked
// for `production` without saying whose; two projects have one; answering with
// either returns a DIFFERENT project's environment under the requested name.
// That failure is invisible — plausible data for the wrong thing — whereas an
// error naming the candidates is something the caller can act on.
type ErrAmbiguousEnvironment struct {
	EnvName  string
	Projects []string
}

func (e *ErrAmbiguousEnvironment) Error() string {
	return fmt.Sprintf("environment %q exists in %d projects (%v); specify which one",
		e.EnvName, len(e.Projects), e.Projects)
}
