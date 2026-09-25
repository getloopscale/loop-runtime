// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package helmtopology implements the `helm-topology-index` quality gate.
//
// Plan: .claude/knowledge-base/plans/helm-topology-index-plan.md
//
// # WHAT IT ANSWERS
//
// "Which cluster does this chart run on?" — a question whose answer today exists only inside
// the four helmfiles. `infra/helm/` is a flat directory of 44 entries that says nothing about
// topology. This gate derives a navigable table from the helmfiles and fails when the committed
// table drifts from them.
//
// # WHY DERIVED AND NOT A FOLDER TREE
//
// The helmfile is executable source of truth: it cannot lie, because it is what installs. A
// folder tree per plane would be a SECOND representation of the same fact, and two
// representations drift — the trap `CLAUDE.md § Helm chart dual system` already documents for
// the charts that exist in duplicate. Measured cost of moving: 370 files and 53 gates, for zero
// behaviour change.
//
// TWO DECLARATION FORMS — reading only one covers 19 of 44 (EC-1)
//
//	own:      chart: ../helm/<name>  or  chart: ../../charts/<name>
//	upstream: chart: <repo>/<name>  +  values: - ../helm/<name>/values.yaml
//
// The second own form is a chart Helmfile installs straight from charts/ — every
// former bootstrap/published pair since it became one chart (docs/decisions.md,
// "One chart per bootstrap/published pair"), and the operator charts.
//
// The form itself classifies own vs upstream, which is a stronger signal than probing for
// Chart.yaml: it describes how the release is INSTALLED rather than inferring from directory
// shape. `argocd` is `chart: argo/argo-cd`; `keda` is `chart: kedacore/keda` with local values.
//
// CONDITIONALS ARE REPRODUCED, NEVER EVALUATED (EC-2)
//
// 20 `{{- if }}` blocks and 13 `installed:` fields live in the mgmt helmfile alone.
// `installed: {{ .Values.inClusterPostgres | default false }}` means postgresql runs on mgmt in
// standalone and NOT in prod. Evaluating the predicate would require picking one environment,
// and the table would then lie about every other one. So the predicate is carried verbatim.
//
// THE DATE IS NOT PART OF THE COMPARISON (D2)
//
// A freshness gate must answer "does the content differ from source?". If the timestamp enters
// the compared body it answers "was this generated today?" instead — a different question whose
// answer is "no" every day after midnight. That defect is live in the sibling `pkg-usage-matrix`
// gate and blocked a push on 2026-08-08 with byte-identical content.
package helmtopology

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	helmfileDir = "infra/helmfile"
	helmDir     = "infra/helm"

	// DefaultDocPath is the generated index. It is OUTPUT: no code reads it, and editing it by
	// hand always loses on the next run.
	DefaultDocPath = "docs/architecture.md"

	bodyStart = "<!-- BEGIN GENERATED TABLE -->"
	bodyEnd   = "<!-- END GENERATED TABLE -->"
)

// Mode selects verification or regeneration.
type Mode int

const (
	ModeCheck Mode = iota
	ModeWrite
)

// Nature is how a chart reaches the cluster.
type Nature string

const (
	NatureOwn      Nature = "ours"
	NatureUpstream Nature = "upstream"
	// NatureUndeclared marks a directory under infra/helm/ that no helmfile mentions. It is
	// listed rather than omitted (EC-6): silence would hide dead charts, and "nobody installs
	// this" is useful information — either debt or a mistake.
	NatureUndeclared Nature = "undeclared"
)

// Entry is one row of the index.
type Entry struct {
	Chart       string
	Clusters    []string
	Nature      Nature
	Version     string // upstream pinned version; empty for own charts
	Conditional string // literal predicate, or "" when unconditional
}

// Report is the gate outcome.
type Report struct {
	Mode    Mode
	Entries []Entry
	Body    string // the deterministic table — what gets compared
	InSync  bool
	Message string
}

