// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package runbookparity refuses an error message that tells the operator to do what the
// runbook forbids.
//
// # The defect, measured
//
// On 2026-08-18, following the runbook `docs/runbooks.md` to fix an error that fired
// every 2 seconds on the droplet, I found the contradiction:
//
//	api/.../argo_step_watcher.go  logged    "build cluster auth failed — rotate loop-runtime-build-sa token"
//	the runbook says                        "Do NOT mint `loop-runtime-build-sa` here"
//
// And the runbook explains the consequence: `loop-runtime-build-sa` is the workflow **executor**, bound
// only to `workflowtaskresults: [create, patch]`. Its token **authenticates** (the 401 goes
// away) and is **forbidden** from listing workflows (403). *"Minting the wrong SA turns a 401
// into a silent 403 that looks like a fresh failure."*
//
// In other words: the error message led the operator to trade a readable defect for a silent
// one. The correct SA is `loop-runtime-workflow-submitter`.
//
// # What this gate checks
//
// That the ServiceAccount **named in the message** is the same one the runbook **prescribes**.
// It is hermetic — it reads the tree, not the cluster — and it was born from a real defect, not
// from a hypothesis.
//
// # The limit, said out loud
//
// It covers **this** family: token-rotation messages that name a ServiceAccount. A message
// that contradicts the runbook in some other way — wrong port, wrong namespace, wrong command —
// passes. Promising general parity between runbook and code would be the overstatement this
// ecosystem already measures in several items.
package runbookparity

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrContradiction means a message names a ServiceAccount the runbook forbids.
var ErrContradiction = errors.New("error message contradicts the runbook")

// ErrRunbookMissing means the prescribing runbook is absent.
//
// Without a prescription there is nothing to compare against, and reporting "no
// contradiction" would be green over nothing.
var ErrRunbookMissing = errors.New("runbook missing — without a prescription there is no parity to check")

// ErrNothingScanned means no rotation message was found.
var ErrNothingScanned = errors.New("no rotation message scanned — finding nothing is not the same as finding nothing wrong")

// runbookRel is the prescribing document.
const runbookRel = "docs/runbooks.md"

var (
	// rotateMsgRE captures the SA the message says to rotate.
	rotateMsgRE = regexp.MustCompile(`rotate\s+([a-z0-9-]+)\s+token`)
	// prescribedRE captures the SA the runbook says to mint.
	prescribedRE = regexp.MustCompile(`create\s+token\s+([a-z0-9-]+)`)
	// forbiddenRE captures the SA the runbook explicitly forbids.
	forbiddenRE = regexp.MustCompile("NOT\\*{0,2}\\s+mint\\s+`?([a-z0-9-]+)`?")
)

// normalize strips markdown blockquote prefixes and collapses whitespace, so a prescription
// broken across lines is read as one sentence.
func normalize(md string) string {
	var b strings.Builder
	for _, line := range strings.Split(md, "\n") {
		b.WriteString(strings.TrimPrefix(strings.TrimSpace(line), "> "))
		b.WriteString(" ")
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Finding is one message contradicting the runbook.
type Finding struct {
	File       string
	Line       int
	Named      string
	Prescribed string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned  int
	Findings []Finding
	Reason   string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// Check compares every rotation message against the runbook's prescription.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, runbookRel)))
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrRunbookMissing, runbookRel)
	}
	// Normalize BEFORE matching: in the real markdown the prohibition breaks across two
	// lines and the second starts with the quote's `> `, so `\s+` does not reach the SA
	// name. The first version of this gate was blind to exactly that, and the population
	// scan passed through blindness rather than cleanliness — the B-090 trap, third time
	// in the session.
	flat := normalize(string(body))
	prescribed := ""
	if m := prescribedRE.FindStringSubmatch(flat); m != nil {
		prescribed = m[1]
	}
	forbidden := map[string]bool{}
	for _, m := range forbiddenRE.FindAllStringSubmatch(flat, -1) {
		forbidden[m[1]] = true
	}
	if prescribed == "" {
		return out, fmt.Errorf("%w: %s prescribes no `create token <sa>`", ErrRunbookMissing, runbookRel)
	}

	err = filepath.Walk(filepath.Join(root, "api"), func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		// #nosec G122 -- `p` comes from the Walk itself over `api/`, a root derived from a
		// constant, and it is only READ. There is no write or execution; the worst case of a
		// symlink is reading a .go that is not ours, and the gate reports the line, it does
		// not follow it.
		f, oerr := os.Open(filepath.Clean(p))
		if oerr != nil {
			// An unread file may be the one that contradicts the runbook.
			return oerr
		}
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		n := 0
		for sc.Scan() {
			n++
			line := sc.Text()
			// A comment is documentation, not emission. The comment that records THIS fix
			// quotes the old message, and the first version of the gate matched it — the
			// `publishhygiene` trap, where the text describing the defect triggers the
			// detector. Rewriting the comment would fix it from the outside and leave the
			// gate wrong.
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			m := rotateMsgRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			out.Scanned++
			named := m[1]
			if named == prescribed {
				continue
			}
			// It is a contradiction only when the runbook FORBIDS the named one. An SA the
			// runbook simply does not mention may belong to another procedure.
			if !forbidden[named] {
				continue
			}
			rel, _ := filepath.Rel(root, p)
			out.Findings = append(out.Findings, Finding{File: rel, Line: n, Named: named, Prescribed: prescribed})
		}
		return sc.Err()
	})
	if err != nil {
		return out, fmt.Errorf("scanning api/: %w", err)
	}

	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}
	if len(out.Findings) > 0 {
		f := out.Findings[0]
		out.Violation = fmt.Errorf("%w: %s:%d says to rotate %q, and the runbook forbids exactly that — the correct SA is %q. The wrong one authenticates and is FORBIDDEN, trading a readable 401 for a silent 403",
			ErrContradiction, f.File, f.Line, f.Named, f.Prescribed)
		return out, nil
	}
	out.Reason = fmt.Sprintf("%d rotation message(s) scanned, all naming the ServiceAccount the runbook prescribes (%s)", out.Scanned, prescribed)
	return out, nil
}
