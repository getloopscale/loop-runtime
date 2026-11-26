// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// validate-chart subcommand — renders one Helm chart and schema-validates the
// render, failing on any render error, an empty render or a schema violation
// (#392).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/chartvalidate"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/kubeconformschema"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/shellrun"
)

const validateChartHelpText = `loopruntime-ops validate-chart — render one Helm chart and schema-validate the render

USAGE:
  loopruntime-ops validate-chart --release NAME --chart DIR [-f FILE]... [--set EXPR]...
                          [--skip KIND,...] [--schema-dir DIR]

Builds the chart's dependencies when Chart.yaml declares any, renders it with
helm template, and validates the render with kubeconform --strict. A render
that fails, one that holds no Kubernetes object, and one kubeconform rejects
each fail, naming the chart.

FLAGS:
  --release NAME    release name given to helm template (required)
  --chart DIR       chart directory (required)
  -f FILE           values file, repeatable
  --set EXPR        helm --set expression, repeatable
  --skip KINDS      comma-separated kinds with no schema anywhere
  --schema-dir DIR  local strict schemas, looked up before kubeconform's
                    defaults (default ` + kubeconformschema.DefaultSchemaDir + `)

EXIT CODES:
  0  the render is valid
  1  usage error, or the chart failed to render or to validate
  5  it could not measure: helm or kubeconform missing, an unreadable chart
`

// repeated is a flag that may be given more than once.
type repeated []string

func (r *repeated) String() string     { return strings.Join(*r, ",") }
func (r *repeated) Set(v string) error { *r = append(*r, v); return nil }

func runValidateChartMain(args []string) int {
	return runValidateChart(args, shellrun.NewDefault(), os.Stdout, os.Stderr)
}

func runValidateChart(args []string, run shellrun.Runner, stdout, stderr io.Writer) int {
	code, out, msg := validateChartResult(args, run)
	return emit(stdout, stderr, code, out, msg)
}

func parseValidateChartArgs(args []string) (chartvalidate.Spec, bool, error) {
	var spec chartvalidate.Spec
	var values, sets repeated
	var skip string
	fs := flag.NewFlagSet("validate-chart", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&spec.Release, "release", "", "")
	fs.StringVar(&spec.Chart, "chart", "", "")
	fs.Var(&values, "f", "")
	fs.Var(&sets, "set", "")
	fs.StringVar(&skip, "skip", "", "")
	fs.StringVar(&spec.SchemaDir, "schema-dir", kubeconformschema.DefaultSchemaDir, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return spec, true, nil
		}
		return spec, false, err
	}
	if fs.NArg() != 0 {
		return spec, false, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if spec.Release == "" || spec.Chart == "" {
		return spec, false, errors.New("--release and --chart are required")
	}
	spec.ValuesFiles, spec.Sets = values, sets
	for _, k := range strings.Split(skip, ",") {
		if k = strings.TrimSpace(k); k != "" {
			spec.SkipKinds = append(spec.SkipKinds, k)
		}
	}
	return spec, false, nil
}

func validateChartResult(args []string, run shellrun.Runner) (int, string, string) {
	spec, help, err := parseValidateChartArgs(args)
	if help {
		return 0, validateChartHelpText, ""
	}
	if err != nil {
		return 1, "", fmt.Sprintf("loopruntime-ops validate-chart: %v\n\n%s", err, validateChartHelpText)
	}
	if info, err := os.Stat(spec.SchemaDir); err != nil {
		return 5, "", fmt.Sprintf("loopruntime-ops validate-chart: schema directory: %v\n", err)
	} else if !info.IsDir() {
		return 5, "", fmt.Sprintf("loopruntime-ops validate-chart: schema directory %s is not a directory\n", spec.SchemaDir)
	}
	out, err := chartvalidate.Validate(context.Background(), spec, run)
	if err != nil {
		return 5, "", fmt.Sprintf("loopruntime-ops validate-chart: %v\n", err)
	}
	if out.Verdict() != verdict.Pass {
		return 1, "", fmt.Sprintf("loopruntime-ops validate-chart: FAIL %s\n", out.Failure)
	}
	return 0, fmt.Sprintf("validate-chart %s (%s): %d object(s) — %s\n", spec.Release, spec.Chart, out.Objects, out.Report), ""
}
