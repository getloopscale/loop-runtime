// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func makeKratosCreds() *Creds {
	c := &Creds{}
	c.GitHub.ClientID = "ghid-1"
	c.GitHub.ClientSecret = "ghsec-1"
	c.Google.ClientID = "ggid-2"
	c.Google.ClientSecret = "ggsec-2"
	c.Resend.APIKey = "re_abc"
	return c
}

const stubKratosYAML = `serve:
  public:
    port: 4433
selfservice:
  methods:
    oidc:
      config:
        providers:
        - id: github
          client_id: stale-gh
          client_secret: stale-ghsec
        - id: google
          client_id: stale-gg
          client_secret: stale-ggsec
  flows:
    login:
      ui_url: http://localhost:4455/login
courier:
  http:
    request_config:
      headers:
        Authorization: Bearer stale-token
`

func TestPatchKratosYAML_UpdatesAllSeven(t *testing.T) {
	cfg := PatchKratosConfig{
		PublicURL:      kratosDefaultPublicURL,
		DashboardURL:   kratosDefaultDashboardURL,
		CloudURL:       kratosDefaultCloudURL,
		CookieDomain:   kratosDefaultCookieDomain,
		GitHubClientID: "ghid-1", GitHubSecret: "ghsec-1",
		GoogleClientID: "ggid-2", GoogleSecret: "ggsec-2",
		ResendAPIKey: "re_abc",
	}
	got, err := patchKratosYAML(stubKratosYAML, cfg)
	if err != nil {
		t.Fatalf("patchKratosYAML: %v", err)
	}

	var out map[string]any
	if err := yaml.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("re-parse: %v", err)
	}

	// Patch 1 — OIDC providers.
	provs := out["selfservice"].(map[string]any)["methods"].(map[string]any)["oidc"].(map[string]any)["config"].(map[string]any)["providers"].([]any)
	if provs[0].(map[string]any)["client_id"] != "ghid-1" {
		t.Errorf("github client_id not patched: %v", provs[0])
	}
	if provs[1].(map[string]any)["client_secret"] != "ggsec-2" {
		t.Errorf("google client_secret not patched: %v", provs[1])
	}

	// Patch 2 — OIDC base_redirect_uri.
	oidcCfg := out["selfservice"].(map[string]any)["methods"].(map[string]any)["oidc"].(map[string]any)["config"].(map[string]any)
	if oidcCfg["base_redirect_uri"] != kratosDefaultPublicURL+"/" {
		t.Errorf("base_redirect_uri = %v", oidcCfg["base_redirect_uri"])
	}

	// Patch 3 — Resend Authorization.
	hdrs := out["courier"].(map[string]any)["http"].(map[string]any)["request_config"].(map[string]any)["headers"].(map[string]any)
	if hdrs["Authorization"] != "Bearer re_abc" {
		t.Errorf("Authorization = %v", hdrs["Authorization"])
	}

	// Patch 4 — serve.public.
	pub := out["serve"].(map[string]any)["public"].(map[string]any)
	if pub["base_url"] != kratosDefaultPublicURL+"/" {
		t.Errorf("serve.public.base_url = %v", pub["base_url"])
	}
	if _, ok := pub["cors"]; !ok {
		t.Errorf("serve.public.cors missing")
	}

	// Patch 5 — cookies.
	cookies := out["cookies"].(map[string]any)
	if cookies["domain"] != kratosDefaultCookieDomain {
		t.Errorf("cookies.domain = %v", cookies["domain"])
	}

	// Patch 6 — flows.
	flows := out["selfservice"].(map[string]any)["flows"].(map[string]any)
	if flows["login"].(map[string]any)["ui_url"] != kratosDefaultDashboardURL+"/login" {
		t.Errorf("flows.login.ui_url = %v", flows["login"])
	}

	// Patch 7 — logout.after.
	after := flows["logout"].(map[string]any)["after"].(map[string]any)
	if after["default_browser_return_url"] != kratosDefaultDashboardURL+"/login" {
		t.Errorf("logout.after.default_browser_return_url = %v", after)
	}
}

func TestPatchKratosYAML_Idempotent(t *testing.T) {
	cfg := PatchKratosConfig{
		PublicURL: kratosDefaultPublicURL, DashboardURL: kratosDefaultDashboardURL,
		CloudURL: kratosDefaultCloudURL, CookieDomain: kratosDefaultCookieDomain,
		GitHubClientID: "gh", GitHubSecret: "s", GoogleClientID: "gg", GoogleSecret: "s2",
		ResendAPIKey: "re",
	}
	once, err := patchKratosYAML(stubKratosYAML, cfg)
	if err != nil {
		t.Fatalf("once: %v", err)
	}
	twice, err := patchKratosYAML(once, cfg)
	if err != nil {
		t.Fatalf("twice: %v", err)
	}
	if once != twice {
		t.Errorf("idempotent failed:\nonce:\n%s\ntwice:\n%s", once, twice)
	}
}

func TestPatchKratosSeed_LoadConfig_SkipWhenCredsAbsent(t *testing.T) {
	deps := Deps{Ctx: context.Background(), Creds: nil}
	got := NewPatchKratosSeed().Run(deps)
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestPatchKratosSeed_LoadConfig_FailWhenMissingFields(t *testing.T) {
	creds := &Creds{}
	// Only github client_id populated — the rest missing → FAIL.
	creds.GitHub.ClientID = "just-one"
	deps := Deps{Ctx: context.Background(), Creds: creds}
	got := NewPatchKratosSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "creds missing") {
		t.Errorf("Reason = %q, want 'creds missing' marker", got.Reason)
	}
}

