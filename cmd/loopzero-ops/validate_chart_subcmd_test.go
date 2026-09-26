// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/shellrun"
)

// realHelm runs helm for real and answers kubeconform with a pass, so these
// tests prove what helm does to a chart without needing kubeconform's schema
// downloads.
type realHelm struct{ helm shellrun.Runner }

func (r realHelm) Run(ctx context.Context, name string, args []string, opts ...shellrun.Option) (*shellrun.Result, error) {
	if name == "kubeconform" {
		return &shellrun.Result{Stdout: "Summary: stubbed"}, nil
	}
	return r.helm.Run(ctx, name, args, opts...)
}

func requireHelm(t *testing.T) realHelm {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		// A failure, not a skip: helm is a declared prerequisite, and a skip
		// would erase the guard on exactly the machine that lacks it.
		t.Fatal("helm is not on PATH, and it is a declared prerequisite of this repository")
	}
	return realHelm{helm: shellrun.NewDefault()}
}

func writeChart(t *testing.T, dir, chartYAML, template string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(chartYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "t.yaml"), []byte(template), 0o600); err != nil {
		t.Fatal(err)
	}
}

const configMapTemplate = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n"

// The #392 scenario, end to end: a chart whose render errors. The pipeline it
// replaced exited 0 here.
func TestValidateChartFailsNamingAChartWhoseRenderErrors(t *testing.T) {
	tools := requireHelm(t)
	chart := filepath.Join(t.TempDir(), "broken")
	writeChart(t, chart, "apiVersion: v2\nname: broken\nversion: 0.1.0\n", `{{ fail "render refused" }}`)
	var out, errOut bytes.Buffer

	code := runValidateChart([]string{"--release", "r", "--chart", chart, "--schema-dir", t.TempDir()}, tools, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout %q stderr %q", code, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), chart) || !strings.Contains(errOut.String(), "render refused") {
		t.Errorf("stderr %q must name the chart and carry the render error", errOut.String())
	}
}

// A fresh clone vendors no chart dependency; the validation builds them rather
// than rendering a chart that cannot render.
func TestValidateChartBuildsAnUnvendoredDependencyBeforeRendering(t *testing.T) {
	tools := requireHelm(t)
	root := t.TempDir()
	writeChart(t, filepath.Join(root, "sub"), "apiVersion: v2\nname: sub\nversion: 0.1.0\n", configMapTemplate)
	wrapper := filepath.Join(root, "wrapper")
	writeChart(t, wrapper, "apiVersion: v2\nname: wrapper\nversion: 0.1.0\n"+
		"dependencies:\n  - name: sub\n    version: 0.1.0\n    repository: file://../sub\n", configMapTemplate)
	var out, errOut bytes.Buffer

	code := runValidateChart([]string{"--release", "r", "--chart", wrapper, "--schema-dir", t.TempDir()}, tools, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit %d, want 0; stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "2 object(s)") {
		t.Errorf("stdout %q, want the wrapper's and the dependency's object counted", out.String())
	}
}

func TestValidateChartRejectsAMissingChartFlag(t *testing.T) {
	var out, errOut bytes.Buffer

	code := runValidateChart([]string{"--release", "r"}, realHelm{}, &out, &errOut)

	if code != 1 || !strings.Contains(errOut.String(), "--chart") {
		t.Fatalf("exit %d stderr %q, want a usage error naming --chart", code, errOut.String())
	}
}

func TestValidateChartReportsAnAbsentSchemaDirectoryAsUnmeasurable(t *testing.T) {
	var out, errOut bytes.Buffer

	code := runValidateChart([]string{"--release", "r", "--chart", t.TempDir(),
		"--schema-dir", filepath.Join(t.TempDir(), "absent")}, realHelm{}, &out, &errOut)

	if code != 5 {
		t.Fatalf("exit %d, want 5; stderr %q", code, errOut.String())
	}
}

func TestValidateChartSplitsTheSkipListOnCommas(t *testing.T) {
	spec, _, err := parseValidateChartArgs([]string{"--release", "r", "--chart", "c",
		"-f", "a.yaml", "-f", "b.yaml", "--set", "x=1", "--skip", "A, B"})

	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(spec.SkipKinds, "|"); got != "A|B" {
		t.Errorf("skip = %q", got)
	}
	if got := strings.Join(spec.ValuesFiles, "|"); got != "a.yaml|b.yaml" {
		t.Errorf("values = %q", got)
	}
}
