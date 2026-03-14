package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// BuildInfo holds the minimal build information needed by the stream handler.
// This avoids importing the db package (which would create an import cycle).
type BuildInfo struct {
	ID     string
	Status string
}

// BuildStoreReader is a minimal interface for reading build status.
// Separated from the full BuildStore to respect ISP.
// Returns BuildInfo to avoid coupling with the db package.
type BuildStoreReader interface {
	GetByID(ctx context.Context, id string) (*BuildInfo, error)
}

// StreamOption configures StreamHandler behavior (used for testing).
type StreamOption func(*StreamHandler)

// WithHeartbeatInterval overrides the default heartbeat interval.
func WithHeartbeatInterval(d time.Duration) StreamOption {
	return func(h *StreamHandler) {
		h.heartbeatInterval = d
	}
}

// WithPollInterval overrides the default poll interval.
func WithPollInterval(d time.Duration) StreamOption {
	return func(h *StreamHandler) {
		h.pollInterval = d
	}
}

// WithLogger sets a structured logger for the handler.
func WithLogger(l *slog.Logger) StreamOption {
	return func(h *StreamHandler) {
		h.logger = l
	}
}

// StreamHandler handles GET /api/v1/builds/:id/stream.
// It streams SSE events for a build, supporting reconnection via Last-Event-ID.
type StreamHandler struct {
	eventStore        EventStore
	buildStore        BuildStoreReader
	logger            *slog.Logger
	heartbeatInterval time.Duration
	pollInterval      time.Duration
}

// NewStreamHandler creates a StreamHandler with the given dependencies.
func NewStreamHandler(eventStore EventStore, buildStore BuildStoreReader, opts ...StreamOption) *StreamHandler {
	h := &StreamHandler{
		eventStore:        eventStore,
		buildStore:        buildStore,
		logger:            slog.Default(),
		heartbeatInterval: 15 * time.Second,
		pollInterval:      1 * time.Second,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeHTTP handles the SSE stream request.
func (h *StreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, `{"code":"MISSING_PARAM","message":"build id is required"}`, http.StatusBadRequest)
		return
	}

	// Verify build exists
	build, err := h.buildStore.GetByID(r.Context(), buildID)
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(loopzeroerrors.ErrBuildNotFound.ToJSON())
			return
		}
		h.logger.Error("stream handler: failed to fetch build",
			"build_id", buildID,
			"error", err,
		)
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"failed to fetch build"}`, http.StatusInternalServerError)
		return
	}

	// Check if ResponseWriter supports flushing (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Determine starting event ID from Last-Event-ID header
	var lastEventID int64
	if leID := r.Header.Get("Last-Event-ID"); leID != "" {
		parsed, parseErr := strconv.ParseInt(leID, 10, 64)
		if parseErr == nil {
			lastEventID = parsed
		}
	}

	// Handle Last-Event-ID: check if journal has events after that ID
	if lastEventID > 0 {
		events, getErr := h.eventStore.GetAfter(r.Context(), buildID, lastEventID)
		if getErr != nil {
			return
		}

		if len(events) == 0 && statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
			// Journal expired: we had a Last-Event-ID but no events found,
			// and build is already terminal
			writeSSEEvent(w, flusher, "0", "JOURNAL_EXPIRED", json.RawMessage(`{}`))
			return
		}

		if len(events) == 0 && !statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
			// Journal expired but build still running — send JOURNAL_EXPIRED
			// and let the stream continue polling for new events
			writeSSEEvent(w, flusher, "0", "JOURNAL_EXPIRED", json.RawMessage(`{}`))
		}

		// Send replayed events
		for _, ev := range events {
			writeSSEEvent(w, flusher, fmt.Sprintf("%d", ev.ID), ev.EventType, ev.Data)
			lastEventID = ev.ID
		}

		// If build is terminal, close after replay
		if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
			return
		}
	} else {
		// No Last-Event-ID: replay all events from the beginning
		events, getErr := h.eventStore.GetAfter(r.Context(), buildID, 0)
		if getErr != nil {
			return
		}

		for _, ev := range events {
			writeSSEEvent(w, flusher, fmt.Sprintf("%d", ev.ID), ev.EventType, ev.Data)
			lastEventID = ev.ID
		}

		// If build is already terminal, close after full replay
		if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
			return
		}
	}

	// Poll loop for new events
	ctx := r.Context()
	pollTicker := time.NewTicker(h.pollInterval)
	heartbeatTicker := time.NewTicker(h.heartbeatInterval)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			return
		case <-heartbeatTicker.C:
			_, writeErr := fmt.Fprintf(w, ":heartbeat\n\n")
			if writeErr != nil {
				return
			}
			flusher.Flush()
		case <-pollTicker.C:
			// Re-check build status
			build, err = h.buildStore.GetByID(ctx, buildID)
			if err != nil {
				return
			}

			// Poll for new events
			events, getErr := h.eventStore.GetAfter(ctx, buildID, lastEventID)
			if getErr != nil {
				return
			}

			for _, ev := range events {
				writeSSEEvent(w, flusher, fmt.Sprintf("%d", ev.ID), ev.EventType, ev.Data)
				lastEventID = ev.ID
			}

			// If build reached terminal state, close stream
			if statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
				return
			}
		}
	}
}

// writeSSEEvent writes a single SSE event in the standard format.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, id string, eventType string, data json.RawMessage) {
	fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", id, eventType, string(data))
	flusher.Flush()
}

