// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"github.com/getloopscale/loop-runtime/pkg/domain"

	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// --- pg stubs -----------------------------------------------------------

type stubPgRow struct {
	values []any
	err    error
}

func (r *stubPgRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i >= len(r.values) {
			break
		}
		switch typed := d.(type) {
		case *string:
			*typed = r.values[i].(string)
		}
	}
	return nil
}

type stubPgConn struct {
	mu      sync.Mutex
	rowScan map[string]*stubPgRow // SQL prefix → row
	execs   []struct {
		sql  string
		args []any
	}
	execErr error
	closed  bool
}

func (c *stubPgConn) QueryRow(ctx context.Context, sql string, args ...any) pgRow {
	c.mu.Lock()
	defer c.mu.Unlock()
	for prefix, r := range c.rowScan {
		if strings.HasPrefix(sql, prefix) {
			return r
		}
	}
	return &stubPgRow{err: io.EOF}
}
func (c *stubPgConn) Exec(ctx context.Context, sql string, args ...any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.execs = append(c.execs, struct {
		sql  string
		args []any
	}{sql, args})
	return c.execErr
}
func (c *stubPgConn) Close(ctx context.Context) error { c.closed = true; return nil }

// pgOf is a Host.PGConnect that hands back conn.
func pgOf(conn pgConn) func(context.Context, string) (pgConn, error) {
	return func(context.Context, string) (pgConn, error) { return conn, nil }
}

// envOf is a Host.Getenv that answers from kv and nothing else.
func envOf(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestTenantCRDPlan_MapsEveryCanonicalTier(t *testing.T) {
	cases := map[domain.PlanTier]string{
		domain.PlanTierStarter:    "starter",
		domain.PlanTierPro:        "growth",
		domain.PlanTierTeam:       "enterprise",
		domain.PlanTierEnterprise: "enterprise",
	}
	for tier, want := range cases {
		got, err := tenantCRDPlan(tier)
		if err != nil || got != want {
			t.Errorf("tenantCRDPlan(%q) = (%q, %v), want (%q, nil)", tier, got, err, want)
		}
	}
}

func TestTenantCRDPlan_RejectsUnknownTier(t *testing.T) {
	if got, err := tenantCRDPlan(domain.PlanTierUnknown); err == nil {
		t.Fatalf("tenantCRDPlan(unknown) = %q, want an error", got)
	}
}

// An unrecognised LOOPRUNTIME_PLAN_TIER used to become "starter" silently — the
// downgrade ADR D4 forbids. It must fail before anything is written.
func TestTenantFixture_FailsOnUnknownPlanTier(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{
		tenantFixtureTenantIDEnv: "tn-x",
		tenantFixturePlanTierEnv: "gold",
	})
	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), DryRun: true})
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "gold") {
		t.Errorf("Reason should name the rejected value: %q", got.Reason)
	}
}

func TestTenantFixture_AcceptsUppercasePlanTier(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{
		tenantFixtureTenantIDEnv: "tn-x",
		tenantFixturePlanTierEnv: "PRO",
	})
	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), DryRun: true})
	if got.Verdict != PASS || !strings.Contains(got.Reason, "plan=pro") {
		t.Fatalf("Verdict = %s (%s), want PASS with plan=pro", got.Verdict, got.Reason)
	}
}

func TestTenantFixture_SkipWhenWrongHelmfileEnv(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{tenantFixtureHelmfileEnvKey: "standalone-multi"})
	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background()})
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestTenantFixture_SkipWhenTenantIDMissing(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{tenantFixtureHelmfileEnvKey: "dev-public"})
	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background()})
	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestTenantFixture_DryRunReports(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{
		tenantFixtureHelmfileEnvKey: "dev-public",
		tenantFixtureTenantIDEnv:    "tn-123",
	})
	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), DryRun: true})
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "dry-run") {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestTenantFixture_UpsertsSubscription(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{
		tenantFixtureHelmfileEnvKey: "dev-public",
		tenantFixtureTenantIDEnv:    "tn-abc",
		tenantFixturePGConnEnv:      "postgres://x",
	})
	conn := &stubPgConn{
		rowScan: map[string]*stubPgRow{
			"SELECT account_id, cell_id": {values: []any{"acct-123", "cell-0"}},
		},
	}
	host.PGConnect = pgOf(conn)

	// Skip HTTP + CR path — no creds + no ctrl client.
	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background()})
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if len(conn.execs) == 0 {
		t.Fatal("no INSERT executed")
	}
	if !strings.Contains(conn.execs[0].sql, "INSERT INTO subscriptions") {
		t.Errorf("wrong SQL: %q", conn.execs[0].sql)
	}
	if !strings.Contains(conn.execs[0].sql, "ON CONFLICT (account_id) DO UPDATE") {
		t.Errorf("missing ON CONFLICT clause: %q", conn.execs[0].sql)
	}
}

func TestTenantFixture_ReplaysWebhookWithSHA256HMAC(t *testing.T) {
	var host Host
	var receivedSig string
	var receivedPayload []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tenantFixtureWebhookPath {
			http.NotFound(w, r)
			return
		}
		receivedSig = r.Header.Get("X-Hub-Signature-256")
		receivedPayload, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	host.DevHTTP = server.Client()

	host.Getenv = envOf(map[string]string{
		tenantFixtureHelmfileEnvKey: "dev-public",
		tenantFixtureTenantIDEnv:    "tn-abc",
		tenantFixtureCloudBaseEnv:   server.URL,
	})

	creds := &Creds{}
	creds.GitHub.WebhookSecret = "whsec_test_abc"

	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), Creds: creds})
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	// Verify HMAC signature.
	mac := hmac.New(sha256.New, []byte("whsec_test_abc"))
	_, _ = mac.Write(receivedPayload)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if receivedSig != wantSig {
		t.Errorf("signature mismatch:\n got  %s\n want %s", receivedSig, wantSig)
	}
	// Payload has the canonical shape.
	if !strings.Contains(string(receivedPayload), `"action":"created"`) {
		t.Errorf("payload missing action: %s", receivedPayload)
	}
}

