// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"strings"
	"testing"
)

// The published loop-runtime-api chart must carry serviceAccount.annotations onto the
// ServiceAccount it renders. On EKS the IRSA role is bound through the
// `eks.amazonaws.com/role-arn` annotation, and the pod identity webhook injects the
// web identity token only for a ServiceAccount that carries it.
//
// values.schema.json already accepted `serviceAccount.annotations`, and the template
// dropped it: a value the chart validates and then ignores is the silent kind of
// misconfiguration, because the install succeeds and the pod simply has no AWS identity.
func TestLoopRuntimeApiServiceAccountCarriesItsAnnotations(t *testing.T) {
	const role = "arn:aws:iam::000000000000:role/loop-runtime-staging-core-loop-runtime-api"
	out := renderChart(t, publishedChart(t, "loop-runtime-api"), "loop-runtime-api", map[string]string{
		`serviceAccount.annotations.eks\.amazonaws\.com/role-arn`: role,
	})

	sa := renderedDocument(out, "kind: ServiceAccount")
	if sa == "" {
		t.Fatal("the render declares no ServiceAccount")
	}
	if !strings.Contains(sa, "eks.amazonaws.com/role-arn: "+role) {
		t.Errorf("the ServiceAccount does not carry the IRSA annotation %q; rendered:\n%s", role, sa)
	}
}

// renderedDocument returns the first YAML document of a multi-document render that
// contains marker, or "" when none does.
func renderedDocument(render, marker string) string {
	for _, doc := range strings.Split(render, "\n---") {
		if strings.Contains(doc, marker) {
			return doc
		}
	}
	return ""
}
