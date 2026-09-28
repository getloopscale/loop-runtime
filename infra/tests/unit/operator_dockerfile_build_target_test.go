// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// operatorBuildLine matches the `go build ... -o /loopzero-<name> <target>` line of an
// operator Dockerfile and captures the build target.
var operatorBuildLine = regexp.MustCompile(`go build [^\n]*-o /loopzero-[a-z-]+ (\S+)`)

// Every operator image must compile its command PACKAGE, not a file inside it. `go
// build ./cmd/x/main.go` compiles that one file as a package of its own, so the day a
// second file joins the command the image fails with `undefined:` — measured on the
// tenant-operator after its config moved to config.go, where every image build broke
// while `go test ./...` stayed green (#406).
func TestEveryOperatorImageBuildsItsCommandPackage(t *testing.T) {
	dockerfiles, err := filepath.Glob("../../../operators/Dockerfile.*-operator")
	if err != nil {
		t.Fatalf("glob operator Dockerfiles: %v", err)
	}
	if len(dockerfiles) < 4 {
		t.Fatalf("found %d operator Dockerfiles, want the 4 split operators: %v", len(dockerfiles), dockerfiles)
	}
	for _, path := range dockerfiles {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		m := operatorBuildLine.FindSubmatch(raw)
		if m == nil {
			t.Errorf("%s: no `go build -o /loopzero-<operator>` line found", filepath.Base(path))
			continue
		}
		if target := string(m[1]); filepath.Ext(target) == ".go" {
			t.Errorf("%s builds the file %s; build its package directory %s instead",
				filepath.Base(path), target, filepath.Dir(target))
		}
	}
}
