// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package chartpairs refuses a chart that exists twice: once under infra/helm/
// (bootstrap, Helmfile L2) and once under charts/ (published OCI, ArgoCD L3).
//
// # The decision it enforces
//
// docs/decisions.md, "One chart per bootstrap/published pair" (2026-09-25). The
// three pairs this repository carried — loopzero-api, loopzero-cloud, and
// admission-policies↔loopzero-admission-policies — were not two renderings of one
// design but two charts that drifted: a free-form env map on one side and typed
// config on the other, RBAC one side granted and the other withheld, 8 of 20
// admission templates diverged before a parity test pinned them. Each pair became
// its charts/ chart, installed by Helmfile with bootstrap values, and the
// infra/helm copy was deleted.
//
// # What it checks, and why both ways
//
// By NAME: a Chart.yaml name present in both trees. That is how loopzero-api and
// loopzero-cloud were paired.
//
// By CONTENT: an infra/helm chart whose templates/ is byte-identical, file for
// file, to a charts/ chart's. A copy renamed on the way in — which is what
// admission-policies was — cannot be found by name, and a registry that had to
// DECLARE such a pair to see it saw only what someone remembered to write down.
// Whole template sets are compared, so two charts that merely share a file are not
// flagged, and a chart with no templates (a library, a values-only wrapper) is not
// compared at all.
//
// # History
//
// Until 2026-09-25 this gate kept a registry of declared pairs
// (infra/quality/chart-pairs.txt) that helm-drift and helm-overlap-parity audited
// for parity. With no pair left, those two gates had nothing to measure and were
// retired; this one now refuses the state they used to tolerate.
package chartpairs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/scanresult"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// ErrDuplicated means a chart exists in both trees, by name or as a template-identical copy.
var ErrDuplicated = errors.New("a chart exists in both infra/helm and charts/")

// ErrNothingScanned means no chart was found at all — silence, not cleanliness.
var ErrNothingScanned = fmt.Errorf("%w: no chart", scanresult.ErrNothingScanned)

var nameRE = regexp.MustCompile(`(?m)^name:\s*(\S+)`)

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	InfraHelm  int
	Charts     int
	Duplicates []string // "infra/helm/<dir> ↔ charts/<dir> (<how>)", sorted
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

// chartInfo is what the check needs of one chart directory.
type chartInfo struct {
	dir       string
	name      string
	templates string // digest of the template set; "" when the chart has none
}

// scanTree reads every chart directly under base. An absent tree holds no charts
// and a directory without Chart.yaml is not a chart; any other read failure is an
// error, because it hides a chart.
func scanTree(base string) ([]chartInfo, error) {
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chart-pairs: read %s: %w", base, err)
	}
	var out []chartInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		b, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "Chart.yaml")))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("chart-pairs: read %s/Chart.yaml: %w", e.Name(), err)
		}
		info := chartInfo{dir: e.Name()}
		if m := nameRE.FindSubmatch(b); m != nil {
			info.name = string(m[1])
		}
		if info.templates, err = templateDigest(filepath.Join(dir, "templates")); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// templateDigest hashes every file under a templates/ directory, path and content,
// in a stable order. An absent or empty templates/ yields "".
func templateDigest(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("chart-pairs: walk %s: %w", dir, err)
	}
	if len(files) == 0 {
		return "", nil
	}
	sort.Strings(files)
	h := sha256.New()
	for _, p := range files {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return "", fmt.Errorf("chart-pairs: relativise %s: %w", p, err)
		}
		b, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return "", fmt.Errorf("chart-pairs: read %s: %w", p, err)
		}
		h.Write([]byte(filepath.ToSlash(rel) + "\x00" + strconv.Itoa(len(b)) + "\x00"))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Check refuses a chart that exists in both trees.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	infra, err := scanTree(filepath.Join(root, "infra", "helm"))
	if err != nil {
		return out, err
	}
	pub, err := scanTree(filepath.Join(root, "charts"))
	if err != nil {
		return out, err
	}
	out.InfraHelm, out.Charts = len(infra), len(pub)
	if out.InfraHelm == 0 && out.Charts == 0 {
		return out, fmt.Errorf("%w: neither infra/helm nor charts/", ErrNothingScanned)
	}

	byName := map[string]chartInfo{}
	byTemplates := map[string]chartInfo{}
	for _, c := range pub {
		if c.name != "" {
			byName[c.name] = c
		}
		if c.templates != "" {
			byTemplates[c.templates] = c
		}
	}
	for _, c := range infra {
		if p, ok := byName[c.name]; ok && c.name != "" {
			out.Duplicates = append(out.Duplicates, fmt.Sprintf("infra/helm/%s ↔ charts/%s (same name)", c.dir, p.dir))
			continue
		}
		if p, ok := byTemplates[c.templates]; ok && c.templates != "" {
			out.Duplicates = append(out.Duplicates, fmt.Sprintf("infra/helm/%s ↔ charts/%s (identical templates)", c.dir, p.dir))
		}
	}
	sort.Strings(out.Duplicates)
	if len(out.Duplicates) > 0 {
		out.Violation = fmt.Errorf("%w: %s — install the charts/ chart from Helmfile with bootstrap values and delete the infra/helm copy (docs/decisions.md, \"One chart per bootstrap/published pair\")",
			ErrDuplicated, strings.Join(out.Duplicates, "; "))
		return out, nil
	}
	return out, nil
}
