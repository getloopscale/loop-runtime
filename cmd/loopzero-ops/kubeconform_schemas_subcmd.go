// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// kubeconform-schemas subcommand — writes the strict kubeconform schemas of
// LoopZero's CRDs from the CRDs themselves (#389), and those of the third-party
// CRDs a wrapper chart's pinned dependency ships (#391).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/kubeconformschema"
)

const kubeconformSchemasHelpText = `loopzero-ops kubeconform-schemas — derive strict kubeconform schemas from CRDs

USAGE:
  loopzero-ops kubeconform-schemas [--check] [--root DIR] [--chart DIR]

Without --chart, reads every CRD in ` + kubeconformschema.DefaultCRDDir + `
and writes one strict schema per version into
` + kubeconformschema.DefaultSchemaDir + `,
removing any schema of the same group that no CRD produces any more.

With --chart, the CRDs are the ones the chart's dependencies ship in their
crds/ directory, at the versions its Chart.lock pins, read from the archives
` + "`helm dependency build`" + ` vendors under the chart's charts/. After upgrading
the dependency, build it and run this again to refresh the schemas.

Either way only the groups the source declares are written, and schemas of
any other group in that directory are never touched.

FLAGS:
  --check      write nothing; exit 1 when a committed schema is missing, stale or orphaned
  --root DIR   repository root (default ".")
  --chart DIR  wrapper chart whose dependencies ship the CRDs, relative to --root

EXIT CODES:
  0  the schemas are (now) in step with the CRDs
  1  usage error, or --check found drift
  5  the CRDs or the schema directory could not be read, or the chart's
     dependencies are not built
`

func runKubeconformSchemasMain(args []string) int {
	return runKubeconformSchemas(args, os.Stdout, os.Stderr)
}

func runKubeconformSchemas(args []string, stdout, stderr io.Writer) int {
	code, out, msg := kubeconformSchemasResult(args)
	return emit(stdout, stderr, code, out, msg)
}

func kubeconformSchemasResult(args []string) (int, string, string) {
	fs := flag.NewFlagSet("kubeconform-schemas", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "")
	root := fs.String("root", ".", "")
	chart := fs.String("chart", "", "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, kubeconformSchemasHelpText, ""
		}
		return 1, "", fmt.Sprintf("loopzero-ops kubeconform-schemas: %v\n\n%s", err, kubeconformSchemasHelpText)
	}
	if fs.NArg() != 0 {
		return 1, "", fmt.Sprintf("loopzero-ops kubeconform-schemas: unexpected argument %q\n\n%s", fs.Arg(0), kubeconformSchemasHelpText)
	}
	source := filepath.Join(*root, kubeconformschema.DefaultCRDDir)
	generate := kubeconformschema.Generate
	if *chart != "" {
		source = filepath.Join(*root, *chart)
		generate = kubeconformschema.GenerateFromChart
	}
	schemaDir := filepath.Join(*root, kubeconformschema.DefaultSchemaDir)

	want, err := generate(source)
	if err != nil {
		return 5, "", fmt.Sprintf("loopzero-ops kubeconform-schemas: %v\n", err)
	}
	if *check {
		drift, err := kubeconformschema.Compare(want, schemaDir)
		if err != nil {
			return 5, "", fmt.Sprintf("loopzero-ops kubeconform-schemas --check: %v\n", err)
		}
		if len(drift) > 0 {
			return 1, "", fmt.Sprintf("loopzero-ops kubeconform-schemas --check: %d schema(s) out of step with the CRDs of %s:\n%s"+
				"Run `loopzero-ops kubeconform-schemas%s` and commit the result.\n", len(drift), source, listDrift(drift), chartFlag(*chart))
		}
		return 0, fmt.Sprintf("kubeconform-schemas: every schema matches the CRDs of %s\n", source), ""
	}

	changed, err := kubeconformschema.WriteSchemas(want, schemaDir)
	if err != nil {
		return 5, "", fmt.Sprintf("loopzero-ops kubeconform-schemas: %v\n", err)
	}
	if len(changed) == 0 {
		return 0, fmt.Sprintf("kubeconform-schemas: already in step with the CRDs of %s\n", source), ""
	}
	return 0, fmt.Sprintf("kubeconform-schemas: %d schema(s) regenerated from %s:\n%s", len(changed), source, listDrift(changed)), ""
}

func chartFlag(chart string) string {
	if chart == "" {
		return ""
	}
	return " --chart " + chart
}

func listDrift(drift []kubeconformschema.Drift) string {
	var b strings.Builder
	for _, d := range drift {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	return b.String()
}
