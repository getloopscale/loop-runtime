// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package chartvalidate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/shellrun"
)

const twoObjects = "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n# a comment-only document\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: b\n"

// fakeTools answers helm and kubeconform from fixed results and records every
// invocation, so a test can assert what ran and in which order.
type fakeTools struct {
	depBuild   *shellrun.Result
	template   *shellrun.Result
	kubeconf   *shellrun.Result
	notFound   string
	calls      []string
	kubeInputs []string
}

func (f *fakeTools) Run(_ context.Context, name string, args []string, _ ...shellrun.Option) (*shellrun.Result, error) {
	f.calls = append(f.calls, name+" "+args[0])
	if name == f.notFound {
		return &shellrun.Result{ExitCode: 1}, &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	var res *shellrun.Result
	switch {
	case name == "helm" && args[0] == "dependency":
		res = f.depBuild
	case name == "helm" && args[0] == "template":
		res = f.template
	case name == "kubeconform":
		input, err := os.ReadFile(args[len(args)-1])
		if err != nil {
			return nil, err
		}
		f.kubeInputs = append(f.kubeInputs, string(input))
		res = f.kubeconf
	default:
		return nil, errors.New("unexpected command " + name)
	}
	if res == nil {
		res = &shellrun.Result{}
	}
	if res.ExitCode != 0 {
		return res, errors.New("exit status 1")
	}
	return res, nil
}

func (f *fakeTools) ran(prefix string) bool {
	for _, c := range f.calls {
		if c == prefix {
			return true
		}
	}
	return false
}

// chartDir writes a Chart.yaml with the given body into a fresh directory.
func chartDir(t *testing.T, chartYAML string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(chartYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const plainChart = "apiVersion: v2\nname: plain\nversion: 0.1.0\n"

const chartWithDependency = "apiVersion: v2\nname: wrapper\nversion: 0.1.0\n" +
	"dependencies:\n  - name: upstream\n    version: 1.0.0\n    repository: https://example.invalid/charts\n"

func TestValidateFailsAndNamesTheChartWhenTheRenderFails(t *testing.T) {
	chart := chartDir(t, plainChart)
	tools := &fakeTools{template: &shellrun.Result{ExitCode: 1, Stderr: "Error: execution error: boom"}}

	out, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools)

	if err != nil {
		t.Fatalf("a failed render is a verdict, not an inability to measure: %v", err)
	}
	if out.Verdict() != verdict.Fail {
		t.Fatalf("verdict = %v, want FAIL for a render that failed", out.Verdict())
	}
	if !strings.Contains(out.Failure, chart) || !strings.Contains(out.Failure, "boom") {
		t.Errorf("failure %q must name the chart and carry helm's error", out.Failure)
	}
	if tools.ran("kubeconform -strict") {
		t.Error("kubeconform ran over a render that failed")
	}
}

func TestValidateFailsWhenTheRenderHoldsNoObject(t *testing.T) {
	chart := chartDir(t, plainChart)
	tools := &fakeTools{template: &shellrun.Result{Stdout: "---\n# only a comment\n"}}

	out, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools)

	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict() != verdict.Fail || !strings.Contains(out.Failure, "no Kubernetes object") {
		t.Fatalf("verdict %v failure %q, want FAIL naming the empty render", out.Verdict(), out.Failure)
	}
}

func TestValidateFailsWhenTheRenderWasTruncated(t *testing.T) {
	chart := chartDir(t, plainChart)
	tools := &fakeTools{template: &shellrun.Result{Stdout: twoObjects, Truncated: true}}

	out, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools)

	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict() != verdict.Fail || !strings.Contains(out.Failure, "truncated") {
		t.Fatalf("verdict %v failure %q, want FAIL on a truncated render", out.Verdict(), out.Failure)
	}
}

func TestValidateBuildsDeclaredDependenciesBeforeRendering(t *testing.T) {
	chart := chartDir(t, chartWithDependency)
	tools := &fakeTools{template: &shellrun.Result{Stdout: twoObjects}}

	out, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools)

	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict() != verdict.Pass {
		t.Fatalf("verdict %v failure %q, want PASS", out.Verdict(), out.Failure)
	}
	if len(tools.calls) < 2 || tools.calls[0] != "helm dependency" || tools.calls[1] != "helm template" {
		t.Errorf("calls = %v, want the dependency build before the render", tools.calls)
	}
}

