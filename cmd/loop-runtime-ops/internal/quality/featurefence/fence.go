// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package featurefence fences api/internal by feature.
//
// api/internal is organized along one axis (docs/decisions.md, 2026-09-25): every
// top-level directory is a FEATURE, a piece of the shared KERNEL, a COMPOSITION
// root, or a TEST-ONLY harness. The registry below names each one and, for
// features and kernel packages, what it may import. It runs on the
// subpackagefence engine, so the registry is refused before the tree is read when
// it is empty, names an unregistered target, or permits a cycle.
//
// On top of the top-level graph the engine checks, a feature has an inside:
//
//   - `<feature>/<name>http` holds HTTP handlers. It imports its own feature and
//     the kernel — never another feature — and only its own subtree, its
//     feature's adapter package and a composition root may import it.
//   - `<feature>/<feature>adapter` holds the driven adapters that satisfy the
//     feature's ports with persistence, Kubernetes or another feature. Only a
//     composition root imports it.
//   - Either sits directly under the feature root, so the role of a package can
//     be read off its path.
//
// A feature's root therefore never depends on its own transport or wiring, and a
// handler can never reach around its feature into another one.
package featurefence

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/subpackagefence"
)

const (
	internalDir     = "api/internal"
	internalPkgPath = "github.com/getloopscale/loop-runtime/api/internal"

	httpSuffix    = "http"
	adapterSuffix = "adapter"
)

// Kind is the role of a top-level directory of api/internal.
type Kind int

const (
	// Feature is a capability the engine serves. It may import the kernel and
	// the features its entry names.
	Feature Kind = iota + 1
	// Kernel is shared by several features. It imports only the kernel packages
	// its entry names — never a feature.
	Kernel
	// Composition assembles features (the router, the background loops). It may
	// import anything that is not itself a composition root or a test harness,
	// and nothing imports it.
	Composition
	// TestOnly holds tests about the tree. It imports what its entry names and
	// nothing imports it.
	TestOnly
)

// Entry registers one top-level directory.
type Entry struct {
	Kind Kind
	// Deps are the registered names this entry may import. For a feature the
	// kernel is implied and Deps lists only other features.
	Deps []string
	// TestOnlyLeaf marks a kernel package that only _test.go files may import.
	TestOnlyLeaf bool
}

// Config controls the gate.
type Config struct {
	RepoRoot string
	// Registry overrides the registry. Tests only.
	Registry map[string]Entry
}

// Why the inner rules refuse.
var (
	ErrHTTPReached     = errors.New("an HTTP package is imported from outside its own subtree, its feature's adapter package and the composition roots")
	ErrHTTPReachesOut  = errors.New("an HTTP package imports another feature — it may import only its own feature and the kernel")
	ErrAdapterReached  = errors.New("an adapter package is imported by something other than a composition root")
	ErrMisplacedRole   = errors.New("an HTTP or adapter package does not sit directly under its feature, or an adapter package is not named <feature>adapter")
	ErrUnknownKind     = errors.New("a registry entry has no kind")
	errInnerRuleBroken = subpackagefence.ErrForbiddenEdge
)

// Fence builds the engine fence from the registry.
func (c Config) Fence() (subpackagefence.Fence, error) {
	reg := c.registry()
	var kernel []string
	for name, e := range reg {
		if e.Kind == Kernel {
			kernel = append(kernel, name)
		}
	}
	sort.Strings(kernel)

	allowed := map[string]map[string]bool{}
	testOnly := map[string]bool{}
	for name, e := range reg {
		set := map[string]bool{}
		switch e.Kind {
		case Feature:
			for _, k := range kernel {
				set[k] = true
			}
			for _, d := range e.Deps {
				set[d] = true
			}
		case Kernel, TestOnly:
			for _, d := range e.Deps {
				set[d] = true
			}
		case Composition:
			for other, oe := range reg {
				if oe.Kind != Composition && oe.Kind != TestOnly {
					set[other] = true
				}
			}
		default:
			return subpackagefence.Fence{}, fmt.Errorf("%w: %s", ErrUnknownKind, name)
		}
		allowed[name] = set
		if e.TestOnlyLeaf {
			testOnly[name] = true
		}
	}
	return subpackagefence.Fence{
		Dir:            internalDir,
		PkgPath:        internalPkgPath,
		Allowed:        allowed,
		Leaves:         map[string]bool{},
		TestOnlyLeaves: testOnly,
	}, nil
}

