// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	sigsyaml "sigs.k8s.io/yaml"

	"github.com/getloopscale/loopzero/api/internal/db"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/id"
)

// PromotionReleaseStore abstracts release persistence for the promotion orchestrator (DIP).
type PromotionReleaseStore interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error)
	Create(ctx context.Context, tx db.DBTX, release *db.Release) error
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
	// SetGitCommit persists the gitops commit SHA so the SyncCallbackActivator
	// can match a sync notification to this release. Without it, the activator
	// drops the callback (revision guard).
	SetGitCommit(ctx context.Context, tx db.DBTX, id string, commitSHA string) error
	// GetByIDForTenant re-fetches a release by ID, scoped by tenant. Used by the
	// Sprint B T2.1 INSIDE-lock re-verify in Phase 5 (ADR D2). Mirrors the
	// reference pattern in rollback_orchestrator.go:208-225.
	GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*db.Release, error)
}

// PromotionEnvTargetStore abstracts environment target operations for promotion (DIP).
type PromotionEnvTargetStore interface {
	Get(ctx context.Context, tenantID, projectID, env string) (*db.EnvironmentTarget, error)
	SetDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error
}

// PromotionGitOpsPusher abstracts manifest push to gitops repo (DIP).
type PromotionGitOpsPusher interface {
	Push(ctx context.Context, opts GitOpsPushOpts) (string, error)
}

// PromotionGitOpsReader abstracts manifest retrieval from git (DIP).
type PromotionGitOpsReader interface {
	GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error)
}

// PromotionSyncClient abstracts ArgoCD sync operations (DIP).
type PromotionSyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
}

// PromotionOrchestratorDeps contains all dependencies for the promotion orchestrator.
type PromotionOrchestratorDeps struct {
	Releases   PromotionReleaseStore
	EnvTargets PromotionEnvTargetStore
	GitReader  PromotionGitOpsReader
	GitPusher  PromotionGitOpsPusher
	Sync       PromotionSyncClient
	Locker     EnvironmentLocker
	BaseDomain string
	Logger     *slog.Logger
}

// PromotionRequest contains the inputs for a promotion operation.
type PromotionRequest struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	FromEnv       string
	ToEnv         string
	CorrelationID string
	UserID        string
}

// PromotionResult contains the outcome of a promotion operation.
type PromotionResult struct {
	PromotionID    string
	SourceRelease  *db.Release
	NewRelease     *db.Release
	FromEnv        string
	ToEnv          string
	URL            string
}

// PromotionOrchestrator coordinates the release promotion workflow.
type PromotionOrchestrator struct {
	deps PromotionOrchestratorDeps
}

// NewPromotionOrchestrator creates a new PromotionOrchestrator.
func NewPromotionOrchestrator(deps PromotionOrchestratorDeps) *PromotionOrchestrator {
	return &PromotionOrchestrator{deps: deps}
}

