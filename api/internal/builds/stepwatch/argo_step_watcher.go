// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package stepwatch

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/events"
	cloudmetrics "github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
)

// ArgoNodePhase enumerates the Argo Workflow node phases we react to.
// Mirrors argoproj.io/v1alpha1 NodePhase but kept as a string here so
// the package does NOT depend on argo's Go API for the core types — the
// K8s adapter is the only place that touches argo types.
type ArgoNodePhase string

const (
	ArgoPhasePending   ArgoNodePhase = "Pending"
	ArgoPhaseRunning   ArgoNodePhase = "Running"
	ArgoPhaseSucceeded ArgoNodePhase = "Succeeded"
	ArgoPhaseFailed    ArgoNodePhase = "Failed"
	ArgoPhaseError     ArgoNodePhase = "Error"
	ArgoPhaseSkipped   ArgoNodePhase = "Skipped"
)

// ArgoNodeEvent is the abstracted event the watcher consumes. The K8s
// informer adapter translates raw `Workflow` resource diffs into a
// sequence of these. Keeping the watcher logic in terms of this struct
// makes it testable without depending on the K8s API.
type ArgoNodeEvent struct {
	BuildID     string
	NodeName    string
	DisplayName string
	NodeType    string // "Pod" | "Retry" | "DAG" | "StepGroup" | ...
	Phase       ArgoNodePhase
	StartedAt   time.Time
	FinishedAt  time.Time // zero when still running
	Message     string    // Argo node-message annotation, when available
}

// WorkflowNodeSource is the read-side of the K8s informer wired against
// the build cluster. Production wires a real K8s SharedIndexInformer;
// tests pass a fake that emits a deterministic event sequence.
type WorkflowNodeSource interface {
	// Run blocks until ctx is cancelled. Calls handler for every node
	// phase change observed across the build cluster.
	Run(ctx context.Context, handler func(ArgoNodeEvent)) error
}

// TerminalBuildIDStore lets the watcher cheaply skip events for builds
// already in a terminal status (EC-4) without round-tripping the main
// store every time. Production wires a thin adapter around the build
// store; tests can pass an in-memory set.
type TerminalBuildIDStore interface {
	// TerminalIDs returns the set of build IDs whose status is one of
	// SUCCEEDED, FAILED, CANCELLED. Loaded once at Start() then refreshed
	// every refreshInterval (defaults to 1h).
	TerminalIDs(ctx context.Context) (map[string]struct{}, error)
}

// ArgoStepWatcher consumes ArgoNodeEvent values from a WorkflowNodeSource
// and emits SSE events to the EventStore + a Prometheus counter. The
// watcher is the canonical fan-out point for sub-step visibility in the
// dashboard (Plan J-17 T1.1).
type ArgoStepWatcher struct {
	source      WorkflowNodeSource
	store       sse.EventStore
	terminalIDs TerminalBuildIDStore
	logger      *slog.Logger
	now         func() time.Time

	// dedup map keyed by buildID + nodeName -> lastPhase. Reset for a
	// step when it transitions out of a terminal phase back to Pending
	// (EC-8 retry handling).
	mu        sync.Mutex
	lastPhase map[string]ArgoNodePhase
	// initial-list terminal cache (EC-4). Empty if not yet populated.
	terminalSet map[string]struct{}
}

// NewArgoStepWatcher constructs a watcher. logger is required; nil panics.
func NewArgoStepWatcher(source WorkflowNodeSource, store sse.EventStore, terminalIDs TerminalBuildIDStore, logger *slog.Logger) *ArgoStepWatcher {
	if logger == nil {
		panic("argo_step_watcher: logger is required")
	}
	return &ArgoStepWatcher{
		source:      source,
		store:       store,
		terminalIDs: terminalIDs,
		logger:      logger,
		now:         time.Now,
		lastPhase:   make(map[string]ArgoNodePhase),
	}
}

