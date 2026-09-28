// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"
	"log/slog"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/getloopscale/loopzero/api/internal/builds/argo"
	"github.com/getloopscale/loopzero/api/internal/builds/stepwatch"
)

// K8sWorkflowNodeSource implements stepwatch.WorkflowNodeSource: it polls Argo Workflows from the build cluster and
// emits ArgoNodeEvent for every Pod node phase observed. Polling (rather
// than informer/watch) keeps the adapter small and resilient to
// reconnects — the watcher dedups duplicate events by (buildID, nodeName)
// + lastPhase so a per-tick re-sweep is safe.
type K8sWorkflowNodeSource struct {
	client       dynamic.Interface
	namespace    string
	pollInterval time.Duration
	logger       *slog.Logger
	// onWatchError is called with HTTP status code + message when the
	// cluster reports auth failures. Wire to ArgoStepWatcher.ReportWatchError.
	onWatchError func(int, string)
}

var _ stepwatch.WorkflowNodeSource = (*K8sWorkflowNodeSource)(nil)

// NewK8sWorkflowNodeSource constructs a poll-based source. Namespace
// empty = all namespaces. pollInterval default 2s if zero.
func NewK8sWorkflowNodeSource(client dynamic.Interface, namespace string, pollInterval time.Duration, logger *slog.Logger) *K8sWorkflowNodeSource {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	return &K8sWorkflowNodeSource{
		client:       client,
		namespace:    namespace,
		pollInterval: pollInterval,
		logger:       logger,
	}
}

// WithWatchErrorHandler wires the EC-1 watcher error reporter. Optional.
func (s *K8sWorkflowNodeSource) WithWatchErrorHandler(fn func(int, string)) *K8sWorkflowNodeSource {
	s.onWatchError = fn
	return s
}

// Run polls Workflows on the configured interval and emits handler calls.
// Returns when ctx is cancelled. Transient list errors are logged and
// retried on the next tick. Per-Workflow parse errors are skipped.
func (s *K8sWorkflowNodeSource) Run(ctx context.Context, handler func(stepwatch.ArgoNodeEvent)) error {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.sweep(ctx, handler)
		}
	}
}

func (s *K8sWorkflowNodeSource) sweep(ctx context.Context, handler func(stepwatch.ArgoNodeEvent)) {
	list, err := s.client.Resource(argo.WorkflowGVR).Namespace(s.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		// EC-1: surface 401/403 to the watcher's dedicated counter so a
		// silent token expiry triggers an alert before events dry up.
		if s.onWatchError != nil {
			switch {
			case k8serrors.IsUnauthorized(err):
				s.onWatchError(401, err.Error())
			case k8serrors.IsForbidden(err):
				s.onWatchError(403, err.Error())
			}
		}
		s.logger.Warn("argo step source: list failed", "error", err)
		return
	}
	for _, wf := range list.Items {
		buildID := stepwatch.BuildIDFromWorkflowName(wf.GetName())
		if buildID == "" {
			continue
		}
		s.emitNodes(&wf, buildID, handler)
	}
}

// emitNodes walks status.nodes and emits one event per Pod node.
func (s *K8sWorkflowNodeSource) emitNodes(wf *unstructured.Unstructured, buildID string, handler func(stepwatch.ArgoNodeEvent)) {
	nodes, found, err := unstructured.NestedMap(wf.Object, "status", "nodes")
	if err != nil || !found {
		return
	}
	for nodeID, nodeAny := range nodes {
		nodeMap, ok := nodeAny.(map[string]interface{})
		if !ok {
			continue
		}
		nodeType, _, _ := unstructured.NestedString(nodeMap, "type")
		if nodeType != "Pod" {
			continue
		}
		phase, _, _ := unstructured.NestedString(nodeMap, "phase")
		displayName, _, _ := unstructured.NestedString(nodeMap, "displayName")
		if displayName == "" {
			displayName = nodeID
		}
		message, _, _ := unstructured.NestedString(nodeMap, "message")

		startedAt := parseK8sTime(nodeMap, "startedAt")
		finishedAt := parseK8sTime(nodeMap, "finishedAt")

		handler(stepwatch.ArgoNodeEvent{
			BuildID:     buildID,
			NodeName:    nodeID,
			DisplayName: displayName,
			NodeType:    nodeType,
			Phase:       stepwatch.ArgoNodePhase(phase),
			StartedAt:   startedAt,
			FinishedAt:  finishedAt,
			Message:     message,
		})
	}
}

func parseK8sTime(m map[string]interface{}, key string) time.Time {
	s, _, _ := unstructured.NestedString(m, key)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
