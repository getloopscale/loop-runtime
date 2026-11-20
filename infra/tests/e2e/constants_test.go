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
	// V9 dogfood 2026-05-30 final: reverted L9 (10min) back to 5min. With L10
	// (kubectl exec bypass for Prova de Vida, commit 2fc25283) most probes
	// converge in seconds, so the long timeout was unnecessary and crowded
	// out total spec count under the 90min suite ceiling. 5min budget covers
	// kubectl-exec convergence (~30s typical) with 10x headroom.
	httpProbeTimeout = 5 * time.Minute

	// probeTimeout is an alias for httpProbeTimeout used by deploy_e2e_test.go.
	probeTimeout = httpProbeTimeout

	// cleanupTimeout is the maximum time to wait for resource cleanup (Prova de Morte).
	// 2026-05-09: bumped 5min → 10min after pristine cleanroom journey suite
	// surfaced 5 false-fails ("Prova de Morte: resources still exist").
	// 2026-05-26: bumped 10min → 15min after dogfood-fix2 LIVE run showed
	// 3 Prova de Morte failures at exactly 600s. In warm-state runs (long
	// suite with rollout-bad-health from Journey 13 leaving ArgoCD app
	// Degraded + multiple project deletes contending for the same single
	// ArgoCD application per tenant), full prune convergence took >10min
	// (resources DID disappear eventually — verified post-suite). 15min
	// absorbs the slowest observed convergence with operator + ArgoCD HA
	// + serialized sync queue.
	cleanupTimeout = 15 * time.Minute

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
	// 2026-05-09: bumped 3min → 6min after Journey 06 rollback v1-active assertion
	// timed out in pristine cleanroom (release was created but convergence loop
	// hadn't fast-pathed it yet). 6min absorbs cleanroom controller cold-start.
	releaseActiveTimeout = 6 * time.Minute
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
// These match the values in infra/scripts/seed/seed.go.

const (
	// e2eTenantID is the seeded test tenant.
	e2eTenantID = "e2e-tenant"

	// e2eTraefikPort is the default local Traefik HTTPS port.
	e2eTraefikPort = "8443"

	// e2eTraefikPortCell2 is the Traefik port for cell-1.
	e2eTraefikPortCell2 = "8444"

	// e2eBaseDomainLocal is the local k3d base domain.
	e2eBaseDomainLocal = "loopzero.local"

	// e2eBaseDomainDrill is the drill DOKS base domain (CF wildcard A record).
	e2eBaseDomainDrill = "drill.getloopscale.com"

	// mgmtKubeContext is the kubectl context for the management cluster.
	mgmtKubeContext = "k3d-loopzero-core"

	// buildKubeContext is the kubectl context for the build cluster.
	buildKubeContext = "k3d-loopzero-build"

	// workloadKubeContext is the kubectl context for the workload cluster.
	workloadKubeContext = "k3d-loopzero-cell-0"
)
