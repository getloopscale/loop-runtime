// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"
	"github.com/getloopscale/loop-runtime/api/internal/logs"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// StreamLogQuerier — alias to ports.StreamLogQuerier (T1.3 of
// deep-review-remediation-plan). The type is canonically defined in
// domain/logs.go; the alias was promoted to the same routes/logs.go file
// to keep existing handler call sites working without churn.

// LogStreamHandlerOpts configures the log streaming handler.
type LogStreamHandlerOpts struct {
	// PollInterval is how often to poll Loki for new log lines. Default: 2s.
	PollInterval time.Duration
	// MaxDuration is the maximum lifetime of a single stream. Default: 30min.
	MaxDuration time.Duration
	// Logger for structured logging. Default: slog.Default().
	Logger *slog.Logger
}

// LogStreamHandler provides SSE endpoints for streaming build and runtime logs.
type LogStreamHandler struct {
	builds       LogBuildStore
	querier      StreamLogQuerier
	pollInterval time.Duration
	maxDuration  time.Duration
	gate         *sse.Gate
	logger       *slog.Logger
}

// NewLogStreamHandler creates a new LogStreamHandler.
func NewLogStreamHandler(builds LogBuildStore, querier StreamLogQuerier, opts LogStreamHandlerOpts) *LogStreamHandler {
	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}
	maxDuration := opts.MaxDuration
	if maxDuration == 0 {
		maxDuration = 30 * time.Minute
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &LogStreamHandler{
		builds:       builds,
		querier:      querier,
		pollInterval: pollInterval,
		maxDuration:  maxDuration,
		gate:         sse.NewGate(0),
		logger:       logger,
	}
}

// logStreamHeartbeat keeps idle proxies from closing a quiet log stream.
const logStreamHeartbeat = 15 * time.Second

// sseLogEvent is the JSON payload for each SSE log event.
// Uses product-friendly field names (no internal terms).
type sseLogEvent struct {
	Timestamp string `json:"timestamp"`
	Line      string `json:"line"`
	Step      string `json:"step,omitempty"`
}

// StreamBuildLogs handles GET /api/v1/builds/{id}/logs/stream.
// Streams build logs as SSE events by polling Loki incrementally.
func (h *LogStreamHandler) StreamBuildLogs(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrMissingField.Wrap("build ID is required"))
		return
	}

	// Verify build exists and enforce tenant isolation.
	// T0.4 of deep-review-2026-05-10-remediation-plan: tenant filter
	// pushed to DB layer via GetByIDForTenant.
	build, err := h.builds.GetByIDForTenant(r.Context(), uc.TenantID, buildID)
	if err != nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInternalError)
		return
	}
	if build == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrBuildNotFound)
		return
	}

	// Resume from the nanosecond timestamp a reconnecting client sends.
	h.serve(w, r, buildID, sse.LastEventID(r), true)
}

// StreamRuntimeLogs handles GET /api/v1/projects/{name}/logs/stream?env=<env>[&app=<app>].
// Streams runtime logs from the workload namespace via Loki.
//
// Query params:
//   - env (required): environment name (e.g., "production")
//   - app (optional, Beta Logs Gate Criterion 3): filter to a single
//     app within the project. When omitted, returns logs from ALL
//     apps in the namespace (legacy behavior). The app label is
//     promoted by promtail's loopruntime-pods scrape job from the pod's
//     runtime.getloopscale.com/app-name label.
func (h *LogStreamHandler) StreamRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	projectName := chi.URLParam(r, "name")
	if projectName == "" {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	env := r.URL.Query().Get("env")
	if env == "" {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrMissingField.Wrap("env query parameter is required"))
		return
	}

	// Beta Logs Gate Criterion 3: per-app filter. Empty = all apps in
	// the project namespace (backward compatible). Non-empty value
	// is passed through to Loki client which adds `app="<value>"`
	// to the LogQL selector.
	app := r.URL.Query().Get("app")

	// Determine starting timestamp:
	//  1. Last-Event-ID header (highest priority — SSE resume after reconnect)
	//  2. ?since= query param (J-19 R1 TimeRangePicker — 2026-06-13 LIVE fix)
	//  3. 0 (defaults to last 1h in QueryBuildLogsSince)
	//
	// J-19 plan v1.2 ADR D3 LIVE walkthrough fix: TimeRangePicker emits
	// `?since=6h`/`?since=24h`/etc. but the runtime handler previously
	// honored only Last-Event-ID. Result: user changed preset but stream
	// always started from default 1h window. parseSinceParam reuses the
	// same canonical accepted shapes as build-log handler (Beta Logs
	// Gate Criterion 7): "Nh" / "Nm" / "Ns" / "Nd" / integer seconds.
	sinceNano := sse.LastEventID(r)
	if sinceNano == 0 {
		if sinceSecs := parseSinceParam(r.URL.Query().Get("since")); sinceSecs > 0 {
			sinceNano = time.Now().Add(-time.Duration(sinceSecs) * time.Second).UnixNano()
		}
	}

	// For runtime logs, the "buildID" is actually a namespace identifier
	// that the Loki querier uses. App is appended as a 5th colon-
	// segment when provided so the loki client can split it without
	// growing the surface for empty-value handling — empty app field
	// means "no filter" downstream.
	runtimeKey := fmt.Sprintf("runtime:%s:%s:%s:%s", uc.TenantID, env, projectName, app)

	h.serve(w, r, runtimeKey, sinceNano, false)
}

