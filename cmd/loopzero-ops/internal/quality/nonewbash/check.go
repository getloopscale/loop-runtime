// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package nonewbash implements the D3 governance gate from
// 2026-05-22-shell-script-strategic-plan: "Creation of new
// bash scripts is FORBIDDEN by default; exceptions require entry in
// infra/quality/shell-scripts-allowlist.md".
//
// The gate scans `git diff --diff-filter=AR base...head -- '*.sh'`
// (added + renamed-to) and fails if any new script is not in the
// allowlist. Modified (M) and deleted (D) scripts are NOT blocked.
//
// EC-04: gate fails LOUD with `BASE_REF_UNREACHABLE` when base-ref is
// unreachable (shallow clone), rather than silently treating "everything
// is new". Operators MUST set actions/checkout fetch-depth: 0.
//
// EC-06: renames (R status) treated as new at the new path.
package nonewbash

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/revrange"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellscript"
)

// Config tells Check where to operate.
type Config struct {
	RepoRoot      string // absolute path to repo root
	AllowlistPath string // path to allowlist MD relative to RepoRoot
	BaseRef       string // base git ref (e.g., "origin/main")
	HeadRef       string // head git ref (e.g., "HEAD")
}

// Outcome captures the result of a Check invocation.
type Outcome struct {
	Pass       bool     // true if no violations
	Violations []string // human-readable per-violation messages
}

// Check is the entry point invoked by `loopzero-ops quality no-new-bash`.
// Returns outcome + error. Error is reserved for INFRASTRUCTURE failures
// (git missing, allowlist unreadable, base-ref unreachable); policy
// violations come back via Outcome.Violations + Pass=false.
func Check(cfg Config) (*Outcome, error) {
	// 1. Read allowlist
	allowlistFull := filepath.Join(cfg.RepoRoot, cfg.AllowlistPath)
	allowlistBytes, err := os.ReadFile(allowlistFull)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// AN ABSENT ALLOWLIST DECLARES NO EXEMPTIONS. It is not an infrastructure
		// failure, and the difference decides whether this gate can run at all.
		//
		// The allowlist lived under `.claude/`, which `3d4075ca3` (2026-09-08)
		// removed from tracking — so it exists in no clone, on no runner, and for
		// no contributor. Measured 2026-09-16: this read exited 5 and blocked the
		// repository's own pre-push hook.
		//
		// Absence reads as "nothing is exempt", the STRICTEST answer available and
		// the one this gate is for: refusing new shell scripts. A gate whose
		// default exempts nothing cannot be weakened by losing its file.
		allowlistBytes = nil
	case err != nil:
		// Present and unreadable is a different claim, and stays fatal: calling it
		// "no exemptions" would silently discard exemptions somebody wrote.
		return nil, fmt.Errorf("read allowlist %s: %w", cfg.AllowlistPath, err)
	}
	allowed, err := parseAllowlist(string(allowlistBytes))
	if err != nil {
		return nil, fmt.Errorf("parse allowlist: %w", err)
	}

	// 2. EC-04 pre-flight: verify base-ref reachable
	if err := revrange.VerifyReachable(cfg.RepoRoot, cfg.BaseRef); err != nil {
		return nil, err
	}

	// 3. Get new (A + R) .sh files from diff
	added, err := gitDiffAdded(cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

	// 4. Check each new script against allowlist
	outcome := &Outcome{Pass: true}
	for _, path := range added {
		if !allowed[path] {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations,
				fmt.Sprintf("new bash script %q not in allowlist (%s). "+
					"Per plan D3: add entry to allowlist + ADR, OR use Go/helmfile alternative.",
					path, cfg.AllowlistPath))
		}
	}
	return outcome, nil
}

// gitDiffAdded runs `git diff --name-only --diff-filter=AR base...head -- '*.sh'`
// and returns the list of paths (added or renamed-to).
//
// Renames are interesting: `--diff-filter=R` shows the new path (the one
// after rename). git diff's default rename detection treats them as A + D
// of separate paths unless -M flag is used; here we explicitly include R
// because the new path is what the allowlist must cover.
func gitDiffAdded(repoRoot, base, head string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=AR",
		base+"..."+head, "--", "*.sh")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w\n%s", err, out)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasSuffix(line, ".sh") {
			continue
		}
		paths = append(paths, line)
	}
	return paths, nil
}

// parseAllowlist extracts the set of allowed .sh paths from the markdown
// content. Robust to:
//   - empty file
//   - non-table content interspersed with tables
//   - non-.sh entries (silently skipped)
//   - whitespace variations in table cells
//   - header separator rows (|---|---|) which never match the .sh pattern
func parseAllowlist(content string) (map[string]bool, error) {
	allowed := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		row, ok := shellscript.ParseAllowlistRow(line)
		if !ok {
			continue
		}
		allowed[row.Path] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return allowed, nil
}
