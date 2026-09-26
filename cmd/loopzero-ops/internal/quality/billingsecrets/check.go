// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package billingsecrets fails billing-secret mounts whose optionality is a literal instead
// of a value.
//
// MEASURED on 2026-08-11 on app-dev: the dashboard pod ran 20h `Running` without
// `loopzero-stripe-webhook` and without `loopzero-stripe-api-keys`, because both are mounted with
// `optional: true`. There was no event, warning or condition — the absence of the
// credential that authorises charging was, to Kubernetes, a perfectly normal configuration.
//
// The gate does not require the secrets to be always mandatory. It requires the decision
// to be EXPRESSIBLE: a literal `true` is the same in every environment, so the hermetic
// and the public environments necessarily share the most permissive choice. A value can
// diverge, and it is that possibility of diverging that the gate protects.
package billingsecrets

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// billingSecretNames are the secrets whose absence kills billing.
//
// A closed list on purpose: a new billing secret has to be added here deliberately, and
// that review is what the gate buys. A prefix heuristic ("anything with stripe") would
// hit more cases and would let nobody decide anything.
var billingSecretNames = []string{
	"loopzero-stripe-webhook",
	"loopzero-stripe-api-keys",
}

// chartTemplate mounts the billing Secrets. There is one loopzero-cloud chart: Helmfile
// installs charts/loopzero-cloud and ArgoCD consumes it; the bootstrap copy under infra/helm
// was deleted when the pair became one chart (docs/decisions.md, "One chart per
// bootstrap/published pair").
const chartTemplate = "charts/loopzero-cloud/templates/deployment.yaml"

// valuesFiles are the files that can mount a Secret with a literal optionality through
// `secrets.additionalSources`: the chart's own values and every Helmfile environment
// overlay of loopzero-cloud. The template alone cannot see a billing Secret listed there,
// and that listing would carry exactly the literal this gate refuses.
var valuesFiles = []string{
	"charts/loopzero-cloud/values*.yaml",
	"infra/helmfile/values/*/loopzero-cloud.yaml.gotmpl",
}

// scanTargets returns the template and every existing values file, in a stable order.
func scanTargets(root string) ([]string, error) {
	targets := []string{chartTemplate}
	for _, pattern := range valuesFiles {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", pattern, err)
		}
		sort.Strings(matches)
		for _, m := range matches {
			rel, err := filepath.Rel(root, m)
			if err != nil {
				return nil, fmt.Errorf("relativise %s: %w", m, err)
			}
			targets = append(targets, filepath.ToSlash(rel))
		}
	}
	return targets, nil
}

// Config is the gate's input.
type Config struct {
	RepoRoot string
}

// Finding is a mount whose optionality cannot vary by environment.
type Finding struct {
	File string
	Line int
	Why  string
}

// Outcome is the gate's result.
type Outcome struct {
	Findings []Finding
	// Scanned lists the files actually read, so that a gate that found nothing because
	// it read nothing does not look like a gate that passed.
	Scanned []string
}

// Verdict is Fail when any billing secret mount has literal optionality.
func (o Outcome) Verdict() verdict.Verdict { return verdict.Of(o.Findings) }

// Check scans the chart template and the values that feed it, and fails literal
// optionality on the billing secrets.
//
// It works on the template's TEXT, not on parsed YAML, for a concrete reason: the file
// contains Go template syntax (`{{ ... }}`), which no YAML parser accepts. Rendering would
// require Helm installed, turning a pure-logic checker into a shell around an external
// binary — which `CLAUDE.md § D5` refuses.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	targets, err := scanTargets(cfg.RepoRoot)
	if err != nil {
		return out, err
	}
	for _, rel := range targets {
		path := filepath.Join(cfg.RepoRoot, rel)
		f, err := os.Open(path) // #nosec G304 -- path derived from a fixed list
		if err != nil {
			if os.IsNotExist(err) {
				// A repository without the chart is not a violation. Inventing a finding
				// here would make the gate fail on any partial checkout.
				continue
			}
			return out, fmt.Errorf("open %s: %w", rel, err)
		}
		findings, err := scanTemplate(f, rel)
		_ = f.Close()
		if err != nil {
			return out, err
		}
		out.Scanned = append(out.Scanned, rel)
		out.Findings = append(out.Findings, findings...)
	}
	sort.Slice(out.Findings, func(i, j int) bool {
		if out.Findings[i].File != out.Findings[j].File {
			return out.Findings[i].File < out.Findings[j].File
		}
		return out.Findings[i].Line < out.Findings[j].Line
	})
	return out, nil
}

// scanTemplate walks the file looking for a literal `optional:` inside the block of a
// billing secret.
//
// The state it carries is deliberately short: the name of the last secret seen. An
// `optional:` is only judged while that name is a billing one, and the state is discarded
// on meeting the next `name:` — which avoids attributing a neighbour's optionality to a
// secret when the indentation changes.
func scanTemplate(r *os.File, rel string) ([]Finding, error) {
	var findings []Finding
	sc := bufio.NewScanner(r)
	current := ""
	n := 0
	for sc.Scan() {
		n++
		// A values list writes the first key of an entry after the dash
		// (`- name: loopzero-stripe-webhook`); a template writes it on its own line.
		line := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "- ")

		if name, ok := strings.CutPrefix(line, "name:"); ok {
			current = ""
			name = strings.Trim(strings.TrimSpace(name), `"'`)
			for _, s := range billingSecretNames {
				if name == s {
					current = s
				}
			}
			continue
		}

		value, ok := strings.CutPrefix(line, "optional:")
		if !ok || current == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.Contains(value, "{{") {
			// Decided by value: it can be optional in the hermetic environment and
			// mandatory in the public one. That is exactly what is wanted.
			continue
		}
		findings = append(findings, Finding{
			File: rel,
			Line: n,
			Why: fmt.Sprintf("`optional: %s` is a literal on secret %s — a literal holds for every "+
				"environment, so the public one inherits the hermetic one's most permissive choice and "+
				"starts without the credential that authorises charging. Use a value "+
				"(`{{ not .Values.billing.required }}`) so the two can diverge", value, current),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return findings, nil
}
