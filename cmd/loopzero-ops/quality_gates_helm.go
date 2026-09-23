// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmdiffstructural"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmdrift"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmfilecontextparity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmoverlapparity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmtopology"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pgpdb"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/vapdeleteguardrail"
)

// M9 Step 7: quality gate entry points for the "helm" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runPostgresqlPDBConfig is the Go home for the postgresql-pdb-config gate
// (migrated from infra/tests/scripts/postgresql-pdb-config_test.sh). Guards the
// B2 regression: the postgresql chart must set primary.pdb.create: false.
func runPostgresqlPDBConfig(args []string) int {
	fs := flag.NewFlagSet("postgresql-pdb-config", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := pgpdb.Check(filepath.Join(*root, "infra", "helm", "postgresql", "values.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgresql-pdb-config: %v\n", err)
		return 1
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "postgresql-pdb-config: FAIL — %s\n", outcome.Message)
		return 1
	}
	fmt.Printf("postgresql-pdb-config: PASS — %s\n", outcome.Message)
	return 0
}

// runHelmOverlapParity enforces that the 3 overlapping bootstrap charts
// (infra/helm/{loopzero-api,loopzero-cloud,admission-policies}) never have versions
// AHEAD of their published counterparts (charts/{loopzero-api,loopzero-cloud,
// loopzero-admission-policies}). T1.2 of plan
// 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md. Closes M06 HIGH gap.
func runHelmOverlapParity(args []string) int {
	fs := flag.NewFlagSet("helm-overlap-parity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := helmoverlapparity.Check(helmoverlapparity.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "helm-overlap-parity: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "helm-overlap-parity: FAIL — %d drift/missing finding(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Fix: align infra/helm/<chart>/Chart.yaml version with charts/<chart>/Chart.yaml,")
		fmt.Fprintln(os.Stderr, "     OR bump published chart in charts/<chart>/ first.")
		return 1
	}
	fmt.Println("helm-overlap-parity: PASS — bootstrap charts in parity with (or behind) published")
	return 0
}