// Start kicks off the watcher loop. Returns when ctx is cancelled OR
// the source.Run returns. Populates the terminal-ID cache once before
// running so initial-list events for already-done builds get skipped.
func (w *ArgoStepWatcher) Start(ctx context.Context) error {
	w.logger.Info("argo step watcher: started")
	defer w.logger.Info("argo step watcher: stopped")

	// EC-4: warm the terminal-ID cache before consuming events.
	if w.terminalIDs != nil {
		ids, err := w.terminalIDs.TerminalIDs(ctx)
		if err != nil {
			// Non-fatal — proceeding without cache means a startup spike
			// of duplicate events for old builds, which dedup handles.
			w.logger.Warn("argo step watcher: terminal cache load failed",
				"error", err)
		} else {
			w.mu.Lock()
			w.terminalSet = ids
			w.mu.Unlock()
			w.logger.Info("argo step watcher: terminal cache loaded",
				"count", len(ids))
		}
	}

	return w.source.Run(ctx, func(ev ArgoNodeEvent) {
		w.handleEvent(ctx, ev)
	})
}

// buildIDPattern extracts the buildID from an Argo node name. Argo
// names nodes after their parent workflow + a random suffix; the
// workflow itself is named `loopzero-build-<buildID>-<random>`. We grep
// for the canonical prefix.
var buildIDPattern = regexp.MustCompile(`loopzero-build-(build-[a-f0-9]+)`)

