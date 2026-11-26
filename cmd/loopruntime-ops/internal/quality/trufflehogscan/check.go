// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package trufflehogscan refuses to call clean what it could not verify.
//
// # The defect this package fixed, measured
//
// Until 2026-08-18 this gate had **three** paths ending in `exit 0`, and two of them
// did not know whether there was a secret:
//
//	binary missing         →  Skipped: true            →  exit 0
//	execution error        →  Skipped: true            →  exit 0   ← the code
//	                          comment said "(network? git access?)" and returned success
//	exit 0 --only-verified →  Pass: "scan clean"       →  exit 0
//
// The third is the one that cost five months. `--only-verified` asks *"is any candidate
// live?"*, and that question **requires the network**. Without a network no candidate verifies, the
// tool exits 0, and the gate printed "scan clean (no secrets detected)". **The absence of an
// answer was read as a negative answer** — and a live DigitalOcean token went through
// 2026-03-22 → 2026-08-17 and several pushes on the same day (B-142, issue #323).
//
// The previous test suite could not have caught anything: three tests, one of them running the
// real gate and discarding the result with `_ = got`.
//
// # One pass, two facts
//
// The `--json` output carries `Verified` **per finding**. One run answers both
// questions — all findings are the CANDIDATE SET, the `Verified:true` ones are the
// LIVE ones. The first draft asked for two passes; the measurement knocked the design down (~70s each,
// and what dominates is the walk over the 5,420 commits, not the network).
//
//	binary missing                       →  FAIL
//	execution error (network, git, timeout) →  FAIL — never again SKIP → exit 0
//	any Verified:true finding            →  FAIL — the baseline does NOT suppress
//	candidate outside the baseline       →  FAIL — new and not adjudicated
//	everything adjudicated, nothing live →  PASS
//
// What changes: a green used to mean *"nothing verified as live"* — a claim the network
// can fabricate. It now means *"the candidate set is exactly what has already been
// adjudicated, and nothing in it is live"*, and the first half is **offline and deterministic**.
// The token would have been blocked on the day it came in: a new candidate. In March, not in August.
//
// # The baseline, and why it is not a bypass
//
// A new candidate **blocks** until someone rewrites the baseline, and the rewrite shows up as a diff
// in the PR — that diff is the adjudication. Same mechanism as the `testpyramid` ratchet
// (B-106) and the `publishhygiene` exemptions (B-130).
//
// It is generated with `--no-verification`, and that was **measured**, not chosen: that mode
// produces a strict superset of the verified one (67 ⊇ 65; the reverse is empty — two `JDBC`
// candidates that verification discards). Generating from the superset prevents the
// opposite failure mode: if the network drops and the verified scan starts seeing those two, they are already
// in the baseline, and the gate does not fail because of a network accident.
//
// # The residual, said out loud
//
// An **already adjudicated** candidate that becomes live is only caught when verification runs.
// It is not a closed hypothesis — it is what is left of this design, and it is written here instead of being
// discovered by someone later.
//
// Decision recorded in
// `.claude/knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-18-secret-gate-cannot-call-unverifiable-clean.md`
// (T2, pending ratification).
package trufflehogscan

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// The four ways this gate refuses. Each is typed so the caller can never collapse
// "I could not verify" into "there is none" — the whole defect this package closed.
var (
	// ErrLiveSecret means a candidate was confirmed live by its provider.
	ErrLiveSecret = errors.New("LIVE credential in the history")
	// ErrUnadjudicated means a candidate is absent from the baseline.
	ErrUnadjudicated = errors.New("new candidate not adjudicated")
	// ErrScannerMissing means trufflehog is not installed.
	//
	// FAIL and not skip: a missing suite shows up in the report, but a missing secret
	// scanner produces exactly the same `exit 0` as a clean repository.
	// Same precedent as `helmfile` in taskfiles/quality.yml.
	ErrScannerMissing = errors.New("trufflehog not found — without it a green cannot tell clean from unscanned")
	// ErrScanFailed means the scan could not complete (network, git, timeout).
	ErrScanFailed = errors.New("scan did not complete")
	// ErrNothingScanned means the scan produced no candidate while the baseline holds some.
	//
	// A baseline with entries and a scan with zero candidates is not a tree that
	// became clean — it is a scan that did not reach the history. Reporting green there is the
	// `PASS — 0 routes` shape this repository has already paid for three times.
	ErrNothingScanned = errors.New("scan reached nothing — finding nothing is not the same as finding nothing wrong")
)

