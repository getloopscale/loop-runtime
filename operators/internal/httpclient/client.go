// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	"github.com/loopzero/loopzero/operators/internal/constants"
)

// skipTLSVerify reads the OPERATOR_INSECURE_SKIP_TLS environment variable.
// Returns true (skip verification) unless the env var is explicitly set to "false" or "0".
// Default is true for backward compatibility with self-signed internal certificates.
//
// SEC-001/CODE-007: When internal services get proper cert-manager issued certificates,
// set OPERATOR_INSECURE_SKIP_TLS=false to enforce TLS verification.
func skipTLSVerify() bool {
	v := strings.ToLower(os.Getenv("OPERATOR_INSECURE_SKIP_TLS"))
	if v == "false" || v == "0" {
		return false
	}
	return true
}

// New returns an HTTP client configured for internal cluster communication.
// TLS verification is controlled by OPERATOR_INSECURE_SKIP_TLS env var (default: true).
// When internal services get proper cert-manager issued certificates, set
// OPERATOR_INSECURE_SKIP_TLS=false to enforce certificate verification.
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