func extractBuildID(workflowName string) string {
	m := buildIDPattern.FindStringSubmatch(workflowName)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

// handleEvent applies dedup + filtering and emits SSE events.
func (w *ArgoStepWatcher) handleEvent(ctx context.Context, ev ArgoNodeEvent) {
	// Filter: only Pod nodes are user-visible steps. Retry / DAG /
	// StepGroup are scaffolding.
	if ev.NodeType != "Pod" {
		return
	}
	if ev.BuildID == "" {
		w.logger.Debug("argo step watcher: missing buildID, skipping")
		return
	}
	// EC-4 — skip if build already terminal at startup snapshot.
	if w.isTerminalBuild(ev.BuildID) {
		return
	}
	// Pending is informational only.
	if ev.Phase == ArgoPhasePending {
		// EC-8 — reset dedup so a retry can emit a fresh `started`.
		w.resetDedup(ev.BuildID, ev.NodeName)
		return
	}
	// Skipped is silent.
	if ev.Phase == ArgoPhaseSkipped {
		return
	}

	// EC-9 (LIVE bug 2026-06-06): for terminal phases (Succeeded /
	// Failed / Error), peek the previous phase BEFORE shouldEmit
	// mutates it. If we never observed Running (poll missed it because
	// the Pod completed between two 2s ticks), we synthesize a `started`
	// event before the terminal one so the UI gets a consistent
	// started→succeeded lifecycle for every step. Without this, the
	// dashboard timeline "jumps" past fast-completing steps and looks
	// stuck on whichever slow step was last visualized as Running.
	priorPhase := w.peekPhase(ev.BuildID, ev.NodeName)

	// Dedup: skip if same phase observed already.
	if !w.shouldEmit(ev.BuildID, ev.NodeName, ev.Phase) {
		return
	}

	// Normalize step name: strip `(0)` retry suffix Argo adds for
	// retry containers. We want the canonical step name.
	stepName := stripRetrySuffix(ev.DisplayName)
	phaseGroup := events.PhaseGroupForStep(stepName)
	stepLabel := stepLabelFor(stepName)

	// emitSyntheticStarted is true when we are about to emit a terminal
	// event (Succeeded/Failed/Error) and we never observed the Running
	// transition for this node. The synthetic started event uses the
	// node's StartedAt timestamp from Argo, which is always populated.
	emitSyntheticStarted := func() bool {
		return priorPhase != ArgoPhaseRunning && !ev.StartedAt.IsZero()
	}

	switch ev.Phase {
	case ArgoPhaseRunning:
		payload := events.BuildStepStartedPayload{
			StepName:   stepName,
			StartedAt:  ev.StartedAt,
			PhaseGroup: phaseGroup,
		}
		w.appendAndCount(ctx, ev.BuildID, events.EventBuildStepStarted, payload, stepLabel, "started")

	case ArgoPhaseSucceeded:
		dur := ev.FinishedAt.Sub(ev.StartedAt).Seconds()
		if dur < 0 {
			dur = 0
		}
		if emitSyntheticStarted() {
			startedPayload := events.BuildStepStartedPayload{
				StepName:   stepName,
				StartedAt:  ev.StartedAt,
				PhaseGroup: phaseGroup,
			}
			w.appendAndCount(ctx, ev.BuildID, events.EventBuildStepStarted, startedPayload, stepLabel, "started")
		}
		payload := events.BuildStepSucceededPayload{
			StepName:        stepName,
			StartedAt:       ev.StartedAt,
			EndedAt:         ev.FinishedAt,
			DurationSeconds: dur,
			PhaseGroup:      phaseGroup,
		}
		w.appendAndCount(ctx, ev.BuildID, events.EventBuildStepSucceeded, payload, stepLabel, "succeeded")

	case ArgoPhaseFailed, ArgoPhaseError:
		dur := ev.FinishedAt.Sub(ev.StartedAt).Seconds()
		if dur < 0 {
			dur = 0
		}
		var errClass ErrorClass
		// EC-7 fast path: workflow terminate annotation = cancel.
		if ev.Message == "" {
			errClass = ErrorClassUnknown
		} else {
			errClass = Classify(ev.Message, "")
		}
		if emitSyntheticStarted() {
			startedPayload := events.BuildStepStartedPayload{
				StepName:   stepName,
				StartedAt:  ev.StartedAt,
				PhaseGroup: phaseGroup,
			}
			w.appendAndCount(ctx, ev.BuildID, events.EventBuildStepStarted, startedPayload, stepLabel, "started")
		}
		payload := events.BuildStepFailedPayload{
			StepName:        stepName,
			StartedAt:       ev.StartedAt,
			EndedAt:         ev.FinishedAt,
			DurationSeconds: dur,
			PhaseGroup:      phaseGroup,
			ErrorClass:      string(errClass),
			ErrorMessage:    truncate(ev.Message, 500),
			Suggestion:      SuggestionFor(errClass),
		}
		w.appendAndCount(ctx, ev.BuildID, events.EventBuildStepFailed, payload, stepLabel, "failed")
	}
}

// peekPhase returns the previously observed phase for a (buildID, nodeName)
// WITHOUT mutating the dedup map. Used by handleEvent to detect
// "succeeded without a prior running observation" (EC-9 synthetic started).
// Separator MUST match shouldEmit / resetDedup ("|") so the lookup hits
// the same entry.
func (w *ArgoStepWatcher) peekPhase(buildID, nodeName string) ArgoNodePhase {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastPhase[buildID+"|"+nodeName]
}

// ReportWatchError is invoked by the K8s adapter's SetWatchErrorHandler
// when the underlying watch returns an error (Plan J-17 T1.1.bis / EC-1).
// 401/403 errors indicate the build-cluster SA token expired — we
// increment a dedicated counter and log so Grafana alerts can page the
// operator before events stop flowing.
//
// We classify based on the K8s API error code:
//   - 401 Unauthorized: token expired or invalid
//   - 403 Forbidden:    RBAC binding regressed
//   - anything else:    transient network blip (no counter, debug log)
func (w *ArgoStepWatcher) ReportWatchError(statusCode int, message string) {
	switch statusCode {
	case 401, 403:
		// CORRECTED 2026-08-18 (B-140): this message said "rotate loopzero-build-sa token", and
		// the runbook it cites FORBIDS that — `loopzero-build-sa` is the workflow EXECUTOR, bound
		// only to `workflowtaskresults: [create, patch]`. Its token authenticates (the 401
		// goes away) and is forbidden to list workflows (403), so following the message
		// traded a readable defect for a silent one. The SA that submits and watches is
		// `loopzero-workflow-submitter`.
		//
		// Found by following the runbook to fix this error live on the droplet, where it
		// fired every 2s. The `runbook-parity` gate guards the parity from here on.
		w.logger.Error("argo step watcher: build cluster auth failed — rotate loopzero-workflow-submitter token",
			"status_code", statusCode,
			"message", message,
			"runbook", "docs/runbooks.md",
		)
		cloudmetrics.ArgoWatcherTokenExpiredTotal.Inc()
	default:
		w.logger.Debug("argo step watcher: transient watch error",
			"status_code", statusCode, "message", message)
	}
}

// HandleDelete is invoked by the K8s adapter when a Workflow resource
// is deleted mid-run. For any Pod node we last saw as Running, emit
// a synthetic `build.step.failed` with error_class=WORKFLOW_DELETED.
func (w *ArgoStepWatcher) HandleDelete(ctx context.Context, buildID string, runningNodes []ArgoNodeEvent) {
	if w.isTerminalBuild(buildID) {
		return
	}
	for _, ev := range runningNodes {
		if ev.NodeType != "Pod" || ev.Phase != ArgoPhaseRunning {
			continue
		}
		stepName := stripRetrySuffix(ev.DisplayName)
		payload := events.BuildStepFailedPayload{
			StepName:        stepName,
			StartedAt:       ev.StartedAt,
			EndedAt:         w.now(),
			DurationSeconds: w.now().Sub(ev.StartedAt).Seconds(),
			PhaseGroup:      events.PhaseGroupForStep(stepName),
			ErrorClass:      string(ErrorClassWorkflowDeleted),
			ErrorMessage:    "Workflow was deleted",
			Suggestion:      SuggestionFor(ErrorClassWorkflowDeleted),
		}
		w.appendAndCount(ctx, buildID, events.EventBuildStepFailed, payload, stepLabelFor(stepName), "failed")
	}
}

// --- helpers ---

// shouldEmit returns true if this is the first time we see (buildID,
// nodeName) at this phase since the last reset.
func (w *ArgoStepWatcher) shouldEmit(buildID, nodeName string, phase ArgoNodePhase) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := buildID + "|" + nodeName
	if last, ok := w.lastPhase[key]; ok && last == phase {
		return false
	}
	w.lastPhase[key] = phase
	return true
}

