// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gomodauthorg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

// B-019 regression test.
//
// The Tiltfile is the documented dev-loop entry point (CLAUDE.md: doctor -> setup -> tilt up).
// Its fail() pre-flight is the FIRST thing a developer without ~/.loopruntime/gomod-token.txt meets,
// and the remediation instruction inside it names the repository the operator must scope a PAT
// to. When that instruction names a stale (org, module) pair, the operator provisions a
// credential against a path the project no longer requires.
//
// The existing gomod-auth-org gate cannot catch this for two independent reasons:
//  1. the Tiltfile is not in its authFiles list, and
//  2. its org set is legitimately {getloopscale, getloopscale} because api/go.mod still requires
//     getloopscale/argo-workflows-go, so the *org* name alone is not a violation. The defect is at the
//     (org, module) PAIR level.
//
// This test asserts the pair, and the version, that the Tiltfile hands a newcomer.

// reTiltContractsRef captures the org named immediately before "/contracts" anywhere in
// the Tiltfile, whether in executable instruction text or in a comment describing the build.
var reTiltContractsRef = regexp.MustCompile(`(getloopscale[a-z]*)/contracts(@v([0-9]+\.[0-9]+\.[0-9]+))?`)

// reRequiredContracts captures the org and version go.mod actually requires.
var reRequiredContracts = regexp.MustCompile(`github\.com/(getloopscale[a-z]*)/contracts\s+v([0-9]+\.[0-9]+\.[0-9]+)`)

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	root := qtestutil.Root(t)
	if _, err := os.Stat(filepath.Join(root, "Tiltfile")); err != nil {
		t.Fatalf("repo root %q has no Tiltfile: %v", root, err)
	}
	return root
}

func requiredContracts(t *testing.T, root string) (org, version string) {
	t.Helper()
	for _, gm := range []string{"api/go.mod", "pkg/go.mod"} {
		raw, err := os.ReadFile(filepath.Join(root, gm))
		if err != nil {
			continue
		}
		if m := reRequiredContracts.FindStringSubmatch(string(raw)); m != nil {
			return m[1], m[2]
		}
	}
	t.Fatal("no go.mod requires loopruntime-contracts; the premise of this test is gone")
	return "", ""
}

// TestTiltfileNamesTheContractsOrgTheModulesRequire fails while the Tiltfile tells a developer
// to scope a credential to an org that no go.mod asks for loopruntime-contracts from.
func TestTiltfileNamesTheContractsOrgTheModulesRequire(t *testing.T) {
	root := repoRootFromTest(t)
	wantOrg, _ := requiredContracts(t, root)

	raw, err := os.ReadFile(filepath.Join(root, "Tiltfile"))
	if err != nil {
		t.Fatalf("read Tiltfile: %v", err)
	}

	for i, line := range strings.Split(string(raw), "\n") {
		for _, m := range reTiltContractsRef.FindAllStringSubmatch(line, -1) {
			if got := m[1]; got != wantOrg {
				t.Errorf("Tiltfile:%d names %s/loopruntime-contracts; go.mod requires %s/loopruntime-contracts\n  line: %s",
					i+1, got, wantOrg, strings.TrimSpace(line))
			}
		}
	}
}

// TestTiltfileNamesTheContractsVersionTheModulesRequire fails while the Tiltfile documents a
// loopruntime-contracts version the workspace no longer builds against.
func TestTiltfileNamesTheContractsVersionTheModulesRequire(t *testing.T) {
	root := repoRootFromTest(t)
	_, wantVersion := requiredContracts(t, root)

	raw, err := os.ReadFile(filepath.Join(root, "Tiltfile"))
	if err != nil {
		t.Fatalf("read Tiltfile: %v", err)
	}

	for i, line := range strings.Split(string(raw), "\n") {
		for _, m := range reTiltContractsRef.FindAllStringSubmatch(line, -1) {
			if m[3] == "" {
				continue // this reference pins no version
			}
			if m[3] != wantVersion {
				t.Errorf("Tiltfile:%d documents loopruntime-contracts v%s; go.mod requires v%s\n  line: %s",
					i+1, m[3], wantVersion, strings.TrimSpace(line))
			}
		}
	}
}