// Promote executes the promotion workflow:
// 1. Validate from != to
// 2. Validate source environment has an ACTIVE release
// 3. Lock target environment
// 4. Fetch manifests from source release
// 5. Push manifests to target environment path in gitops
// 6. Create new release in target environment with PENDING status
// 7. Trigger ArgoCD sync (release activation happens via sync callback)
func (o *PromotionOrchestrator) Promote(ctx context.Context, req PromotionRequest) (*PromotionResult, error) {
	logger := o.deps.Logger.With(
		"tenant_id", req.TenantID,
		"project_id", req.ProjectID,
		"from_env", req.FromEnv,
		"to_env", req.ToEnv,
		"correlation_id", req.CorrelationID,
	)

	// Validate from != to
	if req.FromEnv == req.ToEnv {
		return nil, loopzeroerrors.ErrSameEnvironment
	}

	// Validate source environment has an ACTIVE release (outside lock — read-only)
	sourceRelease, err := o.deps.Releases.GetActiveByEnv(ctx, req.TenantID, req.ProjectID, req.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("promote: get active release from %s: %w", req.FromEnv, err)
	}
	if sourceRelease == nil {
		return nil, loopzeroerrors.ErrNoActiveRelease
	}

	if sourceRelease.GitCommit == "" {
		return nil, fmt.Errorf("promote: source release %s has no git commit", sourceRelease.ID)
	}

	// Fetch manifests from source release (outside lock — read-only on git)
	manifests, err := o.deps.GitReader.GetManifestsAtCommit(ctx, req.TenantID, req.ProjectID, req.FromEnv, sourceRelease.GitCommit)
	if err != nil {
		return nil, fmt.Errorf("promote: fetch manifests from %s: %w", req.FromEnv, err)
	}

	promotionID := "promo-" + id.GenerateReleaseID()
	var newRelease *db.Release

	// Lock target environment and create the new release
	err = o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.ToEnv, func(tx db.DBTX) error {
		// Create new release in target environment.
		// V9 L17 fix: create as READY (not PENDING). The post-push transition
		// at line ~211 dispatches GitMirrorCompleted which the release state
		// machine only allows from READY → PENDING. Creating as PENDING
		// directly here caused the post-push UpdateStatus to fail with
		// INVALID_TRANSITION (→ HTTP 409 → CLI mis-translates to
		// DEPLOY_IN_PROGRESS, hiding the real bug).
		// The "PENDING by default" assumption pre-dates the state machine
		// hardening; the canonical lifecycle is READY → (push) →
		// GitMirrorCompleted → PENDING → (sync) → SyncCompleted → ACTIVE.
		newRelease = &db.Release{
			ID:            "rel-" + id.GenerateReleaseID(),
			BuildID:       sourceRelease.BuildID,
			TenantID:      req.TenantID,
			ProjectID:     req.ProjectID,
			Environment:   req.ToEnv,
			Status:        "READY",
			CorrelationID: req.CorrelationID,
			CreatedBy:     req.UserID,
			Apps:          sourceRelease.Apps,
			Artifacts:     sourceRelease.Artifacts,
		}

		if err := o.deps.Releases.Create(ctx, tx, newRelease); err != nil {
			return fmt.Errorf("promote: create release in %s: %w", req.ToEnv, err)
		}

		// Set desired release pointer for target environment
		if err := o.deps.EnvTargets.SetDesired(ctx, tx, req.TenantID, req.ProjectID, req.ToEnv, newRelease.ID, req.UserID, req.CorrelationID); err != nil {
			return fmt.Errorf("promote: set desired release in %s: %w", req.ToEnv, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Rewrite environment-bound fields (namespace, hostnames, env labels) so
	// the manifests target ToEnv instead of FromEnv. Without this, ArgoCD
	// would create resources in the FromEnv namespace and the ToEnv App
	// would adopt-or-shadow the FromEnv workload — breaking traffic isolation
	// and producing wrong-host IngressRoutes.
	rewritten, rewriteErr := rewriteManifestsForEnv(string(manifests), req.TenantID, req.FromEnv, req.ToEnv, o.deps.BaseDomain)
	if rewriteErr != nil {
		return nil, fmt.Errorf("promote: rewrite manifests for %s: %w", req.ToEnv, rewriteErr)
	}

	// Push manifests to target environment path in gitops
	commitSHA, err := o.deps.GitPusher.Push(ctx, GitOpsPushOpts{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.ToEnv,
		Manifests:     rewritten,
		BuildID:       sourceRelease.BuildID,
		ReleaseID:     newRelease.ID,
		CorrelationID: req.CorrelationID,
	})
	if err != nil {
		return nil, fmt.Errorf("promote: push manifests to %s: %w", req.ToEnv, err)
	}

	logger.Info("promotion manifests pushed",
		"source_release", sourceRelease.ID,
		"new_release", newRelease.ID,
		"commit_sha", commitSHA,
	)

	// Persist the gitops commit and transition the release READY → PENDING.
	// Without this transition the convergence loop never activates the new
	// release (it scans only PENDING rows), so promote completes successfully
	// at the K8s level but the DB pointer never moves and downstream commands
	// (rollback, status, the next promote) see no ACTIVE row in the target.
	// Mirrors container_delivery.go phaseACreateRelease post-Push behavior.
	transitionErr := o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.ToEnv, func(tx db.DBTX) error {
		// Sprint B T2.1 (ADR D2): INSIDE-lock re-verify. Between Lock1 (where
		// newRelease was created in READY state per L17 fix) and Lock2, another
		// concurrent promote / rollback can have transitioned it (race window =
		// the gitops Push between locks). Mirror the rollback_orchestrator
		// Phase 5 pattern (lines 208-225): re-fetch the row, assert the
		// expected source state, abort deterministically with INVALID_TRANSITION
		// → HTTP 409 on mismatch.
		//
		// EC-4 (plan v1.1): target of the re-verify is newRelease (not
		// sourceRelease). The race we close is two promotes both creating
		// rows in the target env and both attempting GitMirrorCompleted on
		// THEIR OWN row — but only one Lock2 holder at a time can call
		// SetGitCommit. The canonical race signal is fresh.Status != READY
		// OR fresh.GitCommit non-empty (another Lock2 holder already
		// advanced the row).
		fresh, fetchErr := o.deps.Releases.GetByIDForTenant(ctx, req.TenantID, newRelease.ID)
		if fetchErr != nil {
			return fmt.Errorf("promote: re-fetch target release inside Lock2: %w", fetchErr)
		}
		if fresh == nil {
			return loopzeroerrors.NewInvalidTransition("READY", fmt.Sprintf(
				"CONCURRENT_PROMOTE: release %s deleted between Lock1 and Lock2", newRelease.ID))
		}
		if fresh.Status != "READY" {
			return loopzeroerrors.NewInvalidTransition(fresh.Status, fmt.Sprintf(
				"CONCURRENT_PROMOTE: release %s no longer READY (current: %s); another promote raced",
				newRelease.ID, fresh.Status))
		}
		if fresh.GitCommit != "" && commitSHA != "" && fresh.GitCommit != commitSHA {
			return loopzeroerrors.NewInvalidTransition("READY", fmt.Sprintf(
				"CONCURRENT_PROMOTE: release %s GitCommit already set to %s; another Lock2 holder advanced first",
				newRelease.ID, fresh.GitCommit))
		}

		if commitSHA != "" {
			if err := o.deps.Releases.SetGitCommit(ctx, tx, newRelease.ID, commitSHA); err != nil {
				return fmt.Errorf("set git commit: %w", err)
			}
		}
		// V9 L17: capture the updated release so callers (and tests)
		// observe the post-transition state (PENDING) rather than the
		// stale in-memory READY snapshot. Without this, the orchestrator
		// returned the release with its initial in-memory state, hiding
		// the actual DB state from downstream consumers.
		updated, err := o.deps.Releases.UpdateStatus(ctx, tx, newRelease.ID, string("GitMirrorCompleted"))
		if err != nil {
			return fmt.Errorf("transition release READY → PENDING: %w", err)
		}
		if updated != nil {
			newRelease = updated
		}
		return nil
	})
	if transitionErr != nil {
		return nil, fmt.Errorf("promote: post-push transition for %s: %w", req.ToEnv, transitionErr)
	}

	// Trigger ArgoCD sync — the sync callback will activate the release
	appName := fmt.Sprintf("%s-%s", req.TenantID, req.ToEnv)
	if err := o.deps.Sync.TriggerSync(ctx, appName); err != nil {
		logger.Error("promote: sync trigger failed — release will be activated by convergence loop",
			"app_name", appName,
			"error", err,
		)
		// Do not fail the promotion — the convergence loop will pick it up
	}

	url := ""
	if o.deps.BaseDomain != "" {
		url = fmt.Sprintf("https://%s-%s.%s", req.ProjectName, req.ToEnv, o.deps.BaseDomain)
	}

	logger.Info("promotion completed",
		"promotion_id", promotionID,
		"source_release", sourceRelease.ID,
		"new_release", newRelease.ID,
		"from_env", req.FromEnv,
		"to_env", req.ToEnv,
	)

	return &PromotionResult{
		PromotionID:   promotionID,
		SourceRelease: sourceRelease,
		NewRelease:    newRelease,
		FromEnv:       req.FromEnv,
		ToEnv:         req.ToEnv,
		URL:           url,
	}, nil
}

// rewriteManifestsForEnv transforms the env-specific fields embedded in
// rendered manifests so they target `toEnv` instead of `fromEnv`. The renderer
// (api/internal/manifests) bakes the environment into:
//   - metadata.namespace (`loopzero-{tenant}-{env}`)
//   - hostnames in IngressRoute spec.routes[].match (`Host(...)`) and
//     Certificate spec.dnsNames (`{tenant}-{project}-{app}-{env}.{baseDomain}`)
//   - the `loopzero.io/environment` label on every Deployment/Service/IngressRoute.
//
// We parse each YAML document, walk it recursively, and apply two rules:
//   1. any string equal to "loopzero-{tenant}-{fromEnv}" becomes
//      "loopzero-{tenant}-{toEnv}";
//   2. any string ending in "-{fromEnv}.{baseDomain}" has the env suffix
//      swapped to toEnv (only when baseDomain is non-empty);
//   3. any string equal to fromEnv when nested under a key named
//      "loopzero.io/environment" becomes toEnv.
//
// Match() route lines in IngressRoute use the full hostname, which rule 2
// covers. We deliberately do NOT do free-form regex over the YAML text so
// we don't accidentally rewrite an unrelated field that happens to contain
// the fromEnv substring.
func rewriteManifestsForEnv(manifests, tenantID, fromEnv, toEnv, baseDomain string) (string, error) {
	if fromEnv == "" || toEnv == "" || fromEnv == toEnv {
		return manifests, nil
	}
	docs := splitYAMLDocs(manifests)
	rewritten := make([]string, 0, len(docs))
	oldNamespace := fmt.Sprintf("loopzero-%s-%s", tenantID, fromEnv)
	newNamespace := fmt.Sprintf("loopzero-%s-%s", tenantID, toEnv)
	oldHostSuffix := ""
	newHostSuffix := ""
	if baseDomain != "" {
		oldHostSuffix = "-" + fromEnv + "." + baseDomain
		newHostSuffix = "-" + toEnv + "." + baseDomain
	}

	for _, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" || trimmed == "---" {
			continue
		}
		var node map[string]interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &node); err != nil || node == nil {
			// Doc isn't a YAML map (scalar/list/comment/empty) — leave verbatim.
			// We only rewrite K8s objects, which are always top-level maps.
			rewritten = append(rewritten, trimmed)
			continue
		}
		rewriteEnvFields(node, oldNamespace, newNamespace, oldHostSuffix, newHostSuffix, fromEnv, toEnv, false)
		rewriteHostInMatch(node, oldHostSuffix, newHostSuffix)
		out, err := sigsyaml.Marshal(node)
		if err != nil {
			return "", fmt.Errorf("marshal rewritten doc: %w", err)
		}
		rewritten = append(rewritten, strings.TrimSpace(string(out)))
	}
	return strings.Join(rewritten, "\n---\n"), nil
}

