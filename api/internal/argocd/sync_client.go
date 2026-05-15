// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package argocd provides real ArgoCD integration for deployment sync operations.
package argocd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// SyncClientConfig holds configuration for connecting to ArgoCD.
type SyncClientConfig struct {
	BaseURL       string        // e.g., "https://argocd-server.argocd.svc:443"
	Token         string        // ArgoCD API token (used if TokenProvider is nil)
	TokenProvider TokenProvider // Optional: dynamic token source (overrides Token string)
	Insecure      bool          // Skip TLS verification (for local dev with self-signed certs)
	CACertPath    string        // SEC-T02b fix: path to CA cert file for TLS verification. When set, Insecure is ignored.
	HTTPClient    *http.Client  // Optional: shared HTTP client with connection pooling. When nil, a default client is created.
	// Timeout configures the HTTP client request timeout. Plan T1.1 D1:
	// previous hardcoded 30s caused dogfood SYNC_FAILED cascades under Ginkgo
	// concurrent load. Zero value uses 90s default (3x headroom for 50k scale).
	Timeout time.Duration
}

// DefaultArgocdClientTimeout is the default HTTP client timeout for ArgoCD
// API calls when not explicitly configured. 90s gives headroom for hard-refresh
// operations under concurrent load while still bounding circuit-breaker probes.
const DefaultArgocdClientTimeout = 90 * time.Second

// SyncClient implements services.SyncClient using the ArgoCD REST API.
// Token is obtained from TokenProvider on each request, enabling zero-downtime rotation.
// MEET-2026-03-28-015: replaced static token string with TokenProvider interface.
//
// Plan T1.1 D2: SyncClient now uses an internal SyncSerializer to linearize
// concurrent sync calls to the same App. This eliminates ArgoCD HTTP 400
// "another operation is already in progress" errors at the source instead of
// relying on retries that consume circuit-breaker budget.
type SyncClient struct {
	baseURL       string
	tokenProvider TokenProvider
	client        *http.Client
	serializer    *SyncSerializer
}

// NewSyncClient creates a SyncClient for real ArgoCD operations.
// When cfg.TokenProvider is set, it is used for dynamic token sourcing.
// Otherwise, cfg.Token is wrapped in a StaticTokenProvider.
func NewSyncClient(cfg SyncClientConfig) *SyncClient {
	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout == 0 {
			timeout = DefaultArgocdClientTimeout
		}
		client = &http.Client{Timeout: timeout}
		// SEC-T02b fix: prefer CA bundle over InsecureSkipVerify.
		// When CACertPath is set, load the CA and verify TLS properly.
		if cfg.CACertPath != "" {
			caCert, err := os.ReadFile(cfg.CACertPath)
			if err != nil {
				slog.Error("argocd: failed to read CA cert, falling back to insecure",
					"path", cfg.CACertPath, "error", err)
				client.Transport = &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // fallback only
				}
			} else {
				pool := x509.NewCertPool()
				pool.AppendCertsFromPEM(caCert)
				client.Transport = &http.Transport{
					TLSClientConfig: &tls.Config{RootCAs: pool},
				}
				slog.Info("argocd: TLS verification enabled with custom CA", "path", cfg.CACertPath)
			}
		} else if cfg.Insecure {
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local dev only
			}
		}
		// MEET-2026-03-30-020: Wrap transport with OTel for trace context propagation.
		// Injects traceparent header into outgoing requests to ArgoCD.
		client.Transport = otelhttp.NewTransport(client.Transport)
	}

	tp := cfg.TokenProvider
	if tp == nil {
		tp = NewStaticTokenProvider(cfg.Token)
	}

	return &SyncClient{
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		tokenProvider: tp,
		client:        client,
		serializer:    NewSyncSerializer(),
	}
}

