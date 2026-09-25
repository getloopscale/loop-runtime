// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scrub

import (
	"strings"
	"testing"
)

// Rule 3 of the repository contract lists the internal terms that must never
// reach user-facing output. Every one of them, plus the infrastructure details
// the push and build-submit errors carry, must be gone after Text — whichever
// path the message leaves by.
func TestTextRemovesEveryRule3InternalTerm(t *testing.T) {
	cases := []struct{ input, leaked string }{
		{"ArgoCD sync failed", "ArgoCD"},
		{"argocd: 401", "argocd"},
		{"GitOps repo unreachable", "GitOps"},
		{"gitops-push rejected", "gitops-push"},
		{"CRD not installed", "CRD"},
		{"missing tenant_id", "tenant_id"},
		{"could not take advisory lock", "advisory lock"},
		{"pg_advisory_xact_lock timed out", "pg_advisory"},
		{"Gitea: Internal Server Error", "Gitea"},
		{"MinIO: access denied", "MinIO"},
		{"kubebuilder marker missing", "kubebuilder"},
		{"envtest control plane down", "envtest"},
		{"Kaniko build failed", "Kaniko"},
		{"cosign verify failed", "cosign"},
		{"sync to cell-0 failed", "cell-0"},
		{"dial postgresql.loopzero-system.svc.cluster.local", "svc.cluster.local"},
		{"system:serviceaccount:loopzero-system:default forbidden", "serviceaccount"},
		{`in namespace "loopzero-system" forbidden`, "loopzero-system"},
		{"Authorization: Bearer ghp_secret1234567890abcd", "ghp_secret"},
	}
	for _, tc := range cases {
		t.Run(tc.leaked, func(t *testing.T) {
			// Act
			got := Text(tc.input)

			// Assert
			if strings.Contains(strings.ToLower(got), strings.ToLower(tc.leaked)) {
				t.Errorf("Text(%q) = %q still carries %q", tc.input, got, tc.leaked)
			}
		})
	}
}

func TestTextKeepsUserTermsThatOnlyResembleInternalOnes(t *testing.T) {
	for _, input := range []string{
		"validation error on project my-argo-test",
		"clone https://github.com/acme/app failed",
		"dial tcp 172.4.5.6:443: connection refused",
	} {
		if got := Text(input); got != input {
			t.Errorf("Text(%q) = %q, want it unchanged", input, got)
		}
	}
}
