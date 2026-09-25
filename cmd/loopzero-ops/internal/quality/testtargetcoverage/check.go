// Package testtargetcoverage asserts that every Go module in the repository has
// its tests executed by a Taskfile target.
//
// # WHY THIS GATE EXISTS
//
// This repository replaced its CI pipeline with 123 local quality gates. What
// proves a gate rejects what it should reject are the gate's own tests — and
// they live in cmd/loopzero-ops, which no task ran. Measured 2026-08-13: of NINE
// modules on disk, `task test:unit` named FOUR lines. `quality:hermetic` claimed
// to run "quality-gate hermetic Go tests" and pointed at `pkg/quality`, relocated
// at M9; it exited 201 and took `quality:all` down with it at the first deps.
//
// Both defects have the same cause: a hand-written target list that was never
// compared against the disk. This gate is that comparison. It is not speculative
// — it guards a failure that already happened twice (B-087, B-088).
//
// # WHY A LIST AND NOT DISCOVERY
//
// A `for` loop over `find -name go.mod` in the Taskfile would remove the list and
// this gate at once. It would also erase a DECISION: a module may legitimately
// have no test (`infra/scripts` is a shell wrapper with zero *_test.go). Discovery
// hides that choice; a named exemption keeps it visible — the same shape as the
// dated allowlist of B-070 and the per-chart exception of B-045.
package testtargetcoverage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// prunedDirs are skipped anywhere in the tree.
//
// `.claude` is the one that matters and the one an independent review caught:
// `.claude/knowledge-base/references/` holds CLONED PEER PROJECTS (gitignored,
// governed by rules/reference-provenance.md), and a peer with its own go.mod was
// picked up as a repo module. The gate then demanded a test target for
// third-party code the provenance rule forbids us from building — failing for a
// developer who has cloned one and passing for a developer who has not, with no
// diff to explain the difference, inside the target delivery-gate.sh runs on
// every push.
//
// The rest mirrors internal/quality/testtargets, which already solved this.
var prunedDirs = map[string]bool{
	".git": true, ".claude": true, "node_modules": true, "vendor": true,
	"_legacy": true, "dist": true, "build": true, "worktrees": true,
}

// DefaultTaskfiles are the files scanned for `go test` invocations.
var DefaultTaskfiles = []string{"taskfiles/test.yml", "taskfiles/quality.yml"}

// ErrModuleUncovered means a module on disk is named by no test target.
var ErrModuleUncovered = errors.New("a Go module has no Taskfile target running its tests")

// ErrNothingScanned means no module was found, so nothing was verified. A gate
// that measured zero subjects and reported success is the `PASS — 0 routes`
// shape this repository has paid for once (B-040).
var ErrNothingScanned = errors.New("no Go module found; the coverage was not verified")

// ErrExemptionWithoutReason means an exemption names a module but not a reason.
var ErrExemptionWithoutReason = errors.New("an exemption must state WHY the module has no test target")

// ErrExemptionExpired means an exemption is past its sunset.
var ErrExemptionExpired = errors.New("an exemption is past its sunset; re-measure it or retire it")

// ErrPackageUncovered means a module has a target, but some test-bearing package of
// it is run by none.
//
// Distinct from ErrModuleUncovered on purpose: "this module has no target at all" and
// "it has a target, and it does not reach these subtrees" are different findings, and the
// second is the one that stayed invisible.
var ErrPackageUncovered = errors.New("a module has a test target, but some test-bearing package is run by none")

// ErrExemptionWithoutSunset means an exemption carries no date.
var ErrExemptionWithoutSunset = errors.New("an exemption must carry a sunset (YYYY-MM-DD)")

// Exemption is a module deliberately left without a test target.
//
// It carries a Sunset for the same reason every other allowlist in this
// repository does (code-quality-golden-rule.md § 4, shell-scripts-allowlist.md):
// an exemption without a date is a decision that outlives its reason, and
// nothing can detect that it went stale.
type Exemption struct {
	Module string
	Reason string
	Sunset string // YYYY-MM-DD, INCLUSIVE — expires the day after
	// Packages exempts SPECIFIC subtrees of the module, relative to its root.
	// Empty exempts the whole module.
	//
	// It exists because of bullet 2 of B-114's DoD: a subtree that requires a cluster gets in
	// through a NAMED exemption with its reason — not by the module's pattern accidentally
	// covering it, which is how the 41 became invisible.
	Packages []string
}

// DefaultExemptions are the modules that have no tests by design. Each carries
// the measurement that justifies it, so a reader can check rather than trust.
var DefaultExemptions = []Exemption{
	{
		Module: "infra/scripts",
		Sunset: "2026-11-11",
		Reason: "zero *_test.go files on disk (measured 2026-08-13); it is a thin Go wrapper over " +
			"operator scripts. A target running `go test` over a module with no tests reports a " +
			"green that means nothing.",
	},
}

// Outcome reports what was compared.
type Outcome struct {
	Modules   []string
	Covered   []string
	Exempt    []string
	Uncovered []string
	// UncoveredPackages names `<module>/<package>` that no target runs. It is the
	// granularity B-114 added: a module can have a target and still leave
	// subtrees outside it.
	UncoveredPackages []string
}

// Config controls the check.
type Config struct {
	RepoRoot   string
	Taskfiles  []string
	Exemptions []Exemption
	// Now overrides the clock for sunset evaluation (tests).
	Now time.Time
}

