// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deletedrefs

import (
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

var spec = Spec{
	Noun:              "file",
	Surfaces:          []string{"callers"},
	IsCaller:          func(string) bool { return true },
	ProvenanceMarkers: []string{"migrated from"},
}

func TestViolations_LiveReferenceToADeletedFileIsReported(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"lib/tool/x.sh":    "echo\n",
		"callers/run.yml":  "run: lib/tool/x.sh\n",
		"callers/prov.yml": "# migrated from lib/tool/x.sh\n",
	})
	qtestutil.CommitOn(t, root, "pr", map[string]string{"lib/tool/x.sh": qtestutil.DeleteMarker})
	got, err := Violations(spec, root, "main", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got[0], "callers/run.yml:1") {
		t.Fatalf("want one violation at callers/run.yml:1, got %v", got)
	}
}

func TestViolations_SameBasenameElsewhereDoesNotCollide(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{
		"dr/preflight.sh":     "echo\n",
		"callers/run.yml":     "run: parity/preflight.sh\n",
		"parity/preflight.sh": "echo\n",
	})
	qtestutil.CommitOn(t, root, "pr", map[string]string{"dr/preflight.sh": qtestutil.DeleteMarker})
	got, err := Violations(spec, root, "main", "HEAD")
	if err != nil || len(got) != 0 {
		t.Fatalf("want no violation, got %v %v", got, err)
	}
}

func TestViolations_UnreachableBaseIsAnError(t *testing.T) {
	root := qtestutil.InitRepo(t, map[string]string{"a": "b\n"})
	if _, err := Violations(spec, root, "origin/nope", "HEAD"); err == nil ||
		!strings.Contains(err.Error(), "BASE_REF_UNREACHABLE") {
		t.Fatalf("want BASE_REF_UNREACHABLE, got %v", err)
	}
}
