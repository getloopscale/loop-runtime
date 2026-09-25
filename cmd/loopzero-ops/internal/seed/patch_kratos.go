// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// PatchKratosSeed is the Go port of
// infra/scripts/local/patch-kratos-dev-public.sh (+ patch_kratos_dev_public.py).
//
// Idempotent LIVE patch of the ory-kratos-config ConfigMap when helmfile
// sync drifts. 7 fixes applied to `kratos.yaml`:
//
//  1. OIDC providers (github + google): client_id / client_secret from creds
//  2. selfservice.methods.oidc.config.base_redirect_uri = auth-dev URL
//  3. courier.http.request_config.headers.Authorization = Bearer <resend>
//  4. serve.public.base_url + CORS (allowed_origins: dashboard + cloud)
//  5. cookies.domain + session.cookie.domain = loopzero.example eTLD+1
//  6. selfservice.flows.* ui_url + allowed_return_urls (app-dev URLs)
//  7. selfservice.flows.logout.after.default_browser_return_url
//
// After apply, restarts Deployment ory-kratos + StatefulSet ory-kratos-courier
// (Kratos does not SIGHUP — config re-read only at process startup).
type PatchKratosSeed struct{}

func NewPatchKratosSeed() *PatchKratosSeed { return &PatchKratosSeed{} }
func (s *PatchKratosSeed) Name() string    { return "patch-kratos" }

const (
	kratosNamespace          = "loopzero-auth"
	kratosConfigMap          = "ory-kratos-config"
	kratosConfigKey          = "kratos.yaml"
	kratosDeployment         = "ory-kratos"
	kratosCourierStatefulSet = "ory-kratos-courier"

	kratosDefaultPublicURL    = "https://auth-dev.loopzero.example"
	kratosDefaultDashboardURL = "https://app-dev.loopzero.example"
	kratosDefaultCloudURL     = "https://cloud-dev.loopzero.example"
	kratosDefaultCookieDomain = "loopzero.example"
)

// PatchKratosConfig collects the values patched into kratos.yaml. Package-level
// so tests can override the URL defaults without going through env fallback.
type PatchKratosConfig struct {
	PublicURL      string
	DashboardURL   string
	CloudURL       string
	CookieDomain   string
	GitHubClientID string
	GitHubSecret   string
	GoogleClientID string
	GoogleSecret   string
	ResendAPIKey   string
}

func (s *PatchKratosSeed) Run(d Deps) Result {
	cfg, res, halt := s.loadConfig(d)
	if halt {
		return res
	}

	cm, err := d.Kube.GetConfigMap(d.Ctx, kratosNamespace, kratosConfigMap)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	current := cm.Data[kratosConfigKey]
	if current == "" {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ConfigMap %s/%s has no %s key", kratosNamespace, kratosConfigMap, kratosConfigKey)}
	}
	patched, err := patchKratosYAML(current, cfg)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("patch kratos.yaml: %v", err)}
	}
	if patched == current {
		return Result{Verdict: PASS, Reason: "kratos.yaml already canonical (no diff)"}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would patch kratos.yaml + rollout kratos + courier"}
	}
	if err := d.Kube.PatchConfigMapData(d.Ctx, kratosNamespace, kratosConfigMap,
		map[string]string{kratosConfigKey: patched}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("patch %s/%s: %v", kratosNamespace, kratosConfigMap, err)}
	}
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, kratosNamespace, kratosDeployment); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout deployment %s: %v", kratosDeployment, err)}
	}
	if err := rolloutStatefulSet(d.Ctx, d.CtrlClient, kratosNamespace, kratosCourierStatefulSet); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout statefulset %s: %v", kratosCourierStatefulSet, err)}
	}
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, kratosNamespace, kratosDeployment, d.settleTimeout(kratosSettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s did not settle after the config patch: %v", kratosDeployment, err)}
	}
	return Result{Verdict: PASS, Reason: "kratos ConfigMap patched (7 fixes) + kratos + courier rolled out"}
}

// Verify probes the ConfigMap contains the canonical URLs. LIVE HTTP probe
// against auth-dev URL is not attempted here (belongs to the LIVE e2e test).
func (s *PatchKratosSeed) Verify(d Deps) Result {
	cfg, res, halt := s.loadConfig(d)
	if halt {
		return res
	}
	cm, err := d.Kube.GetConfigMap(d.Ctx, kratosNamespace, kratosConfigMap)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	current := cm.Data[kratosConfigKey]
	if !strings.Contains(current, cfg.PublicURL) {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("kratos.yaml missing PublicURL %s", cfg.PublicURL)}
	}
	if !strings.Contains(current, cfg.DashboardURL) {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("kratos.yaml missing DashboardURL %s", cfg.DashboardURL)}
	}
	return Result{Verdict: PASS, Reason: "kratos.yaml carries canonical dev-public URLs"}
}

