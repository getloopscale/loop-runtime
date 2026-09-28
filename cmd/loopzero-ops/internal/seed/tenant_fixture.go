// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/credshape"
	"github.com/getloopscale/loopzero/pkg/domain"
)

// TenantFixtureSeed is the Go port of
// infra/scripts/local/seed-tenant-fixture-canonical.sh.
//
// Post-Cleanroom seed for tenant fixture state used by LIVE dogfood tests.
// Without this seed, dogfood-environments Scenario E (plan-limit 402) cannot
// run + dogfood-project-connect webhook tests fail.
//
// Layers:
//
//  1. Upsert canonical subscription (plan tier) for the test tenant so plan
//     limits don't trigger 402 during burst tests. --starter/--pro/--team
//     select tier (env override LOOPZERO_PLAN_TIER).
//
//  2. Replay canonical GitHub App `installation.created` webhook via the
//     production HMAC-SHA256 signed path (not direct DB insert) so the
//     broker-validate + handler-process + PG-write chain is exercised.
//
//  3. Ensure Tenant CR + ArgoCD AppProject (unstructured — CRDs may not be
//     in scheme; unstructured avoids the registration coupling). The Tenant
//     CR carries the cell the engine placed the tenant in (tenants.cell_id,
//     read in layer 1); without the database it is not written at all
//     rather than written with a guessed cell.
//
// This seed is dev-public-only (SKIP when LOOPZERO_HELMFILE_ENV != "dev-public").
type TenantFixtureSeed struct{}

func NewTenantFixtureSeed() *TenantFixtureSeed { return &TenantFixtureSeed{} }
func (s *TenantFixtureSeed) Name() string      { return "tenant-fixture" }

const (
	tenantFixtureDefaultCloudBase      = "https://app-dev.loopzero.example"
	tenantFixtureDefaultInstallationID = "138325349"
	tenantFixtureDefaultAccountLogin   = "getloopscale"
	tenantFixtureDefaultPlanTier       = "team"

	tenantFixtureCloudBaseEnv    = "LOOPZERO_CLOUD_BASE"
	tenantFixtureInstallIDEnv    = "LOOPZERO_GH_INSTALLATION_ID"
	tenantFixtureAccountLoginEnv = "LOOPZERO_GH_ACCOUNT_LOGIN"
	tenantFixturePlanTierEnv     = "LOOPZERO_PLAN_TIER"
	tenantFixtureTenantIDEnv     = "LOOPZERO_TENANT_ID"
	tenantFixturePGConnEnv       = "LOOPZERO_PG_CONN" // e.g. postgres://loopzero:loopzero-dev-password@127.0.0.1:15432/loopzero?sslmode=disable
	tenantFixtureHelmfileEnvKey  = "LOOPZERO_HELMFILE_ENV"

	tenantFixtureNamespace   = "loopzero-system"
	tenantFixtureProject     = "sample"
	tenantFixtureEnvName     = "production"
	tenantFixtureEnvCluster  = "workload"
	tenantFixtureWebhookPath = "/api/v1/webhooks/github"
)

// pgxConnect opens a pgx connection behind the pgConn surface the seeds use.
func pgxConnect(ctx context.Context, connString string) (pgConn, error) {
	c, err := pgx.Connect(ctx, connString)
	if err != nil {
		return nil, err
	}
	return &pgConnAdapter{c}, nil
}

// pgConn is the surface tenant-fixture uses; tests substitute an in-memory impl.
type pgConn interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgRow
	Exec(ctx context.Context, sql string, args ...any) error
	Close(ctx context.Context) error
}

// pgRow is the minimal Scan surface.
type pgRow interface {
	Scan(dest ...any) error
}

type pgConnAdapter struct{ inner *pgx.Conn }

func (a *pgConnAdapter) QueryRow(ctx context.Context, sql string, args ...any) pgRow {
	return a.inner.QueryRow(ctx, sql, args...)
}
func (a *pgConnAdapter) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := a.inner.Exec(ctx, sql, args...)
	return err
}
func (a *pgConnAdapter) Close(ctx context.Context) error { return a.inner.Close(ctx) }