// defaultTimeout — the scan measures ~70s on this tree; the ceiling exists so that a
// hung scan fails instead of locking the delivery gate forever (EC-4).
const defaultTimeout = 20 * time.Minute

// Scanner runs the secret scan. It is the seam that takes the network and the binary out of the
// tests' path (DIP) — without it, proving the offline behavior would require being offline.
type Scanner interface {
	Scan(ctx context.Context, repoRoot string) (string, error)
}

// Config drives the check.
type Config struct {
	RepoRoot     string
	BaselinePath string
	Timeout      time.Duration
	// Scanner defaults to the real trufflehog process.
	Scanner Scanner
}

// Outcome reports what was established — never merely that nothing was seen.
type Outcome struct {
	Candidates    int
	Live          int
	NewCandidates []Finding
	StaleEntries  []Entry
	Reason        string
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

// Check runs the scan and applies the policy.
func Check(ctx context.Context, cfg Config) (Outcome, error) {
	var out Outcome

	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	basePath := cfg.BaselinePath
	if basePath == "" {
		basePath = filepath.Join(root, defaultBaselineRel)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	scanner := cfg.Scanner
	if scanner == nil {
		scanner = execScanner{}
	}

	// The baseline is read BEFORE the scan: without the adjudicated set no green is possible,
	// and spending 70s only to find that out afterwards is waste.
	base, err := LoadBaseline(basePath)
	if err != nil {
		return out, err
	}

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := scanner.Scan(scanCtx, root)
	if err != nil {
		// The guarantee comes from HERE, not from the implementation: any Scanner that returns a raw
		// error — network, deadline, git — becomes ErrScanFailed. Leaving that to each
		// implementation is how the skip would come back without anyone noticing.
		if errors.Is(err, ErrScannerMissing) || errors.Is(err, ErrScanFailed) {
			return out, err
		}
		return out, fmt.Errorf("%w: %w", ErrScanFailed, err)
	}
	findings, err := ParseFindings(strings.NewReader(raw))
	if err != nil {
		return out, err
	}

	out.Candidates = len(findings)
	for _, f := range findings {
		if f.Live {
			out.Live++
		}
	}
	out.NewCandidates = base.Unadjudicated(findings)
	out.StaleEntries = base.Stale(findings)

	// Before any verdict about content: did the scan reach the history?
	// If the tree really WAS cleaned, the fix is to rewrite the baseline — a human confirming
	// "yes, everything is gone". The gate does not decide that on its own, silently.
	if len(base.Entries) > 0 && out.Candidates == 0 {
		return out, fmt.Errorf("%w: the baseline has %d entry(ies) and the scan returned zero candidate(s)",
			ErrNothingScanned, len(base.Entries))
	}

	// Deliberate order: the live one fails BEFORE the unadjudicated one, because it is the more
	// serious defect and because the baseline cannot suppress it (ADR-3).
	if out.Live > 0 {
		var names []string
		for _, f := range findings {
			if f.Live {
				names = append(names, f.String())
			}
		}
		out.Violation = fmt.Errorf("%w: %s", ErrLiveSecret, strings.Join(names, "; "))
		return out, nil
	}
	if len(out.NewCandidates) > 0 {
		var names []string
		for _, f := range out.NewCandidates {
			names = append(names, f.String())
		}
		out.Violation = fmt.Errorf("%w (%d): %s — remove the secret, or adjudicate with `--write` (which REFUSES while any live one exists)",
			ErrUnadjudicated, len(out.NewCandidates), strings.Join(names, "; "))
		return out, nil
	}

	// The reason says what was ESTABLISHED. "scan clean" is the phrase that sustained five
	// months of exposure: it does not distinguish "there is none" from "I did not verify".
	out.Reason = fmt.Sprintf("%d candidate(s) scanned, all adjudicated; %d verified as live; %d baseline entry(ies) with no finding in this pass",
		out.Candidates, out.Live, len(out.StaleEntries))
	return out, nil
}

// execScanner runs the real trufflehog process.
type execScanner struct {
	binary string
	// noVerification drives baseline generation — the measured superset.
	noVerification bool
}

func (e execScanner) Scan(ctx context.Context, root string) (string, error) {
	bin := e.binary
	if bin == "" {
		bin = "trufflehog"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScannerMissing, err)
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScanFailed, err)
	}
	args := []string{"git", "file://" + abs, "--json", "--no-update"}
	if e.noVerification {
		args = append(args, "--no-verification")
	}

	// #nosec G204 -- `path` comes from exec.LookPath over a binary name owned by the
	// gate itself ("trufflehog"), overridden only by tests; `args` are literals and the
	// repository root already made absolute. Nothing here is user input.
	cmd := exec.CommandContext(ctx, path, args...)
	stdout, runErr := cmd.Output()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && isScanRan(exitErr.ExitCode()) {
			return string(stdout), nil
		}
		return "", fmt.Errorf("%w: %w", ErrScanFailed, runErr)
	}
	return string(stdout), nil
}