// Regexes over the helmfile TEXT. The files are `.gotmpl` and carry Go template syntax, so any
// YAML parser fails on them — the same trap the vap-delete-guardrail gate documents.
var (
	reOwnChart  = regexp.MustCompile(`(?m)^\s*chart:\s*(?:\.\./helm|\.\./\.\./charts)/([A-Za-z0-9_.-]+)\s*$`)
	reValuesRef = regexp.MustCompile(`(?m)^\s*-\s*\.\./helm/([A-Za-z0-9_.-]+)/[^\s]*values[^\s]*$`)
	reRelease   = regexp.MustCompile(`(?m)^\s*-\s*name:\s*(\S+)`)
	reVersion   = regexp.MustCompile(`(?m)^\s*version:\s*"?([0-9][^"\s]*)"?\s*$`)
	reInstalled = regexp.MustCompile(`(?m)^\s*installed:\s*(.+?)\s*$`)
)

// Check runs the gate against repoRoot.
func Check(repoRoot string, mode Mode, docPath string) (*Report, error) {
	if docPath == "" {
		docPath = DefaultDocPath
	}
	rep := &Report{Mode: mode}

	entries, err := collect(repoRoot)
	if err != nil {
		return nil, err
	}
	rep.Entries = entries
	rep.Body = renderTable(entries)

	full := filepath.Join(repoRoot, docPath)
	raw, readErr := os.ReadFile(full) //nolint:gosec // operator-controlled path

	if readErr != nil {
		// EC-3: a missing document is an actionable failure, never a raw I/O error. The gate is
		// wired into `quality:gates`, which the delivery-gate runs on `git push` — an unhelpful
		// error here blocks every push with a message that does not say what to do.
		if mode == ModeWrite {
			return rep, writeDoc(full, rep.Body)
		}
		rep.InSync = false
		rep.Message = fmt.Sprintf(
			"%s is missing — run `loopzero-ops quality helm-topology-index --write` and commit it",
			docPath)
		return rep, nil
	}

	committed, ok := extractBody(string(raw))
	if !ok {
		rep.InSync = false
		rep.Message = fmt.Sprintf(
			"%s has no generated-table markers — run `loopzero-ops quality helm-topology-index --write` and commit it",
			docPath)
		return rep, nil
	}

	if strings.TrimSpace(committed) == strings.TrimSpace(rep.Body) {
		rep.InSync = true
		return rep, nil
	}
	if mode == ModeWrite {
		return rep, writeDoc(full, rep.Body)
	}
	rep.InSync = false
	rep.Message = fmt.Sprintf(
		"%s is out of date — run `loopzero-ops quality helm-topology-index --write` and commit it",
		docPath)
	return rep, nil
}

