// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package billinglayers refuses a billing chain whose layers stop having an owner.
//
// # The finding, measured
//
// On 2026-08-11, FIVE independent layers were measured in which missing billing
// configuration is tolerated — the Secret mount, the seed, the cost aggregator, the cost
// gate and the usage report. All of them failing towards the SAME side — do not charge,
// do not block, do not verify — and none of them saying so. Layer 3 announces its own
// death with a `logger.Info("cost aggregator disabled")`: the same log level as a
// successful boot.
//
// Each choice is defensible in isolation. Together they form a system that, faced with any
// missing configuration, charges less and blocks less — and tells nobody. **The direction
// of the error is the finding**: a billing system that always errs to the same side is not
// tolerant, it is biased (B-038).
//
// # What this gate checks, and what it does NOT check
//
// It does NOT fix the layers — that belongs to items B-033 and B-034. It guarantees that
// their list is **enumerable, owned, and impossible to shrink in silence**:
//
//	declared guard that does not resolve   →  FAILS
//	`open:` debt whose item was closed     →  FAILS   ← the ratchet
//	fewer layers than the floor            →  FAILS
//	file with an input and no declaration  →  FAILS
//	sibling repository absent              →  PARTIAL, named — never a silent green
//
// # The anchor is DATA, and the reason is concrete
//
// It lives in the registry itself, and not as literals in this file. Written in Go, it
// would make this file reference a billing input — and the gate would fail itself. As
// data, it becomes reviewable instead of hidden in a constant.
//
// The choice of anchor was MEASURED before it was made: a name heuristic
// (`billing|stripe|metering|cost`) matches **142** files — noise, not control, and the arms
// race that `publishhygiene` refused halfway through for IPs. By reference to an input,
// **21** (16 here, 5 in the sibling).
//
// # Limits, stated out loud
//
// A layer that degrades **without touching any known input** goes unnoticed. It happened
// during this very slice: the cost gate (`api/internal/server/router.go`) cited no input
// of the initial anchor, and only came in when the enforcer's identifier was added to the
// registry. The anchor is a list, and lists age.
//
// And this paragraph has already failed this file once: the previous version spelled that
// identifier out, which made the gate find itself. Taking the anchor out of the code is
// not enough if the PROSE reintroduces it — and declaring this file as a reference would
// resolve it from the outside, leaving a blind spot exactly where the lock is audited.
//
// And a `test:` guard is checked by EXISTENCE, not by content: the gate does not know
// whether that test asserts the invariant. Telling the two apart would require semantic
// analysis of tests, which nothing in this repository does.
//
// The surface sweep covers THIS repository only. The sibling's layers are checked by
// declaration — the files exist, the guard resolves — but a NEW file there that references
// an input is not detected here. That is deliberate: sweeping the sibling would let a
// change made by someone else, in another repository, block this one's push. A gate that
// fails because of other people's work gets switched off, and takes the protection it gave
// with it.
//
// Decision recorded in
// `.claude/knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-18-billing-layers-registry.md`
// (T2, pending ratification).
package billinglayers

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrUndeclaredSurface means a file references a billing input without being declared.
var ErrUndeclaredSurface = errors.New("file references a billing input and is not declared")

// ErrBelowFloor means the registry shrank below its declared minimum.
var ErrBelowFloor = errors.New("fewer layers than the declared floor")

// ErrNothingScanned means the scan reached no file.
var ErrNothingScanned = errors.New("no file swept — finding nothing is not the same as finding nothing wrong")

// RegistryRelPath is where the declared registry lives.
const RegistryRelPath = "infra/quality/billing-layers.txt"

// Config drives the check.
type Config struct {
	RepoRoot string
	// SiblingRoot is `../loop-console`. Empty or absent → PARTIAL, never a silent green.
	SiblingRoot string
}

// BrokenGuard is one layer whose declared protection does not hold.
type BrokenGuard struct {
	Layer string
	Why   string
}

