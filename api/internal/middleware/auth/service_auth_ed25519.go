// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package auth

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/getloopscale/contracts/serviceauth"
)

// serviceAuthSignaturePresent counts how often each request carried the
// X-Loop-Runtime-Signature header — drives the Phase 1 → Phase 2 cutover gate
// (operator waits for 100% present over 7 consecutive days per Q3).
var serviceAuthSignaturePresent = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "loop_runtime_service_auth_signature_present_total",
	Help: "Service-auth requests broken down by whether X-Loop-Runtime-Signature was present. Drives Phase 1 → Phase 2 cutover gate (7-day soak at 100% present rate per ADR 2026-05-13-service-auth-ed25519-adoption Q3).",
}, []string{"outcome"}) // outcome = present | absent

// serviceAuthSignatureRejected counts signed-request rejections by
// reason, so an operator can distinguish replay attacks (timestamp +
// nonce) from forgeries (invalid sig) in the dashboards.
var serviceAuthSignatureRejected = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "loop_runtime_service_auth_signature_rejected_total",
	Help: "Service-auth signed-request rejections by reason: invalid_signature, replay_window, replay_nonce, no_trusted_keys, malformed_input.",
}, []string{"reason"})

// signatureRejectReason classifies a rejection so the audit log and
// the Prometheus counter agree on terminology. Single source of truth
// for the strings the dashboards depend on.
type signatureRejectReason string

const (
	rejectInvalidSig     signatureRejectReason = "invalid_signature"
	rejectStaleTimestamp signatureRejectReason = "replay_window"
	rejectReplayNonce    signatureRejectReason = "replay_nonce"
	rejectNoTrustedKeys  signatureRejectReason = "no_trusted_keys"
	rejectMalformedInput signatureRejectReason = "malformed_input"
)

// verifyServiceAuthSignature evaluates the three signature headers
// (X-Loop-Runtime-Signature, X-Loop-Runtime-Timestamp, X-Loop-Runtime-Nonce) against the
// canonical payload built from the request. Returns ok=true iff every
// gate passes (signature valid + timestamp within window + nonce
// claimed). Returns ok=false with the rejection reason on any gate
// failure.
//
// The presence-vs-absence decision is made by the CALLER (auth middleware)
// — this function only runs when all three signature headers are present.
func (a *Authenticator) verifyServiceAuthSignature(r *http.Request, userID, tenantID string) (ok bool, reason signatureRejectReason) {
	sig := r.Header.Get("X-Loop-Runtime-Signature")
	tsStr := r.Header.Get("X-Loop-Runtime-Timestamp")
	nonce := r.Header.Get("X-Loop-Runtime-Nonce")

	tsInt, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return false, rejectMalformedInput
	}
	asserted := time.Unix(tsInt, 0)
	if err := a.cfg.ReplayWindow.Validate(asserted, time.Now()); err != nil {
		return false, rejectStaleTimestamp
	}

	// Verify the signature BEFORE claiming the nonce. Otherwise a
	// flood of forged signatures would fill the nonce cache and act
	// as a DoS vector (each forged request burns a (user, nonce) slot
	// for the full TTL).
	in := serviceauth.PayloadInput{
		UserID: userID, TenantID: tenantID, Timestamp: tsInt,
		Nonce: nonce, Method: r.Method, Path: r.URL.Path,
	}
	if err := serviceauth.Verify(a.cfg.Ed25519PublicKeys, sig, in); err != nil {
		if errors.Is(err, serviceauth.ErrNoTrustedKeys) {
			return false, rejectNoTrustedKeys
		}
		return false, rejectInvalidSig
	}

	// Claim the nonce only after the signature is proven valid.
	// Per-user scope means a flood of forged-sig requests cannot
	// exhaust another user's nonce namespace.
	if err := a.cfg.NonceStore.Claim(r.Context(), userID, nonce); err != nil {
		return false, rejectReplayNonce
	}
	return true, ""
}

// hasServiceAuthSignatureHeaders returns true iff all three signature
// headers are present. Used to distinguish "client signed but the sig
// is invalid" (a strong attack signal) from "client did not sign at
// all" (Phase 1 default).
func hasServiceAuthSignatureHeaders(r *http.Request) bool {
	return r.Header.Get("X-Loop-Runtime-Signature") != "" &&
		r.Header.Get("X-Loop-Runtime-Timestamp") != "" &&
		r.Header.Get("X-Loop-Runtime-Nonce") != ""
}
