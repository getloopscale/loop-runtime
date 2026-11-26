// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scheduledimage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// tree builds a synthetic repository: a chart with a scheduled resource, its values,
// and the publication matrix.
func tree(t *testing.T, chart, template, values, matrix string) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("infra/helm", chart, "templates/cron.yaml"), template)
	write(filepath.Join("infra/helm", chart, "values.yaml"), values)
	write(".github/workflows/release-artifacts.yml", matrix)
	return root
}

const matrixWithLoopRuntimeAPI = `          ALL='[{"image":"loop-runtime-api","dockerfile":"api/Dockerfile","context":"."}]'` + "\n"

const cronTemplate = `apiVersion: argoproj.io/v1alpha1
kind: CronWorkflow
spec:
  templates:
    - container:
        image: "{{ .Values.img.repository }}:{{ .Values.img.tag }}"
`

func TestOurImageWithAPublicationPathPasses(t *testing.T) {
	root := tree(t, "c1", cronTemplate,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: ghcr.io/getloopscale/loop-runtime-api\n  tag: develop\n",
		matrixWithLoopRuntimeAPI)
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("a published image passes: %v (%+v)", err, out.Findings)
	}
	if out.Scanned != 1 {
		t.Fatalf("scanned=%d", out.Scanned)
	}
}

// The B-019 defect: a schedule pointing at an image of OURS that nobody publishes.
func TestOurImageWithoutAPublicationPathFails(t *testing.T) {
	root := tree(t, "c1", cronTemplate,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: registry.digitalocean.com/loop-runtime-system/loop-runtime-ops\n  tag: v0.1.0\n",
		matrixWithLoopRuntimeAPI)
	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}}))
	if !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("an image with no publication path fails; err=%v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Image == "" {
		t.Fatalf("the finding must NAME the image; got %+v", out.Findings)
	}
}

// A third-party image is not ours to publish.
func TestThirdPartyImageIsNotOurs(t *testing.T) {
	for _, img := range []string{"curlimages/curl", "bitnami/kubectl", "quay.io/openbao/openbao", "ghcr.io/openbao/openbao-snapshot-agent"} {
		root := tree(t, "c1", cronTemplate,
			"crons:\n  a:\n    enabled: true\nimg:\n  repository: "+img+"\n  tag: v1\n",
			matrixWithLoopRuntimeAPI)
		if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
			t.Fatalf("%s is third-party: %v", img, err)
		}
	}
}

// A literal image in the template (not through values) is judged too.
func TestLiteralImageInTemplateIsJudged(t *testing.T) {
	lit := "kind: CronJob\nspec:\n  template:\n    spec:\n      containers:\n        - image: registry.digitalocean.com/loop-runtime-system/loop-runtime-ops:v0.1.0\n"
	root := tree(t, "c1", lit, "crons:\n  a:\n    enabled: true\n", matrixWithLoopRuntimeAPI)
	if _, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}})); !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("a literal fails too; err=%v", err)
	}
}

// A chart WITHOUT a scheduled resource is not scanned — the gate measures scheduling, not images.
func TestChartWithoutScheduleIsNotScanned(t *testing.T) {
	noCron := "kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - image: registry.digitalocean.com/loop-runtime-system/loop-runtime-ops:v0.1.0\n"
	root := tree(t, "c1", noCron, "x: 1\n", matrixWithLoopRuntimeAPI)
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("with no schedule there is nothing to scan, and zero scanned fails; err=%v (%+v)", err, out)
	}
}

// Absence is a FAILURE, never "no violation".
func TestMissingMatrixFails(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "infra/helm/c1/templates/cron.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, []byte(cronTemplate), 0o600)
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrMatrixUnreadable) {
		t.Fatalf("without the matrix nothing says what is published; err=%v", err)
	}
}

// The real population.
func TestRealTree(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: repoRoot}))
	if err != nil {
		for _, f := range out.Findings {
			t.Errorf("%s — %s: %s", f.Chart, f.Image, f.Why)
		}
		t.Fatalf("real tree: %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero scheduled manifests scanned — finding nothing is not finding nothing wrong")
	}
}

// The form the real tree uses: the image is assembled in a template variable
// (`{{- $image := printf "%s:%s" .Values.a .Values.b }}`) and used afterwards
// (`image: {{ $image | quote }}`). An inline-only resolver passed over it for the
// wrong reason: not because the tree was clean, but because it could not see.
func TestImageThroughATemplateVariableIsResolved(t *testing.T) {
	real := `{{- $image := printf "%s:%s" .Values.loopRuntimeOpsImage.repository .Values.loopRuntimeOpsImage.tag }}
kind: CronWorkflow
spec:
  templates:
    - container:
        image: {{ $image | quote }}
`
	root := tree(t, "c1", real,
		"crons:\n  a:\n    enabled: true\nloopRuntimeOpsImage:\n  repository: registry.digitalocean.com/loop-runtime-system/loop-runtime-ops\n  tag: v0.1.0\n",
		matrixWithLoopRuntimeAPI)
	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}}))
	if !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("the real form must be resolved; err=%v (%+v)", err, out.Findings)
	}
	if len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Image, "loop-runtime-ops") {
		t.Fatalf("the image must be NAMED: %+v", out.Findings)
	}
}

