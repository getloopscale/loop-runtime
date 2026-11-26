// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C11.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
// Source: docs/decisions/operator-split-invariants.md (I-7)
//
// Pins that ONLY tenant-operator may grant
// rbac.authorization.k8s.io/clusterroles+clusterrolebindings.
// app/preview/platform-operator MUST NOT have these grants.
//
// Why: ClusterRole creation is a privilege-escalation primitive. Only
// tenant-operator legitimately needs it (per-tenant scoped role
// provisioning). Granting to other split operators = unjustified
// escalation.

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitOperators_OnlyTenantOpGrantsClusterRoleEscalation(t *testing.T) {
	// Reads the raw template (before helm rendering). Line-level
	// containment check is sufficient because we control the template
	// shape (not user-modifiable values).
	type chartCheck struct {
		chart      string
		mayGrantCR bool
	}
	cases := []chartCheck{
		{"loopruntime-tenant-operator", true}, // legitimate
		{"loopruntime-app-operator", false},
		{"loopruntime-preview-operator", false},
		{"loopruntime-platform-operator", false},
	}

	for _, c := range cases {
		path := filepath.Join(chartsDir(), c.chart, "templates", "clusterrole.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: clusterrole.yaml missing: %v", c.chart, err)
		}
		text := string(data)

		// Look for the exact triple: rbac.authorization.k8s.io +
		// clusterroles + a write verb. Read-only (get/list/watch) is
		// fine; writes (create/update/delete/patch) are the escalation
		// primitive we forbid for non-tenant operators.
		hasCR := strings.Contains(text, "rbac.authorization.k8s.io") &&
			strings.Contains(text, "clusterroles") &&
			(strings.Contains(text, "create") ||
				strings.Contains(text, "delete") ||
				strings.Contains(text, "update") ||
				strings.Contains(text, "patch"))

		if hasCR && !c.mayGrantCR {
			t.Errorf("%s: ClusterRole grants `clusterroles` write verbs but I-7 forbids it (only tenant-operator may)", c.chart)
		}
		if !hasCR && c.mayGrantCR {
			// Note: the platform-operator may legitimately get this in
			// future if the cell-pinning code is extracted there. The
			// test pins CURRENT design — opens a clear amendment path.
			t.Logf("note: %s does not grant clusterroles writes (expected: %v) — confirm this is still desired", c.chart, c.mayGrantCR)
		}
	}
}

func TestSplitOperators_NoneGrantClusterAdminWildcards(t *testing.T) {
	// No split chart should have a wildcard rule like apiGroups: ["*"]
	// resources: ["*"] verbs: ["*"]. That's effective cluster-admin and
	// would defeat the entire least-privilege purpose of the split.
	for _, op := range splitOperators {
		path := filepath.Join(chartsDir(), op, "templates", "clusterrole.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		text := string(data)
		// Detect the exact wildcard escalation pattern. We look for the
		// concrete YAML strings the template would emit.
		if strings.Contains(text, `apiGroups: ["*"]`) && strings.Contains(text, `verbs: ["*"]`) {
			t.Errorf("%s: ClusterRole grants apiGroups: [*] verbs: [*] — that's cluster-admin, defeats split purpose", op)
		}
	}
}