func splitYAMLDocs(manifests string) []string {
	parts := strings.Split(manifests, "\n---\n")
	if len(parts) == 1 {
		// Some renderings use leading "---"; handle leading separator too.
		parts = strings.Split(manifests, "---\n")
	}
	return parts
}

// rewriteEnvFields walks the YAML node and rewrites:
//   - exact namespace strings;
//   - hostname strings (-{fromEnv}.{baseDomain} suffix);
//   - values nested under "loopzero.io/environment" key (set inEnvLabel=true when descending).
func rewriteEnvFields(node interface{}, oldNs, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv string, inEnvLabel bool) {
	switch v := node.(type) {
	case map[string]interface{}:
		for k, child := range v {
			childInLabel := inEnvLabel || k == "loopzero.io/environment"
			if s, ok := child.(string); ok {
				v[k] = rewriteString(s, oldNs, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv, childInLabel)
				continue
			}
			rewriteEnvFields(child, oldNs, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv, childInLabel)
		}
	case []interface{}:
		for i, child := range v {
			if s, ok := child.(string); ok {
				v[i] = rewriteString(s, oldNs, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv, inEnvLabel)
				continue
			}
			rewriteEnvFields(child, oldNs, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv, inEnvLabel)
		}
	}
}

func rewriteString(s, oldNs, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv string, inEnvLabel bool) string {
	if s == oldNs {
		return newNs
	}
	if oldHostSuffix != "" && strings.HasSuffix(s, oldHostSuffix) {
		return strings.TrimSuffix(s, oldHostSuffix) + newHostSuffix
	}
	if inEnvLabel && s == fromEnv {
		return toEnv
	}
	return s
}

// rewriteHostInMatch handles the IngressRoute Match expression where the
// hostname is embedded in a Traefik rule string like:
//   Host(`tn-foo-app-staging.loopzero.local`)
// rewriteString cannot match this directly because the hostname is wrapped.
// We do a substring replace ONLY on -{fromEnv}.{baseDomain} suffix occurrences
// inside Match strings to keep the blast radius minimal.
func rewriteHostInMatch(node interface{}, oldHostSuffix, newHostSuffix string) {
	if oldHostSuffix == "" || oldHostSuffix == newHostSuffix {
		return
	}
	switch v := node.(type) {
	case map[string]interface{}:
		for k, child := range v {
			if s, ok := child.(string); ok && k == "match" {
				v[k] = strings.ReplaceAll(s, oldHostSuffix, newHostSuffix)
				continue
			}
			rewriteHostInMatch(child, oldHostSuffix, newHostSuffix)
		}
	case []interface{}:
		for _, child := range v {
			rewriteHostInMatch(child, oldHostSuffix, newHostSuffix)
		}
	}
}
