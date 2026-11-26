// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	sigsyaml "sigs.k8s.io/yaml"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/id"
	"github.com/getloopscale/loop-runtime/pkg/naming"
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
	Get(ctx context.Context, tenantID, projectID, env string) (*store.EnvironmentTarget, error)
	SetDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error
}

// PromotionGitOpsPusher abstracts manifest push to gitops repo (DIP).
type PromotionGitOpsPusher interface {
	Push(ctx context.Context, opts deploycontracts.GitOpsPushOpts) (string, error)
}

// PromotionGitOpsReader abstracts manifest retrieval from git (DIP).
type PromotionGitOpsReader interface {
	GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error)
}

// PromotionSyncClient abstracts ArgoCD sync operations (DIP).
type PromotionSyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
}

// AppExposure names one app of a release in one environment.
type AppExposure struct {
	TenantID    string
	Project     string
	Environment string
	AppName     string
	ServicePort int32
}

// PromotionAppExposer makes an app reachable in an environment: it ensures the
// application resource on the management cluster, from which the app operator
// keeps the route the edge reads. The deploy path does the same for the apps
// of a build (builds.ApplicationCREnsurer).
type PromotionAppExposer interface {
	ExposeApp(ctx context.Context, in AppExposure) error
}

// PromotionOrchestratorDeps contains all dependencies for the promotion orchestrator.
type PromotionOrchestratorDeps struct {
	// Apps exposes the promoted apps in the target environment. Nil skips it,
	// for installs without a management cluster client.
	Apps       PromotionAppExposer
	Releases   PromotionReleaseStore
	EnvTargets PromotionEnvTargetStore
	GitReader  PromotionGitOpsReader
	GitPusher  PromotionGitOpsPusher
	Sync       PromotionSyncClient
	Locker     deploycontracts.EnvironmentLocker
	BaseDomain string
	// PublicDomain is the canonical wildcard domain used to compose user-facing
	// Live URLs (e.g., "prod-dev.getloopscale.app"). When non-empty it takes
	// precedence over BaseDomain in the response URL — mirroring the post-#61
	// retrofit applied to ReleaseConvergenceLoop + SyncCallbackActivator
	// (setup_orchestrator.go:296-302). BaseDomain is the internal cluster
	// domain (e.g., "loopruntime.local") and was previously leaking into the promote
	// response — Red Team loopruntime-cloud#120 sub-finding 3 (regression of #61 on
	// the promote path which was missed during the original retrofit).
	PublicDomain string
	Logger       *slog.Logger
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
	PromotionID   string
	SourceRelease *db.Release
	NewRelease    *db.Release
	FromEnv       string
	ToEnv         string
	URL           string
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
		return nil, loopruntimeerrors.ErrSameEnvironment
	}

	// Validate source environment has an ACTIVE release (outside lock — read-only)
	sourceRelease, err := o.deps.Releases.GetActiveByEnv(ctx, req.TenantID, req.ProjectID, req.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("promote: get active release from %s: %w", req.FromEnv, err)
	}
	if sourceRelease == nil {
		return nil, loopruntimeerrors.ErrNoActiveRelease
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
			// 2026-06-30 (closes Red Team loopruntime-cloud#120 sub-finding 2):
			// id.GenerateReleaseID() ALREADY includes the canonical "rel-"
			// prefix per pkg/id/id.go:40 ("rel-{16hex}"). The manual prefix
			// concatenation here produced "rel-rel-{16hex}" — failing
			// id.IsValidReleaseID() and confusing downstream consumers.
			// PG releases.id has no CHECK constraint enforcing the format
			// (migrations/000001_initial_schema.up.sql:156), so the malformed
			// ID persisted silently — the bug was invisible at the DB layer.
			ID:            id.GenerateReleaseID(),
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
	rewritten, rewriteErr := rewriteManifestsForEnv(string(manifests), req.TenantID, req.ProjectID, req.FromEnv, req.ToEnv, o.deps.BaseDomain)
	if rewriteErr != nil {
		return nil, fmt.Errorf("promote: rewrite manifests for %s: %w", req.ToEnv, rewriteErr)
	}

	// Push manifests to target environment path in gitops
	commitSHA, err := o.deps.GitPusher.Push(ctx, deploycontracts.GitOpsPushOpts{
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
			return loopruntimeerrors.NewInvalidTransition("READY", fmt.Sprintf(
				"CONCURRENT_PROMOTE: release %s deleted between Lock1 and Lock2", newRelease.ID))
		}
		if fresh.Status != "READY" {
			return loopruntimeerrors.NewInvalidTransition(fresh.Status, fmt.Sprintf(
				"CONCURRENT_PROMOTE: release %s no longer READY (current: %s); another promote raced",
				newRelease.ID, fresh.Status))
		}
		if fresh.GitCommit != "" && commitSHA != "" && fresh.GitCommit != commitSHA {
			return loopruntimeerrors.NewInvalidTransition("READY", fmt.Sprintf(
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
	appName, nameErr := naming.ArgoAppName(req.TenantID, req.ProjectID, req.ToEnv)
	if nameErr != nil {
		return nil, fmt.Errorf("promote: argocd application name for %s: %w", req.ToEnv, nameErr)
	}
	if err := o.deps.Sync.TriggerSync(ctx, appName); err != nil {
		logger.Error("promote: sync trigger failed — release will be activated by convergence loop",
			"app_name", appName,
			"error", err,
		)
		// Do not fail the promotion — the convergence loop will pick it up
	}

	o.exposeApps(ctx, logger, req, newRelease)

	// 2026-06-30 (closes Red Team loopruntime-cloud#120 sub-finding 3 — #61 regression
	// on promote path): prefer PublicDomain over BaseDomain so the response URL
	// is the canonical user-facing Live URL ("prod-dev.getloopscale.app") not the
	// internal cluster placeholder ("loopruntime.local"). Fallback to BaseDomain
	// preserves back-compat for setups (local Tilt dev) where LOOPRUNTIME_PUBLIC_DOMAIN
	// is unset. Mirrors the canonical pattern at setup_orchestrator.go:296-302
	// already applied to ReleaseConvergenceLoop + SyncCallbackActivator.
	//
	// 2026-07-01 (closes Red Team loopruntime-cloud#159): switch from the simplified
	// legacy formula `{ProjectName}-{ToEnv}.{domain}` (which produced hostnames
	// like `loopzerokit-example-production.prod-dev.getloopscale.app` that Traefik 404'd
	// because they lacked the tenant + entryApp segments) to the FAANG canonical
	// naming.AppURL(tenantID, projectID, entryApp, envName, publicDomain) — the
	// SAME formula the IngressRoute renderer + status.go already use. EntryApp
	// is resolved from newRelease.Apps via the canonical
	// naming.ResolveEntryAppFromApps SoT (server > frontend non-static > empty).
	// When entryApp resolves empty (worker-only / static-only release) URL is
	// suppressed — fail-closed matches status.go's ADR D3 pattern (dashboard
	// then renders "URL pending"). Pre-fix: promote reported success + URL
	// that 404s; post-fix: promote emits the same URL that serves.
	url := entryAppURL(req.TenantID, req.ProjectName, req.ToEnv, newRelease.Apps,
		UserFacingDomain(o.deps.PublicDomain, o.deps.BaseDomain))

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
//   - metadata.namespace (canonical `loopruntime-{tenant}-{project}-{env}` since the
//     2026-06-07 multi-project refactor; legacy fixtures use `loopruntime-{tenant}-{env}`)
//   - hostnames in IngressRoute spec.routes[].match (`Host(...)`) and
//     Certificate spec.dnsNames (`{tenant}-{project}-{app}-{env}.{baseDomain}`)
//   - the `runtime.getloopscale.com/environment` label on every Deployment/Service/IngressRoute.
//
// We parse each YAML document, walk it recursively, and apply two rules:
//  1. any string equal to the canonical OR legacy namespace for fromEnv
//     becomes the corresponding canonical namespace for toEnv;
//  2. any string ending in "-{fromEnv}.{baseDomain}" has the env suffix
//     swapped to toEnv (only when baseDomain is non-empty);
//  3. any string equal to fromEnv when nested under a key named
//     "runtime.getloopscale.com/environment" becomes toEnv.
//
// Match() route lines in IngressRoute use the full hostname, which rule 2
// covers. We deliberately do NOT do free-form regex over the YAML text so
// we don't accidentally rewrite an unrelated field that happens to contain
// the fromEnv substring.
//
// Sponsor LIVE-caught 2026-06-18 (Staff-level Phase 2 troubleshooting):
// before this commit the rewriter only knew the legacy namespace format
// (no project segment). After the 2026-06-07 multi-project refactor the
// renderer started emitting `loopruntime-{tenant}-{project}-{env}` but the
// rewriter's exact-match string comparison was never updated to match.
// Result: rewrite was silently a no-op on real renderer output; manifests
// shipped to gitops with the FromEnv namespace verbatim; resources
// deployed into the preview namespace instead of production; ArgoCD
// reported "Synced + Healthy" because the manifest told it to do exactly
// what it did. Bug latent until first multi-project tenant promote in
// production. Fix is to use the canonical naming helper for the target
// namespace; the legacy format is still recognized as a fromEnv match
// so callers with pre-refactor fixtures keep working.
func rewriteManifestsForEnv(manifests, tenantID, projectID, fromEnv, toEnv, baseDomain string) (string, error) {
	if fromEnv == "" || toEnv == "" || fromEnv == toEnv {
		return manifests, nil
	}
	docs := splitYAMLDocs(manifests)
	rewritten := make([]string, 0, len(docs))
	// The namespace is the (tenant, project, env) triple. The two-segment fallback for an
	// empty projectID was deleted: zero namespaces in that shape exist in any cluster
	// (measured 2026-08-08), the promote route always fills ProjectID from the path param,
	// and the price of the fallback was rewriting manifests to a namespace that does not
	// exist — sending the deploy into the void with no error.
	newNamespace, err := naming.Namespace(tenantID, projectID, toEnv)
	if err != nil {
		return "", fmt.Errorf("promote: target namespace for %s/%s: %w", projectID, toEnv, err)
	}
	oldNamespaceCanonical, err := naming.Namespace(tenantID, projectID, fromEnv)
	if err != nil {
		return "", fmt.Errorf("promote: source namespace for %s/%s: %w", projectID, fromEnv, err)
	}
	oldNamespaces := []string{}
	if oldNamespaceCanonical != "" {
		oldNamespaces = append(oldNamespaces, oldNamespaceCanonical)
	}
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
		rewriteEnvFields(node, oldNamespaces, newNamespace, oldHostSuffix, newHostSuffix, fromEnv, toEnv, false)
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
//   - exact namespace strings (matching ANY of the oldNs candidates —
//     canonical "loopruntime-{t}-{p}-{e}" + legacy "loopruntime-{t}-{e}");
//   - hostname strings (-{fromEnv}.{baseDomain} suffix);
//   - values nested under "runtime.getloopscale.com/environment" key (set inEnvLabel=true when descending).
func rewriteEnvFields(node interface{}, oldNs []string, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv string, inEnvLabel bool) {
	switch v := node.(type) {
	case map[string]interface{}:
		for k, child := range v {
			childInLabel := inEnvLabel || k == "runtime.getloopscale.com/environment"
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

func rewriteString(s string, oldNs []string, newNs, oldHostSuffix, newHostSuffix, fromEnv, toEnv string, inEnvLabel bool) string {
	for _, candidate := range oldNs {
		if s == candidate {
			return newNs
		}
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
//
//	Host(`tn-foo-app-staging.loopruntime.local`)
//
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

// exposeApps makes every app of the promoted release reachable in the target
// environment. The manifests alone run the workload; the public route comes
// from the application resource a deploy ensures and a promote did not, so a
// promoted environment answered 404 on its own host (#124).
//
// A failure is logged and does not fail the promote: the release is delivered
// by then, and the next promote or deploy to the environment ensures it again.
func (o *PromotionOrchestrator) exposeApps(ctx context.Context, logger *slog.Logger, req PromotionRequest, rel *db.Release) {
	if o.deps.Apps == nil || rel == nil || rel.Apps == nil {
		return
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(rel.Apps, &apps); err != nil {
		logger.Error("promote: the release's app list could not be read, so its apps were not exposed in the target environment",
			"release_id", rel.ID, "to_env", req.ToEnv, "error", err)
		return
	}
	for _, app := range apps {
		port := int32(app.Port)
		if port == 0 {
			port = 80
		}
		in := AppExposure{TenantID: req.TenantID, Project: req.ProjectID, Environment: req.ToEnv, AppName: app.Name, ServicePort: port}
		if err := o.deps.Apps.ExposeApp(ctx, in); err != nil {
			logger.Error("promote: the app could not be exposed in the target environment; it runs without a public route",
				"release_id", rel.ID, "app", app.Name, "to_env", req.ToEnv, "error", err)
			continue
		}
		logger.Info("promote: app exposed in the target environment", "app", app.Name, "to_env", req.ToEnv)
	}
}