func TestValidateDoesNotBuildDependenciesForAChartThatDeclaresNone(t *testing.T) {
	chart := chartDir(t, plainChart)
	tools := &fakeTools{template: &shellrun.Result{Stdout: twoObjects}}

	if _, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools); err != nil {
		t.Fatal(err)
	}
	if tools.ran("helm dependency") {
		t.Errorf("calls = %v, a chart with no dependencies needs no build", tools.calls)
	}
}

func TestValidateFailsAndNamesTheChartWhenTheDependencyBuildFails(t *testing.T) {
	chart := chartDir(t, chartWithDependency)
	tools := &fakeTools{depBuild: &shellrun.Result{ExitCode: 1, Stderr: "Error: no repository definition"}}

	out, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools)

	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict() != verdict.Fail || !strings.Contains(out.Failure, chart) || !strings.Contains(out.Failure, "no repository definition") {
		t.Fatalf("verdict %v failure %q, want FAIL naming the chart and the build error", out.Verdict(), out.Failure)
	}
	if tools.ran("helm template") {
		t.Error("rendered a chart whose dependencies could not be built")
	}
}

func TestValidateFailsWhenKubeconformRejectsTheRender(t *testing.T) {
	chart := chartDir(t, plainChart)
	tools := &fakeTools{
		template: &shellrun.Result{Stdout: twoObjects},
		kubeconf: &shellrun.Result{ExitCode: 1, Stdout: "ConfigMap a is invalid: unknown field"},
	}

	out, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools)

	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict() != verdict.Fail || !strings.Contains(out.Failure, "unknown field") {
		t.Fatalf("verdict %v failure %q, want FAIL carrying kubeconform's report", out.Verdict(), out.Failure)
	}
}

func TestValidatePassesAndCountsTheObjectsKubeconformWasGiven(t *testing.T) {
	chart := chartDir(t, plainChart)
	tools := &fakeTools{
		template: &shellrun.Result{Stdout: twoObjects},
		kubeconf: &shellrun.Result{Stdout: "Summary: 2 resources found in 1 file - Valid: 2"},
	}

	out, err := Validate(context.Background(), Spec{Release: "r", Chart: chart, SkipKinds: []string{"Foo"}}, tools)

	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict() != verdict.Pass || out.Objects != 2 {
		t.Fatalf("verdict %v objects %d failure %q, want PASS over 2 objects", out.Verdict(), out.Objects, out.Failure)
	}
	if len(tools.kubeInputs) != 1 || tools.kubeInputs[0] != twoObjects {
		t.Errorf("kubeconform was given %q, want the render itself", tools.kubeInputs)
	}
}

func TestValidateReportsAMissingToolAsAnInabilityToMeasure(t *testing.T) {
	chart := chartDir(t, plainChart)
	tools := &fakeTools{notFound: "helm"}

	_, err := Validate(context.Background(), Spec{Release: "r", Chart: chart}, tools)

	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v, want the missing binary reported as an error", err)
	}
}

func TestValidateRefusesAChartDirectoryWithNoChartYAML(t *testing.T) {
	_, err := Validate(context.Background(), Spec{Release: "r", Chart: t.TempDir()}, &fakeTools{})

	if err == nil {
		t.Fatal("a directory that is not a chart must not read as a pass")
	}
}

func TestKubeconformArgsLookUpTheLocalSchemasBeforeTheDefaults(t *testing.T) {
	args := kubeconformArgs(Spec{SchemaDir: "/s", SkipKinds: []string{"A", "B"}}, "/tmp/render.yaml")

	got := strings.Join(args, " ")
	want := "-strict -summary -schema-location /s/{{.ResourceKind}}-{{.Group}}-{{.ResourceAPIVersion}}.json " +
		"-schema-location default -skip A,B /tmp/render.yaml"
	if got != want {
		t.Errorf("args = %q\nwant   %q", got, want)
	}
}

func TestTemplateArgsCarryValuesFilesAndSets(t *testing.T) {
	args := templateArgs(Spec{Release: "r", Chart: "c", ValuesFiles: []string{"v.yaml"}, Sets: []string{"a=1"}})

	if got := strings.Join(args, " "); got != "template r c -f v.yaml --set a=1" {
		t.Errorf("args = %q", got)
	}
}
