// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package trufflehogscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// stubScanner is the seam that takes the network and the binary out of the test's path (DIP).
type stubScanner struct {
	out string
	err error
}

func (s stubScanner) Scan(context.Context, string) (string, error) { return s.out, s.err }

func baseWith(t *testing.T, fs []Finding) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "b.json")
	if err := WriteBaseline(p, fs); err != nil {
		t.Fatal(err)
	}
	return p
}

func run(t *testing.T, sc Scanner, basePath string) (Outcome, error) {
	t.Helper()
	return violationOf(Check(context.Background(), Config{
		RepoRoot: t.TempDir(), BaselinePath: basePath, Scanner: sc,
	}))
}

// ADR-3. Human adjudication covers "this is not a secret" — never "this secret may live here".
func TestLiveFindingFailsEvenWhenInTheBaseline(t *testing.T) {
	dead := found(t, line("DigitalOceanToken", "x.ts", fakeSecret(), false))
	base := baseWith(t, dead) // same identity: Verified does not go into the hash
	live := line("DigitalOceanToken", "x.ts", fakeSecret(), true)

	out, err := run(t, stubScanner{out: live}, base)
	if !errors.Is(err, ErrLiveSecret) {
		t.Fatalf("a live finding fails even when adjudicated; err=%v", err)
	}
	if out.Live != 1 {
		t.Fatalf("the live one must be counted; got %+v", out)
	}
}

// The B-142 case — and it would have been blocked in MARCH, not in August.
func TestNewCandidateFails(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", pgURI(), false)))
	out, err := run(t, stubScanner{out: strings.Join([]string{
		line("Postgres", "a.md", pgURI(), false),
		line("DigitalOceanToken", "infra/b.ts", fakeSecret(), false),
	}, "\n")}, base)

	if !errors.Is(err, ErrUnadjudicated) {
		t.Fatalf("a candidate outside the baseline fails; err=%v", err)
	}
	if len(out.NewCandidates) != 1 || out.NewCandidates[0].Detector != "DigitalOceanToken" {
		t.Fatalf("the new one must be NAMED to be adjudicable; got %+v", out.NewCandidates)
	}
}

// Measured path 2: the code named the network as the cause and returned exit 0.
func TestExecutionErrorFailsAndIsNotASkip(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", "x", false)))
	_, err := run(t, stubScanner{err: errors.New("dial tcp: network is unreachable")}, base)
	if !errors.Is(err, ErrScanFailed) {
		t.Fatalf("an execution error fails; err=%v", err)
	}
	if errors.Is(err, ErrLiveSecret) {
		t.Fatal("an execution error is not the same defect as a live secret")
	}
}

// Path 1: a missing scanner produces the SAME exit 0 as a clean repository.
func TestMissingBinaryFails(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", "x", false)))
	_, err := violationOf(Check(context.Background(), Config{
		RepoRoot: t.TempDir(), BaselinePath: base,
		Scanner: execScanner{binary: "trufflehog-that-does-not-exist-" + t.Name()},
	}))
	if !errors.Is(err, ErrScannerMissing) {
		t.Fatalf("a missing binary fails; err=%v", err)
	}
}

func TestAdjudicatedSetWithoutLiveFindingsPasses(t *testing.T) {
	fs := found(t,
		line("Postgres", "a.md", pgURI(), false),
		line("URI", "b.md", credURI(), false))
	base := baseWith(t, fs)

	out, err := run(t, stubScanner{out: strings.Join([]string{
		line("Postgres", "a.md", pgURI(), false),
		line("URI", "b.md", credURI(), false),
	}, "\n")}, base)
	if err != nil {
		t.Fatalf("an adjudicated set with no live findings passes: %v (%+v)", err, out)
	}
	if out.Candidates != 2 || out.Live != 0 {
		t.Fatalf("wrong counts: %+v", out)
	}
}

