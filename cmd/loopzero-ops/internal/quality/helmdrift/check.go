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

// approval is one file's licence to differ between the two charts.
type approval struct {
	ADR    string
	Reason string
	// Inherited marks an entry carried over from the old per-PAIR blanket
	// approval, whose per-file reason was never measured. It is honest debt:
	// the entry keeps the gate green without claiming an understanding nobody
	// has. Tracked as B-076.
	Inherited bool
}

// approvedDriftFiles registry, keyed "published:bootstrap:filename".
//
// # Why the key carries the filename
//
// It used to be keyed by PAIR alone. A pair in that map was intentional
// forever — every file it would ever drift on, including files nobody had
// seen. That is precisely how `vap-tenant-cr-shape.yaml` (a fail-closed
// ValidatingAdmissionPolicy) and two `externalsecret-*-admin-key.yaml`
// templates went missing from published charts without the gate noticing: the
// pair was already blessed, so it stopped looking.
//
// Per-file is the difference between "we know it differs and why" and "we
// stopped looking". Deliberately no globs: a glob would re-introduce the same
// approves-future-members defect at a smaller scale, which is the whole bug.
var approvedDriftFiles = map[string]approval{}

// The 21 approvals inherited from ADR-0043's blanket pair approval, each now
// carrying what was MEASURED about it rather than the one sentence they shared.
//
// Method: render both charts of the pair with `helm template` under default
// values and compare the kinds actually produced. A filename is not a resource —
// two charts can differ in every filename and install the same thing — and that
// is what the classes below record. Same method B-077 established when it proved
// its fence renders rather than checking for a file.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-14-helm-drift-per-file-reasons
func init() {
	// CLASS A — both charts render the resource; only the file declaring it has a
	// different name. This is NOT resource drift, and the gate reports it because
	// it compares filenames. Approved as a workaround for how the gate measures,
	// not because a divergence was accepted. The fix is B-110.
	both := "ambos renderizam este recurso; a divergência é de nome de arquivo, não de recurso instalado"
	for _, k := range []string{
		"loopzero-api:loopzero-api:service.yaml",
		"loopzero-api:loopzero-api:clusterrole.yaml",
		"loopzero-api:loopzero-api:clusterrolebinding.yaml",
		"loopzero-cloud:loopzero-cloud:service.yaml",
	} {
		approvedDriftFiles[k] = approval{ADR: "ADR-0043", Reason: both}
	}

	// CLASS B — the file exists on one side and NEITHER chart produces the
	// resource under default values: it is behind a toggle, a CRD that may be
	// absent, or an environment that opts in.
	conditional := "não renderiza sob os values padrão em nenhum dos dois charts; é condicional"
	for _, k := range []string{
		"loopzero-api:loopzero-api:cronjob-day30-verify.yaml",
		"loopzero-api:loopzero-api:cronjob-day60-aes-delete.yaml",
		"loopzero-api:loopzero-api:configmap-runtime.yaml",
		"loopzero-api:loopzero-api:secret-runtime.yaml",
		"loopzero-cloud:loopzero-cloud:servicemonitor.yaml",
		"loopzero-cloud:loopzero-cloud:externalsecret-lens-admin-key.yaml",
		"loopzero-cloud:loopzero-cloud:externalsecret-rag-admin-key.yaml",
		"loopzero-cloud:loopzero-cloud:configmap-runtime.yaml",
		"loopzero-cloud:loopzero-cloud:secret-runtime.yaml",
	} {
		approvedDriftFiles[k] = approval{ADR: "ADR-0043", Reason: conditional}
	}

	// CLASS C — exactly one side produces the resource. This is the asymmetry the
	// gate exists to watch, and it is the class where a future removal would be a
	// real regression rather than a naming artefact.
	onlyPublished := "só o publicado renderiza este recurso; o de bootstrap não produz nenhum do mesmo kind"
	for _, k := range []string{
		"loopzero-api:loopzero-api:configmap.yaml",
		"loopzero-api:loopzero-api:secret.yaml",
		"loopzero-cloud:loopzero-cloud:configmap.yaml",
		"loopzero-cloud:loopzero-cloud:secret.yaml",
		"loopzero-cloud:loopzero-cloud:networkpolicy.yaml",
		"loopzero-cloud:loopzero-cloud:serviceaccount.yaml",
	} {
		approvedDriftFiles[k] = approval{ADR: "ADR-0043", Reason: onlyPublished}
	}
	onlyBootstrap := "só o bootstrap renderiza este recurso; o publicado não produz nenhum do mesmo kind"
	for _, k := range []string{
		"loopzero-api:loopzero-api:ingressroute.yaml",
		"loopzero-cloud:loopzero-cloud:ingressroute.yaml",
	} {
		approvedDriftFiles[k] = approval{ADR: "ADR-0043", Reason: onlyBootstrap}
	}
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
	// Unapproved lists the drifted files no per-file approval covers. These are
	// what fail the gate — the named files, not the pair.
	Unapproved []string
	// InheritedFiles lists approved files whose reason is inherited debt rather
	// than a measured rationale, each carrying that reason verbatim (B-076).
	InheritedFiles []string
}

// Outcome captures the result.
type Outcome struct {
	Pass   bool
	Drifts []Drift
	// InheritedApprovals counts drifted files whose approval was carried over
	// from the old per-PAIR blanket, with no per-file reason ever measured.
	//
	// It is surfaced on every run on purpose. A debt that lives only in a source
	// comment is a debt nobody is reminded of; printed next to the verdict, it
	// stays countable until B-076 closes it.
	InheritedApprovals int
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

	// Every drifted file must carry its own approval. One unapproved file fails
	// the pair — the pair being "known to drift" is not a licence for the next
	// file to drift silently.
	for _, f := range append(append([]string{}, onlyPub...), onlyBoot...) {
		if a, ok := approvedDriftFiles[pairKey+":"+f]; ok {
			d.ADR = a.ADR
			if a.Inherited {
				outcome.InheritedApprovals++
				d.InheritedFiles = append(d.InheritedFiles, f+" — "+a.Reason)
			}
			continue
		}
		d.Unapproved = append(d.Unapproved, f)
	}
	d.Intentional = len(d.Unapproved) == 0
	if !d.Intentional {
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
