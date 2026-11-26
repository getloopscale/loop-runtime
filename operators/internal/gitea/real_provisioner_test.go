// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitea

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The tenant GitOps repository is created auto-initialised on the tenant's
// branch, because ArgoCD clones it before anything is pushed.
func TestRealProvisioner_CreatesTheTenantRepoAutoInitialisedOnTheTenantBranch(t *testing.T) {
	var created map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/repos/loopruntime/tn-acme-gitops":
			w.WriteHeader(http.StatusNotFound)
		case "POST /api/v1/orgs/loopruntime/repos":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	url, err := NewRealProvisioner(srv.URL, "tok", "loopruntime").EnsureRepo(context.Background(), "tn-acme-gitops", "release")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if want := srv.URL + "/loopruntime/tn-acme-gitops.git"; url != want {
		t.Errorf("repoURL = %q, want %q", url, want)
	}
	if created["auto_init"] != true || created["default_branch"] != "release" {
		t.Errorf("create body = %v, want auto_init=true default_branch=release", created)
	}
}

// A tenant recreated under the name of a deleted one gets its archived
// repository back in a pushable state (CHG-002b).
func TestRealProvisioner_UnarchivesTheRepoOfARecreatedTenant(t *testing.T) {
	var unarchived bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/repos/loopruntime/tn-acme-gitops":
			_, _ = w.Write([]byte(`{"archived":true}`))
		case "PATCH /api/v1/repos/loopruntime/tn-acme-gitops":
			unarchived = true
			_, _ = w.Write([]byte(`{"archived":false}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	if _, err := NewRealProvisioner(srv.URL, "tok", "loopruntime").EnsureRepo(context.Background(), "tn-acme-gitops", "main"); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if !unarchived {
		t.Error("an archived tenant repository must be unarchived")
	}
}
