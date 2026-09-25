// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"testing"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/getloopscale/loopzero/api/internal/builds/argo"
	"github.com/getloopscale/loopzero/api/internal/builds/stepwatch"
)

func workflow(name string, nodes map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"metadata":   map[string]interface{}{"name": name, "namespace": "argo"},
		"status":     map[string]interface{}{"nodes": nodes},
	}}
}

func nodeSource(objects ...runtime.Object) (*K8sWorkflowNodeSource, *dynamicfake.FakeDynamicClient) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{argo.WorkflowGVR: "WorkflowList"}, objects...)
	return NewK8sWorkflowNodeSource(client, "argo", 0, slog.Default()), client
}

func sweepEvents(s *K8sWorkflowNodeSource) []stepwatch.ArgoNodeEvent {
	var got []stepwatch.ArgoNodeEvent
	s.sweep(context.Background(), func(ev stepwatch.ArgoNodeEvent) { got = append(got, ev) })
	sort.Slice(got, func(i, j int) bool { return got[i].NodeName < got[j].NodeName })
	return got
}

func TestK8sWorkflowNodeSource_EmitsPodNodesOfBuildWorkflows(t *testing.T) {
	s, _ := nodeSource(
		workflow("loopzero-build-build-abc123-x7k2p", map[string]interface{}{
			"n1": map[string]interface{}{"type": "Pod", "phase": "Running", "displayName": "clone-repo",
				"startedAt": "2026-09-25T10:00:00Z"},
			"n2": map[string]interface{}{"type": "DAG", "phase": "Running"},
			"n3": map[string]interface{}{"type": "Pod", "phase": "Failed", "message": "exit 1"},
		}),
		workflow("unrelated-workflow", map[string]interface{}{
			"n9": map[string]interface{}{"type": "Pod", "phase": "Running"},
		}),
	)

	got := sweepEvents(s)

	if len(got) != 2 {
		t.Fatalf("want the 2 Pod nodes of the build workflow, got %+v", got)
	}
	if got[0].BuildID != "build-abc123" || got[0].DisplayName != "clone-repo" ||
		got[0].Phase != stepwatch.ArgoNodePhase("Running") || got[0].StartedAt.IsZero() {
		t.Errorf("n1 = %+v", got[0])
	}
	if got[1].DisplayName != "n3" || got[1].Message != "exit 1" {
		t.Errorf("a node without displayName falls back to its id: %+v", got[1])
	}
}

func TestK8sWorkflowNodeSource_ReportsAuthFailuresToTheWatcher(t *testing.T) {
	s, client := nodeSource()
	client.PrependReactor("list", "workflows", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewUnauthorized("token expired")
	})
	var status int
	s.WithWatchErrorHandler(func(code int, _ string) { status = code })

	if got := sweepEvents(s); len(got) != 0 {
		t.Fatalf("a failed list emits nothing, got %+v", got)
	}
	if status != 401 {
		t.Errorf("reported status = %d, want 401", status)
	}
}

func TestK8sWorkflowNodeSource_OtherListFailuresAreNotReportedAsAuth(t *testing.T) {
	s, client := nodeSource()
	client.PrependReactor("list", "workflows", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	reported := false
	s.WithWatchErrorHandler(func(int, string) { reported = true })

	sweepEvents(s)

	if reported {
		t.Error("a non-auth list failure must not be reported as an auth failure")
	}
}
