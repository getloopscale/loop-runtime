// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package helmoverlapparity is the T1.2 gate of plan
// 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md. It enforces that
// the 3 bootstrap charts (infra/helm/) that overlap with published charts
// (charts/) never have a version GREATER than their published counterpart.
//
// Rationale (M06 Helm audit HIGH gap): bootstrap charts (Helmfile L2) and
// published charts (ArgoCD L3, OCI registry) are independently maintained.
// If `infra/helm/loopzero-api` advances to 0.3.0 while `charts/loopzero-api` stays
// at 0.2.0, Helmfile syncs a bootstrap that is "newer" than what production
// ArgoCD ever sees — silent drift between local-bootstrap and prod-GitOps.
//
// Acceptable states:
//   - bootstrap_version == published_version  (parity)
//   - bootstrap_version <  published_version  (bootstrap intentionally older)
//
// Forbidden state:
//   - bootstrap_version >  published_version  (bootstrap leading; FAIL)
//
// Naming asymmetry (intentional, verified 2026-05-26):
//   - infra/helm/admission-policies  ↔  charts/loopzero-admission-policies
//   - infra/helm/loopzero-api            ↔  charts/loopzero-api
//   - infra/helm/loopzero-cloud          ↔  charts/loopzero-cloud
package helmoverlapparity

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Pair maps a bootstrap chart dir to its published counterpart.
type Pair struct {
	BootstrapDir string // relative to RepoRoot/infra/helm
	PublishedDir string // relative to RepoRoot/charts
}

// DefaultPairs are the 3 overlapping charts per CLAUDE.md "Helm Chart Dual System".
var DefaultPairs = []Pair{
	{BootstrapDir: "loopzero-api", PublishedDir: "loopzero-api"},
	{BootstrapDir: "loopzero-cloud", PublishedDir: "loopzero-cloud"},
	{BootstrapDir: "admission-policies", PublishedDir: "loopzero-admission-policies"},
}

// Violation captures a single drift finding.
type Violation struct {
	BootstrapPath    string
	PublishedPath    string
	BootstrapVersion string
	PublishedVersion string
	Reason           string // human-readable explanation
}

func (v Violation) String() string {
	return fmt.Sprintf("%s (%s) > %s (%s) — %s",
		v.BootstrapPath, v.BootstrapVersion,
		v.PublishedPath, v.PublishedVersion,
		v.Reason)
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Violations []Violation
}

// chartYaml represents only the fields we read.
type chartYaml struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// Check verifies all DefaultPairs and returns an Outcome.
//
// Missing Chart.yaml in either side of a pair = Violation with reason="missing".
// Unparseable semver = Violation with reason="invalid version".
// Bootstrap > published = Violation with reason="drift".
func Check(cfg Config) (*Outcome, error) {
	out := &Outcome{Pass: true}
	for _, pair := range DefaultPairs {
		bootstrapPath := filepath.Join(cfg.RepoRoot, "infra/helm", pair.BootstrapDir, "Chart.yaml")
		publishedPath := filepath.Join(cfg.RepoRoot, "charts", pair.PublishedDir, "Chart.yaml")

		bv, bErr := readChartVersion(bootstrapPath)
		pv, pErr := readChartVersion(publishedPath)

		if bErr != nil {
			out.Violations = append(out.Violations, Violation{
				BootstrapPath: relativeOrAbs(cfg.RepoRoot, bootstrapPath),
				PublishedPath: relativeOrAbs(cfg.RepoRoot, publishedPath),
				Reason:        fmt.Sprintf("missing/unreadable bootstrap: %v", bErr),
			})
			continue
		}
		if pErr != nil {
			out.Violations = append(out.Violations, Violation{
				BootstrapPath:    relativeOrAbs(cfg.RepoRoot, bootstrapPath),
				PublishedPath:    relativeOrAbs(cfg.RepoRoot, publishedPath),
				BootstrapVersion: bv,
				Reason:           fmt.Sprintf("missing/unreadable published: %v", pErr),
			})
			continue
		}

		cmp, err := compareSemver(bv, pv)
		if err != nil {
			out.Violations = append(out.Violations, Violation{
				BootstrapPath:    relativeOrAbs(cfg.RepoRoot, bootstrapPath),
				PublishedPath:    relativeOrAbs(cfg.RepoRoot, publishedPath),
				BootstrapVersion: bv,
				PublishedVersion: pv,
				Reason:           fmt.Sprintf("invalid version: %v", err),
			})
			continue
		}
		if cmp > 0 {
			out.Violations = append(out.Violations, Violation{
				BootstrapPath:    relativeOrAbs(cfg.RepoRoot, bootstrapPath),
				PublishedPath:    relativeOrAbs(cfg.RepoRoot, publishedPath),
				BootstrapVersion: bv,
				PublishedVersion: pv,
				Reason:           "drift: bootstrap chart leads published",
			})
		}
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func readChartVersion(path string) (string, error) {
	// #nosec G304 -- path is composed of fixed RepoRoot + DefaultPairs entries.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var c chartYaml
	if err := yaml.Unmarshal(data, &c); err != nil {
		return "", fmt.Errorf("yaml parse: %w", err)
	}
	if strings.TrimSpace(c.Version) == "" {
		return "", fmt.Errorf("empty version field")
	}
	return c.Version, nil
}

func relativeOrAbs(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// compareSemver returns -1, 0, or 1 if a < b, a == b, a > b.
// Handles MAJOR.MINOR.PATCH and MAJOR.MINOR.PATCH-prerelease per semver.org.
// Pre-release is considered lower than the corresponding release
// (1.0.0-rc1 < 1.0.0). Build metadata (after +) is ignored.
func compareSemver(a, b string) (int, error) {
	// Strip build metadata
	a = stripBuildMeta(a)
	b = stripBuildMeta(b)

	aCore, aPre := splitPrerelease(a)
	bCore, bPre := splitPrerelease(b)

	aParts, err := parseCore(aCore)
	if err != nil {
		return 0, fmt.Errorf("a=%q: %w", a, err)
	}
	bParts, err := parseCore(bCore)
	if err != nil {
		return 0, fmt.Errorf("b=%q: %w", b, err)
	}

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1, nil
		}
		if aParts[i] > bParts[i] {
			return 1, nil
		}
	}

	// Core equal — pre-release compare
	if aPre == "" && bPre == "" {
		return 0, nil
	}
	if aPre == "" {
		// release > pre-release
		return 1, nil
	}
	if bPre == "" {
		return -1, nil
	}
	// Both have prerelease — lexicographic
	if aPre < bPre {
		return -1, nil
	}
	if aPre > bPre {
		return 1, nil
	}
	return 0, nil
}

func stripBuildMeta(v string) string {
	if idx := strings.Index(v, "+"); idx >= 0 {
		return v[:idx]
	}
	return v
}

func splitPrerelease(v string) (core, pre string) {
	if idx := strings.Index(v, "-"); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

func parseCore(s string) ([3]int, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("expected 3 dot-separated components, got %d", len(parts))
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, fmt.Errorf("component %d (%q): %w", i, p, err)
		}
		if n < 0 {
			return [3]int{}, fmt.Errorf("component %d negative", i)
		}
		out[i] = n
	}
	return out, nil
}
