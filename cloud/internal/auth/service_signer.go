// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/loopzero/loopzero/pkg/serviceauth"
)

// ServiceSigner attaches the three dual-control headers
// (X-LoopZero-Signature, X-LoopZero-Timestamp, X-LoopZero-Nonce) to outbound
// engine requests on behalf of the authenticated dashboard user.
//
// The private key is held only by the cloud proxy — no other binary
// should accept the same secret. Materialized via ESO from a Pulumi-
// encrypted secret per ADR 2026-05-13-service-auth-ed25519-adoption
// § Key management.
type ServiceSigner struct {
	priv ed25519.PrivateKey
}

// NewServiceSigner constructs a signer from a raw Ed25519 private key.
// Returns an error if the key is the wrong length — guards against
// silent misconfiguration. The signer is safe for concurrent use.
func NewServiceSigner(priv ed25519.PrivateKey) (*ServiceSigner, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("cloud service-signer: invalid private key size %d (want %d)", len(priv), ed25519.PrivateKeySize)
	}
	return &ServiceSigner{priv: priv}, nil
}

// NewServiceSignerFromBase64 is the production constructor used in
// cloud bootstrap: the env var LOOPZERO_SERVICE_AUTH_ED25519_PRIVATE_KEY
// carries a base64-encoded Ed25519 private key (RFC § Key management).
//
// Returns an error if:
//   - The string is empty (zero-value env var)
//   - Decoding fails (malformed base64)
//   - The decoded bytes are not exactly ed25519.PrivateKeySize
//
// Per RFC Q5 (fail-closed): the cloud proxy must NOT start if the
// signer cannot be constructed. The caller in cmd/main.go propagates
// this error to the readiness probe so the LB stops sending traffic.
func NewServiceSignerFromBase64(encoded string) (*ServiceSigner, error) {
	if encoded == "" {
		return nil, errors.New("cloud service-signer: private key is empty (set LOOPZERO_SERVICE_AUTH_ED25519_PRIVATE_KEY)")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("cloud service-signer: private key is not valid base64: %w", err)
	}
	return NewServiceSigner(ed25519.PrivateKey(raw))
}

// ErrMissingIdentityHeaders is returned by Attach when the upstream
// caller has not set X-LoopZero-User-ID + X-LoopZero-Tenant. Signing without
// identity would produce a signature over an empty canonical payload,
// which the engine would reject — but the reject would surface in
// engine audit logs rather than the cloud proxy, hiding the bug.
// Failing here produces a clear cloud-side error and a 502 to the
// dashboard caller.
var ErrMissingIdentityHeaders = errors.New("cloud service-signer: missing X-LoopZero-User-ID or X-LoopZero-Tenant on outbound request")

// Attach signs the canonical payload derived from the request and
// adds the three signature headers in-place. Idempotent: calling
// Attach twice would overwrite the existing headers with a fresh
// signature (different nonce + timestamp), so calling it more than
// once is harmless but wasteful.
//
// The path is taken from r.URL.Path (not RawPath) — the engine middleware
// reads r.URL.Path verbatim, so the cloud signer must use the same
// representation. Query string is intentionally NOT included in the
// canonical payload (RFC: method + path only).
func (s *ServiceSigner) Attach(r *http.Request) error {
	userID := r.Header.Get("X-LoopZero-User-ID")
	tenantID := r.Header.Get("X-LoopZero-Tenant")
	if userID == "" || tenantID == "" {
		return ErrMissingIdentityHeaders
	}

	nonce, err := generateNonce()
	if err != nil {
		return fmt.Errorf("cloud service-signer: nonce generation failed: %w", err)
	}
	ts := time.Now().Unix()

	in := serviceauth.PayloadInput{
		UserID: userID, TenantID: tenantID, Timestamp: ts,
		Nonce: nonce, Method: r.Method, Path: r.URL.Path,
	}
	sig, err := serviceauth.Sign(s.priv, in)
	if err != nil {
		return fmt.Errorf("cloud service-signer: sign failed: %w", err)
	}

	r.Header.Set("X-LoopZero-Signature", sig)
	r.Header.Set("X-LoopZero-Timestamp", fmt.Sprintf("%d", ts))
	r.Header.Set("X-LoopZero-Nonce", nonce)
	return nil
}

// generateNonce produces 16 cryptographically-random bytes encoded as
// base64. 16 bytes = 128 bits → birthday collision probability is
// negligible (~2^-64 after 2^32 requests).
func generateNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}
