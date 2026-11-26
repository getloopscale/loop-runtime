// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Unit tests for the kubeRootCAFetcher (T1.1 of plan
// 2026-05-25-live-ca-fetch-clustercredential).
//
// These tests use client-go's fake.Clientset so they run in milliseconds
// without envtest, covering all error branches deterministically.
//
// Note: the public-API tests live here; the EC-13 "insecure bootstrap"
// behavior is tested via an httptest server in
// TestKubeRootCAFetcher_InsecureBootstrap below — that's the only test
// that exercises the real kubernetes.NewForConfig + transport stack.
package platform

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

const samplePEM = `-----BEGIN CERTIFICATE-----
MIIBhDCCASmgAwIBAgIQXyz7w0FjmFm3xQDPxL7AejAKBggqhkjOPQQDAjAjMSEw
HwYDVQQDDBhrM3Mtc2VydmVyLWNhQDE3Mzk3MTI3MjUwHhcNMjUwMTE2MDQyMzMy
WhcNMzUwMTE0MDQyMzMyWjAjMSEwHwYDVQQDDBhrM3Mtc2VydmVyLWNhQDE3Mzk3
MTI3MjUwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAAQF0xT9TF5VqgHBJzD1y7uA
M+kEf+rzqkRPGcUOcMA2GcGNyJ/jqfWHkHv5LhHC0DrEnsvU72fNcuC4qK8r38Lz
o0IwQDAOBgNVHQ8BAf8EBAMCAqQwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQU
SrJVZGT8GnppA1aoxnxOTRA5QqkwCgYIKoZIzj0EAwIDSAAwRQIgYBGl/zT0WAQK
PMOgGoLcGogq7zM6q5IMVqXkUE7PNlMCIQDw7+xktbI3DMSCFVdHpnIVlAehBSCJ
PfMxiWB3aXKqAA==
-----END CERTIFICATE-----`

// newFakeRestConfigFromFake creates a *rest.Config that wraps a
// fake.Clientset. Most kubeRootCAFetcher tests don't need a real HTTP
// roundtripper — we override fetcher's internal call by injecting a custom
// kubernetes.Interface via a test-only seam.
//
// Production code calls kubernetes.NewForConfig(restConfig). For tests we
// need to bypass that call to inject a fake client; we do this by exposing
// kubeRootCAFetcherFromClient as an unexported variant used only by tests.
// Coverage tradeoff: NewForConfig itself is exercised by the real
// InsecureBootstrap test below.

func TestKubeRootCAFetcher_Success(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "default",
		},
		Data: map[string]string{"ca.crt": samplePEM},
	}
	cs := fake.NewSimpleClientset(cm)

	got, err := fetchClusterCAWithClient(context.Background(), cs, "default")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if string(got) != samplePEM {
		t.Errorf("returned PEM does not match input")
	}
}

func TestKubeRootCAFetcher_ConfigMapMissing(t *testing.T) {
	cs := fake.NewSimpleClientset() // no ConfigMap

	_, err := fetchClusterCAWithClient(context.Background(), cs, "default")
	if err == nil {
		t.Fatal("expected error when ConfigMap missing")
	}
	if !strings.Contains(err.Error(), "get configmap") {
		t.Errorf("error message should mention 'get configmap', got: %v", err)
	}
}

func TestKubeRootCAFetcher_CAKeyMissing(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "default",
		},
		Data: map[string]string{"other.key": "value"}, // no ca.crt
	}
	cs := fake.NewSimpleClientset(cm)

	_, err := fetchClusterCAWithClient(context.Background(), cs, "default")
	if err == nil {
		t.Fatal("expected error when ca.crt key missing")
	}
	if !strings.Contains(err.Error(), "ca.crt key missing or empty") {
		t.Errorf("expected 'ca.crt key missing or empty', got: %v", err)
	}
}

func TestKubeRootCAFetcher_CAEmpty(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "default",
		},
		Data: map[string]string{"ca.crt": ""},
	}
	cs := fake.NewSimpleClientset(cm)

	_, err := fetchClusterCAWithClient(context.Background(), cs, "default")
	if err == nil {
		t.Fatal("expected error when ca.crt is empty string")
	}
	if !strings.Contains(err.Error(), "ca.crt key missing or empty") {
		t.Errorf("expected 'ca.crt key missing or empty', got: %v", err)
	}
}

func TestKubeRootCAFetcher_NotPEM(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "default",
		},
		Data: map[string]string{"ca.crt": "this is not a PEM block"},
	}
	cs := fake.NewSimpleClientset(cm)

	_, err := fetchClusterCAWithClient(context.Background(), cs, "default")
	if err == nil {
		t.Fatal("expected error when ca.crt is not PEM")
	}
	if !strings.Contains(err.Error(), "not a PEM block") {
		t.Errorf("expected 'not a PEM block', got: %v", err)
	}
}