func (s *TenantFixtureSeed) Run(d Deps) Result {
	env := d.Host.getenv(tenantFixtureHelmfileEnvKey)
	if env != "" && env != "dev-public" {
		return Result{Verdict: SKIP, Reason: fmt.Sprintf("LOOPZERO_HELMFILE_ENV=%q (only dev-public applicable)", env)}
	}
	tenantID := d.Host.getenv(tenantFixtureTenantIDEnv)
	if tenantID == "" {
		return Result{Verdict: SKIP, Reason: "LOOPZERO_TENANT_ID not set (usually seeded by seed-admin-loopzero-dev)"}
	}
	rawTier := d.Host.envOr(tenantFixturePlanTierEnv, tenantFixtureDefaultPlanTier)
	tier, err := domain.ParsePlanTier(strings.ToLower(strings.TrimSpace(rawTier)))
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s=%q: %v (expected starter, pro, team or enterprise)", tenantFixturePlanTierEnv, rawTier, err)}
	}
	crdPlan, err := tenantCRDPlan(tier)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	planTier := tier.String()

	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would upsert subscription plan=%s + replay GH webhook + ensure Tenant CR + AppProject for %s", planTier, tenantID)}
	}

	// Layer 1 — read the tenant's placement and upsert the subscription
	// (skip-graceful when PG conn absent).
	var cellID string
	if pgConn := d.Host.getenv(tenantFixturePGConnEnv); pgConn != "" {
		placed, err := upsertTenantSubscription(d.Ctx, d.Host, pgConn, tenantID, planTier)
		if err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 subscription upsert: %v", err)}
		}
		cellID = placed
	}

	// Layer 2 — GitHub webhook replay (skip-graceful when creds absent).
	var webhookWarn string
	if d.Creds != nil && !credshape.IsPlaceholder(d.Creds.GitHub.WebhookSecret) {
		if err := replayGitHubInstallationWebhook(d.Ctx, d.Host, d.Creds.GitHub.WebhookSecret); err != nil {
			// Non-fatal — the deploy chain may not be ready yet (bash parity).
			// FIX-4 (M24): surface the error in the result reason instead of
			// dropping it with `_ = err` — a silent swallow hid a real webhook
			// failure (.claude/rules/error-handling.md: no silent swallowing).
			// Still non-fatal (warning, not FAIL) — the intent the stale comment
			// documented but never implemented.
			webhookWarn = fmt.Sprintf(" (webhook replay warning: %v)", err)
		}
	}

	// Layer 3a + 3b — Tenant CR + AppProject unstructured.
	var crNote string
	if d.CtrlClient != nil {
		if cellID == "" {
			crNote = fmt.Sprintf(" (Tenant CR skipped: %s not set, so the tenant's placed cell is unknown)", tenantFixturePGConnEnv)
		} else if err := ensureTenantCR(d.Ctx, d.CtrlClient, tenantID, cellID, crdPlan, d.Host.envOr("LOOPZERO_TENANT_DISPLAY_NAME", "LoopZero Dev Tenant")); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3a Tenant CR: %v", err)}
		}
		if err := ensureAppProject(d.Ctx, d.CtrlClient, tenantID); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3b AppProject: %v", err)}
		}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("tenant fixture seeded (tenant=%s plan=%s)%s%s", tenantID, planTier, webhookWarn, crNote)}
}

// Verify probes that Tenant CR + AppProject exist for the target tenant.
func (s *TenantFixtureSeed) Verify(d Deps) Result {
	env := d.Host.getenv(tenantFixtureHelmfileEnvKey)
	if env != "" && env != "dev-public" {
		return Result{Verdict: SKIP, Reason: "not dev-public"}
	}
	tenantID := d.Host.getenv(tenantFixtureTenantIDEnv)
	if tenantID == "" {
		return Result{Verdict: SKIP, Reason: "LOOPZERO_TENANT_ID unset"}
	}
	if d.CtrlClient == nil {
		return Result{Verdict: SKIP, Reason: "no ctrl client"}
	}
	var tenant unstructured.Unstructured
	tenant.SetGroupVersionKind(schema.GroupVersionKind{Group: "loopzero.io", Version: "v1", Kind: "Tenant"})
	if err := d.CtrlClient.Get(d.Ctx, ctrlclient.ObjectKey{Namespace: tenantFixtureNamespace, Name: tenantID}, &tenant); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Tenant CR %s missing: %v", tenantID, err)}
	}
	var project unstructured.Unstructured
	project.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "AppProject"})
	if err := d.CtrlClient.Get(d.Ctx, ctrlclient.ObjectKey{Namespace: "argocd", Name: "loopzero-" + tenantID}, &project); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("AppProject loopzero-%s missing: %v", tenantID, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("Tenant CR + AppProject present for %s", tenantID)}
}

