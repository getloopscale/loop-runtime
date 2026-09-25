// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/cliout"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmdrift"
)

// driftRenderer writes one helm-drift outcome in one output format.
type driftRenderer func(stdout, stderr io.Writer, o *helmdrift.Outcome) error

// driftRenderers is the --format table; adding a format is adding a row.
var driftRenderers = map[string]driftRenderer{
	"human":              renderDriftHuman(false),
	"github-annotations": renderDriftHuman(true),
	"json":               renderDriftJSON,
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
	return func(outW, errW io.Writer, o *helmdrift.Outcome) error {
		stdout, stderr := cliout.New(outW), cliout.New(errW)
		for _, e := range o.RenderErrors {
			stderr.Printf("✗ helm template failed: %s\n", e)
		}
		kindsClean := len(o.Drifts) == 0 && len(o.RenderErrors) == 0
		if kindsClean {
			stdout.Println("✓ charts/ ↔ infra/helm/: both sides install the same kinds of resource.")
		}
		for _, d := range o.Drifts {
			marker, status := "⚠", "needs a registry entry ("+helmdrift.RegistryRelPath+") or a fix"
			if d.Intentional {
				marker, status = "ℹ", "intentional (recorded under decision: "+d.Decision+")"
			}
			if annotations && !d.Intentional {
				stdout.Printf("::warning::Drift detected for %s: templates differ\n", d.Pair)
			}
			stdout.Printf("%s %s\n", marker, d.Pair)
			if len(d.OnlyInPublished) > 0 {
				stdout.Printf("  Only in published: %s\n", strings.Join(d.OnlyInPublished, " "))
			}
			if len(d.OnlyInBootstrap) > 0 {
				stdout.Printf("  Only in bootstrap: %s\n", strings.Join(d.OnlyInBootstrap, " "))
			}
			stdout.Printf("  STATUS: %s\n", status)
			for _, f := range d.Unapproved {
				stdout.Printf("  UNRECORDED: %s\n", f)
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
					stdout.Printf("::warning::RBAC divergence for %s: %d undeclared cell(s)\n", d.Pair, len(d.Undeclared))
				}
			}
			stdout.Printf("%s %s — RBAC\n", marker, d.Pair)
			for _, line := range d.ReportLines() {
				stdout.Printf("  %s\n", line)
			}
		}
		switch {
		case !o.Pass:
			stderr.Println("❌ Drifts with no registry entry detected (" + helmdrift.RegistryRelPath + ").")
		case !kindsClean:
			stdout.Println("✓ All drifts are recorded in " + helmdrift.RegistryRelPath + " (intentional).")
		}
		return errors.Join(stdout.Err(), stderr.Err())
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
