// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package dashboardrootparity refuses a runbook that names a build path the edge does not
// serve.
//
// # The defect, measured
//
// B-039, 2026-08-18. Deploying a fix to the dashboard required finding out where the code
// lives on the droplet, and finding out was expensive because the obvious path misleads:
//
//	~/loop-console                     is a clone of the UMBRELLA (loop-runtime-workspace), not of the product
//	~/.buildmeta-cloud/loop-console    ad-hoc, refspec restricted to `develop`
//	~/.b017-prova/loop-console         ad-hoc, restricted refspec, and from ANOTHER organisation
//	~/loop-console/loop-console          the canonical one — full refspec, and what Caddy serves
//
// The canonical one always worked: `git fetch` populates `origin/workspace` normally there.
// What did not exist was anyone saying which of the four it was — and the two broken ones are
// the ones a search finds first.
//
// # Why a gate, and not only documentation
//
// Because documentation alone rots in the most dangerous direction. Whoever changes the
// Caddyfile's `root *` leaves the runbook pointing at a path that is no longer served — and
// then the next person pays for the same discovery WITH a document that confirms the error,
// which is worse than having no document.
//
// # The limit, stated out loud
//
// It compares two declarations in this tree: the Caddyfile's `root *` and the path cited in
// the runbook. It does not prove the directory exists on the droplet, that the clone there is
// on the right remote, or that `dist` was built. That half is live and does not fit in a
// hermetic gate.
package dashboardrootparity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrDivergent means the runbook names a path the edge does not serve.
var ErrDivergent = errors.New("the runbook names a path the edge does not serve")

// ErrUndocumented means the runbook names no build path at all — the state before B-039.
var ErrUndocumented = errors.New("the runbook does not say where the dashboard is built")

// ErrNothingServed means the Caddyfile serves no file root, so there is no parity to check.
//
// Reporting "in agreement" here would be green over nothing.
var ErrNothingServed = errors.New("the Caddyfile declares no file root — there is no parity to verify")

// ErrMissingFile means one of the two sides is absent.
var ErrMissingFile = errors.New("file missing")

const (
	caddyfileRel = "infra/local/Caddyfile"
	runbookRel   = "docs/runbooks.md"
)

var (
	// rootRE captures the path served by `root *`.
	rootRE = regexp.MustCompile(`(?m)^\s*root\s+\*\s+(\S+)`)
	// homeRE recognises the workstation prefix, to compare it with the `~/` form.
	//
	// The runbook MUST NOT carry `/home/<user>/`: the `publish-hygiene` gate refuses a
	// workstation path in a versioned file, on the grounds that published history is
	// retroactively public. The `Caddyfile` carries it because it is an exemption WITH A
	// DEADLINE, which is acknowledged debt — not a licence to spread it.
	homeRE = regexp.MustCompile(`^/(?:home|Users)/[^/]+/`)
)

// normalizeHome replaces the workstation prefix with `~/`, so the two sides are comparable
// without the document having to cite anyone's absolute path.
func normalizeHome(p string) string {
	return homeRE.ReplaceAllString(p, "~/")
}

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports both sides, so a failure names them.
type Outcome struct {
	Served     string
	Documented string
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

// Check compares the served root with the path the runbook documents.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	caddy, err := os.ReadFile(filepath.Clean(filepath.Join(root, caddyfileRel)))
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrMissingFile, caddyfileRel)
	}
	m := rootRE.FindStringSubmatch(string(caddy))
	if m == nil {
		return out, fmt.Errorf("%w: %s", ErrNothingServed, caddyfileRel)
	}
	out.Served = m[1]

	// The runbook speaks of the clone's DIRECTORY; Caddy serves `<clone>/dashboard/dist`.
	// Comparing the prefix is what links the two without requiring the text to repeat the suffix.
	expected := normalizeHome(strings.TrimSuffix(out.Served, "/dashboard/dist"))

	runbook, err := os.ReadFile(filepath.Clean(filepath.Join(root, runbookRel)))
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrMissingFile, runbookRel)
	}
	text := string(runbook)

	if strings.Contains(text, expected) {
		out.Documented = expected
		return out, nil
	}

	// It documents SOME clone path, but a different one: divergence, which is the dangerous case.
	cloneRE := regexp.MustCompile(`([~/][A-Za-z0-9_./-]*loop-console[A-Za-z0-9_./-]*)`)
	if c := cloneRE.FindString(text); c != "" {
		out.Documented = c
		out.Violation = fmt.Errorf("%w: serves %q, runbook says %q", ErrDivergent, out.Served, c)
		return out, nil
	}
	out.Violation = fmt.Errorf("%w: the edge serves %q and %s does not mention it",
		ErrUndocumented, out.Served, runbookRel)
	return out, nil
}
