// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package k8s

import (
	"fmt"
	"net/http"
	"sync"

	"k8s.io/client-go/tools/clientcmd"
)

// kubeconfigTokenSource serves the build cluster's bearer token, re-reading it
// from the kubeconfig on disk when the previous one is rejected.
//
// Issue #242 fix 3. loopruntime-api built its build-cluster client once at startup, so
// the bearer token was a value captured at process start. When the operator
// rotated `loopruntime-cluster-kubeconfigs.build.kubeconfig`, the mounted file changed
// and nothing else did: every deploy kept authenticating with a credential that
// had already been replaced, and the only recovery was restarting the pod. The
// watcher already NOTICED this (`ReportWatchError` logs "build cluster auth
// failed" and bumps ArgoWatcherTokenExpiredTotal on 401/403) — it just had no
// way to act on it.
//
// The native Kubernetes answer is `tokenFile:` in the kubeconfig, which
// client-go re-reads — that is how projected SA tokens rotate without a
// restart. This deployment writes the token INLINE (`token: <literal>`, see
// infra/helmfile/scripts/lib/scoped-kubeconfig.sh), so that path would require
// changing the generator, the Secret shape and the chart mount in lockstep,
// across every cluster that consumes a scoped kubeconfig. This achieves the
// same effect in the single place that can change without that blast radius.
//
// The value is CACHED and refreshed only on Invalidate(). Re-parsing the
// kubeconfig per request would put YAML parsing on the hot path of every
// workflow poll in order to serve a credential that changes weekly.
type kubeconfigTokenSource struct {
	path    string
	context string

	mu     sync.Mutex
	cached string
}

// newKubeconfigTokenSource returns a source reading `path`, honouring
// `contextName` when the kubeconfig holds several contexts (empty = current).
func newKubeconfigTokenSource(path, contextName string) *kubeconfigTokenSource {
	return &kubeconfigTokenSource{path: path, context: contextName}
}

// Token returns the cached bearer token, reading the kubeconfig on first use
// and after every Invalidate().
//
// A read failure is returned as an error rather than an empty string on
// purpose: an empty bearer token produces a 401 indistinguishable from an
// expired credential, which sends the operator to the rotation runbook for a
// file that is simply missing.
func (s *kubeconfigTokenSource) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != "" {
		return s.cached, nil
	}

	overrides := &clientcmd.ConfigOverrides{}
	if s.context != "" {
		overrides.CurrentContext = s.context
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: s.path},
		overrides,
	).ClientConfig()
	if err != nil {
		return "", fmt.Errorf("build kubeconfig %q: %w", s.path, err)
	}
	if cfg.BearerToken == "" {
		return "", fmt.Errorf("build kubeconfig %q carries no bearer token", s.path)
	}

	s.cached = cfg.BearerToken
	return s.cached, nil
}

// Invalidate drops the cached token so the next Token() re-reads the file.
// Called from the 401/403 path — the point at which the current credential is
// known to be rejected.
func (s *kubeconfigTokenSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = ""
}

// reloadingRoundTripper injects the current token and, on a 401/403, drops the
// cache so the NEXT request picks up a rotated credential.
//
// It deliberately does NOT retry the failed request in place. A retry inside
// the transport hides the rejection from `ReportWatchError`, which is what
// raises ArgoWatcherTokenExpiredTotal and pages the operator; silently healing
// while the underlying rotation problem persists is how a credential outage
// becomes invisible. One poll fails loudly, the next succeeds.
type reloadingRoundTripper struct {
	base   http.RoundTripper
	source *kubeconfigTokenSource
}

func (rt *reloadingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := rt.source.Token()
	if err != nil {
		return nil, err
	}

	// net/http's RoundTripper contract forbids mutating the request.
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+token)

	resp, err := rt.base.RoundTrip(clone)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		rt.source.Invalidate()
	}
	return resp, nil
}

// NewReloadingTransportWrapper returns a WrapTransport function for a
// rest.Config so the build-cluster client picks up a rotated kubeconfig without
// a pod restart (issue #242 fix 3).
//
// Wire it as:
//
//	restCfg.WrapTransport = build.NewReloadingTransportWrapper(path, context)
//	restCfg.BearerToken = ""   // the wrapper owns the Authorization header
//
// Clearing BearerToken matters: client-go would otherwise install its own
// bearer round-tripper carrying the STARTUP token, and whichever header is set
// last wins — reintroducing the exact staleness this removes.
func NewReloadingTransportWrapper(kubeconfigPath, contextName string) func(http.RoundTripper) http.RoundTripper {
	source := newKubeconfigTokenSource(kubeconfigPath, contextName)
	return func(base http.RoundTripper) http.RoundTripper {
		return &reloadingRoundTripper{base: base, source: source}
	}
}