func TestPatchKratosSeed_Run_PatchesAndRolloutsBoth(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: kratosNamespace, Name: kratosConfigMap},
		Data:       map[string]string{kratosConfigKey: stubKratosYAML},
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: kratosNamespace, Name: kratosDeployment},
	}
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: kratosNamespace, Name: kratosCourierStatefulSet},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm, dep, ss).Build()
	deps := Deps{
		Ctx:        context.Background(),
		Kube:       NewKubeClient(c),
		CtrlClient: c,
		Creds:      makeKratosCreds(),
	}
	got := NewPatchKratosSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	// Assert ConfigMap was patched.
	var patched corev1.ConfigMap
	_ = c.Get(context.Background(), fakeKey(kratosNamespace, kratosConfigMap), &patched)
	if !strings.Contains(patched.Data[kratosConfigKey], "auth-dev.loopzero.example") {
		t.Errorf("ConfigMap not patched: %q", patched.Data[kratosConfigKey])
	}
	// Assert Deployment + StatefulSet were rolled out (annotation set).
	var d appsv1.Deployment
	_ = c.Get(context.Background(), fakeKey(kratosNamespace, kratosDeployment), &d)
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Errorf("Deployment restartedAt annotation not set")
	}
	var s appsv1.StatefulSet
	_ = c.Get(context.Background(), fakeKey(kratosNamespace, kratosCourierStatefulSet), &s)
	if s.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Errorf("StatefulSet restartedAt annotation not set")
	}
}

func TestPatchKratosSeed_Run_DryRunNoWrite(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: kratosNamespace, Name: kratosConfigMap},
		Data:       map[string]string{kratosConfigKey: stubKratosYAML},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).Build()
	deps := Deps{
		Ctx:    context.Background(),
		Kube:   NewKubeClient(c),
		DryRun: true,
		Creds:  makeKratosCreds(),
	}
	got := NewPatchKratosSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "dry-run") {
		t.Errorf("Reason = %q, want dry-run marker", got.Reason)
	}
	// Assert ConfigMap NOT patched.
	var untouched corev1.ConfigMap
	_ = c.Get(context.Background(), fakeKey(kratosNamespace, kratosConfigMap), &untouched)
	if strings.Contains(untouched.Data[kratosConfigKey], "auth-dev.loopzero.example") {
		t.Errorf("dry-run mutated ConfigMap")
	}
}

func TestPatchKratosSeed_Run_PassWhenAlreadyCanonical(t *testing.T) {
	// Pre-run patchKratosYAML so the ConfigMap is already in target state.
	cfg := PatchKratosConfig{
		PublicURL: kratosDefaultPublicURL, DashboardURL: kratosDefaultDashboardURL,
		CloudURL: kratosDefaultCloudURL, CookieDomain: kratosDefaultCookieDomain,
		GitHubClientID: "ghid-1", GitHubSecret: "ghsec-1",
		GoogleClientID: "ggid-2", GoogleSecret: "ggsec-2",
		ResendAPIKey: "re_abc",
	}
	canonical, _ := patchKratosYAML(stubKratosYAML, cfg)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: kratosNamespace, Name: kratosConfigMap},
		Data:       map[string]string{kratosConfigKey: canonical},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).Build()
	deps := Deps{
		Ctx:   context.Background(),
		Kube:  NewKubeClient(c),
		Creds: makeKratosCreds(),
	}
	got := NewPatchKratosSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "no diff") {
		t.Errorf("Reason = %q, want 'no diff'", got.Reason)
	}
}

func TestPatchKratosSeed_Run_FailWhenConfigMapMissing(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		Ctx:   context.Background(),
		Kube:  NewKubeClient(c),
		Creds: makeKratosCreds(),
	}
	got := NewPatchKratosSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestPatchKratosSeed_Verify_PassWhenCanonical(t *testing.T) {
	cfg := PatchKratosConfig{
		PublicURL: kratosDefaultPublicURL, DashboardURL: kratosDefaultDashboardURL,
		CloudURL: kratosDefaultCloudURL, CookieDomain: kratosDefaultCookieDomain,
		GitHubClientID: "gh", GitHubSecret: "s", GoogleClientID: "gg", GoogleSecret: "s2",
		ResendAPIKey: "re",
	}
	canonical, _ := patchKratosYAML(stubKratosYAML, cfg)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: kratosNamespace, Name: kratosConfigMap},
		Data:       map[string]string{kratosConfigKey: canonical},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(cm).Build()
	deps := Deps{
		Ctx:   context.Background(),
		Kube:  NewKubeClient(c),
		Creds: makeKratosCreds(),
	}
	got := NewPatchKratosSeed().Verify(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestPatchKratosSeed_Name(t *testing.T) {
	if got := NewPatchKratosSeed().Name(); got != "patch-kratos" {
		t.Errorf("Name = %q", got)
	}
}

func TestTrimTrailingSlash(t *testing.T) {
	cases := map[string]string{
		"http://x":   "http://x",
		"http://x/":  "http://x",
		"http://x//": "http://x",
	}
	for in, want := range cases {
		if got := trimTrailingSlash(in); got != want {
			t.Errorf("trimTrailingSlash(%q) = %q, want %q", in, got, want)
		}
	}
}
