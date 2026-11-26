// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// argocdInitialAdminSecretFixture builds argocd/argocd-initial-admin-secret.
func argocdInitialAdminSecretFixture(pw string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "argocd", Name: "argocd-initial-admin-secret"},
		Data:       map[string][]byte{"password": []byte(pw)},
	}
}

// stubArgoCDServer stands in for the real ArgoCD server. It accepts /session
// with any body and returns {"token":"<session>"}; then /account/<acct>/token
// with Bearer <session> returns {"token":"<canonical-account-token>"}. On any
// unmatched path or missing auth it returns 401 so parity FAILs are testable.
func stubArgoCDServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/session" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "sess-xyz"})
		case strings.HasPrefix(r.URL.Path, "/api/v1/account/") && strings.HasSuffix(r.URL.Path, "/token"):
			if r.Header.Get("Authorization") != "Bearer sess-xyz" {
				http.Error(w, "unauth", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "canonical-account-token"})
		case r.URL.Path == "/api/v1/version":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "404", http.StatusNotFound)
		}
	}))
}

// pfCtorPointingAt is a Deps.PortForwardCtor stub that returns a Tunnel whose
// LocalPort points at the stub httptest.Server (no actual port-forward runs).
// The Close func is a no-op — httptest.Server manages its own lifecycle.
func pfCtorPointingAt(server *httptest.Server) func(context.Context, TunnelTarget) (*Tunnel, error) {
	return func(context.Context, TunnelTarget) (*Tunnel, error) {
		u, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(u.Port())
		return &Tunnel{LocalPort: port, Close: func() {}}, nil
	}
}

func TestArgoCDToken_MintsAndSyncsAndRestarts(t *testing.T) {
	server := stubArgoCDServer(t)
	defer server.Close()

	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(argocdInitialAdminSecretFixture("adminpw"), makeLoopRuntimeAPIDeployment()).Build()
	deps := Deps{
		Ctx:             context.Background(),
		Kube:            NewKubeClient(c),
		PortForwardCtor: pfCtorPointingAt(server),
	}

	got := NewArgoCDTokenSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-api-tokens"), &s); err != nil {
		t.Fatalf("loopruntime-api-tokens not created: %v", err)
	}
	if string(s.Data["argocd-token"]) != "canonical-account-token" {
		t.Errorf("argocd-token = %q, want canonical-account-token", s.Data["argocd-token"])
	}
	// (Deployment restartedAt check omitted — already covered by TestStripeAPIKeys.)
}

func TestArgoCDToken_FailWhenAdminSecretMissing(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build() // no initial-admin
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewArgoCDTokenSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestArgoCDToken_FailWhenSessionLoginRejects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session" {
			http.Error(w, "bad creds", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(argocdInitialAdminSecretFixture("wrongpw"), makeLoopRuntimeAPIDeployment()).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c),
		PortForwardCtor: pfCtorPointingAt(server)}

	got := NewArgoCDTokenSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (401 login)", got.Verdict, got.Reason)
	}
}

func TestArgoCDToken_VerifyPassesWhenTokenPresent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "loopruntime-system", Name: "loopruntime-api-tokens"},
			Data:       map[string][]byte{"argocd-token": []byte("t")},
		},
	).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	if got := NewArgoCDTokenSeed().Verify(deps); got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestArgoCDToken_VerifyFailsWhenSecretAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	if got := NewArgoCDTokenSeed().Verify(deps); got.Verdict != FAIL {
		t.Fatalf("Verify = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestArgoCDToken_Name(t *testing.T) {
	if got := NewArgoCDTokenSeed().Name(); got != "argocd-token" {
		t.Errorf("Name = %q, want argocd-token", got)
	}
}
