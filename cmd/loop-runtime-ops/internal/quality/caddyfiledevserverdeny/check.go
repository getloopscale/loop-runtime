// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package caddyfiledevserverdeny enforces defense-in-depth for loop-console#141
// (SECURITY): the app-dev.getloopscale.com host block MUST explicitly 404 the
// dev-server-only paths so that even a stale/dev build accidentally landed
// in DASHBOARD_DIST cannot expose /src/*, /.env, /@vite/*, /node_modules/*,
// or config files.
//
// The actual root cause for #141 was upstream (the loop-console dashboard pod
// running `vite dev` instead of serving the prod bundle via nginx); that fix
// belongs in the loop-console repo. This edge-side deny is defense-in-depth
// against future regressions.
//
// Invariants:
//  1. `@dev_server_only` named matcher is defined in the app-dev block.
//  2. It covers, at minimum: /src/*, /@vite/*, /node_modules/*, /.env,
//     /vite.config.*, /package.json.
//  3. The block explicitly responds 404 for the matcher via
//     `respond @dev_server_only 404`.
//  4. The matcher is declared BEFORE any /assets/ or catch-all handler so
//     the deny cannot be bypassed via `try_files` fallback to index.html.
package caddyfiledevserverdeny

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qgshell"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Skipped    bool // Caddyfile absent
	Violations []string
}

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

// requiredPaths are the minimum path prefixes the @dev_server_only matcher
// MUST cover per loop-console#141 reproduction. Missing any triggers a
// violation with the specific path listed.
var requiredPaths = []string{
	"/src/*",
	"/@vite/*",
	"/node_modules/*",
	"/.env",
	"/vite.config.*",
	"/package.json",
}

var blockOpenRe = regexp.MustCompile(`(?m)^app-dev\.getloopscale\.com\s*\{`)

// Check enforces the loop-console#141 defense-in-depth invariants against
// the app-dev host block of infra/local/Caddyfile.
func Check(cfg Config) (*Outcome, error) {
	caddyfilePath := filepath.Join(cfg.RepoRoot, "infra", "local", "Caddyfile")
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Outcome{Pass: true, Skipped: true}, nil
		}
		return nil, fmt.Errorf("read Caddyfile: %w", err)
	}

	content := string(data)
	out := &Outcome{Pass: true}

	block, ok := extractAppDevBlock(content)
	if !ok {
		out.Pass = false
		out.Violations = append(out.Violations,
			"app-dev.getloopscale.com block not found in infra/local/Caddyfile")
		return out, nil
	}

	// Invariant 1: @dev_server_only matcher is defined.
	matcherRe := regexp.MustCompile(`@dev_server_only\s*\{`)
	if !matcherRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_dev_server_only_matcher_defined — @dev_server_only named matcher missing in app-dev block (loop-console#141 defense-in-depth)")
		return out, nil
	}

	// Invariant 2: matcher covers all required paths.
	matcherBody, ok := extractMatcherBody(block)
	if !ok {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_dev_server_only_matcher_body_parseable — @dev_server_only matcher body could not be parsed")
		return out, nil
	}
	for _, path := range requiredPaths {
		if !strings.Contains(matcherBody, path) {
			out.Pass = false
			out.Violations = append(out.Violations, fmt.Sprintf(
				"assert_dev_server_only_covers_%s — @dev_server_only matcher does not cover %q (regression of loop-console#141)",
				sanitize(path), path))
		}
	}

	// Invariant 3: block responds 404 for the matcher.
	respondRe := regexp.MustCompile(`respond\s+@dev_server_only\s+404`)
	if !respondRe.MatchString(block) {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_dev_server_only_returns_404 — `respond @dev_server_only 404` missing (loop-console#141)")
	}

	// Invariant 4: matcher declared BEFORE the catch-all `handle {` (else
	// bypass via try_files fallback). Match `handle {` at the START of a
	// line (after leading whitespace) so comments referencing the pattern
	// (`# catch-all handle {}`) do not count.
	matcherIdx := strings.Index(block, "@dev_server_only")
	catchAllRe := regexp.MustCompile(`(?m)^\s*handle\s*\{`)
	catchAllLoc := catchAllRe.FindStringIndex(block)
	catchAllIdx := -1
	if catchAllLoc != nil {
		catchAllIdx = catchAllLoc[0]
	}
	if matcherIdx == -1 || catchAllIdx == -1 || matcherIdx > catchAllIdx {
		out.Pass = false
		out.Violations = append(out.Violations,
			"assert_matcher_before_catchall — @dev_server_only must appear BEFORE `handle {` (catch-all) so try_files fallback cannot bypass the deny")
	}

	return out, nil
}

// extractAppDevBlock returns the body between the opening
// `app-dev.getloopscale.com {` and its matching closing brace.
func extractAppDevBlock(content string) (string, bool) {
	return qgshell.ExtractBraceBlock(content, blockOpenRe)
}

// extractMatcherBody returns the body between `@dev_server_only {` and its
// matching closing brace within the app-dev block.
func extractMatcherBody(block string) (string, bool) {
	return qgshell.ExtractBraceBlock(block, devServerMatcherOpenRe)
}

// devServerMatcherOpenRe opens the @dev_server_only matcher block.
var devServerMatcherOpenRe = regexp.MustCompile(`@dev_server_only\s*\{`)

// sanitize turns a path like /src/* into a lowercase identifier-safe token
// for the violation ID (assert_dev_server_only_covers_src_).
func sanitize(path string) string {
	r := strings.NewReplacer(
		"/", "_", "*", "", ".", "_", "@", "at_",
	)
	return strings.TrimLeft(r.Replace(path), "_")
}