// loadConfig gathers URLs (env override) + secret material (creds file). Returns
// non-empty Result on missing required fields — Verdict is FAIL / SKIP.
func (s *PatchKratosSeed) loadConfig(d Deps) (PatchKratosConfig, Result, bool) {
	if d.Creds == nil {
		return PatchKratosConfig{}, Result{Verdict: SKIP, Reason: "creds absent (no --creds-file / dev-credentials.json)"}, true
	}
	cfg := PatchKratosConfig{
		PublicURL:      d.Host.envOr("KRATOS_PUBLIC_URL", kratosDefaultPublicURL),
		DashboardURL:   d.Host.envOr("DASHBOARD_URL", kratosDefaultDashboardURL),
		CloudURL:       d.Host.envOr("CLOUD_URL", kratosDefaultCloudURL),
		CookieDomain:   d.Host.envOr("COOKIE_DOMAIN", kratosDefaultCookieDomain),
		GitHubClientID: d.Creds.GitHub.ClientID,
		GitHubSecret:   d.Creds.GitHub.ClientSecret,
		GoogleClientID: d.Creds.Google.ClientID,
		GoogleSecret:   d.Creds.Google.ClientSecret,
		ResendAPIKey:   d.Creds.Resend.APIKey,
	}
	// Fail-hard on missing required — bash returned exit 1 with a clear
	// message; Go returns FAIL with a matching diagnostic.
	missing := []string{}
	if cfg.GitHubClientID == "" {
		missing = append(missing, "github.client_id")
	}
	if cfg.GitHubSecret == "" {
		missing = append(missing, "github.client_secret")
	}
	if cfg.GoogleClientID == "" {
		missing = append(missing, "google.client_id")
	}
	if cfg.GoogleSecret == "" {
		missing = append(missing, "google.client_secret")
	}
	if cfg.ResendAPIKey == "" {
		missing = append(missing, "resend.api_key")
	}
	if len(missing) > 0 {
		return PatchKratosConfig{}, Result{Verdict: FAIL, Reason: fmt.Sprintf("creds missing: %v", missing)}, true
	}
	return cfg, Result{}, false
}

// patchKratosYAML applies the 7 canonical patches to the input yaml body and
// returns the re-serialized yaml. Pure function — no I/O. Testable byte-for-byte.
func patchKratosYAML(input string, cfg PatchKratosConfig) (string, error) {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(input), &root); err != nil {
		return "", fmt.Errorf("parse kratos.yaml: %w", err)
	}
	app := trimTrailingSlash(cfg.DashboardURL)
	pub := trimTrailingSlash(cfg.PublicURL)

	// Patch 1 — OIDC providers.
	ss := ensureMap(root, "selfservice")
	methods := ensureMap(ss, "methods")
	oidc := ensureMap(methods, "oidc")
	oidcCfg := ensureMap(oidc, "config")
	if provs, ok := oidcCfg["providers"].([]any); ok {
		for _, p := range provs {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch pm["id"] {
			case "github":
				pm["client_id"] = cfg.GitHubClientID
				pm["client_secret"] = cfg.GitHubSecret
			case "google":
				pm["client_id"] = cfg.GoogleClientID
				pm["client_secret"] = cfg.GoogleSecret
			}
		}
	}
	// Patch 2 — OIDC base_redirect_uri.
	oidcCfg["base_redirect_uri"] = pub + "/"

	// Patch 3 — Resend Authorization.
	courier := ensureMap(root, "courier")
	http := ensureMap(courier, "http")
	rc := ensureMap(http, "request_config")
	hdrs := ensureMap(rc, "headers")
	hdrs["Authorization"] = "Bearer " + cfg.ResendAPIKey

	// Patch 4 — serve.public.
	serve := ensureMap(root, "serve")
	public := ensureMap(serve, "public")
	public["base_url"] = pub + "/"
	if _, ok := public["port"]; !ok {
		public["port"] = 4433
	}
	public["cors"] = map[string]any{
		"enabled":           true,
		"allow_credentials": true,
		"allowed_origins":   []any{cfg.DashboardURL, cfg.CloudURL},
		"allowed_methods":   []any{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		"allowed_headers":   []any{"Authorization", "Content-Type", "Cookie", "X-Session-Token"},
		"exposed_headers":   []any{"Content-Type", "Set-Cookie"},
	}

	// Patch 5 — cookies + session.cookie.
	root["cookies"] = map[string]any{"domain": cfg.CookieDomain, "same_site": "Lax"}
	session := ensureMap(root, "session")
	cookie := ensureMap(session, "cookie")
	cookie["domain"] = cfg.CookieDomain
	if _, ok := session["lifespan"]; !ok {
		session["lifespan"] = "24h"
	}

	// Patch 6 + 7 — selfservice flows.
	ss["default_browser_return_url"] = app + "/"
	ss["allowed_return_urls"] = []any{app, app + "/", cfg.CloudURL, cfg.CloudURL + "/"}
	flows := ensureMap(ss, "flows")
	ensureMap(flows, "login")["ui_url"] = app + "/login"
	ensureMap(flows, "registration")["ui_url"] = app + "/register"
	ensureMap(flows, "verification")["ui_url"] = app + "/verification"
	ensureMap(flows, "recovery")["ui_url"] = app + "/recovery"
	ensureMap(flows, "settings")["ui_url"] = app + "/settings"
	ensureMap(flows, "error")["ui_url"] = app + "/login"
	logout := ensureMap(flows, "logout")
	after := ensureMap(logout, "after")
	after["default_browser_return_url"] = app + "/login"

	out, err := yaml.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("marshal kratos.yaml: %w", err)
	}
	return string(out), nil
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if v, ok := parent[key].(map[string]any); ok {
		return v
	}
	m := map[string]any{}
	parent[key] = m
	return m
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// rolloutStatefulSet annotates the StatefulSet pod template restartedAt.
// Idempotent (same-second rerun is a no-op). NotFound → nil (bash parity —
// courier StatefulSet may not be present in older charts).
func rolloutStatefulSet(ctx context.Context, cli ctrlclient.Client, namespace, name string) error {
	if cli == nil {
		return nil // fake-client tests bypass
	}
	var ss appsv1.StatefulSet
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, &ss); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if ss.Spec.Template.Annotations == nil {
		ss.Spec.Template.Annotations = map[string]string{}
	}
	ss.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	return cli.Update(ctx, &ss)
}