// Features returns the registered feature names, sorted. Gates that scan what
// the features hold read it instead of keeping their own list of directories.
func Features() []string {
	var out []string
	for name, e := range registry {
		if e.Kind == Feature {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// HTTPPackageDirs returns, relative to repoRoot, the directory of every HTTP
// package under api/internal: each `<top>/<name>http` child. Gates that scan the
// handler layer read it instead of naming directories; feature-fence is what
// guarantees an HTTP package sits nowhere else. An absent api/internal is an
// error, never an empty result.
func HTTPPackageDirs(repoRoot string) ([]string, error) {
	base := filepath.Join(repoRoot, internalDir)
	tops, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", internalDir, err)
	}
	var out []string
	for _, top := range tops {
		if !top.IsDir() || skipped(top.Name()) {
			continue
		}
		children, err := os.ReadDir(filepath.Join(base, top.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s/%s: %w", internalDir, top.Name(), err)
		}
		for _, c := range children {
			if c.IsDir() && strings.HasSuffix(c.Name(), httpSuffix) {
				out = append(out, filepath.Join(internalDir, top.Name(), c.Name()))
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (c Config) registry() map[string]Entry {
	if c.Registry != nil {
		return c.Registry
	}
	return registry
}

// Check runs the engine over the top-level graph, then the inner rules of every
// feature. Inner-rule violations are reported as edges of the same outcome.
func Check(cfg Config) (subpackagefence.Outcome, error) {
	f, err := cfg.Fence()
	if err != nil {
		return subpackagefence.Outcome{}, err
	}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	out, err := subpackagefence.Check(root, f)
	if err != nil {
		return out, err
	}
	if out.Violation != nil && !errors.Is(out.Violation, subpackagefence.ErrForbiddenEdge) &&
		!errors.Is(out.Violation, subpackagefence.ErrTestOnlyLeafFromProduction) &&
		!errors.Is(out.Violation, subpackagefence.ErrUnregisteredSubpackage) {
		return out, nil
	}
	inner, err := checkInner(root, cfg.registry())
	if err != nil {
		return out, err
	}
	out.Edges = append(out.Edges, inner.edges...)
	out.Violation = errors.Join(out.Violation, inner.err())
	return out, nil
}

// role is where a package directory sits in the layout.
type role struct {
	top  string // top-level directory
	kind Kind
	// unit is the HTTP or adapter package directory the path lies in, relative
	// to api/internal ("" when the path is in neither).
	unit    string
	http    bool
	adapter bool
}

func roleOf(rel string, reg map[string]Entry) role {
	segs := strings.Split(rel, "/")
	r := role{top: segs[0], kind: reg[segs[0]].Kind}
	if r.kind != Feature {
		return r
	}
	for i := 1; i < len(segs); i++ {
		switch {
		case strings.HasSuffix(segs[i], httpSuffix):
			r.http = true
		case strings.HasSuffix(segs[i], adapterSuffix):
			r.adapter = true
		default:
			continue
		}
		r.unit = strings.Join(segs[:i+1], "/")
		return r
	}
	return r
}

func (r role) within(unit string) bool {
	return unit != "" && (r.unit == unit)
}

type innerResult struct {
	edges  []subpackagefence.Edge
	causes []error
}

func (r innerResult) err() error {
	if len(r.edges) == 0 {
		return nil
	}
	seen := map[error]bool{}
	var unique []error
	for _, c := range r.causes {
		if !seen[c] {
			seen[c] = true
			unique = append(unique, c)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].Error() < unique[j].Error() })
	return fmt.Errorf("%s: %w: %w", internalDir, errInnerRuleBroken, errors.Join(unique...))
}

func (r *innerResult) add(cause error, from, to, file string, line int) {
	r.edges = append(r.edges, subpackagefence.Edge{From: from, To: to, File: file, Line: line})
	r.causes = append(r.causes, cause)
}

// checkInner walks every Go file under api/internal and applies the rules that
// hold inside a feature.
func checkInner(repoRoot string, reg map[string]Entry) (innerResult, error) {
	var res innerResult
	base := filepath.Join(repoRoot, internalDir)
	fset := token.NewFileSet()
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != base && skipped(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		relFile, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		relDir := filepath.ToSlash(filepath.Dir(relFile))
		if relDir == "." {
			return nil
		}
		from := roleOf(relDir, reg)
		if misplaced(relDir, from) {
			res.add(ErrMisplacedRole, relDir, relDir, path, 1)
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, imp := range file.Imports {
			target, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return fmt.Errorf("%s: import %s: %w", path, imp.Path.Value, err)
			}
			rest, ok := strings.CutPrefix(target, internalPkgPath+"/")
			if !ok {
				continue
			}
			to := roleOf(rest, reg)
			line := fset.Position(imp.Pos()).Line
			if cause := violation(from, to); cause != nil {
				res.add(cause, relDir, rest, path, line)
			}
		}
		return nil
	})
	return res, err
}

// misplaced reports an HTTP or adapter package that is not a direct child of
// its feature, or an adapter package not named after its feature.
func misplaced(relDir string, r role) bool {
	if r.unit == "" || relDir != r.unit {
		return false
	}
	segs := strings.Split(r.unit, "/")
	if len(segs) != 2 {
		return true
	}
	return r.adapter && segs[1] != r.top+adapterSuffix
}

// violation returns the rule an import from `from` into `to` breaks, or nil.
func violation(from, to role) error {
	composing := from.kind == Composition || from.kind == TestOnly
	switch {
	case to.http:
		sameFeatureAdapter := from.adapter && from.top == to.top
		if !from.within(to.unit) && !sameFeatureAdapter && !composing {
			return ErrHTTPReached
		}
	case to.adapter:
		if !from.within(to.unit) && !composing {
			return ErrAdapterReached
		}
	}
	if from.http && to.kind == Feature && to.top != from.top {
		return ErrHTTPReachesOut
	}
	return nil
}

// skipped mirrors the go tool: testdata and dot/underscore directories are not
// packages.
func skipped(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "api/internal is organized by feature (docs/decisions.md, 2026-09-25). One of:\n" +
		"  · a top-level directory nobody registered — decide whether it is a feature,\n" +
		"    kernel, composition root or test harness and add it to the registry in\n" +
		"    featurefence/registry.go with what it may import;\n" +
		"  · a kernel package reached a feature — the kernel is what features share, so\n" +
		"    the code that needs the feature belongs in that feature;\n" +
		"  · a feature reached a feature its entry does not name — add the edge only if\n" +
		"    it is a real dependency; the registry refuses one that closes a cycle;\n" +
		"  · an HTTP package reached another feature, or was reached from outside its\n" +
		"    subtree — handlers talk to their own feature; wiring across features\n" +
		"    belongs in the feature's adapter package or a composition root;\n" +
		"  · an adapter package was imported by feature code — only server,\n" +
		"    backgroundloops and api/cmd wire adapters;\n" +
		"  · an HTTP or adapter package is nested, or an adapter package is not named\n" +
		"    <feature>adapter."
}