// isScanRan reports whether an exit code means the scan completed.
//
// With `--fail` trufflehog exits 183 when it finds something; without it, it exits 0. This gate does not pass
// `--fail` (it decides from the JSON), but accepts both so it does not depend on which behavior
// the installed version has (EC-7).
func isScanRan(code int) bool { return code == 0 || code == 183 }

// Generated reports an adjudicated baseline rewritten from a scan.
type Generated struct {
	// Candidates is the number of findings the baseline now adjudicates.
	Candidates int
}

// Verdict is Pass: generating is an operation, and a Generated exists only
// when the baseline was written. A live finding refuses the write, as an error.
func (Generated) Verdict() verdict.Verdict { return verdict.Pass }

// Generate (re)writes the adjudicated baseline.
//
// Runs with `--no-verification` ON PURPOSE: that mode is a strict superset of the
// verified one (measured 67 ⊇ 65), and generating from the superset is what prevents a false
// red when the network drops. Generating from the smaller set would produce exactly that — and a
// lock that fails because of a network accident gets switched off.
//
// The refusal when a live finding exists lives in WriteBaseline, where it cannot be bypassed.
func Generate(ctx context.Context, cfg Config) (Generated, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	basePath := cfg.BaselinePath
	if basePath == "" {
		basePath = filepath.Join(root, defaultBaselineRel)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	scanner := cfg.Scanner
	if scanner == nil {
		scanner = execScanner{noVerification: true}
	}

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := scanner.Scan(scanCtx, root)
	if err != nil {
		if errors.Is(err, ErrScannerMissing) || errors.Is(err, ErrScanFailed) {
			return Generated{}, err
		}
		return Generated{}, fmt.Errorf("%w: %w", ErrScanFailed, err)
	}
	findings, err := ParseFindings(strings.NewReader(raw))
	if err != nil {
		return Generated{}, err
	}
	if err := WriteBaseline(basePath, findings); err != nil {
		return Generated{}, err
	}
	b, err := LoadBaseline(basePath)
	if err != nil {
		return Generated{}, err
	}
	return Generated{Candidates: len(b.Entries)}, nil
}