// upsertTenantSubscription materializes the subscription row idempotently and
// returns the cell the engine placed the tenant in. The plan tier has no engine
// write path — subscriptions come from the billing control plane — so this
// fixture stands in for it; the tenant itself must already exist (provisioned
// through the engine by the admin-loopzero-dev seed).
func upsertTenantSubscription(ctx context.Context, h Host, pgConnStr, tenantID, planTier string) (string, error) {
	conn, err := h.pgConnect(ctx, pgConnStr)
	if err != nil {
		return "", fmt.Errorf("pgx connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var accountID, cellID string
	if err := conn.QueryRow(ctx, "SELECT account_id, cell_id FROM tenants WHERE id=$1", tenantID).Scan(&accountID, &cellID); err != nil {
		return "", fmt.Errorf("resolve account and cell of tenant %s: %w", tenantID, err)
	}
	subID := "sub-dev-" + tenantID
	sql := `INSERT INTO subscriptions (id, tenant_id, account_id, plan, status)
	        VALUES ($1, $2, $3, $4, 'active')
	        ON CONFLICT (account_id) DO UPDATE SET plan=$4, status='active', updated_at=now()`
	if err := conn.Exec(ctx, sql, subID, tenantID, accountID, planTier); err != nil {
		return "", fmt.Errorf("insert subscription: %w", err)
	}
	return cellID, nil
}

// replayGitHubInstallationWebhook POSTs a canonical installation.created
// payload with HMAC-SHA256 signature to the cloud webhook endpoint.
func replayGitHubInstallationWebhook(ctx context.Context, h Host, whsec string) error {
	base := h.envOr(tenantFixtureCloudBaseEnv, tenantFixtureDefaultCloudBase)
	installID := h.envOr(tenantFixtureInstallIDEnv, tenantFixtureDefaultInstallationID)
	accountLogin := h.envOr(tenantFixtureAccountLoginEnv, tenantFixtureDefaultAccountLogin)

	payload := fmt.Sprintf(
		`{"action":"created","installation":{"id":%s,"account":{"login":%q,"type":"User"},"target_type":"User","repository_selection":"all"},"sender":{"login":%q}}`,
		installID, accountLogin, accountLogin,
	)
	mac := hmac.New(sha256.New, []byte(whsec))
	_, _ = mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+tenantFixtureWebhookPath, bytes.NewReader([]byte(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("install-canonical-replay-%d", time.Now().Unix()))
	req.Header.Set("X-Hub-Signature-256", sig)

	resp, err := h.devHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		snip := string(body)
		if len(snip) > 200 {
			snip = snip[:200]
		}
		return fmt.Errorf("webhook replay HTTP %d: %s", resp.StatusCode, snip)
	}
	return nil
}

// tenantCRDPlan maps a canonical subscription tier onto the Tenant CRD's plan
// enum (starter;growth;enterprise, operators/api/v1/tenant_types.go). The CRD
// predates the four-tier pricing: pro is its "growth", and team — which has no
// CRD value of its own — takes the enterprise capabilities it was sold with.
// An unknown tier is an error, never a starter default (ADR D4).
func tenantCRDPlan(tier domain.PlanTier) (string, error) {
	switch tier {
	case domain.PlanTierStarter:
		return "starter", nil
	case domain.PlanTierPro:
		return "growth", nil
	case domain.PlanTierTeam, domain.PlanTierEnterprise:
		return "enterprise", nil
	default:
		return "", fmt.Errorf("no Tenant CRD plan for tier %q: %w", tier, domain.ErrUnknownPlanTier)
	}
}

// ensureTenantCR creates or updates the Tenant CR (unstructured — CRD may not
// be in the scheme in fake-client tests; unstructured decouples).
func ensureTenantCR(ctx context.Context, cli ctrlclient.Client, tenantID, cellID, crdPlan, displayName string) error {
	tenant := &unstructured.Unstructured{}
	tenant.SetGroupVersionKind(schema.GroupVersionKind{Group: "loopzero.io", Version: "v1", Kind: "Tenant"})
	tenant.SetNamespace(tenantFixtureNamespace)
	tenant.SetName(tenantID)
	spec := map[string]any{
		"cellID":      cellID,
		"displayName": displayName,
		"plan":        crdPlan,
		"environments": []any{
			map[string]any{
				"cluster": tenantFixtureEnvCluster,
				"name":    tenantFixtureEnvName,
				"project": tenantFixtureProject,
			},
		},
		"gitops": map[string]any{
			"branch":   "main",
			"repoName": tenantID + "-gitops",
		},
		"limits": map[string]any{
			"maxBuildConcurrency": int64(2),
			"maxCPU":              "4",
			"maxMemory":           "8Gi",
			"maxStorage":          "20Gi",
		},
		"preview": map[string]any{
			"maxEnvironments": int64(3),
			"ttlHours":        int64(72),
		},
	}
	tenant.Object["spec"] = spec
	return applyUnstructured(ctx, cli, tenant)
}

// ensureAppProject creates or updates the ArgoCD AppProject (unstructured).
func ensureAppProject(ctx context.Context, cli ctrlclient.Client, tenantID string) error {
	project := &unstructured.Unstructured{}
	project.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "AppProject"})
	project.SetNamespace("argocd")
	project.SetName("loopzero-" + tenantID)
	spec := map[string]any{
		"description": fmt.Sprintf("Canonical AppProject for tenant %s", tenantID),
		"sourceRepos": []any{"*"},
		"destinations": []any{
			map[string]any{"namespace": fmt.Sprintf("loopzero-%s-*", tenantID), "server": "*"},
			map[string]any{"namespace": tenantFixtureNamespace, "server": "*"},
		},
		"clusterResourceWhitelist":   []any{map[string]any{"group": "*", "kind": "*"}},
		"namespaceResourceWhitelist": []any{map[string]any{"group": "*", "kind": "*"}},
	}
	project.Object["spec"] = spec
	return applyUnstructured(ctx, cli, project)
}

// applyUnstructured Create-or-Update on unstructured objects. Preserves
// resourceVersion for optimistic-concurrency Update.
func applyUnstructured(ctx context.Context, cli ctrlclient.Client, obj *unstructured.Unstructured) error {
	err := cli.Create(ctx, obj)
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	// Fetch existing to carry rv forward.
	var existing unstructured.Unstructured
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}, &existing); err != nil {
		return err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	return cli.Update(ctx, obj)
}