// ValidateToken checks that the configured token is valid by calling
// GET /api/v1/session/userinfo. Returns ErrDeployServiceAuthFailed if invalid.
// Called at API startup to fail fast with a clear error (MEET-013 Phase 1.7).
func (c *SyncClient) ValidateToken(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/session/userinfo", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("validate token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenProvider.Token())

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("validate token: deployment sync service unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return loopzeroerrors.ErrDeployServiceAuthFailed
	}
	return nil
}

// TriggerSync initiates a sync operation on an ArgoCD Application.
// MEET-2026-04-22-058 (B12): no annotation mutation anymore. The operator no
// longer sets `argocd.argoproj.io/skip-reconcile`, so the API does not need
// to remove it. Application CR metadata is owned exclusively by the operator
// (K8s RBAC), not by the API (ArgoCD REST RBAC) — single-writer discipline.
//
// CHG-2026-05-05-DOGFOOD-B32: TriggerSync now hard-refreshes the Application
// before submitting the sync operation. ArgoCD repo-server caches manifest
// generation errors (e.g., "app path does not exist") for ~5 minutes per
// (repo, path, revision). When tenant-controller bootstraps a gitops repo
// with an empty initial commit before `loopzero deploy` writes manifests, the
// first comparison caches the error. Subsequent syncs reuse the cache and
// surface as "InvalidSpecError: Application referencing project X which does
// not exist" — a misleading message that has nothing to do with the AppProject.
// Hard-refresh (GET ?refresh=hard) drops the cache and forces re-clone.
func (c *SyncClient) TriggerSync(ctx context.Context, appName string) error {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.argocd.sync_trigger",
		trace.WithAttributes(
			attribute.String("loopzero.argocd.app_name", appName),
		))
	defer span.End()

	// Plan T1.1 D2: serialize concurrent sync calls to the same App. This
	// eliminates ArgoCD HTTP 400 "another operation is already in progress"
	// at the source. Calls to DIFFERENT apps proceed in parallel.
	//
	// SyncClient invariant: serializer is non-nil after NewSyncClient. Test
	// helpers that bypass NewSyncClient (deprecated pattern) MUST be updated
	// to use the constructor — see argocd_test.go newHTTPTestSyncClient.
	release, err := c.serializer.AcquireApp(ctx, appName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "serializer acquire")
		return err
	}
	defer release()

	// MEET-2026-04-22-058 (B12): no removeSkipReconcile call — operator no longer
	// sets the annotation, so there is nothing to remove.

	// CHG-2026-05-05-DOGFOOD-B32: best-effort hard-refresh to invalidate
	// repo-server manifest error cache from prior failed comparisons.
	// Failure does NOT block the sync — repo-server may have already
	// invalidated the cache via the controller's normal refresh interval.
	if err := c.hardRefresh(ctx, appName); err != nil {
		slog.Warn("argocd: hard refresh before sync failed (proceeding)",
			"app", appName, "error", err)
		span.AddEvent("hard_refresh_failed",
			trace.WithAttributes(attribute.String("error", err.Error())))
	}

	url := fmt.Sprintf("%s/api/v1/applications/%s/sync", c.baseURL, appName)

	body := strings.NewReader(`{"prune": true}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("argocd sync: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenProvider.Token())
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("argocd sync: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		// CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15: signal the token provider
		// to mint a fresh token. KubeMintingTokenProvider implements
		// AuthFailureNotifier; StaticTokenProvider does not — it's a no-op.
		// The next caller (or the circuit-breaker retry) picks up the new
		// token via tokenProvider.Token() in subsequent requests.
		if notifier, ok := c.tokenProvider.(AuthFailureNotifier); ok {
			notifier.OnAuthFailure()
		}
		span.RecordError(loopzeroerrors.ErrDeployServiceAuthFailed)
		span.SetStatus(codes.Error, "auth failed")
		return loopzeroerrors.ErrDeployServiceAuthFailed
	}
	if resp.StatusCode == 404 {
		syncErr := fmt.Errorf("argocd sync: application %q not found", appName)
		span.RecordError(syncErr)
		span.SetStatus(codes.Error, syncErr.Error())
		return syncErr
	}
	// MEET-2026-03-29-019: Return typed error for 409 so callers can
	// distinguish "operation in progress" from other failures.
	// This error must NOT count toward the circuit breaker threshold.
	if resp.StatusCode == 409 {
		span.RecordError(loopzeroerrors.ErrSyncOperationInProgress)
		span.SetStatus(codes.Error, "sync operation in progress")
		return loopzeroerrors.ErrSyncOperationInProgress
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		syncErr := fmt.Errorf("argocd sync: status %d: %s", resp.StatusCode, string(respBody))
		span.RecordError(syncErr)
		span.SetStatus(codes.Error, syncErr.Error())
		return syncErr
	}

	return nil
}

// MEET-2026-04-22-058 (B12): removeSkipReconcile deleted. The skip-reconcile
// annotation is no longer set by the operator, so there is nothing to remove.
// Operator owns Application CR metadata exclusively (K8s RBAC); the API does
// not touch the Application CR via ArgoCD REST any more. Single-writer discipline.

// hardRefresh forces ArgoCD repo-server to re-clone and re-render manifests
// for an Application, invalidating cached errors from prior failed comparisons.
//
// CHG-2026-05-05-DOGFOOD-B32: see TriggerSync godoc for the bug context.
// This method is intended to be called by TriggerSync as a defensive measure
// before the sync POST. Best-effort: callers should NOT block on errors here.
//
// Uses a bounded timeout (15s) so a slow repo-server cannot stall sync.
func (c *SyncClient) hardRefresh(ctx context.Context, appName string) error {
	refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/v1/applications/%s?refresh=hard", c.baseURL, appName)
	req, err := http.NewRequestWithContext(refreshCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("hard refresh: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenProvider.Token())

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("hard refresh: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		// Same auth-failure signal path as TriggerSync.
		if notifier, ok := c.tokenProvider.(AuthFailureNotifier); ok {
			notifier.OnAuthFailure()
		}
		return loopzeroerrors.ErrDeployServiceAuthFailed
	}
	if resp.StatusCode == 404 {
		return fmt.Errorf("hard refresh: application %q not found", appName)
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hard refresh: status %d: %s", resp.StatusCode, string(respBody))
	}
	// Drain body so HTTP keep-alive can reuse the connection.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// TerminateOperation cancels a running sync operation on an ArgoCD Application.
// MEET-2026-03-29-019: Used when TriggerSync returns ErrSyncOperationInProgress
// and the operation is stuck. This clears the operation lock so a new sync can start.
func (c *SyncClient) TerminateOperation(ctx context.Context, appName string) error {
	url := fmt.Sprintf("%s/api/v1/applications/%s/operation", c.baseURL, appName)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("argocd terminate: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenProvider.Token())
	// B11 (CHG-2026-04-23): ArgoCD rejects DELETE without Content-Type
	// with HTTP 415 "Invalid content type", blocking stuck-operation recovery.
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("argocd terminate: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return loopzeroerrors.ErrDeployServiceAuthFailed
	}
	// 404 means no operation is running — idempotent success
	if resp.StatusCode == 404 {
		return nil
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("argocd terminate: status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// WaitForSync polls ArgoCD until the application is synced and the specified
// deployedApps are healthy, or until the timeout expires.
// If deployedApps is empty, checks global Application health (legacy behavior).
// Polls every 10 seconds per spec §10.1.
func (c *SyncClient) WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 10 * time.Second

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("argocd wait: sync timeout after %s for %q", timeout, appName)
		}

		synced, healthy, err := c.GetAppStatus(ctx, appName, deployedApps)
		if err != nil {
			return fmt.Errorf("argocd wait: %w", err)
		}

		if synced && healthy {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("argocd wait: context cancelled: %w", ctx.Err())
		case <-time.After(pollInterval):
			// continue polling
		}
	}
}

// resourceStatus represents an individual resource's health in the ArgoCD Application.
type resourceStatus struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Health *struct {
		Status string `json:"status"`
	} `json:"health,omitempty"`
}

// GetAppStatus returns the sync and health status of an ArgoCD Application.
// When deployedApps is non-empty, health is checked per-resource for only the
// Deployments matching those app names — so one degraded app doesn't block others.
// Exported for use by the ReleaseConvergenceLoop (MEET-2026-03-27-012).
func (c *SyncClient) GetAppStatus(ctx context.Context, appName string, deployedApps []string) (synced bool, healthy bool, err error) {
	url := fmt.Sprintf("%s/api/v1/applications/%s", c.baseURL, appName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenProvider.Token())

	resp, err := c.client.Do(req)
	if err != nil {
		return false, false, fmt.Errorf("get app status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return false, false, loopzeroerrors.ErrDeployServiceAuthFailed
	}
	if resp.StatusCode == 404 {
		return false, false, fmt.Errorf("application %q not found", appName)
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return false, false, fmt.Errorf("get app status %d: %s", resp.StatusCode, string(respBody))
	}

	var app struct {
		Status struct {
			Sync struct {
				Status   string `json:"status"`
				Revision string `json:"revision"`
			} `json:"sync"`
			Health struct {
				Status string `json:"status"`
			} `json:"health"`
			Resources []resourceStatus `json:"resources"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return false, false, fmt.Errorf("decode app status: %w", err)
	}

	isSynced := app.Status.Sync.Status == "Synced"

	// If no specific apps to check, use global Application health (legacy).
	if len(deployedApps) == 0 {
		return isSynced, app.Status.Health.Status == "Healthy", nil
	}

	// Per-resource health: check only Deployments matching the deployed app names.
	// This ensures one degraded app doesn't block deploys of other apps in the
	// same ArgoCD Application (which manages all apps in a tenant/environment).
	appSet := make(map[string]bool, len(deployedApps))
	for _, name := range deployedApps {
		appSet[name] = true
	}

	matchedCount := 0
	for _, res := range app.Status.Resources {
		// Workload primitive is either Deployment (legacy) or Rollout
		// (progressive delivery — argo-rollouts-progressive-delivery-plan T1.3).
		// Both share the resource name `<project>-<app>` per resourceName().
		// Without this, releases backed by Rollouts stay PENDING forever
		// because matchedCount never reaches len(deployedApps), causing
		// every promote/rollback test (and any first-deploy where convergence
		// is the activator) to time out at 3min. Discovered live during
		// post-canary-plan dogfood gate run (J05 + J06 regression).
		if res.Kind != "Deployment" && res.Kind != "Rollout" {
			continue
		}
		if !appSet[res.Name] {
			continue
		}
		matchedCount++
		if res.Health == nil || res.Health.Status != "Healthy" {
			return isSynced, false, nil
		}
	}

	// If we found all workload primitives (Deployment OR Rollout) and they're
	// all healthy, consider it healthy. If no matching primitives found yet
	// (still rolling out), not healthy.
	return isSynced, matchedCount >= len(deployedApps), nil
}

// GetSyncRevision returns the git revision that ArgoCD has synced for an application.
// Used to verify the synced revision matches the expected commit SHA.
// MEET-2026-03-27-012 Phase 1.2: prevents silent wrong-version activation.
func (c *SyncClient) GetSyncRevision(ctx context.Context, appName string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/applications/%s", c.baseURL, appName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.tokenProvider.Token())

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get sync revision: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "", loopzeroerrors.ErrDeployServiceAuthFailed
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("get sync revision: status %d", resp.StatusCode)
	}

	var app struct {
		Status struct {
			Sync struct {
				Revision string `json:"revision"`
			} `json:"sync"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return "", fmt.Errorf("decode sync revision: %w", err)
	}
	return app.Status.Sync.Revision, nil
}
