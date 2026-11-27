// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package gomodauthorg implements the `gomod-auth-org` quality gate.
//
// # WHAT IT GUARDS
//
// Every place that authenticates to fetch a private Go module must name the SAME GitHub org
// the go.mod files actually require. When the two drift, git fetches the module with no
// credentials — and GitHub answers a private repo without auth using "not found", the exact
// message it uses for a repository that does not exist. The build then fails pointing at the
// wrong cause.
//
// # WHY A GATE AND NOT A FIX
//
// This defect shipped twice in two days from a single root cause. Commit 0b4bbd63d (2026-08-06)
// rewrote api/go.mod and pkg/go.mod from `getloopscaledev` to `getloopscale`. The places that
// authenticate did not follow:
//
//   - .github/workflows/ci.yml — found 2026-08-07, blocked every PR
//   - api/Dockerfile           — found 2026-08-08, broke the image build on merge to develop
//
// Fixing the first instance without sweeping for siblings is what let the second reach
// develop. A gate turns "remember to grep" into an assertion.
//
// # WHAT IT DOES NOT CHECK
//
// Whether the credential itself is valid — that is a live secret, not a repository fact. This
// gate only proves the org NAMES agree, which is the failure that produced a misleading error.
package gomodauthorg

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// authFiles are the places that configure module authentication. Each is checked against the
// orgs the go.mod files require.
var authFiles = []string{
	"api/Dockerfile",
	".github/workflows/ci.yml",
}

// goModFiles declare which orgs are actually required.
var goModFiles = []string{
	"api/go.mod",
	"pkg/go.mod",
	"operators/go.mod",
	"cmd/loop-runtime-ops/go.mod",
}

var (
	// A private module requirement in go.mod: `github.com/<org>/<repo> v1.2.3`
	reRequire = regexp.MustCompile(`github\.com/(getloopscale[a-z]*)/[A-Za-z0-9_.-]+\s+v[0-9]`)
	// Any org named on a line that configures auth (GOPRIVATE or a git url rewrite).
	reAnyOrg = regexp.MustCompile(`github\.com[:/](getloopscale[a-z]*)/`)
)

// Finding is one gate violation.
type Finding struct {
	File    string
	Message string
}

// Report is the gate result.
type Report struct {
	RequiredOrgs []string
	Findings     []Finding
	Skipped      []string
}

// OK reports whether the gate passed.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Verdict is Fail when any build file authenticates against another org.
func (r Report) Verdict() verdict.Verdict { return verdict.Of(r.Findings) }

// Check runs the gate against repoRoot.
func Check(repoRoot string) (Report, error) {
	var rep Report

	required := map[string]bool{}
	for _, gm := range goModFiles {
		raw, err := os.ReadFile(filepath.Join(repoRoot, gm)) //nolint:gosec // repo-relative
		if err != nil {
			rep.Skipped = append(rep.Skipped, gm)
			continue
		}
		for _, m := range reRequire.FindAllStringSubmatch(string(raw), -1) {
			required[m[1]] = true
		}
	}
	if len(required) == 0 {
		// No private module is required anywhere: nothing to keep in sync.
		return rep, nil
	}
	for o := range required {
		rep.RequiredOrgs = append(rep.RequiredOrgs, o)
	}
	sort.Strings(rep.RequiredOrgs)

	for _, af := range authFiles {
		raw, err := os.ReadFile(filepath.Join(repoRoot, af)) //nolint:gosec // repo-relative
		if err != nil {
			rep.Skipped = append(rep.Skipped, af)
			continue
		}
		body := string(raw)

		// Only lines that actually configure auth count. A comment mentioning the old org is
		// history, not a credential path — flagging it would train people to delete the record
		// of why something changed.
		var authOrgs []string
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if !strings.Contains(line, "GOPRIVATE") && !strings.Contains(line, "insteadOf") {
				continue
			}
			for _, m := range reAnyOrg.FindAllStringSubmatch(line, -1) {
				authOrgs = append(authOrgs, m[1])
			}
		}
		if len(authOrgs) == 0 {
			continue // this file configures no module auth
		}

		have := map[string]bool{}
		for _, o := range authOrgs {
			have[o] = true
		}
		for _, want := range rep.RequiredOrgs {
			if !have[want] {
				rep.Findings = append(rep.Findings, Finding{
					File: af,
					Message: fmt.Sprintf(
						"authenticates to %s but the go.mod files require %q — git will fetch it "+
							"unauthenticated, and GitHub answers a private repo without auth with "+
							"\"not found\", so the build fails naming the wrong cause",
						strings.Join(sortedKeys(have), ", "), want),
				})
			}
		}
	}
	return rep, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
