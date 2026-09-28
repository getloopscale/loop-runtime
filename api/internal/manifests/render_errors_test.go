// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"strings"
	"testing"
)

// A render that cannot produce valid names must fail the call, not crash the
// request goroutine and not emit a namespace Kubernetes would reject.

func TestRenderDocsReturnsAnErrorWhenAnAppNameIsNotAValidKubernetesName(t *testing.T) {
	// Arrange
	input := baseInput()
	input.Apps = []AppManifestInput{{Name: "Bad_Name", Type: ServiceTypeServer, Port: 8080, ImageRef: "reg/acme:1"}}

	// Act
	_, err := RenderDocs(input)

	// Assert
	if err == nil {
		t.Fatal("RenderDocs accepted an app name that is not a valid Kubernetes name")
	}
}

func TestRenderDocsReturnsAnErrorWhenTheEnvironmentIsEmpty(t *testing.T) {
	// Arrange
	input := baseInput()
	input.Environment = ""
	input.Apps = []AppManifestInput{{Name: "api", Type: ServiceTypeServer, Port: 8080, ImageRef: "reg/acme:1"}}

	// Act
	_, err := RenderDocs(input)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "env") {
		t.Fatalf("err = %v, want an error naming the empty environment", err)
	}
}

func TestRenderDocsReturnsAnErrorWhenTheNamespaceIsNotAValidKubernetesName(t *testing.T) {
	// Arrange
	input := baseInput()
	input.TenantID = "Acme_Corp"
	input.Apps = []AppManifestInput{{Name: "worker", Type: ServiceTypeWorker, ImageRef: "reg/acme:1"}}

	// Act
	_, err := RenderDocs(input)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("err = %v, want an error naming the invalid namespace", err)
	}
}
