// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package releaseadapter

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func newArgoAppLabelReader(objects ...runtime.Object) *DynamicArgoAppLabelReader {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{argoApplicationGVR: "ApplicationList"}, objects...)
	return NewDynamicArgoAppLabelReader(client, "")
}

func TestDynamicArgoAppLabelReader_ReturnsTheApplicationLabels(t *testing.T) {
	app := &unstructured.Unstructured{}
	app.SetAPIVersion("argoproj.io/v1alpha1")
	app.SetKind("Application")
	app.SetNamespace("argocd")
	app.SetName("tn-1-web-production")
	app.SetLabels(map[string]string{"runtime.getloopscale.com/tenant": "tn-1", "runtime.getloopscale.com/environment": "production"})

	labels, err := newArgoAppLabelReader(app).GetAppLabels(context.Background(), "tn-1-web-production")

	if err != nil {
		t.Fatalf("GetAppLabels: %v", err)
	}
	if labels["runtime.getloopscale.com/tenant"] != "tn-1" || labels["runtime.getloopscale.com/environment"] != "production" {
		t.Errorf("labels = %v", labels)
	}
}

func TestDynamicArgoAppLabelReader_MissingApplicationIsAnError(t *testing.T) {
	_, err := newArgoAppLabelReader().GetAppLabels(context.Background(), "absent")

	if err == nil {
		t.Fatal("expected an error for an absent Application")
	}
}

func TestDynamicArgoAppLabelReader_UnconfiguredReaderIsAnError(t *testing.T) {
	var r *DynamicArgoAppLabelReader

	if _, err := r.GetAppLabels(context.Background(), "x"); err == nil {
		t.Fatal("expected an error from an unconfigured reader")
	}
}
