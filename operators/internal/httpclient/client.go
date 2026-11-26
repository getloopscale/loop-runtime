// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package httpclient provides a shared HTTP client for internal cluster communication.
//
// All operator HTTP clients that communicate with internal services (Gitea, ArgoCD, API)
// should use New() instead of constructing their own http.Client with InsecureSkipVerify.
// This centralizes TLS configuration so that when internal services get proper
// cert-manager issued certificates, a single change hardens all clients (ARCH-005).
package httpclient

import (
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

// skipTLSVerify reads the OPERATOR_INSECURE_SKIP_TLS environment variable.
// Returns true (skip verification) ONLY when the env var is explicitly set
// to "true" or "1". Default is false (secure — verify TLS) per T2.5 / SEC-001.
//
// COMP-007 / SEC-001 / T2.5: previously this was inverted (default skip=true),
// which meant every operator HTTP client silently disabled TLS verification
// out of the box. Now: secure-by-default; opt in to skip via OPERATOR_INSECURE_SKIP_TLS=true
// when running against self-signed internal certs that haven't yet been
// migrated to cert-manager.
func skipTLSVerify() bool {
	v := strings.ToLower(os.Getenv("OPERATOR_INSECURE_SKIP_TLS"))
	return v == "true" || v == "1"
}

// New returns an HTTP client configured for internal cluster communication.
// TLS verification is enabled by default; set OPERATOR_INSECURE_SKIP_TLS=true
// to opt in to skip-verify (e.g., for dev environments using self-signed certs
// not yet rotated to cert-manager-issued ones).
func New() *http.Client {
	return NewWithTimeout(constants.HTTPTimeoutDefault)
}

// NewWithTimeout returns an HTTP client with a custom timeout.
// The TLS configuration is the same as New().
func NewWithTimeout(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: skipTLSVerify(), //nolint:gosec // controlled via OPERATOR_INSECURE_SKIP_TLS env var
			},
		},
	}
}
