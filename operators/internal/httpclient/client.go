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
	"time"

	"github.com/loopzero/loopzero/operators/internal/constants"
)

// New returns an HTTP client configured for internal cluster communication.
// Uses InsecureSkipVerify because internal services use self-signed certificates.
// When internal services get proper cert-manager issued certificates, update this
// single function to remove InsecureSkipVerify.
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
				InsecureSkipVerify: true, //nolint:gosec // internal cluster services use self-signed certs
			},
		},
	}
}