// The green must say WHAT WAS ESTABLISHED. "scan clean" is the phrase that sustained
// five months of exposure: it does not distinguish "there is none" from "I did not verify".
func TestGreenSaysWhatWasEstablished(t *testing.T) {
	fs := found(t, line("Postgres", "a.md", pgURI(), false))
	out, err := run(t, stubScanner{out: line("Postgres", "a.md", pgURI(), false)}, baseWith(t, fs))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Reason, "clean") {
		t.Fatalf("the reason must not recycle the phrase that hid the defect: %q", out.Reason)
	}
	if !strings.Contains(out.Reason, "1") {
		t.Fatalf("the reason must cite what was measured: %q", out.Reason)
	}
}

// EC-4. A hung scan locks the delivery gate forever.
func TestExpiredDeadlineFails(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", "x", false)))
	_, err := violationOf(Check(context.Background(), Config{
		RepoRoot: t.TempDir(), BaselinePath: base, Timeout: time.Nanosecond,
		Scanner: sleepyScanner{},
	}))
	if !errors.Is(err, ErrScanFailed) {
		t.Fatalf("an expired deadline fails, never skips; err=%v", err)
	}
}

type sleepyScanner struct{}

func (sleepyScanner) Scan(ctx context.Context, _ string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// EC-7. With --fail trufflehog exits 183; without it, it exits 0. The gate must not depend on which
// behavior the installed version has.
func TestExitCodes0And183AreValidRuns(t *testing.T) {
	for _, code := range []int{0, 183} {
		if !isScanRan(code) {
			t.Fatalf("code %d is a valid run", code)
		}
	}
	for _, code := range []int{1, 2, 127} {
		if isScanRan(code) {
			t.Fatalf("code %d is an execution error", code)
		}
	}
}

// EC-2 at the gate level: without a baseline, no green is possible.
func TestNoBaselineMeansNoGreen(t *testing.T) {
	_, err := run(t, stubScanner{out: ""}, filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("without a baseline the gate cannot report clean; err=%v", err)
	}
}

// The real population (rules/testing.md § 4.2). The synthetic mutants above prove the lock
// SEES each shape; only this proves what it sees in what exists — and it is the assertion the
// previous suite did not have (three tests, `_ = got`, zero behavioral assertions).
func TestRealTreeIsAdjudicated(t *testing.T) {
	// No -short guard: nothing in cmd/loopruntime-ops runs with that flag, and the scan of the
	// population is precisely the assertion that cannot be skipped (rules/testing.md § 4.2).
	// It costs ~70s, which is the price of the gate being born measured against the tree that ships.
	if _, err := os.Stat(filepath.Join(repoRoot, defaultBaselineRel)); err != nil {
		t.Fatalf("the baseline must be versioned: %v", err)
	}
	out, err := violationOf(Check(context.Background(), Config{RepoRoot: repoRoot}))
	if err != nil {
		for i, f := range out.NewCandidates {
			if i == 10 {
				t.Errorf("... and %d more", len(out.NewCandidates)-10)
				break
			}
			t.Errorf("new: %s [%s] %s", f.File, f.Detector, f.ID())
		}
		t.Fatalf("real tree: %v", err)
	}
	if out.Candidates == 0 {
		t.Fatal("zero candidates scanned — finding nothing is not the same as finding nothing wrong")
	}
}

// A finding from this very slice's REVIEW phase. A baseline with 39 entries and a scan
// with ZERO candidates is not a repository that became clean — it is a scan that did not
// reach the history. Reporting green there is the exact shape of the defect B-143 measures,
// reintroduced through the back door.
//
// The direction of the error is deliberate: if the tree really WAS cleaned, the fix is to rewrite the
// baseline (which then has zero entries) — a human confirming "yes, everything is gone". The
// opposite would be the gate deciding that on its own, silently.
func TestNonEmptyBaselineWithEmptyScanFails(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", pgURI(), false)))
	_, err := run(t, stubScanner{out: ""}, base)
	if !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("a scan that reached nothing cannot report clean; err=%v", err)
	}
}

// The boundary: an empty baseline AND an empty scan is a genuinely clean repository.
func TestEmptyBaselineWithEmptyScanPasses(t *testing.T) {
	base := baseWith(t, nil)
	if _, err := run(t, stubScanner{out: ""}, base); err != nil {
		t.Fatalf("a repository with no candidate at all passes: %v", err)
	}
}