func TestKubeRootCAFetcher_ContextCancelled(t *testing.T) {
	// fake.Clientset does NOT honor ctx cancellation (it's synchronous
	// in-memory). To get a meaningful test we hit a real TLS server via
	// FetchClusterCA + httptest, and set a deadline that expires before
	// the handler can respond.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/namespaces/default/configmaps/kube-root-ca.crt", func(w http.ResponseWriter, r *http.Request) {
		// Block longer than the test deadline.
		select {
		case <-r.Context().Done():
			// context cancelled; just return without writing
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	parsedURL, _ := url.Parse(server.URL)
	restCfg := &rest.Config{Host: parsedURL.String()}

	f := newKubeRootCAFetcher()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := f.FetchClusterCA(ctx, restCfg)
	if err == nil {
		t.Fatal("expected error when context deadline exceeded")
	}
	// Error surface from client-go usually contains "context deadline" or
	// "context canceled" depending on timing.
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context-related error, got: %v", err)
	}
}

func TestKubeRootCAFetcher_CustomNamespace(t *testing.T) {
	// CR points fetcher at a non-default namespace (CIS Level 2 hardened
	// clusters that lock down `default`).
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "kube-public",
		},
		Data: map[string]string{"ca.crt": samplePEM},
	}
	cs := fake.NewSimpleClientset(cm)

	got, err := fetchClusterCAWithClient(context.Background(), cs, "kube-public")
	if err != nil {
		t.Fatalf("unexpected error for custom namespace: %v", err)
	}
	if string(got) != samplePEM {
		t.Errorf("PEM mismatch in custom namespace test")
	}
}

// EC-15: nil-protect — DI bug in tests should not panic.
func TestKubeRootCAFetcher_NilRestConfig(t *testing.T) {
	f := newKubeRootCAFetcher()
	_, err := f.FetchClusterCA(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when restConfig is nil, not panic")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("error should mention 'nil', got: %v", err)
	}
}

// EC-16: strip UTF-8 BOM prepended by some cert pipelines.
func TestKubeRootCAFetcher_PEMWithBOM(t *testing.T) {
	bom := "\xef\xbb\xbf"
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-root-ca.crt", Namespace: "default"},
		Data:       map[string]string{"ca.crt": bom + samplePEM},
	}
	cs := fake.NewSimpleClientset(cm)

	got, err := fetchClusterCAWithClient(context.Background(), cs, "default")
	if err != nil {
		t.Fatalf("expected success with BOM stripped, got: %v", err)
	}
	if strings.HasPrefix(string(got), bom) {
		t.Errorf("returned PEM still has BOM prefix")
	}
	if !strings.HasPrefix(string(got), "-----BEGIN") {
		t.Errorf("returned PEM should start with -----BEGIN after BOM strip")
	}
}

// EC-13 (CRITICAL): the production fetcher MUST use Insecure: true on the
// cloned rest.Config so that bootstrap circular dependency does not block
// the fetch after CA rotation. We prove this by spinning up an httptest TLS
// server with a self-signed cert that DOESN'T match the caData in the
// restConfig. If the fetcher used the restConfig caData for verification,
// the connection would fail with x509. With Insecure: true, the connection
// succeeds and the ConfigMap is returned.
//
// This test exercises the REAL kubernetes.NewForConfig + TLS stack —
// complementing the fake.Clientset tests above.
func TestKubeRootCAFetcher_InsecureBootstrap(t *testing.T) {
	// Build a minimal in-memory K8s API server that serves
	// GET /api/v1/namespaces/default/configmaps/kube-root-ca.crt
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "default",
		},
		Data: map[string]string{"ca.crt": samplePEM},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	encoder := json.NewSerializerWithOptions(
		json.DefaultMetaFactory, scheme, scheme,
		json.SerializerOptions{Yaml: false, Pretty: false, Strict: false},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/namespaces/default/configmaps/kube-root-ca.crt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = encoder.Encode(cm, w)
	})
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	// Build a restConfig whose CAData is BOGUS — does NOT match server.TLS.
	// Production: same situation when k3s rotates CA and snapshot is stale.
	bogusCA := []byte(samplePEM) // valid PEM structure but unrelated cert
	parsedURL, _ := url.Parse(server.URL)
	restCfg := &rest.Config{
		Host: parsedURL.String(),
		TLSClientConfig: rest.TLSClientConfig{
			CAData: bogusCA,
		},
		// No bearer token needed for unauthenticated httptest endpoint.
	}

	f := newKubeRootCAFetcher()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := f.FetchClusterCA(ctx, restCfg)
	if err != nil {
		t.Fatalf("EC-13 regression: fetcher failed against bogus CA — Insecure clone is broken: %v", err)
	}
	if string(got) != samplePEM {
		t.Errorf("returned PEM does not match server-side ConfigMap data")
	}
}

// Helper: prove samplePEM is a valid PEM block (sanity for test setup).
func TestSamplePEMIsValid(t *testing.T) {
	block, _ := pem.Decode([]byte(samplePEM))
	if block == nil {
		t.Fatal("samplePEM does not decode as PEM block — test setup bug")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		// Acceptable: synthetic PEM may not parse as cert; we only need PEM framing
		// for our fetcher's --BEGIN check.
		t.Logf("note: samplePEM block parses as raw PEM but not as x509 cert (OK for our needs): %v", err)
	}
	_ = tls.Config{} // silence unused import
}
