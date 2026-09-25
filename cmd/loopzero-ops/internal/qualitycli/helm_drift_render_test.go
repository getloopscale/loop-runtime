// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmdrift"
)

func sampleDriftOutcome() *helmdrift.Outcome {
	return &helmdrift.Outcome{
		Pass: false,
		Drifts: []helmdrift.Drift{
			{Pair: "loopzero-api", OnlyInPublished: []string{"pdb.yaml"}, Unapproved: []string{"pdb.yaml"}},
			{Pair: "loopzero-cloud", Intentional: true, Decision: "Chart-pair drift is recorded, never blessed per pair", OnlyInBootstrap: []string{"job.yaml"}},
		},
		RBACDrifts: []helmdrift.RBACDrift{{
			Pair:       "loopzero-api",
			Undeclared: []helmdrift.Cell{{Resource: "secrets", Verb: "delete"}},
		}},
		RenderErrors: []string{"loopzero-api: helm template exited 1"},
	}
}

func TestHelmDriftJSON_KeepsTheLegacySchemaAndAddsRenderErrors(t *testing.T) {
	var out bytes.Buffer
	if err := renderDriftJSON(&out, &bytes.Buffer{}, sampleDriftOutcome()); err != nil {
		t.Fatalf("renderDriftJSON: %v", err)
	}
	var got struct {
		Drifts []struct {
			Pair                     string   `json:"pair"`
			Intentional              bool     `json:"intentional"`
			ADR                      *string  `json:"adr"`
			Severity                 string   `json:"severity"`
			TemplatesOnlyInPublished []string `json:"templates_only_in_published"`
			TemplatesOnlyInBootstrap []string `json:"templates_only_in_bootstrap"`
		} `json:"drifts"`
		RBAC []struct {
			Pair       string   `json:"pair"`
			Undeclared []string `json:"undeclared"`
		} `json:"rbac"`
		RenderErrors []string `json:"render_errors"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if len(got.Drifts) != 2 || got.Drifts[0].Severity != "high" || got.Drifts[0].ADR != nil {
		t.Errorf("unintentional drift = %+v", got.Drifts[0])
	}
	if got.Drifts[1].Severity != "info" || got.Drifts[1].ADR == nil || *got.Drifts[1].ADR != "Chart-pair drift is recorded, never blessed per pair" {
		t.Errorf("intentional drift = %+v", got.Drifts[1])
	}
	if got.Drifts[0].TemplatesOnlyInBootstrap == nil {
		t.Error("an empty template list must encode as [], not null")
	}
	if len(got.RBAC) != 1 || got.RBAC[0].Undeclared[0] != "(core)/secrets/delete" {
		t.Errorf("rbac = %+v", got.RBAC)
	}
	if len(got.RenderErrors) != 1 {
		t.Errorf("a failed render must reach the JSON consumer: %+v", got.RenderErrors)
	}
}

func TestHelmDriftHuman_ReportsRenderErrorsDriftsAndRBAC(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := renderDriftHuman(false)(&out, &errOut, sampleDriftOutcome()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"loopzero-api", "Only in published: pdb.yaml", "needs a registry entry (" + helmdrift.RegistryRelPath + ") or a fix",
		"intentional (recorded under decision: Chart-pair drift is recorded, never blessed per pair)", "loopzero-api — RBAC"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(errOut.String(), "helm template failed") {
		t.Errorf("stderr = %s", errOut.String())
	}
	if strings.Contains(out.String(), "::warning::") {
		t.Error("the plain human format must not emit GitHub annotations")
	}
}

func TestHelmDriftAnnotations_AnnotateOnlyWhatNeedsAction(t *testing.T) {
	var out bytes.Buffer
	if err := renderDriftHuman(true)(&out, &bytes.Buffer{}, sampleDriftOutcome()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "::warning::"); n != 2 {
		t.Errorf("want 2 annotations (unapproved template drift + undeclared RBAC), got %d:\n%s", n, out.String())
	}
	if strings.Contains(out.String(), "::warning::Drift detected for loopzero-cloud") {
		t.Error("an intentional drift must not be annotated")
	}
}

func TestHelmDriftHuman_CleanRunSaysSo(t *testing.T) {
	var out bytes.Buffer
	if err := renderDriftHuman(false)(&out, &bytes.Buffer{}, &helmdrift.Outcome{Pass: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "install the same kinds of resource") {
		t.Errorf("stdout = %s", out.String())
	}
}

// An unknown --format used to fall through to the human output silently.
func TestHelmDrift_UnknownFormatIsAUsageError(t *testing.T) {
	g, ok := Lookup("helm-drift")
	if !ok {
		t.Fatal("helm-drift is not registered")
	}
	var stdout, stderr bytes.Buffer
	if rc := g.Run([]string{"--root", t.TempDir(), "--format", "xml"}, &stdout, &stderr); rc != 1 {
		t.Fatalf("rc = %d, want 1 (usage); stderr = %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), `unknown --format "xml"`) {
		t.Errorf("stderr does not name the bad format: %s", stderr.String())
	}
}
