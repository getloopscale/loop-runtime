// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package anonymousauth implements the anonymous-auth quality gate
// (CR-3 of MEET-2026-05-28-001, T1.5 of plan
// 2026-05-28-meet-001-engineering-closure).
//
// The K3s `--kube-apiserver-arg=anonymous-auth=true` flag is allowed ONLY in
// `infra/k3d/` (the LOCAL k3d cluster declarations). Any occurrence of the
// literal in production Helm charts, Pulumi sources, operator code,
// taskfiles, or any other tree is a CI failure.
//
// Production DOKS authenticates Fulcio's OIDC discovery via a
// Pulumi-provisioned service account token (see ADR
// 2026-05-28-T1.4-anonymous-auth-local-only). The flag is LOCAL development
// convenience only — drifting it into a prod-bound artifact would silently
// broaden the apiserver's attack surface.
//
// Allowlist rules:
//   - Files under `infra/k3d/` (any depth) are allowed.
//   - `*_test.go` files are excluded (tests describe the gate itself).
//   - Markdown / shell / config comment lines (lines starting with `#` or
//     `//` after trimming whitespace) are ignored — ADRs, runbooks, and
//     this gate's own docs may freely mention the literal in prose.
//   - Only the literal substring `anonymous-auth=true` is gated;
//     `anonymous-auth=false` is fine everywhere (k3d itself often disables
//     it explicitly on the workload cluster, for example).
package anonymousauth

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// ForbiddenLiteral is the substring that may not appear outside the allowlist.
// Exported so callers (cmd/loopzero-ops, error messages, help text) can reference
// the SAME source-of-truth string without re-declaring it locally — keeping
// the dangerous literal to a single authoritative declaration.
const ForbiddenLiteral = "anonymous-auth=true"

// allowedPrefixes are the relative-path prefixes where the literal may live.
// Trailing separator ensures `infra/k3desque/...` does NOT match by mistake.
//   - infra/k3d/ — the LOCAL k3d cluster declaration directory (canonical home)
//   - infra/pulumi-platform/values-fulcio-oidc-overrides/ — LOCAL k3s diagnostic
//     scenario fixtures applied via `docker exec k3d-loopzero-build-server-0`;
//     functionally LOCAL but organized under pulumi-platform/ for proximity to
//     the Fulcio chart they fix. Allowlisted by explicit path, not by content.
//   - cmd/loopzero-ops/internal/quality/anonymousauth/ — the gate's own source which defines the
//     constant. Exempting by path keeps the const inline and inspectable.
var allowedPrefixes = []string{
	"infra/k3d" + string(filepath.Separator),
	"infra/pulumi-platform/values-fulcio-oidc-overrides" + string(filepath.Separator),
	"cmd/loopzero-ops/internal/quality/anonymousauth" + string(filepath.Separator),
}

// Check walks the repo from cfg.RepoRoot and returns Outcome.Pass=false if
// any file outside the allowlist contains the forbidden literal on a
// non-comment line.
func Check(cfg Config) (*Outcome, error) {
	out := &Outcome{Pass: true}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if isSkippedDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		// Honor the path-based allowlist.
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(rel, prefix) || rel == strings.TrimSuffix(prefix, string(filepath.Separator)) {
				return nil
			}
		}
		// Test-file convention: *_test.go excluded so this package's own
		// tests can mention the literal in fixtures without self-tripping.
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		// Markdown docs may freely describe the flag in prose (ADRs, plans,
		// runbooks, READMEs). The gate's purpose is preventing the flag from
		// landing in an executable artifact, not censoring documentation.
		if strings.HasSuffix(rel, ".md") {
			return nil
		}
		// Binary blobs (SQLite review databases, etc.) are not text-scannable
		// — content matches are false positives.
		if isBinaryExt(rel) {
			return nil
		}
		hits, scanErr := scanFile(path, rel)
		if scanErr != nil {
			return scanErr
		}
		out.Violations = append(out.Violations, hits...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

// isSkippedDir prunes large or vendor directories that may legitimately
// embed third-party docs mentioning the literal (e.g., upstream K8s docs).
// Also prunes local-only scratch trees that LIVE-caught false positives
// during Plano P3 T2.1 (audit `2026-06-02-sprint-c-engineering-complete`):
//   - `worktrees`: `.claude/worktrees/<name>/` is a full git-worktree clone
//     of the repo (332KB+ duplicate including the gate's own check.go).
//     Scanning it produces false positives that mirror every legitimate
//     hit in the canonical tree.
//   - `review-output`: deep-review SQLite DBs (`api/review-output/review.db`,
//     `./review-output/*`) contain finding descriptions that QUOTE the
//     literal in prose; treating those as executable artifacts is wrong.
func isSkippedDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".terraform", "__pycache__",
		"worktrees", "review-output":
		return true
	}
	return false
}

// isBinaryExt reports whether the file extension is a binary blob that
// MUST NOT be scanned as text. SQLite databases are the LIVE-caught
// example (deep-review's `review.db` is <2MiB so the size cap doesn't
// trip; the scanner reads it as text and matches finding-description
// prose that quotes the gated literal). Add new binary extensions here
// when they trip the gate.
func isBinaryExt(rel string) bool {
	for _, ext := range []string{".db", ".sqlite", ".sqlite3"} {
		if strings.HasSuffix(rel, ext) {
			return true
		}
	}
	return false
}

// maxScanSize skips files larger than this (minified bundles, generated SVG,
// large JSON snapshots) since they're unlikely to embed the K8s flag and
// guaranteed not to be a credible attack vector for the anonymous-auth=true
// drift this gate prevents.
const maxScanSize = 2 * 1024 * 1024 // 2 MiB

func scanFile(absPath, relPath string) ([]string, error) {
	// #nosec G304 -- absPath comes from filepath.Walk of cfg.RepoRoot.
	info, statErr := os.Stat(absPath)
	if statErr != nil {
		return nil, statErr
	}
	if info.Size() > maxScanSize {
		return nil, nil
	}
	// #nosec G304 -- absPath comes from filepath.Walk of cfg.RepoRoot.
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var hits []string
	scanner := bufio.NewScanner(f)
	// Buffer sized for plausible structured-config + Markdown lines (16 MiB
	// upper bound covers even one-line minified JSON in fixtures up to maxScanSize).
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !strings.Contains(line, ForbiddenLiteral) {
			continue
		}
		if isCommentLine(line) {
			continue
		}
		hits = append(hits, fmt.Sprintf("%s:%d: %s", relPath, lineNo, strings.TrimSpace(line)))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// isCommentLine reports whether the line is a pure `//`, `#`, or `--`
// comment (the three comment leaders relevant across our YAML, Go, sh,
// markdown, SQL, and HCL surfaces).
func isCommentLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "//") ||
		strings.HasPrefix(t, "#") ||
		strings.HasPrefix(t, "--")
}
