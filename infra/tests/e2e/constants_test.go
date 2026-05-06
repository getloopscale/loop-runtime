// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Shared constants for all E2E test suites.
//
// Centralizes timeouts, forbidden terms (Rule 3), and allowed exit codes (Rule 4).
// All test files in the e2e package should reference these constants instead of
// defining their own local copies.
package e2e

import "time"

// --- Timeouts (ADR D8) ---

const (
	// deployTimeout is the maximum time to wait for a full deploy cycle
	// (build + sign + gitops + sync + probe).
	deployTimeout = 25 * time.Minute

	// scaleTimeout is the maximum time to wait for a scale operation.
	scaleTimeout = 2 * time.Minute

	// promoteTimeout is the maximum time to wait for a promote operation.
	promoteTimeout = 5 * time.Minute

	// rollbackTimeout is the maximum time to wait for a rollback operation.
	rollbackTimeout = 3 * time.Minute

	// httpProbeTimeout is the maximum time to wait for an HTTP endpoint to respond.
	httpProbeTimeout = 5 * time.Minute

	// probeTimeout is an alias for httpProbeTimeout used by deploy_e2e_test.go.
	probeTimeout = httpProbeTimeout

	// cleanupTimeout is the maximum time to wait for resource cleanup (Prova de Morte).
	cleanupTimeout = 5 * time.Minute

	// cliCommandTimeout is the maximum time for a non-deploy CLI command.
	cliCommandTimeout = 30 * time.Second

	// buildPollInterval is how often to poll the build status API.
	buildPollInterval = 15 * time.Second

	// httpPollInterval is how often to poll HTTP endpoints.
	httpPollInterval = 5 * time.Second

	// cleanupPollInterval is how often to poll for resource deletion.
	cleanupPollInterval = 10 * time.Second

	// releasePollInterval is how often to poll release status.
	releasePollInterval = 5 * time.Second

	// releaseActiveTimeout is the maximum time to wait for a release to become ACTIVE.
	releaseActiveTimeout = 3 * time.Minute
)

// --- Forbidden Internal Terms (Rule 3, CLAUDE.md) ---
//
// These terms MUST NEVER appear in CLI output, error messages, or --json responses.
// This is the canonical list — all test files should use this instead of local copies.

var forbiddenInternalTerms = []string{
	"ArgoCD",
	"argocd",
	"GitOps",
	"gitops-push",
	"CRD",
	"tenant_id",
	"advisory lock",
	"pg_advisory",
	"Gitea",
	"MinIO",
	"kubebuilder",
	"envtest",
	"Kaniko",
}

// concurrencyForbiddenTerms extends forbiddenInternalTerms with stricter
// concurrency-specific forbidden words: "lock" and "advisory" should never
// leak in user-facing output of concurrent-deploy rejection paths.
//
// These two terms are intentionally NOT in forbiddenInternalTerms — many
// non-concurrency commands legitimately mention "lock" (e.g., login token).
// Only the concurrent-deploy assertions need this stricter list.
var concurrencyForbiddenTerms = func() []string {
	out := make([]string, 0, len(forbiddenInternalTerms)+2)
	out = append(out, forbiddenInternalTerms...)
	out = append(out, "lock", "advisory")
	return out
}()

// --- Allowed Exit Codes (Rule 4, CLAUDE.md) ---

var allowedExitCodesMap = map[int]string{
	0: "success",
	1: "usage error",
	2: "build failure",
	3: "deploy failure",
	4: "auth error",
	5: "partial failure",
}

// --- Build Terminal States ---

var buildTerminalStates = map[string]bool{
	"SUCCEEDED": true,
	"FAILED":    true,
	"CANCELLED": true,
	"TIMEOUT":   true,
}

// --- E2E Test Tenant Constants ---
//
// These match the values in infra/tests/seed/seed.go.

const (
	// e2eTenantID is the seeded test tenant.
	e2eTenantID = "e2e-tenant"

	// e2eTraefikPort is the default local Traefik HTTPS port.
	e2eTraefikPort = "8443"

	// e2eTraefikPortCell2 is the Traefik port for cell-2.
	e2eTraefikPortCell2 = "8444"

	// mgmtKubeContext is the kubectl context for the management cluster.
	mgmtKubeContext = "k3d-loopzero-mgmt"

	// buildKubeContext is the kubectl context for the build cluster.
	buildKubeContext = "k3d-loopzero-build"

	// workloadKubeContext is the kubectl context for the workload cluster.
	workloadKubeContext = "k3d-loopzero-workload"
)