// Outcome reports what was established.
type Outcome struct {
	Layers      int
	Scanned     int
	Broken      []BrokenGuard
	Undeclared  []string
	NotVerified []string
	Partial     bool
	Reason      string
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

// Check reads the registry and verifies it is honest.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	f, err := os.Open(filepath.Clean(filepath.Join(root, RegistryRelPath)))
	if err != nil {
		out.Violation = fmt.Errorf("%w: could not read %s: %w", ErrRegistryMalformed, RegistryRelPath, err)
		return out, nil
	}
	reg, perr := ParseRegistry(f)
	_ = f.Close()
	if errors.Is(perr, ErrRegistryMalformed) {
		out.Violation = perr
		return out, nil
	}
	if perr != nil {
		return out, perr
	}
	out.Layers = len(reg.Layers)

	sibling := cfg.SiblingRoot
	if sibling != "" {
		if st, serr := os.Stat(sibling); serr != nil || !st.IsDir() {
			sibling = ""
		}
	}
	resolver := NewGuardResolver(root, sibling, reg.Debts)

	// EC-4: the floor is what stops the registry from silencing itself.
	if out.Layers < reg.Floor {
		out.Violation = fmt.Errorf("%w: %d layers declared, floor %d — really removing a layer requires lowering the floor in the same diff",
			ErrBelowFloor, out.Layers, reg.Floor)
		return out, nil
	}

	declared := map[string]bool{RegistryRelPath: true} // EC-1: citing the input is its definition
	for _, ref := range reg.References {
		declared[ref.File] = true
	}

	for _, l := range reg.Layers {
		if resolver.Root(l.Repo) == "" {
			out.Partial = true
			out.NotVerified = append(out.NotVerified, l.ID+" ("+l.Repo+")")
			continue
		}
		for _, rel := range l.Files {
			if l.Repo == "loop-runtime" {
				declared[rel] = true
			}
			if err := resolver.resolveFile(rel, l.Repo, "layer file"); err != nil {
				out.Broken = append(out.Broken, BrokenGuard{Layer: l.ID, Why: err.Error()})
			}
		}
		if err := resolver.Resolve(l.Guard, l.Repo); err != nil {
			out.Broken = append(out.Broken, BrokenGuard{Layer: l.ID, Why: err.Error()})
		}
	}

	files, gerr := versionedFiles(root)
	if gerr != nil {
		return out, gerr
	}
	out.Scanned = len(files)
	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}
	out.Undeclared, err = undeclaredSurfaces(root, files, reg.Anchor, declared)
	if err != nil {
		return out, err
	}

	if len(out.Broken) > 0 {
		out.Violation = fmt.Errorf("%w: %s", ErrGuardUnresolved, describe(out.Broken))
		return out, nil
	}
	if len(out.Undeclared) > 0 {
		out.Violation = fmt.Errorf("%w (%d): %s — declare it as a layer, or as a reference with the reason it is not one",
			ErrUndeclaredSurface, len(out.Undeclared), strings.Join(out.Undeclared, ", "))
		return out, nil
	}

	out.Reason = fmt.Sprintf("%d layer(s) with a guard that resolves, %d file(s) swept", out.Layers, out.Scanned)
	if out.Partial {
		out.Reason += fmt.Sprintf(" — PARTIAL COVERAGE: %s not verified, sibling repository absent",
			strings.Join(out.NotVerified, ", "))
	}
	return out, nil
}

func describe(b []BrokenGuard) string {
	parts := make([]string, 0, len(b))
	for _, x := range b {
		parts = append(parts, x.Layer+": "+x.Why)
	}
	return strings.Join(parts, "; ")
}

// undeclaredSurfaces finds files that touch a billing input without being declared.
// A versioned file it cannot read is an error: it may be exactly the new
// layer the sweep exists to catch.
func undeclaredSurfaces(root string, files, anchor []string, declared map[string]bool) ([]string, error) {
	var out []string
	for _, rel := range files {
		if declared[rel] || !scannable(rel) {
			continue
		}
		hit, err := referencesAnchor(filepath.Join(root, rel), anchor)
		if err != nil {
			return nil, fmt.Errorf("billing-layers: %s is versioned and unreadable, so it was not swept: %w", rel, err)
		}
		if hit {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

// scannable limits the sweep to the kinds of file a layer can live in.
//
// Tests are left out on purpose: a test that cites the input is exercising the layer,
// not being one. Including them would force every test file to be declared, and the
// registry would turn into a list of tests.
func scannable(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, "_test.sh") {
		return false
	}
	for _, ext := range []string{".go", ".yaml", ".yml", ".gotmpl"} {
		if strings.HasSuffix(rel, ext) {
			return true
		}
	}
	return false
}

func referencesAnchor(abs string, anchor []string) (bool, error) {
	f, err := os.Open(filepath.Clean(abs))
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		for _, a := range anchor {
			if strings.Contains(line, a) {
				return true, nil
			}
		}
	}
	return false, sc.Err()
}

// versionedFiles lists what git tracks — what will be published.
func versionedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w", root, err)
	}
	var out []string
	for _, l := range strings.Split(string(body), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}
