// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package helmdrift detects drift between published charts (charts/) and
// bootstrap charts (infra/helm/) by comparing template-file sets. A drift is
// acceptable only if it has an approving ADR (per ADR-0041, gate CH-G16).
//
// Plan T5.3 of docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/audit-helm-drift.sh. Pure-Go (os.ReadDir + set diff
// + static registry) — no external CLI (bash used find/comm/awk). Genuine
// pure-logic win per D5.
package helmdrift

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// conceptualPairs maps published-chart → bootstrap-chart when names differ.
var conceptualPairs = map[string]string{
	"loopzero-admission-policies": "admission-policies",
}

// approvedDrifts registry: "published:bootstrap" → ADR id. Drifts in this map
// are intentional (do NOT fail the gate).
var approvedDrifts = map[string]string{
	"loopzero-api:loopzero-api":     "ADR-0043",
	"loopzero-cloud:loopzero-cloud": "ADR-0043",
}

// Config drives the gate.
type Config struct {
	RepoRoot string
}

// Drift describes one chart-pair drift.
type Drift struct {
	Pair            string
	Intentional     bool
	ADR             string
	OnlyInPublished []string
	OnlyInBootstrap []string
}

// Outcome captures the result.
type Outcome struct {
	Pass   bool
	Drifts []Drift
}

// Check audits all loopzero-* published charts + conceptual pairs for drift.
func Check(cfg Config) (*Outcome, error) {
	outcome := &Outcome{Pass: true}

	// Same-name overlaps: charts/loopzero-*/ that also exist in infra/helm/.
	chartsDir := filepath.Join(cfg.RepoRoot, "charts")
	entries, err := os.ReadDir(chartsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read charts/: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "loopzero-") {
			continue
		}
		name := e.Name()
		if dirExists(filepath.Join(cfg.RepoRoot, "infra/helm", name)) {
			auditPair(cfg.RepoRoot, name, name, outcome)
		}
	}

	// Conceptual pairs (different names).
	// Deterministic order for stable output.
	var pubNames []string
	for p := range conceptualPairs {
		pubNames = append(pubNames, p)
	}
	sort.Strings(pubNames)
	for _, pub := range pubNames {
		auditPair(cfg.RepoRoot, pub, conceptualPairs[pub], outcome)
	}

	return outcome, nil
}

// auditPair compares template sets for one published↔bootstrap pair and, if
// they drift, records a Drift (intentional if an ADR approves it; else fails).
func auditPair(repoRoot, publishedName, bootstrapName string, outcome *Outcome) {
	pairKey := publishedName + ":" + bootstrapName
	publishedDir := filepath.Join(repoRoot, "charts", publishedName, "templates")
	bootstrapDir := filepath.Join(repoRoot, "infra/helm", bootstrapName, "templates")

	if !dirExists(publishedDir) && !dirExists(bootstrapDir) {
		return
	}

	pubTemplates := listTemplates(publishedDir)
	bootTemplates := listTemplates(bootstrapDir)
	onlyPub, onlyBoot := setDiff(pubTemplates, bootTemplates)

	if len(onlyPub) == 0 && len(onlyBoot) == 0 {
		return // no drift
	}

	d := Drift{
		Pair:            pairKey,
		OnlyInPublished: onlyPub,
		OnlyInBootstrap: onlyBoot,
	}
	if adr, ok := approvedDrifts[pairKey]; ok {
		d.Intentional = true
		d.ADR = adr
	} else {
		outcome.Pass = false
	}
	outcome.Drifts = append(outcome.Drifts, d)
}

// listTemplates returns sorted *.yaml/*.yml filenames in a templates dir.
func listTemplates(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// setDiff returns (onlyInA, onlyInB) for two sorted/unsorted string slices.
func setDiff(a, b []string) (onlyA, onlyB []string) {
	bset := map[string]bool{}
	for _, x := range b {
		bset[x] = true
	}
	aset := map[string]bool{}
	for _, x := range a {
		aset[x] = true
		if !bset[x] {
			onlyA = append(onlyA, x)
		}
	}
	for _, x := range b {
		if !aset[x] {
			onlyB = append(onlyB, x)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return onlyA, onlyB
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
