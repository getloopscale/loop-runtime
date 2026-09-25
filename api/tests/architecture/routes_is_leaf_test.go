// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package architecture

import (
	"os/exec"
	"strings"
	"testing"
)

// TestImportGraph_RoutesIsLeaf is the architectural guard for T1.3 of
// deep-review-remediation-plan (closes find_arch_001 + find_dep_routes_type_hub).
//
// Before T1.3, services/, db/, k8s/, loki/, github/ packages all imported
// the routes/ package upward to consume types like CustomDomain,
// CreatePreviewRequest, AttestationStore, LogEntry, EnvironmentEntry,
// TenantCRSpec. This was a DIP inversion: lower-layer packages depended
// on the HTTP handler package. The fix migrated those types to domain/
// and replaced the routes/ imports with domain/.
//
// This test enforces the invariant going forward: production code in
// services/, db/, k8s/, loki/, and github/ must NOT import routes/. If
// you find yourself wanting to add such an import, the type you need
// belongs in domain/, not routes/.
//
// Why these five packages specifically:
//   - services/ — orchestrates business logic; should depend on domain/+pkg/
//   - db/ — persistence; only domain types
//   - k8s/ — kubectl/CRD operations; only domain types
//   - loki/ — log query infrastructure; only domain types
//   - github/ — GitHub integration; only domain types
//
// Excluded (legitimate top-down imports of routes/):
//   - server/ — bootstrap wiring
//   - adapters/ — translates services into route interfaces
//   - cmd/ — main entry points
func TestImportGraph_RoutesIsLeaf(t *testing.T) {
	guarded := []string{
		"github.com/getloopscale/loopzero/api/internal/services/...",
		"github.com/getloopscale/loopzero/api/internal/db/...",
		"github.com/getloopscale/loopzero/api/internal/k8s/...",
		"github.com/getloopscale/loopzero/api/internal/loki/...",
		"github.com/getloopscale/loopzero/api/internal/github/...",
	}

	for _, pkg := range guarded {
		out, err := exec.Command("go", "list", "-f",
			"{{.ImportPath}}: {{.Imports}}", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list %s failed: %v\n%s", pkg, err, out)
		}

		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.Contains(line, "github.com/getloopscale/loopzero/api/internal/routes") {
				t.Errorf("DIP violation: package imports routes/ upward\n  %s\n"+
					"types belonging to the routes/ surface should be moved to "+
					"api/internal/domain/ first, then imported by the lower-layer "+
					"package. See T1.3 of deep-review-remediation-plan.", line)
			}
		}
	}
}
