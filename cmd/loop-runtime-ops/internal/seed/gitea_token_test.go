// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// giteaAdminWithCreds builds gitea-admin with (username, password, existing token).
func giteaAdminWithCreds(user, pw, existingToken string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "gitea-admin"},
		Data: map[string][]byte{
			"username": []byte(user),
			"password": []byte(pw),
			"token":    []byte(existingToken),
		},
	}
}

// stubGiteaServer stands in for the real Gitea. It implements just enough of
// the API to exercise the 4-layer chain. The `orgExists` flag simulates
// whether GET /orgs/loop-runtime returns 200 or 404 (before POST /orgs).
func stubGiteaServer(t *testing.T, orgExists bool) (*httptest.Server, *string) {
	t.Helper()
	newToken := "canonical-gitea-sha1"
	orgExistsRef := &orgExists
	mintedRef := new(string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/version":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/v1/user" && r.Method == http.MethodGet:
			u, p, ok := r.BasicAuth()
			if !ok || u == "" || p == "" {
				http.Error(w, "unauth", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/v1/orgs/loop-runtime" && r.Method == http.MethodGet:
			if *orgExistsRef {
				w.WriteHeader(http.StatusOK)
			} else {
				http.Error(w, "not found", http.StatusNotFound)
			}
		case r.URL.Path == "/api/v1/orgs" && r.Method == http.MethodPost:
			*orgExistsRef = true
			w.WriteHeader(http.StatusCreated)
		case strings.HasPrefix(r.URL.Path, "/api/v1/users/") && strings.HasSuffix(r.URL.Path, "/tokens") && r.Method == http.MethodGet:
			// Return one legacy token so DELETE loop is exercised.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 42, "name": "loop-runtime-api-token"},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/users/") && strings.Contains(r.URL.Path, "/tokens/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/api/v1/users/") && strings.HasSuffix(r.URL.Path, "/tokens") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			*mintedRef = newToken
			_ = json.NewEncoder(w).Encode(map[string]string{"sha1": newToken})
		default:
			http.Error(w, "404", http.StatusNotFound)
		}
	}))
	return server, mintedRef
}

func TestGiteaToken_HappyPath_CreatesOrgAndMintsAndSyncs3Secrets(t *testing.T) {
	server, minted := stubGiteaServer(t, false /* org does NOT exist yet */)
	defer server.Close()

	// Pre-seed loop-runtime-api and loop-runtime-tenant-operator Deployments so Layer 4 rolls.
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		giteaAdminWithCreds("loop-runtime-admin", "adminpw", "stale-token"),
		makeLoopRuntimeAPIDeployment(),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "loop-runtime-api-tokens"},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "loop-runtime-tenant-operator"},
		},
	).Build()
	deps := Deps{
		Ctx:             context.Background(),
		Kube:            NewKubeClient(c),
		PortForwardCtor: pfCtorPointingAt(server),
	}

	got := NewGiteaTokenSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if *minted != "canonical-gitea-sha1" {
		t.Errorf("token not minted via server stub")
	}
	// Assert 3 consumer Secrets carry the new token.
	for _, want := range []struct {
		ns, name, key string
	}{
		{"loop-runtime-system", "gitea-admin", "token"},
		{"loop-runtime-system", "loop-runtime-api-tokens", "gitea-token"},
		{"loop-runtime-system", "loop-runtime-tenant-operator", "GITEA_TOKEN"},
	} {
		var s corev1.Secret
		if err := c.Get(context.Background(), fakeKey(want.ns, want.name), &s); err != nil {
			t.Fatalf("get %s/%s: %v", want.ns, want.name, err)
		}
		if string(s.Data[want.key]) != "canonical-gitea-sha1" {
			t.Errorf("%s/%s.%s = %q, want canonical-gitea-sha1", want.ns, want.name, want.key, s.Data[want.key])
		}
	}
}

func TestGiteaToken_FailWhenGiteaAdminMissing(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewGiteaTokenSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestGiteaToken_FailWhenBasicAuthRejects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/version" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "unauth", http.StatusUnauthorized)
	}))
	defer server.Close()
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		giteaAdminWithCreds("u", "wrongpw", ""),
	).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c),
		PortForwardCtor: pfCtorPointingAt(server)}

	got := NewGiteaTokenSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestGiteaToken_VerifyPassesWhen3SecretsFilled(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		giteaAdminWithCreds("u", "p", "t1"),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "loop-runtime-api-tokens"},
			Data: map[string][]byte{"gitea-token": []byte("t2")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "loop-runtime-tenant-operator"},
			Data: map[string][]byte{"GITEA_TOKEN": []byte("t3")}},
	).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	if got := NewGiteaTokenSeed().Verify(deps); got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestGiteaToken_VerifyFailsWhenAnySecretEmpty(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(
		giteaAdminWithCreds("u", "p", "t1"),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "loop-runtime-api-tokens"},
			Data: map[string][]byte{}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "loop-runtime-tenant-operator"},
			Data: map[string][]byte{"GITEA_TOKEN": []byte("t3")}},
	).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	if got := NewGiteaTokenSeed().Verify(deps); got.Verdict != FAIL {
		t.Fatalf("Verify = %s (%s), want FAIL (loop-runtime-api-tokens empty)", got.Verdict, got.Reason)
	}
}

func TestGiteaToken_Name(t *testing.T) {
	if got := NewGiteaTokenSeed().Name(); got != "gitea-token" {
		t.Errorf("Name = %q, want gitea-token", got)
	}
}
