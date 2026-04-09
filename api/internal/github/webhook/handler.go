// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package webhook handles incoming GitHub App webhook events.
// Validates HMAC-SHA256 signatures, parses event types, and dispatches
// to typed handlers for push, pull_request, and installation events.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	maxBodySize       = 10 * 1024 * 1024 // 10MB
	signatureHeader   = "X-Hub-Signature-256"
	eventHeader       = "X-GitHub-Event"
	deliveryHeader    = "X-GitHub-Delivery"
)

// PushHandler is called when a push event is received.
type PushHandler func(event PushEvent) error

// PullRequestHandler is called when a pull_request event is received.
type PullRequestHandler func(event PullRequestEvent) error

// InstallationHandler is called when an installation event is received.
type InstallationHandler func(event InstallationEvent) error

// Handler receives GitHub webhook events, validates signatures, and dispatches.
type Handler struct {
	secret           []byte
	logger           *slog.Logger
	onPush           PushHandler
	onPullRequest    PullRequestHandler
	onInstallation   InstallationHandler
}

// NewHandler creates a webhook handler with the given HMAC secret.
func NewHandler(secret string, logger *slog.Logger) *Handler {
	return &Handler{
		secret: []byte(secret),
		logger: logger,
	}
}

// OnPush registers a handler for push events.
func (h *Handler) OnPush(fn PushHandler) *Handler {
	h.onPush = fn
	return h
}

// OnPullRequest registers a handler for pull_request events.
func (h *Handler) OnPullRequest(fn PullRequestHandler) *Handler {
	h.onPullRequest = fn
	return h
}

// OnInstallation registers a handler for installation events.
func (h *Handler) OnInstallation(fn InstallationHandler) *Handler {
	h.onInstallation = fn
	return h
}

// ServeHTTP handles POST /api/v1/webhooks/github.
// Validates HMAC-SHA256, parses event type, dispatches to handler.
// Always returns 200 to GitHub (async processing — errors are logged, not returned).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	deliveryID := r.Header.Get(deliveryHeader)
	eventType := r.Header.Get(eventHeader)
	signature := r.Header.Get(signatureHeader)

	log := h.logger.With("delivery_id", deliveryID, "event", eventType)

	// Validate signature presence
	if signature == "" {
		log.Warn("webhook: missing signature header")
		http.Error(w, "Missing signature", http.StatusUnauthorized)
		return
	}

	// Read body (limited to maxBodySize)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		log.Error("webhook: read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate HMAC-SHA256
	if !h.validateSignature(body, signature) {
		log.Warn("webhook: invalid signature")
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	log.Info("webhook: event received", "size", len(body))

	// Dispatch event (async — return 200 immediately)
	// Errors are logged, not returned to GitHub.
	go h.dispatch(eventType, body, log)

	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"ok":true}`)
}

// validateSignature checks HMAC-SHA256 signature.
// GitHub sends: sha256=<hex-encoded HMAC>
func (h *Handler) validateSignature(body []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sigHex := strings.TrimPrefix(signature, "sha256=")
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, h.secret)
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(sigBytes, expected)
}

// dispatch routes the event to the appropriate handler.
func (h *Handler) dispatch(eventType string, body []byte, log *slog.Logger) {
	switch eventType {
	case "push":
		if h.onPush == nil {
			return
		}
		var event PushEvent
		if err := json.Unmarshal(body, &event); err != nil {
			log.Error("webhook: parse push event failed", "error", err)
			return
		}
		if err := h.onPush(event); err != nil {
			log.Error("webhook: push handler failed", "error", err, "repo", event.Repository.FullName)
		}

	case "pull_request":
		if h.onPullRequest == nil {
			return
		}
		var event PullRequestEvent
		if err := json.Unmarshal(body, &event); err != nil {
			log.Error("webhook: parse pull_request event failed", "error", err)
			return
		}
		if err := h.onPullRequest(event); err != nil {
			log.Error("webhook: pull_request handler failed", "error", err, "repo", event.Repository.FullName)
		}

	case "installation":
		if h.onInstallation == nil {
			return
		}
		var event InstallationEvent
		if err := json.Unmarshal(body, &event); err != nil {
			log.Error("webhook: parse installation event failed", "error", err)
			return
		}
		if err := h.onInstallation(event); err != nil {
			log.Error("webhook: installation handler failed", "error", err)
		}

	default:
		log.Debug("webhook: unhandled event type", "type", eventType)
	}
}
