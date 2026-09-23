// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package acyclicgraph

import (
	"path/filepath"
	"strings"
	"testing"
)

// A Go file the gate cannot parse is a file whose imports it never read. In an
// import-graph gate that is a hole in the DAG check, so it must fail rather
// than be skipped as "not our concern".
func TestAcyclicGraph_UnparseableGoFile_FailsInsteadOfBeingSkipped(t *testing.T) {
	engine := layout(t)
	writeGo(t, engine, "pkg/broken/broken.go", "package broken\n\nimport (\n\t\"github.com/getloopscale/loopzero-cloud/x\"\n")
	_, err := Check(Config{RepoRoot: engine})
	if err == nil || !strings.Contains(err.Error(), filepath.Join("pkg", "broken", "broken.go")) {
		t.Fatalf("want an error naming the unparseable file, got %v", err)
	}
}