// runHelmDiffStructural is the gate replacing helm-diff-structural.sh: fails
// CI only on STRUCTURALLY RISKY chart diffs (security-resource removal,
// namespace change, security-context downgrade). Shells out to `diff -u`.
func runHelmDiffStructural(args []string) int {
	fs := flag.NewFlagSet("helm-diff-structural", flag.ContinueOnError)
	chart := fs.String("chart", "", "chart name (diagnostics)")
	base := fs.String("base", "", "path to base rendered YAML")
	pr := fs.String("pr", "", "path to PR rendered YAML")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *base == "" || *pr == "" {
		fmt.Fprintln(os.Stderr, "helm-diff-structural: --base and --pr are required")
		return 1
	}

	outcome, err := helmdiffstructural.Check(helmdiffstructural.Config{
		Chart:    *chart,
		BasePath: *base,
		PRPath:   *pr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "helm-diff-structural: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	if !outcome.Pass {
		return 1
	}
	return 0
}

// driftRenderer writes one helm-drift outcome in one output format.
type driftRenderer func(stdout, stderr io.Writer, o *helmdrift.Outcome) error

// driftRenderers is the --format table; adding a format is adding a row.
var driftRenderers = map[string]driftRenderer{
	"human":              renderDriftHuman(false),
	"github-annotations": renderDriftHuman(true),
	"json":               renderDriftJSON,
}

func runHelmDrift(args []string) int {
	fs := flag.NewFlagSet("helm-drift", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	format := fs.String("format", "human", "output format: human | github-annotations | json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	render, ok := driftRenderers[*format]
	if !ok {
		fmt.Fprintf(os.Stderr, "helm-drift: unknown --format %q (use human | github-annotations | json)\n", *format)
		return 1
	}
	o, err := helmdrift.Check(helmdrift.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "helm-drift: %v\n", err)
		return 5
	}
	if err := render(os.Stdout, os.Stderr, o); err != nil {
		fmt.Fprintf(os.Stderr, "helm-drift: %v\n", err)
		return 5
	}
	return helmDriftExitCode(o)
}

func helmDriftExitCode(o *helmdrift.Outcome) int {
	if !o.Pass {
		return 1
	}
	return 0
}

// driftJSON is one template-set drift in the machine-readable format — the
// schema of the original bash gate, kept field for field.
type driftJSON struct {
	Pair        string `json:"pair"`
	Intentional bool   `json:"intentional"`
	// ADR keeps its field name (the --json contract); it carries the
	// docs/decisions.md row the recorded drift cites.
	ADR                      *string  `json:"adr"`
	Severity                 string   `json:"severity"`
	TemplatesOnlyInPublished []string `json:"templates_only_in_published"`
	TemplatesOnlyInBootstrap []string `json:"templates_only_in_bootstrap"`
}

// renderDriftJSON writes the outcome as one JSON document. render_errors is
// additive: before it, a failed `helm template` reached the JSON consumer only
// as a failing exit code.
func renderDriftJSON(stdout, _ io.Writer, o *helmdrift.Outcome) error {
	drifts := make([]driftJSON, 0, len(o.Drifts))
	for _, d := range o.Drifts {
		row := driftJSON{
			Pair: d.Pair, Intentional: d.Intentional, Severity: "high",
			TemplatesOnlyInPublished: nonNil(d.OnlyInPublished),
			TemplatesOnlyInBootstrap: nonNil(d.OnlyInBootstrap),
		}
		if d.Intentional {
			decision := d.Decision
			row.Severity, row.ADR = "info", &decision
		}
		drifts = append(drifts, row)
	}
	b, err := json.Marshal(struct {
		Drifts       []driftJSON `json:"drifts"`
		RBAC         []rbacJSON  `json:"rbac"`
		RenderErrors []string    `json:"render_errors"`
	}{Drifts: drifts, RBAC: rbacDriftJSON(o.RBACDrifts), RenderErrors: nonNil(o.RenderErrors)})
	if err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}
	_, err = fmt.Fprintln(stdout, string(b))
	return err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// renderDriftHuman writes the report for a person; with annotations it also
// emits a GitHub `::warning::` for every finding that needs action.
func renderDriftHuman(annotations bool) driftRenderer {
	return func(stdout, stderr io.Writer, o *helmdrift.Outcome) error {
		for _, e := range o.RenderErrors {
			fmt.Fprintf(stderr, "✗ helm template failed: %s\n", e)
		}
		kindsClean := len(o.Drifts) == 0 && len(o.RenderErrors) == 0
		if kindsClean {
			fmt.Fprintln(stdout, "✓ charts/ ↔ infra/helm/: both sides install the same kinds of resource.")
		}
		for _, d := range o.Drifts {
			marker, status := "⚠", "needs a registry entry ("+helmdrift.RegistryRelPath+") or a fix"
			if d.Intentional {
				marker, status = "ℹ", "intentional (recorded under decision: "+d.Decision+")"
			}
			if annotations && !d.Intentional {
				fmt.Fprintf(stdout, "::warning::Drift detected for %s: templates differ\n", d.Pair)
			}
			fmt.Fprintf(stdout, "%s %s\n", marker, d.Pair)
			if len(d.OnlyInPublished) > 0 {
				fmt.Fprintf(stdout, "  Only in published: %s\n", strings.Join(d.OnlyInPublished, " "))
			}
			if len(d.OnlyInBootstrap) > 0 {
				fmt.Fprintf(stdout, "  Only in bootstrap: %s\n", strings.Join(d.OnlyInBootstrap, " "))
			}
			fmt.Fprintf(stdout, "  STATUS: %s\n", status)
			for _, f := range d.Unapproved {
				fmt.Fprintf(stdout, "  UNRECORDED: %s\n", f)
			}
		}
		// What the two sides GRANT is reported apart from what they INSTALL,
		// so a reader can tell which question a finding answers; every
		// undeclared and undecided cell is named, never only counted.
		for _, d := range o.RBACDrifts {
			marker := "ℹ"
			if len(d.Undeclared) > 0 {
				marker = "⚠"
				if annotations {
					fmt.Fprintf(stdout, "::warning::RBAC divergence for %s: %d undeclared cell(s)\n", d.Pair, len(d.Undeclared))
				}
			}
			fmt.Fprintf(stdout, "%s %s — RBAC\n", marker, d.Pair)
			for _, line := range d.ReportLines() {
				fmt.Fprintf(stdout, "  %s\n", line)
			}
		}
		switch {
		case !o.Pass:
			fmt.Fprintln(stderr, "❌ Drifts with no registry entry detected ("+helmdrift.RegistryRelPath+").")
		case !kindsClean:
			fmt.Fprintln(stdout, "✓ All drifts are recorded in "+helmdrift.RegistryRelPath+" (intentional).")
		}
		return nil
	}
}

// rbacJSON mirrors RBACDrift for the machine-readable format. The cells are
// rendered as strings so a consumer reads the same spelling the human report and
// the registry use.
type rbacJSON struct {
	Pair       string   `json:"pair"`
	Declared   int      `json:"declared"`
	Undeclared []string `json:"undeclared"`
	Undecided  []string `json:"undecided"`
}

func rbacDriftJSON(drifts []helmdrift.RBACDrift) []rbacJSON {
	out := []rbacJSON{}
	for _, d := range drifts {
		row := rbacJSON{Pair: d.Pair, Declared: d.Declared, Undeclared: []string{}, Undecided: []string{}}
		for _, c := range d.Undeclared {
			row.Undeclared = append(row.Undeclared, c.String())
		}
		for _, c := range d.Undecided {
			row.Undecided = append(row.Undecided, c.String())
		}
		out = append(out, row)
	}
	return out
}

// runVAPDeleteGuardrail implements the `vap-delete-guardrail` gate (plan
// d4-vap-delete-guardrail T1.3, ADR 2026-08-07-capi-byoc-cluster-lifecycle D4).
//
// Guards the mgmt boundary resources — and their CRDs — against unauthorized DELETE.
// The CRD rule is not redundant with the instance rules: deleting the CRD collects every
// instance without any instance rule being consulted, which is the vector of the reference
// incident this policy exists to prevent.
func runVAPDeleteGuardrail(args []string) int {
	fs := flag.NewFlagSet("vap-delete-guardrail", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	rep := vapdeleteguardrail.Check(*root, nil)
	for _, w := range rep.Warnings {
		fmt.Fprintf(os.Stderr, "vap-delete-guardrail: WARN: %s\n", w)
	}
	if rep.OK() {
		fmt.Println("vap-delete-guardrail: OK")
		return 0
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(os.Stderr, "vap-delete-guardrail: FAIL [%s] %s\n", f.Check, f.Message)
	}
	return 1
}

// runHelmTopologyIndex implements the `helm-topology-index` gate (plan helm-topology-index).
//
// Derives "which cluster does this chart run on?" from the four helmfiles — the executable
// source of truth — instead of encoding it a second time as a folder tree. Two representations
// of the same fact drift; the helmfile cannot, because it is what installs.
func runHelmTopologyIndex(args []string) int {
	fs := flag.NewFlagSet("helm-topology-index", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	doc := fs.String("doc", "", "generated index path (default docs/architecture.md)")
	write := fs.Bool("write", false, "regenerate the index instead of verifying it")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	mode := helmtopology.ModeCheck
	if *write {
		mode = helmtopology.ModeWrite
	}
	rep, err := helmtopology.Check(*root, mode, *doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helm-topology-index: %v\n", err)
		return 5
	}
	if *write {
		fmt.Printf("helm-topology-index: wrote index with %d entries\n", len(rep.Entries))
		return 0
	}
	if rep.InSync {
		fmt.Printf("helm-topology-index: PASS — %d charts indexed, in sync with the helmfiles\n",
			len(rep.Entries))
		return 0
	}
	fmt.Fprintf(os.Stderr, "helm-topology-index: FAIL — %s\n", rep.Message)
	return 1
}

// runHelmfileContextParity — B-044 / slice-3 T1.3.
//
// Obtains the property that helmfile itself does NOT provide: a task whose
// --kube-context disagrees with the kubeContext its helmfile declares reproves.
// Measured against helmfile 1.1.6, the flag silently overrides the declaration,
// so declaring the context fixes the dangerous default and this gate supplies
// the conflict.
func runHelmfileContextParity(args []string) int {
	fs := flag.NewFlagSet("helmfile-context-parity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := helmfilecontextparity.Check(helmfilecontextparity.Config{RepoRoot: *root})
	fmt.Println(o.Summary())
	for _, d := range o.Divergences {
		fmt.Printf("  DIVERGE %s:%d — %s é invocado com --kube-context %q, mas declara %q\n",
			d.Taskfile, d.Line, d.Helmfile, d.Invoked, d.Declared)
	}
	for _, m := range o.MissingEnv {
		fmt.Printf("  VAR AUSENTE %s\n", m)
	}
	for _, u := range o.Undeclared {
		fmt.Printf("  SEM CONTEXTO %s — um `helmfile sync` sem flag herdaria o contexto ativo do kubectl\n", u)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "helmfile-context-parity: %v\n", err)
		return 1
	}
	fmt.Println("PASS — every literal helmfile invocation targets the cluster its helmfile declares")
	return 0
}
