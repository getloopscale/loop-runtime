// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// edgeDeps points the edge probe at srv, with the given data-plane services deployed.
func edgeDeps(t *testing.T, srv *httptest.Server, services ...string) Deps {
	t.Helper()
	var objs []ctrlclient.Object
	for _, svc := range services {
		objs = append(objs, readyDeployment(dataPlaneNamespace, svc))
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(objs...).Build()
	return Deps{
		Ctx: context.Background(), Kube: NewKubeClient(c), CtrlClient: c,
		Host: Host{Getenv: func(k string) string {
			if k == dataPlaneEdgeURLEnv {
				return srv.URL
			}
			return ""
		}},
	}
}

// forwardAuthDenial answers the way loop-console's forward-auth handler does without a key.
func forwardAuthDenial(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED","message":"Invalid or missing API key."}`))
}

func TestDataPlaneEdgePassesWhenForwardAuthDeniesAnAnonymousCall(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		forwardAuthDenial(w)
	}))
	defer srv.Close()

	got := NewDataPlaneEdgeSeed().Run(edgeDeps(t, srv, "loop-runtime-memory", "loop-runtime-rag", "loop-runtime-lens"))

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	want := "/v1/memory/ /v1/rag/ /v1/lens/"
	if strings.Join(paths, " ") != want {
		t.Errorf("probed %v, want %s", paths, want)
	}
}

func TestDataPlaneEdgeFailsWhenTheServiceAnswersWithoutForwardAuth(t *testing.T) {
	// The service's own 401 means the edge let an anonymous call reach it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED","message":"Bearer token required"}`))
	}))
	defer srv.Close()

	got := NewDataPlaneEdgeSeed().Run(edgeDeps(t, srv, "loop-runtime-memory"))

	if got.Verdict != FAIL || !strings.Contains(got.Reason, "/v1/memory/") {
		t.Fatalf("Verdict = %s (%s), want FAIL naming /v1/memory/", got.Verdict, got.Reason)
	}
}

func TestDataPlaneEdgeFailsWhenTheEdgeDoesNotRouteToAnAuthorizer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	got := NewDataPlaneEdgeSeed().Run(edgeDeps(t, srv, "loop-runtime-rag"))

	if got.Verdict != FAIL || !strings.Contains(got.Reason, "502") {
		t.Fatalf("Verdict = %s (%s), want FAIL reporting 502", got.Verdict, got.Reason)
	}
}

func TestDataPlaneEdgeProbesOnlyDeployedServices(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		forwardAuthDenial(w)
	}))
	defer srv.Close()

	NewDataPlaneEdgeSeed().Run(edgeDeps(t, srv, "loop-runtime-lens"))

	if strings.Join(paths, " ") != "/v1/lens/" {
		t.Errorf("probed %v, want only /v1/lens/", paths)
	}
}

func TestDataPlaneEdgeSkipsWhenNoDataPlaneServiceIsDeployed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { forwardAuthDenial(w) }))
	defer srv.Close()

	if got := NewDataPlaneEdgeSeed().Run(edgeDeps(t, srv)); got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}
