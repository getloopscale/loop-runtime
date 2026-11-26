// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package decisionsdeletion implements the T1.14 gate: fail CI if a protected
// governance document is deleted in a PR without a sibling file carrying a
// `superseded_by:` reference to it.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3 replaced
// infra/scripts/check-decisions-deletion.sh with this package.
//
// The protected set follows the docs/ consolidation of 2026-09-11: the ADRs,
// meeting records and runbooks that used to live under docs/wiki/ became the
// living documents docs/decisions.md and docs/runbooks.md. Deleting one of
// them requires a superseding .md in the same directory whose content says
// `superseded_by: <basename>` (or lists the basename as a YAML item).
//
// Every git failure is a gate error. An unknown ref, a shallow clone or a
// non-repository used to read as "nothing deleted", which is a clean result
// the gate never measured.
package decisionsdeletion

import (
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/revrange"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// ProtectedDocs are the governance documents under deletion guard, relative
// to the repository root. Each must exist at the base ref.
var ProtectedDocs = []string{
	"docs/decisions.md",
	"docs/runbooks.md",
}

// Config drives the gate.
type Config struct {
	RepoRoot string // dir to run git in (default ".")
	BaseRef  string // default origin/main
	HeadRef  string // default HEAD
}

// Outcome captures the result.
type Outcome struct {
	Pass         bool
	DeletedFiles []string // protected files deleted in the diff
	Superseded   []string // "deleted -> superseder" for properly-superseded ones
	Violations   []string // deleted without a superseder
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check runs the gate.
func Check(cfg Config) (*Outcome, error) {
	root, base, head := withDefaults(cfg)
	for _, ref := range []string{base, head} {
		if err := revrange.VerifyReachable(root, ref); err != nil {
			return nil, err
		}
	}
	for _, doc := range ProtectedDocs {
		if _, err := git(root, "cat-file", "-e", base+":"+doc); err != nil {
			return nil, fmt.Errorf("decisions-deletion: protected document %s does not exist at %s, "+
				"so the gate would guard nothing — update ProtectedDocs to the current layout: %w", doc, base, err)
		}
	}

	deleted, err := deletedProtectedDocs(root, base, head)
	if err != nil {
		return nil, err
	}

	out := &Outcome{Pass: true, DeletedFiles: deleted}
	for _, doc := range deleted {
		superseder, err := findSuperseder(root, head, doc)
		if err != nil {
			return nil, err
		}
		if superseder == "" {
			out.Violations = append(out.Violations, doc)
			continue
		}
		out.Superseded = append(out.Superseded, fmt.Sprintf("%s -> %s", doc, superseder))
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func withDefaults(cfg Config) (root, base, head string) {
	root, base, head = cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef
	if root == "" {
		root = "."
	}
	if base == "" {
		base = "origin/main"
	}
	if head == "" {
		head = "HEAD"
	}
	return root, base, head
}

// deletedProtectedDocs lists the protected documents deleted between base and
// head. The diff runs over the whole tree with rename detection: restricting
// it to the protected paths would hide a rename's destination and report the
// renamed document as deleted.
func deletedProtectedDocs(root, base, head string) ([]string, error) {
	lines, err := gitLines(root, "diff", "-M", "--name-status", "--diff-filter=D", base+"..."+head)
	if err != nil {
		return nil, err
	}
	protected := make(map[string]bool, len(ProtectedDocs))
	for _, doc := range ProtectedDocs {
		protected[doc] = true
	}
	var deleted []string
	for _, l := range lines {
		fields := strings.Split(l, "\t")
		if len(fields) == 2 && protected[fields[1]] {
			deleted = append(deleted, fields[1])
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}

// findSuperseder returns the first .md sibling of deleted at head whose
// content references its basename via `superseded_by:` or a YAML list item,
// or "" when there is none.
func findSuperseder(root, head, deleted string) (string, error) {
	q := regexp.QuoteMeta(path.Base(deleted))
	re := regexp.MustCompile(`(?m)^superseded_by:.*` + q + `|^\s+- ` + q)
	dir := path.Dir(deleted)
	candidates, err := gitLines(root, "ls-tree", "--name-only", head, dir+"/")
	if err != nil {
		return "", err
	}
	for _, cand := range candidates {
		if !strings.HasSuffix(cand, ".md") {
			continue
		}
		body, err := git(root, "show", head+":"+cand)
		if err != nil {
			return "", err
		}
		if re.MatchString(body) {
			return cand, nil
		}
	}
	return "", nil
}

// gitLines runs git under root and returns its non-empty output lines.
func gitLines(root string, args ...string) ([]string, error) {
	out, err := git(root, args...)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// git runs one git command under root; a failure carries its stderr.
func git(root string, args ...string) (string, error) {
	// #nosec G204 -- refs and paths come from the gate's own config.
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return "", fmt.Errorf("decisions-deletion: git %s: %w: %s", strings.Join(args, " "), err, stderr)
	}
	return string(out), nil
}
