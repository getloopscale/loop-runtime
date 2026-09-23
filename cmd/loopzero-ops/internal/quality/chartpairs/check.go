// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package chartpairs keeps the registry of charts that exist in BOTH trees
// honest, and is the single reader of that registry for every gate that audits
// a pair.
//
// # The defect, measured
//
// B-056, 2026-08-18. `infra/helm/` (bootstrap, Helmfile L2) and `charts/`
// (published OCI, ArgoCD L3) coexist by design, and CLAUDE.md requires a chart
// present in both to be kept in parity. CLAUDE.md itself had drifted: it claimed
// three same-name pairs, one of which (`infra/helm/loopzero-admission-policies`)
// never existed. A parity contract naming a pair with one side dilutes the pairs
// that are real.
//
// # One registry, three readers
//
// infra/quality/chart-pairs.txt declares every pair — same-name ones and the
// conceptual ones whose charts carry different names. This gate compares it
// with disk; helm-drift and helm-overlap-parity load it through LoadRegistry.
// Until 2026-09-23 those two kept their own lists (a disk scan plus a
// hard-coded map, and a hard-coded slice), so the three disagreed on how many
// pairs exist.
//
// # The limit, stated
//
// This gate verifies that each pair EXISTS, not that the two sides agree; the
// content comparison is helm-drift's and helm-overlap-parity's job.
package chartpairs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ErrUndeclared means a same-name pair exists on disk and the registry does not
// declare it.
var ErrUndeclared = errors.New("a chart pair exists on disk and is not declared")

// ErrStale means the registry declares a pair one of whose charts is gone.
var ErrStale = errors.New("a declared chart pair no longer exists")

// ErrNothingScanned means no chart was found at all — silence, not cleanliness.
var ErrNothingScanned = errors.New("no chart scanned — finding nothing is not finding nothing wrong")

// ErrRegistryMissing means the declared registry is absent or malformed.
var ErrRegistryMissing = errors.New("chart pair registry missing or malformed")

// RegistryRelPath is the pair registry, relative to the repository root.
const RegistryRelPath = "infra/quality/chart-pairs.txt"

var nameRE = regexp.MustCompile(`(?m)^name:\s*(\S+)`)

// Pair is one declared bootstrap↔published chart pair.
type Pair struct {
	Bootstrap string // directory under infra/helm
	Published string // directory under charts
	Reason    string
}

// Label names the pair: the shared name, or bootstrap↔published when the two
// charts carry different names.
func (p Pair) Label() string {
	if p.Bootstrap == p.Published {
		return p.Bootstrap
	}
	return p.Bootstrap + "↔" + p.Published
}

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	InfraHelm  int
	Charts     int
	Pairs      []string // declared pairs present on disk, by Label
	Undeclared []string
	Stale      []string
}

// LoadRegistry reads the declared pairs, sorted by label.
func LoadRegistry(repoRoot string) ([]Pair, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, RegistryRelPath)) // #nosec G304 -- fixed path under the repo root
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrRegistryMissing, RegistryRelPath, err)
	}
	var pairs []Pair
	for n, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.SplitN(line, "|", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("%w: %s:%d wants `<infra/helm dir> | <charts dir> | <reason>`, got %q",
				ErrRegistryMissing, RegistryRelPath, n+1, line)
		}
		p := Pair{Bootstrap: strings.TrimSpace(f[0]), Published: strings.TrimSpace(f[1]), Reason: strings.TrimSpace(f[2])}
		if p.Bootstrap == "" || p.Published == "" || p.Reason == "" {
			return nil, fmt.Errorf("%w: %s:%d has an empty field", ErrRegistryMissing, RegistryRelPath, n+1)
		}
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Label() < pairs[j].Label() })
	return pairs, nil
}

// chartNames maps the `name:` of every Chart.yaml directly under base to its
// directory. An absent tree holds no charts and a directory without
// Chart.yaml is not a chart; any other read failure is an error, because it
// hides a name.
func chartNames(base string) (map[string]string, error) {
	out := map[string]string{}
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chart-pairs: read %s: %w", base, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(filepath.Join(base, e.Name(), "Chart.yaml")))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("chart-pairs: read %s/Chart.yaml: %w", e.Name(), err)
		}
		if m := nameRE.FindSubmatch(b); m != nil {
			out[string(m[1])] = e.Name()
		}
	}
	return out, nil
}

// Check compares the pairs on disk with the declared registry.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	infra, err := chartNames(filepath.Join(root, "infra", "helm"))
	if err != nil {
		return out, err
	}
	pub, err := chartNames(filepath.Join(root, "charts"))
	if err != nil {
		return out, err
	}
	out.InfraHelm, out.Charts = len(infra), len(pub)
	if out.InfraHelm == 0 && out.Charts == 0 {
		return out, fmt.Errorf("%w: neither infra/helm nor charts/", ErrNothingScanned)
	}

	declared, err := LoadRegistry(root)
	if err != nil {
		return out, err
	}
	isDeclared := map[[2]string]bool{}
	for _, p := range declared {
		isDeclared[[2]string{p.Bootstrap, p.Published}] = true
		if dirHasChart(root, "infra/helm", p.Bootstrap) && dirHasChart(root, "charts", p.Published) {
			out.Pairs = append(out.Pairs, p.Label())
		} else {
			out.Stale = append(out.Stale, p.Label())
		}
	}
	for name, infraDir := range infra {
		if pubDir, ok := pub[name]; ok && !isDeclared[[2]string{infraDir, pubDir}] {
			out.Undeclared = append(out.Undeclared, Pair{Bootstrap: infraDir, Published: pubDir}.Label())
		}
	}
	sort.Strings(out.Undeclared)
	sort.Strings(out.Stale)

	if len(out.Undeclared) > 0 {
		return out, fmt.Errorf("%w: %s — declare it in %s", ErrUndeclared, strings.Join(out.Undeclared, ", "), RegistryRelPath)
	}
	if len(out.Stale) > 0 {
		return out, fmt.Errorf("%w: %s — remove it from %s", ErrStale, strings.Join(out.Stale, ", "), RegistryRelPath)
	}
	return out, nil
}

func dirHasChart(root, tree, dir string) bool {
	_, err := os.Stat(filepath.Join(root, tree, dir, "Chart.yaml"))
	return err == nil
}