// TestTenantFixture_SurfacesWebhookWarningOnFailure (FIX-4, M24): a failed webhook
// replay is non-fatal (still PASS) but MUST surface in the reason, not be silently
// swallowed (was `_ = err`). error-handling.md: no silent swallowing.
func TestTenantFixture_SurfacesWebhookWarningOnFailure(t *testing.T) {
	var host Host
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	host.DevHTTP = server.Client()
	host.Getenv = envOf(map[string]string{
		tenantFixtureHelmfileEnvKey: "dev-public",
		tenantFixtureTenantIDEnv:    "tn-abc",
		tenantFixtureCloudBaseEnv:   server.URL,
	})

	creds := &Creds{}
	creds.GitHub.WebhookSecret = "whsec_test_abc"

	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), Creds: creds})

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS (webhook failure is non-fatal)", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "webhook replay warning") {
		t.Errorf("Reason must surface the swallowed webhook error, got: %s", got.Reason)
	}
}

func TestTenantFixture_CreatesTenantCRAndAppProject(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{
		tenantFixtureHelmfileEnvKey: "dev-public",
		tenantFixtureTenantIDEnv:    "tn-abc",
		tenantFixturePGConnEnv:      "postgres://x",
	})
	host.PGConnect = pgOf(&stubPgConn{rowScan: map[string]*stubPgRow{
		"SELECT account_id, cell_id": {values: []any{"acct-123", "cell-sfo3-02"}},
	}})
	c := fake.NewClientBuilder().Build()
	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), CtrlClient: c})
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var tenant unstructured.Unstructured
	tenant.SetGroupVersionKind(schema.GroupVersionKind{Group: "runtime.getloopscale.com", Version: "v1", Kind: "Tenant"})
	if err := c.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "loopruntime-system", Name: "tn-abc"}, &tenant); err != nil {
		t.Fatalf("Tenant CR missing: %v", err)
	}
	// The CR carries the cell the engine placed the tenant in, never a
	// fixture's own guess.
	if got := tenant.Object["spec"].(map[string]any)["cellID"]; got != "cell-sfo3-02" {
		t.Errorf("cellID = %v, want the placed cell cell-sfo3-02", got)
	}
	if tenant.Object["spec"].(map[string]any)["plan"] != "enterprise" {
		t.Errorf("plan = %v, want 'enterprise' (team → enterprise map)", tenant.Object["spec"].(map[string]any)["plan"])
	}

	var project unstructured.Unstructured
	project.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "AppProject"})
	if err := c.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "argocd", Name: "loopruntime-tn-abc"}, &project); err != nil {
		t.Fatalf("AppProject missing: %v", err)
	}
}

func TestTenantFixture_ApplyUnstructured_UpdatesWhenExists(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	first := &unstructured.Unstructured{}
	first.SetGroupVersionKind(schema.GroupVersionKind{Group: "runtime.getloopscale.com", Version: "v1", Kind: "Tenant"})
	first.SetNamespace("loopruntime-system")
	first.SetName("tn-x")
	first.Object["spec"] = map[string]any{"plan": "starter"}
	if err := applyUnstructured(context.Background(), c, first); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	second := &unstructured.Unstructured{}
	second.SetGroupVersionKind(schema.GroupVersionKind{Group: "runtime.getloopscale.com", Version: "v1", Kind: "Tenant"})
	second.SetNamespace("loopruntime-system")
	second.SetName("tn-x")
	second.Object["spec"] = map[string]any{"plan": "enterprise"}
	if err := applyUnstructured(context.Background(), c, second); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	var got unstructured.Unstructured
	got.SetGroupVersionKind(schema.GroupVersionKind{Group: "runtime.getloopscale.com", Version: "v1", Kind: "Tenant"})
	_ = c.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "loopruntime-system", Name: "tn-x"}, &got)
	if got.Object["spec"].(map[string]any)["plan"] != "enterprise" {
		t.Errorf("second apply did not update plan: %v", got.Object["spec"])
	}
}

func TestTenantFixture_Name(t *testing.T) {
	if got := NewTenantFixtureSeed().Name(); got != "tenant-fixture" {
		t.Errorf("Name = %q", got)
	}
}

// Without the database the tenant's placed cell is unknown, so the Tenant CR is
// not written with a guessed one; the AppProject, which names no cell, is.
func TestTenantFixture_SkipsTheTenantCRWithoutThePlacedCell(t *testing.T) {
	var host Host
	host.Getenv = envOf(map[string]string{
		tenantFixtureHelmfileEnvKey: "dev-public",
		tenantFixtureTenantIDEnv:    "tn-abc",
	})
	c := fake.NewClientBuilder().Build()

	got := NewTenantFixtureSeed().Run(Deps{Host: host, Ctx: context.Background(), CtrlClient: c})

	var tenant unstructured.Unstructured
	tenant.SetGroupVersionKind(schema.GroupVersionKind{Group: "runtime.getloopscale.com", Version: "v1", Kind: "Tenant"})
	err := c.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "loopruntime-system", Name: "tn-abc"}, &tenant)
	if got.Verdict != PASS || err == nil || !strings.Contains(got.Reason, tenantFixturePGConnEnv) {
		t.Fatalf("Run = %s (%s), Tenant CR lookup err=%v; want PASS, no CR, and the reason naming %s", got.Verdict, got.Reason, err, tenantFixturePGConnEnv)
	}
}