// serve opens the stream and polls Loki with the shared sse.Loop until the
// client leaves, the stream is done, the source keeps failing, or the
// platform cap (maxDuration) is reached.
//
// For build logs (isBuild), the stream is done once the build is terminal
// and no new lines arrived for two consecutive polls. Runtime logs stream
// until the client leaves or the cap.
func (h *LogStreamHandler) serve(w http.ResponseWriter, r *http.Request, queryKey string, sinceNano int64, isBuild bool) {
	leave, ok := h.gate.Enter()
	if !ok {
		sse.WriteTooManyStreams(w)
		return
	}
	defer leave()

	stream, err := sse.Open(w)
	if err != nil {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.maxDuration)
	defer cancel()

	tail := &logTail{h: h, stream: stream, queryKey: queryKey, sinceNano: sinceNano, isBuild: isBuild}
	err = sse.Loop{Interval: h.pollInterval, Heartbeat: logStreamHeartbeat}.Run(ctx, stream, tail.poll)
	switch {
	case errors.Is(err, sse.ErrPollRetriesExhausted):
		h.logger.Warn("log stream: log source kept failing, closing", "key", queryKey, "error", err)
		_ = stream.Send("", "stream.error", []byte(`{"reason":"Logs are temporarily unavailable","action":"reconnect"}`))
	case err != nil:
		h.logger.Info("log stream: client gone", "key", queryKey, "error", err)
	case errors.Is(ctx.Err(), context.DeadlineExceeded) && r.Context().Err() == nil:
		h.logger.Info("log stream: max duration reached", "key", queryKey)
		// Beta Logs Gate Criterion 6 — say why the stream ended and what to do.
		_ = stream.Send("", "stream.timeout", []byte(`{"reason":"Stream open for 30 minutes — platform cap to prevent forgotten sessions from holding resources. Rerun `+"`"+`loopruntime logs --build <id> --follow`+"`"+` to resume from the latest position; the stream picks up where this one ended."}`))
	}
}

// logTail is one open log stream: its Loki cursor and end-of-build policy.
type logTail struct {
	h          *LogStreamHandler
	stream     *sse.Stream
	queryKey   string
	sinceNano  int64
	isBuild    bool
	emptyPolls int
}

// maxEmptyPollsAfterTerminal is how many empty polls after a terminal build
// the stream waits for straggling lines before it reports stream.done.
const maxEmptyPollsAfterTerminal = 2

// poll is one tick of the loop (an sse.PollFunc).
func (t *logTail) poll(ctx context.Context) (bool, error) {
	entries, err := t.h.querier.QueryBuildLogsSince(ctx, t.queryKey, t.sinceNano, 500)
	if err != nil {
		return false, fmt.Errorf("querying logs since %d: %w", t.sinceNano, err)
	}
	if err := t.send(entries); err != nil {
		return true, err
	}
	if !t.isBuild {
		return false, nil
	}
	return t.buildFinished(ctx, len(entries))
}

// send emits each Loki entry as a `log` event whose id is its nanosecond
// timestamp (the resume anchor), and advances the cursor.
func (t *logTail) send(entries []logs.LogEntry) error {
	for _, entry := range entries {
		nsID := timestampToNano(entry.Timestamp)
		data, err := json.Marshal(sseLogEvent{Timestamp: entry.Timestamp, Line: entry.Content, Step: entry.Step})
		if err != nil {
			return fmt.Errorf("encoding log line: %w", err)
		}
		if err := t.stream.Send(strconv.FormatInt(nsID, 10), "log", data); err != nil {
			return err
		}
		if nsID > t.sinceNano {
			t.sinceNano = nsID
		}
	}
	return nil
}

// buildFinished reports done — after writing stream.done — once the build is
// terminal and no new lines arrived for maxEmptyPollsAfterTerminal polls. The
// tenant was authorized at the handler entry, so GetByID is safe here.
func (t *logTail) buildFinished(ctx context.Context, newLines int) (bool, error) {
	build, err := t.h.builds.GetByID(ctx, t.queryKey)
	if err != nil {
		return false, fmt.Errorf("reading build status: %w", err)
	}
	if build == nil || !statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
		return false, nil
	}
	if newLines == 0 {
		t.emptyPolls++
	} else {
		t.emptyPolls = 0
	}
	if t.emptyPolls < maxEmptyPollsAfterTerminal {
		return false, nil
	}
	t.h.logger.Info("log stream: build terminal, no more logs", "build_id", t.queryKey, "status", build.Status)
	data, err := json.Marshal(map[string]string{"status": build.Status})
	if err != nil {
		return true, fmt.Errorf("encoding stream.done: %w", err)
	}
	return true, t.stream.Send("", "stream.done", data)
}

// timestampToNano converts an RFC3339 timestamp string to nanoseconds since epoch.
// Falls back to current time on parse failure.
func timestampToNano(ts string) int64 {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try RFC3339Nano
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return time.Now().UnixNano()
		}
	}
	return t.UnixNano()
}