// The same form with an image that IS published — the boundary that keeps the
// resolver from failing everything that goes through a variable.
func TestTemplateVariableWithPublishedImagePasses(t *testing.T) {
	real := `{{- $image := printf "%s:%s" .Values.img.repository .Values.img.tag }}
kind: CronWorkflow
spec:
  templates:
    - container:
        image: {{ $image | quote }}
`
	root := tree(t, "c1", real,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: ghcr.io/getloopscale/loop-runtime-api\n  tag: develop\n",
		matrixWithLoopRuntimeAPI)
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("a published image through a variable passes: %v", err)
	}
}

// An exemption needs a reason AND a sunset. Without both it declares nothing, it only silences.
func TestExemptionNeedsReasonAndSunset(t *testing.T) {
	root := tree(t, "c1", cronTemplate,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: registry.digitalocean.com/loop-runtime-system/loop-runtime-ops\n  tag: v1\n",
		matrixWithLoopRuntimeAPI)
	for _, e := range []Exemption{
		{Image: "loop-runtime-ops", Sunset: "2026-11-18"},
		{Image: "loop-runtime-ops", Reason: "the crons do not run the binary"},
	} {
		if _, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{e}})); err == nil {
			t.Fatalf("an incomplete exemption must not pass: %+v", e)
		}
	}
	ok := Exemption{Image: "loop-runtime-ops", Reason: "the crons do not run the binary", Sunset: "2026-11-18"}
	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{ok}}))
	if err != nil {
		t.Fatalf("a complete exemption passes: %v", err)
	}
	if out.Exempt != 1 {
		t.Fatalf("the exemption must be COUNTED so its existence shows; got %d", out.Exempt)
	}
}

// An event-triggered WorkflowTemplate has the same property that decides the
// criterion — the execution is autonomous — and the same chart used the impossible
// image in both a cron and a WorkflowTemplate.
func TestWorkflowTemplateIsJudgedToo(t *testing.T) {
	wt := `kind: WorkflowTemplate
spec:
  templates:
    - container:
        image: "{{ .Values.img.repository }}:{{ .Values.img.tag }}"
`
	root := tree(t, "c1", wt,
		"img:\n  repository: registry.digitalocean.com/loop-runtime-system/loop-runtime-ops\n  tag: v0.1.0\n",
		matrixWithLoopRuntimeAPI)
	if _, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}})); !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("event-triggered execution fails the same way; err=%v", err)
	}
}

// An image the gate cannot resolve (an include, a conditional) used to fall to
// "third-party" and pass, while the package doc promised to report it.
func TestUnresolvedImageExpressionIsAFinding(t *testing.T) {
	inc := "kind: CronJob\nspec:\n  template:\n    spec:\n      containers:\n        - image: {{ include \"x.image\" . }}\n"
	root := tree(t, "c1", inc, "crons:\n  a:\n    enabled: true\n", matrixWithLoopRuntimeAPI)

	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}}))

	if !errors.Is(err, ErrNoPublicationPath) || len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Why, "template expression") {
		t.Fatalf("an unresolved image must be a finding; out=%+v err=%v", out, err)
	}
}

// A values file that cannot be parsed must not become "no values" silently.
func TestMalformedValuesIsAnError(t *testing.T) {
	root := tree(t, "c1", cronTemplate, "img: [unclosed\n", matrixWithLoopRuntimeAPI)

	if _, err := violationOf(Check(Config{RepoRoot: root})); err == nil || errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("a malformed values.yaml must be an error of its own; err=%v", err)
	}
}

// `{{ .Values.x | default "lit" }}` is the form two charts of this tree use: the
// value when set, the literal otherwise.
func TestDefaultPipeIsResolved(t *testing.T) {
	tmpl := "kind: CronJob\nspec:\n  template:\n    spec:\n      containers:\n        - image: {{ .Values.job.image | default \"registry.digitalocean.com/loop-runtime-system/loop-runtime-ops:v1\" }}\n"
	root := tree(t, "c1", tmpl, "job: {}\n", matrixWithLoopRuntimeAPI)

	out, err := violationOf(Check(Config{RepoRoot: root, Exemptions: []Exemption{}}))

	if !errors.Is(err, ErrNoPublicationPath) || len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Image, "loop-runtime-ops:v1") {
		t.Fatalf("the default literal must be the resolved image; out=%+v err=%v", out, err)
	}
}