// resetDedup is called when a node transitions back to Pending (retry).
// Clearing the entry lets a subsequent Running emit a fresh `started`.
func (w *ArgoStepWatcher) resetDedup(buildID, nodeName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.lastPhase, buildID+"|"+nodeName)
}

func (w *ArgoStepWatcher) isTerminalBuild(buildID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.terminalSet == nil {
		return false
	}
	_, ok := w.terminalSet[buildID]
	return ok
}

func (w *ArgoStepWatcher) appendAndCount(ctx context.Context, buildID, eventType string, payload any, stepLabel, status string) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.logger.Error("argo step watcher: marshal payload failed",
			"build_id", buildID, "event_type", eventType, "error", err)
		return
	}
	if _, err := w.store.Append(ctx, buildID, eventType, body); err != nil {
		w.logger.Error("argo step watcher: event store append failed",
			"build_id", buildID, "event_type", eventType, "error", err)
		return
	}
	cloudmetrics.DeploymentsStepEventsTotal.WithLabelValues(stepLabel, status).Inc()
}

// stepLabelFor returns the Prometheus label value: the step's family, or
// "other" (EC-16 cardinality cap).
func stepLabelFor(name string) string {
	return events.StepMetricLabel(name)
}

// stripRetrySuffix removes the Argo retry suffix `(N)` from a step name.
// Example: `build-image-api(0)` -> `build-image-api`.
var retrySuffixPattern = regexp.MustCompile(`\(\d+\)$`)

func stripRetrySuffix(name string) string {
	return retrySuffixPattern.ReplaceAllString(name, "")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// BuildIDFromWorkflowName returns the build id encoded in an Argo Workflow
// name (`loopzero-build-<buildID>-<random>`), or "" when the workflow is not a
// build. adapters.K8sWorkflowNodeSource uses it to tag node events.
func BuildIDFromWorkflowName(workflowName string) string {
	return extractBuildID(workflowName)
}
