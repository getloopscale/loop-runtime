// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package platform holds the reconcilers that bootstrap and maintain the
// platform itself — cluster credentials, CA material and the PlatformBootstrap
// lifecycle.
//
// kubeRootCAFetcher reads the live cluster CA from the target cluster's
// kube-root-ca.crt ConfigMap on every reconcile. Used by
// ClusterCredentialReconciler.reconcileArgoCDClusterSecret to populate the
// ArgoCD cluster Secret's tlsClientConfig.caData with a fresh value,
// instead of relying on the kubeconfig snapshot captured at bootstrap.
//
// Plan: 2026-05-25-live-ca-fetch-clustercredential-plan (v1.1)
// ADRs: D1 (live fetch) + D2 (graceful fallback) + D3 (DIP) + EC-13 (insecure bootstrap)
//
// Why insecure-bootstrap (EC-13):
// the restConfig passed in carries CAData from the same stale snapshot we
// are trying to refresh. Using that CAData to validate the connection
// that fetches the fresh CA is logically impossible after rotation
// (TLS handshake fails first → fetch never runs → Op B silently no-op).
// We clone the restConfig with TLSClientConfig{Insecure:true} for the
// fetch call ONLY. The payload IS the public CA itself — there's nothing
// to keep secret. ArgoCD then uses the fetched CA for all subsequent
// secure traffic. The insecure window is exactly one GET per reconcile,
// never user data, never write operations. See ADR D1 trade-off.
package platform

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// caFetcher abstracts how the reconciler obtains the live cluster CA.
// Production uses kubeRootCAFetcher (calls client-go); tests inject fakes
// (see ca_fetcher_test_helpers.go).
//
// DIP per ADR D3: the reconciler depends on this interface, not on
// concrete client-go calls — enables unit testing all error branches in
// milliseconds without envtest.
type caFetcher interface {
	FetchClusterCA(ctx context.Context, restConfig *rest.Config) ([]byte, error)
}

// kubeRootCAFetcher is the production implementation: it connects to the
// target cluster and reads `default/kube-root-ca.crt` ConfigMap. The
// returned []byte is the raw PEM block (not base64-encoded) — the caller
// is responsible for encoding before storing in ArgoCD's Secret format.
type kubeRootCAFetcher struct {
	// namespace where kube-root-ca.crt lives on the target cluster.
	// Production: "default" (universal K8s default since 1.21).
	// Hardened clusters (CIS Level 2) may need an alternate namespace
	// such as kube-public.
	namespace string
}

func newKubeRootCAFetcher() *kubeRootCAFetcher {
	return &kubeRootCAFetcher{namespace: "default"}
}

// utf8BOM is the UTF-8 byte order mark sometimes prepended by cert
// generators that write files in BOM-aware mode (EC-16). Stripped before
// PEM validation so the "-----BEGIN" prefix check matches.
const utf8BOM = "\xef\xbb\xbf"

// FetchClusterCA reads kube-root-ca.crt from the target cluster and
// returns the ca.crt PEM bytes. Honors ctx cancellation.
//
// Error cases (all return wrapped error, never panic):
//   - restConfig is nil (EC-15)
//   - kubernetes.NewForConfig fails
//   - ConfigMap missing (NotFound)
//   - data["ca.crt"] key missing
//   - data["ca.crt"] empty string
//   - data["ca.crt"] not a PEM block (no -----BEGIN prefix after BOM strip)
//   - context cancelled or deadline exceeded
//
// On success returns (PEM bytes, nil). On failure returns (nil, error).
// Never returns (nil, nil) — caller can rely on this invariant.
func (f *kubeRootCAFetcher) FetchClusterCA(ctx context.Context, restConfig *rest.Config) ([]byte, error) {
	// EC-15: nil-protect. A DI bug in tests would otherwise panic in
	// rest.CopyConfig. Production wiring never passes nil but defending
	// at the boundary is cheap.
	if restConfig == nil {
		return nil, fmt.Errorf("kube-root-ca fetcher: restConfig is nil")
	}

	// EC-13 (CRITICAL): bootstrap circular dependency defense.
	// Clone the config and force Insecure for THIS one call. See package
	// doc above + ADR D1 for the trade-off rationale.
	cfg := rest.CopyConfig(restConfig)
	cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube-root-ca fetcher: create client: %w", err)
	}

	return fetchClusterCAWithClient(ctx, cs, f.namespace)
}

// fetchClusterCAWithClient is the inner implementation that takes an
// already-constructed kubernetes.Interface. Exposed as an unexported
// package-level function so unit tests can inject fake.Clientset without
// going through kubernetes.NewForConfig (which requires real TLS handshake).
//
// The InsecureBootstrap test (ca_fetcher_test.go) exercises the full
// FetchClusterCA path including NewForConfig + transport, ensuring the
// EC-13 clone behavior holds against a real httptest TLS server.
func fetchClusterCAWithClient(ctx context.Context, cs kubernetes.Interface, namespace string) ([]byte, error) {
	cm, err := cs.CoreV1().ConfigMaps(namespace).
		Get(ctx, "kube-root-ca.crt", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("kube-root-ca fetcher: get configmap: %w", err)
	}
	pem, ok := cm.Data["ca.crt"]
	if !ok || pem == "" {
		return nil, fmt.Errorf("kube-root-ca fetcher: ca.crt key missing or empty")
	}
	// EC-16: strip UTF-8 BOM if present (some cert pipelines write it).
	pem = strings.TrimPrefix(pem, utf8BOM)
	// Sanity: PEM block must start with `-----BEGIN`. Catches HTML proxy
	// fronting (login page in response body) + binary garbage corruption.
	if !strings.HasPrefix(pem, "-----BEGIN") {
		return nil, fmt.Errorf("kube-root-ca fetcher: ca.crt is not a PEM block")
	}
	return []byte(pem), nil
}
