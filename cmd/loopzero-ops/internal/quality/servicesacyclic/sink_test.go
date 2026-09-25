// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package servicesacyclic

import (
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
)

// Every context depends on api/internal/deploycontracts, so a single import
// from that sink back into a context closes a cycle through it. The sink lives
// outside api/internal/services, so the subpackage fence alone never reads it.

func TestCheck_SinkImportingAContextFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/deploycontracts/c.go", importing("deploycontracts", "build"))

	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("a sink importing a context must be a forbidden edge; got %v", err)
	}
	if !strings.Contains(err.Error(), "deploycontracts") || !strings.Contains(err.Error(), "build") {
		t.Errorf("the error must name the sink and the context it reaches, got %q", err)
	}
}

func TestCheck_SinkWithoutContextImportsPasses(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/deploycontracts/c.go", "package deploycontracts\n\nimport _ \"context\"\n")

	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("a sink that imports no context must pass, got %v", err)
	}
}

func TestCheck_SinkTestFileImportingAContextFails(t *testing.T) {
	root := legalTree(t)
	writeGo(t, root, "api/internal/deploycontracts/c_test.go", importing("deploycontracts", "platform"))

	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("a test file in the sink importing a context still couples the two; got %v", err)
	}
}