// Check compares the modules on disk against the modules named by a test target.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	files := cfg.Taskfiles
	if files == nil {
		files = DefaultTaskfiles
	}
	exemptions := cfg.Exemptions
	if exemptions == nil {
		exemptions = DefaultExemptions
	}

	now := cfg.Now
	if now.IsZero() {
		now = time.Now()
	}
	exempt := map[string]bool{}
	exemptPkgs := map[string][]string{}
	for _, e := range exemptions {
		if strings.TrimSpace(e.Reason) == "" {
			return Outcome{}, fmt.Errorf("%w: %q", ErrExemptionWithoutReason, e.Module)
		}
		if strings.TrimSpace(e.Sunset) == "" {
			return Outcome{}, fmt.Errorf("%w: %q", ErrExemptionWithoutSunset, e.Module)
		}
		sunset, perr := time.Parse("2006-01-02", e.Sunset)
		if perr != nil {
			return Outcome{}, fmt.Errorf("%w: %q has bad sunset %q", ErrExemptionWithoutSunset, e.Module, e.Sunset)
		}
		if now.After(sunset.AddDate(0, 0, 1)) {
			return Outcome{}, fmt.Errorf("%w: %q (sunset %s)", ErrExemptionExpired, e.Module, e.Sunset)
		}
		if len(e.Packages) > 0 {
			exemptPkgs[e.Module] = append(exemptPkgs[e.Module], e.Packages...)
			continue
		}
		exempt[e.Module] = true
	}

	modules, err := findModules(root)
	if err != nil {
		return Outcome{}, err
	}
	if len(modules) == 0 {
		return Outcome{}, ErrNothingScanned
	}

	targets, err := readTargets(root, files)
	if err != nil {
		return Outcome{}, err
	}

	var out Outcome
	out.Modules = modules
	for _, m := range modules {
		switch {
		case exempt[m]:
			out.Exempt = append(out.Exempt, m)
			continue
		case !runsTests(targets, m):
			out.Uncovered = append(out.Uncovered, m)
			continue
		}
		out.Covered = append(out.Covered, m)

		// The module has a target. The missing question: does the target REACH every package
		// that has a test? Measured in B-092, `infra/tests` passed because of a
		// `./unit/...` while 41 files in three subtrees did not run.
		uncov, uerr := uncoveredPackages(root, m, targets)
		if uerr != nil {
			return out, uerr
		}
		for _, pkg := range uncov {
			if pkgExempt(exemptPkgs[m], pkg) {
				out.Exempt = append(out.Exempt, m+"/"+pkg)
				continue
			}
			out.UncoveredPackages = append(out.UncoveredPackages, m+"/"+pkg)
		}
	}
	if len(out.Uncovered) > 0 {
		return out, ErrModuleUncovered
	}
	if len(out.UncoveredPackages) > 0 {
		return out, ErrPackageUncovered
	}
	return out, nil
}

// pkgExempt reports whether pkg is covered by a declared package exemption.
//
// An exemption of `e2e` covers `e2e/sub`: whoever exempts a subtree for requiring a cluster
// is exempting the subtree, and forcing every package in it to be listed would produce a list
// that goes stale with every new directory.
func pkgExempt(exempt []string, pkg string) bool {
	for _, e := range exempt {
		if pkg == e || strings.HasPrefix(pkg, e+"/") {
			return true
		}
	}
	return false
}

// findModules walks the tree for go.mod, pruning the trees in prunedDirs.
func findModules(root string) ([]string, error) {
	var mods []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if prunedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(p))
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil // no root module in this repo, by design
		}
		mods = append(mods, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(mods)
	return mods, nil
}

// readTargets concatenates the taskfiles. A missing file is an error: scanning
// fewer files than intended silently shrinks what the gate can see.
func readTargets(root string, files []string) (string, error) {
	var b strings.Builder
	for _, f := range files {
		body, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrNothingScanned, err)
		}
		b.Write(body)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// runsTests reports whether some target changes into the module and runs
// `go test`. Both halves are required on the same logical line: `cd cmd/loopzero-ops
// && go build ./...` was exactly the false positive that let this defect live —
// quality:hermetic BUILT the gate module and never tested it.
func runsTests(targets, module string) bool {
	for _, raw := range strings.Split(targets, "\n") {
		line := stripComment(raw)
		// The trailing space is the PREFIX BOUNDARY, and it is load-bearing:
		// without it `cd operators/api && go test` would mark `operators`
		// covered. TestCheck_PrefixBoundaryIsRespected pins both directions —
		// a review measured that dropping it survives every other test.
		i := strings.Index(line, "cd "+module+" ")
		if i < 0 {
			continue
		}
		// `go test` must come after this cd and BEFORE the next one. Cutting
		// only at the cd is not enough: in
		// `cd api && go build ./... && cd ../pkg && go test ./...`
		// the tail still contains `go test`, and `api` would count as tested
		// because a LATER command tests a DIFFERENT module — defeating the
		// build-is-not-test rule this gate exists for. Measured by a review;
		// pinned by TestCheck_GoTestForAnotherModuleDoesNotCount.
		scope := line[i+len("cd "+module+" "):]
		if next := strings.Index(scope, "cd "); next >= 0 {
			scope = scope[:next]
		}
		if strings.Contains(scope, "go test") {
			return true
		}
	}
	return false
}

// stripComment drops a YAML comment so that a commented-out or echoed command
// cannot satisfy the matcher. Text that never executes must not count as
// coverage — that is the same class of claim the gate refuses elsewhere.
func stripComment(line string) string {
	if i := strings.Index(line, "#"); i >= 0 {
		line = line[:i]
	}
	if strings.Contains(line, "echo ") {
		return ""
	}
	return line
}
