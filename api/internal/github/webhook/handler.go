// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package webhook handles incoming GitHub App webhook events.
// Validates HMAC-SHA256 signatures, parses event types, and dispatches
// to typed handlers for push, pull_request, and installation events.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/getloopscale/loopzero/api/internal/metrics"
)

const (
	maxBodySize     = 10 * 1024 * 1024 // 10MB
	signatureHeader = "X-Hub-Signature-256"
	eventHeader     = "X-GitHub-Event"
	deliveryHeader  = "X-GitHub-Delivery"
)

// dispatchTimeout bounds one event's processing. The handler has already
// answered GitHub, so nothing else would stop a stuck dispatch.
const dispatchTimeout = 5 * time.Minute

// PushHandler is called when a push event is received.
type PushHandler func(ctx context.Context, event PushEvent) error

// PullRequestHandler is called when a pull_request event is received.
type PullRequestHandler func(ctx context.Context, event PullRequestEvent) error

// InstallationHandler is called when an installation event is received.
type InstallationHandler func(ctx context.Context, event InstallationEvent) error

// Handler receives GitHub webhook events, validates signatures, and dispatches.
type Handler struct {
	secret         []byte
	logger         *slog.Logger
	lifetime       context.Context
	onPush         PushHandler
	onPullRequest  PullRequestHandler
	onInstallation InstallationHandler
}

// NewHandler creates a webhook handler with the given HMAC secret. lifetime
// is the server's: every dispatch derives its context from it, so shutdown
// cancels in-flight processing.
func NewHandler(lifetime context.Context, secret string, logger *slog.Logger) *Handler {
	return &Handler{
		secret:   []byte(secret),
		logger:   logger,
		lifetime: lifetime,
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

	// Plan J-09 T4.1 (2026-06-05): wire X-GitHub-Delivery as the
	// correlation_id so downstream logs (autodeploy, build submit,
	// release activation) share a single key per delivery. Falls back
	// to any X-Correlation-Id a front-proxy already injected.
	correlationID := deliveryID
	if correlationID == "" {
		correlationID = r.Header.Get("X-Correlation-Id")
	}
	log := h.logger.With(
		"delivery_id", deliveryID,
		"event", eventType,
		"correlation_id", correlationID,
	)

	// Validate signature presence
	if signature == "" {
		log.Warn("webhook: missing signature header")
		// Plan J-09 T4.2: status=invalid_signature so SLO queries can
		// surface attacker probes / misconfigured secret separately
		// from real traffic.
		metrics.GitHubWebhookDeliveriesTotal.WithLabelValues(eventType, "invalid_signature").Inc()
		http.Error(w, "Missing signature", http.StatusUnauthorized)
		return
	}

	// Read body (limited to maxBodySize)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		log.Error("webhook: read body failed", "error", err)
		metrics.GitHubWebhookDeliveriesTotal.WithLabelValues(eventType, "read_error").Inc()
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Validate HMAC-SHA256
	if !h.validateSignature(body, signature) {
		log.Warn("webhook: invalid signature")
		metrics.GitHubWebhookDeliveriesTotal.WithLabelValues(eventType, "invalid_signature").Inc()
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	_, span := otel.Tracer("loopzero-api").Start(r.Context(), "loopzero.api.webhook.github.receive",
		trace.WithAttributes(
			attribute.String("loopzero.webhook.event_type", eventType),
			attribute.String("loopzero.webhook.delivery_id", deliveryID),
		))
	defer span.End()

	log.Info("webhook: event received", "size", len(body))

	// Dispatch event (async — return 200 immediately)
	// Errors are logged, not returned to GitHub.
	metrics.GitHubWebhookDeliveriesTotal.WithLabelValues(eventType, "accepted").Inc()
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
	ctx, cancel := context.WithTimeout(h.lifetime, dispatchTimeout)
	defer cancel()

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
		if err := h.onPush(ctx, event); err != nil {
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
		if err := h.onPullRequest(ctx, event); err != nil {
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
		if err := h.onInstallation(ctx, event); err != nil {
			log.Error("webhook: installation handler failed", "error", err)
		}

	default:
		log.Debug("webhook: unhandled event type", "type", eventType)
	}
}