// collect walks the helmfiles and infra/helm/ and builds the entry set.
func collect(root string) ([]Entry, error) {
	type acc struct {
		clusters    map[string]bool
		nature      Nature
		version     string
		conditional string
	}
	byChart := map[string]*acc{}

	get := func(name string) *acc {
		if a, ok := byChart[name]; ok {
			return a
		}
		a := &acc{clusters: map[string]bool{}}
		byChart[name] = a
		return a
	}

	hfs, err := filepath.Glob(filepath.Join(root, helmfileDir, "*.helmfile.yaml.gotmpl"))
	if err != nil {
		return nil, fmt.Errorf("helm-topology: glob helmfiles: %w", err)
	}
	sort.Strings(hfs)

	for _, hf := range hfs {
		cluster := strings.SplitN(filepath.Base(hf), ".", 2)[0]
		raw, err := os.ReadFile(hf) //nolint:gosec // repo-relative, operator-controlled
		if err != nil {
			return nil, fmt.Errorf("helm-topology: read %s: %w", hf, err)
		}
		for _, blk := range splitReleases(string(raw)) {
			// EC-1: two forms. `chart: ../helm/X` is ours; a remote chart with local values is
			// upstream. The form is the classifier.
			if m := reOwnChart.FindStringSubmatch(blk); m != nil {
				a := get(m[1])
				a.clusters[cluster] = true
				a.nature = NatureOwn
				a.conditional = firstOr(a.conditional, conditionalOf(blk))
				continue
			}
			// EC-4: a release may list several values files from the same directory; the set
			// keyed by directory name collapses them into one entry.
			for _, vm := range reValuesRef.FindAllStringSubmatch(blk, -1) {
				a := get(vm[1])
				a.clusters[cluster] = true
				if a.nature == "" {
					a.nature = NatureUpstream
				}
				if a.version == "" {
					if v := reVersion.FindStringSubmatch(blk); v != nil {
						a.version = v[1]
					}
				}
				a.conditional = firstOr(a.conditional, conditionalOf(blk))
			}
		}
	}

	// EC-6: directories nobody declares are listed, not omitted.
	dirs, err := os.ReadDir(filepath.Join(root, helmDir))
	if err != nil {
		return nil, fmt.Errorf("helm-topology: read %s: %w", helmDir, err)
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, seen := byChart[d.Name()]; !seen {
			byChart[d.Name()] = &acc{clusters: map[string]bool{}, nature: NatureUndeclared}
		}
	}

	out := make([]Entry, 0, len(byChart))
	for name, a := range byChart {
		cl := make([]string, 0, len(a.clusters))
		for c := range a.clusters {
			cl = append(cl, c)
		}
		sort.Strings(cl)
		out = append(out, Entry{
			Chart: name, Clusters: cl, Nature: a.nature,
			Version: a.version, Conditional: a.conditional,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chart < out[j].Chart })
	return out, nil
}

// splitReleases cuts the helmfile into per-release chunks. Releases begin with `- name:` at the
// list level; everything until the next one belongs to it.
func splitReleases(s string) []string {
	idx := reRelease.FindAllStringIndex(s, -1)
	if len(idx) == 0 {
		return nil
	}
	out := make([]string, 0, len(idx))
	for i, m := range idx {
		end := len(s)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, s[m[0]:end])
	}
	return out
}

