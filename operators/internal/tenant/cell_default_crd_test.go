// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/domain"
)

// cellIDDefault captures the kubebuilder default rendered for a `cellID:`
// property: the `default:` line that immediately follows the field name.
var cellIDDefault = regexp.MustCompile(`(?m)^\s+cellID:\n\s+default:\s+(\S+)\s*$`)

// TestCRDCellIDDefaultIsTheDefaultCellID pins the one place the default cell id
// cannot import the Go constant — the kubebuilder default marker rendered into
// the CRD schema — to domain.DefaultCellID, so a CR created without a cell and a
// row created without a cell land in the same cell.
func TestCRDCellIDDefaultIsTheDefaultCellID(t *testing.T) {
	for _, crdPath := range []string{
		"../../config/crd/bases/loopzero.io_tenants.yaml",
		"../../config/crd/bases/loopzero.io_clustercredentials.yaml",
	} {
		t.Run(crdPath, func(t *testing.T) {
			content, err := os.ReadFile(crdPath)
			if err != nil {
				t.Fatalf("read CRD YAML: %v", err)
			}

			match := cellIDDefault.FindSubmatch(content)

			if match == nil {
				t.Fatalf("%s declares no default for cellID", crdPath)
			}
			if got := string(match[1]); got != domain.DefaultCellID {
				t.Fatalf("%s defaults cellID to %q, want domain.DefaultCellID %q", crdPath, got, domain.DefaultCellID)
			}
		})
	}
}