// conditionalOf returns the literal predicate guarding a release, or "" when unconditional.
// EC-2: reproduced verbatim, never evaluated — evaluating would mean picking one environment.
func conditionalOf(block string) string {
	if m := reInstalled.FindStringSubmatch(block); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func firstOr(existing, candidate string) string {
	if existing != "" {
		return existing
	}
	return candidate
}

func renderTable(entries []Entry) string {
	var sb strings.Builder
	sb.WriteString("| Chart | Cluster(s) | Nature | Version | Conditional |\n")
	sb.WriteString("|---|---|---|---|---|\n")
	for _, e := range entries {
		clusters := strings.Join(e.Clusters, ", ")
		if clusters == "" {
			clusters = "—"
		}
		version := e.Version
		if version == "" {
			version = "—"
		}
		cond := e.Conditional
		if cond == "" {
			cond = "always"
		}
		// A helmfile predicate can contain `|` — Go template pipelines are common, e.g.
		// `{{ .Values.inClusterPostgres | default false }}`. Unescaped, markdown reads it as a
		// column separator and the row silently renders with a phantom extra column. Found
		// against the real repo: 5 of 44 rows were broken; no fixture had a pipe.
		fmt.Fprintf(&sb, "| `%s` | %s | %s | %s | `%s` |\n",
			escapePipe(e.Chart), escapePipe(clusters), e.Nature, escapePipe(version), escapePipe(cond))
	}
	return sb.String()
}

// escapePipe protects a markdown table cell from a literal `|` in its content.
func escapePipe(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func extractBody(doc string) (string, bool) {
	i := strings.Index(doc, bodyStart)
	j := strings.Index(doc, bodyEnd)
	if i < 0 || j < 0 || j < i {
		return "", false
	}
	return doc[i+len(bodyStart) : j], true
}

func writeDoc(full, body string) error {
	// Splice between the markers; never replace the file.
	//
	// This used to build header + table + footer and write the whole document, which is
	// right for an output nobody else touches and destructive for `docs/architecture.md`
	// — a hand-maintained document that the 2026-09-11 consolidation made the default
	// path, and that `pkg-usage-matrix` also writes into through its own marker pair.
	// Measured 2026-09-16: one --write took it from 217 lines to 78 and deleted the
	// sibling's section. The gate printed "run --write and commit it" on every failing
	// run, so the remedy it named was the thing that destroyed the file.
	raw, err := os.ReadFile(full) // #nosec G304 -- path is the configured doc under the repo root
	if os.IsNotExist(err) {
		// Nothing to preserve: the gate creates the document it is pointed at when that
		// document does not exist, markers included, and owns it from then on. Only an
		// EXISTING document is somebody else's, and that is the one this must not replace.
		if mkErr := os.MkdirAll(filepath.Dir(full), 0o750); mkErr != nil {
			return fmt.Errorf("helm-topology: mkdir: %w", mkErr)
		}
		fresh := docHeader() + bodyStart + "\n" + body + bodyEnd + "\n" + docFooter()
		if wErr := os.WriteFile(full, []byte(fresh), 0o600); wErr != nil {
			return fmt.Errorf("helm-topology: write %s: %w", full, wErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("helm-topology: read %s: %w", full, err)
	}
	doc := string(raw)

	startIdx := strings.Index(doc, bodyStart)
	endIdx := strings.Index(doc, bodyEnd)
	switch {
	case startIdx < 0:
		return fmt.Errorf("helm-topology: %s has no %s marker — add the marker pair where the table belongs; this gate does not own the document", full, bodyStart)
	case endIdx < 0:
		return fmt.Errorf("helm-topology: %s has no %s marker", full, bodyEnd)
	case endIdx < startIdx:
		return fmt.Errorf("helm-topology: %s has the markers inverted (%s precedes %s)", full, bodyEnd, bodyStart)
	}

	var sb strings.Builder
	sb.WriteString(doc[:startIdx])
	sb.WriteString(bodyStart)
	sb.WriteString("\n")
	sb.WriteString(body)
	sb.WriteString(bodyEnd)
	sb.WriteString(doc[endIdx+len(bodyEnd):])

	if err := os.WriteFile(full, []byte(sb.String()), 0o600); err != nil {
		return fmt.Errorf("helm-topology: write %s: %w", full, err)
	}
	return nil
}

func docHeader() string {
	return `---
type: reference
title: "Helm Topology — what runs on which cluster"
---

# Helm Topology

> **Generated document. Do not edit by hand** — the next run of the gate overwrites it.
> Source: ` + "`infra/helmfile/*.helmfile.yaml.gotmpl`" + `. Regenerate with:
> ` + "`loopzero-ops quality helm-topology-index --write`" + `

Answers "which cluster does this chart run on?" without opening the helmfiles. Every row
comes from the release declaration, not from a folder convention.

**How to read the columns**

- **Nature** — taken from the *form of the declaration*: ` + "`chart: ../helm/X`" + ` is a
  chart of ours; a remote chart with local ` + "`values:`" + ` is configuration of an
  upstream one. ` + "`undeclared`" + ` is a directory no helmfile mentions — debt or a
  mistake, and never omitted from here.
- **Conditional** — the ` + "`installed:`" + ` predicate **literally**, not evaluated.
  ` + "`always`" + ` means unconditional. A predicate here means the release exists in some
  environments and not others: ` + "`postgresql`" + ` runs on core in standalone and not in
  production.

**Scope:** what Helmfile (L2) installs — charts of ` + "`infra/helm/`" + ` and the charts of
` + "`charts/`" + ` it installs directly. What ArgoCD (L3) deploys from ` + "`charts/`" + ` into
tenant environments is not here.

`
}

func docFooter() string {
	return "\nThe generation date stays out of the table by design: a freshness gate answers\n" +
		"\"has the content diverged from the source?\", not \"was this generated today?\".\n"
}
