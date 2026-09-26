// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// kubeconform-schemas subcommand — writes the strict kubeconform schemas of
// LoopZero's CRDs from the CRDs themselves (#389).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/kubeconformschema"
)

const kubeconformSchemasHelpText = `loopzero-ops kubeconform-schemas — derive the loopzero.io kubeconform schemas from the CRDs

USAGE:
  loopzero-ops kubeconform-schemas [--check] [--root DIR]

Reads every CRD in ` + kubeconformschema.DefaultCRDDir + `
and writes one strict schema per version into
` + kubeconformschema.DefaultSchemaDir + `,
removing any schema of the same group that no CRD produces any more. The
third-party schemas in that directory are never touched.

FLAGS:
  --check     write nothing; exit 1 when a committed schema is missing, stale or orphaned
  --root DIR  repository root (default ".")

EXIT CODES:
  0  the schemas are (now) in step with the CRDs
  1  usage error, or --check found drift
  5  the CRDs or the schema directory could not be read
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
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, kubeconformSchemasHelpText, ""
		}
		return 1, "", fmt.Sprintf("loopzero-ops kubeconform-schemas: %v\n\n%s", err, kubeconformSchemasHelpText)
	}
	if fs.NArg() != 0 {
		return 1, "", fmt.Sprintf("loopzero-ops kubeconform-schemas: unexpected argument %q\n\n%s", fs.Arg(0), kubeconformSchemasHelpText)
	}
	crdDir := filepath.Join(*root, kubeconformschema.DefaultCRDDir)
	schemaDir := filepath.Join(*root, kubeconformschema.DefaultSchemaDir)

	if *check {
		drift, err := kubeconformschema.Diff(crdDir, schemaDir)
		if err != nil {
			return 5, "", fmt.Sprintf("loopzero-ops kubeconform-schemas --check: %v\n", err)
		}
		if len(drift) > 0 {
			return 1, "", fmt.Sprintf("loopzero-ops kubeconform-schemas --check: %d schema(s) out of step with the CRDs:\n%s"+
				"Run `loopzero-ops kubeconform-schemas` and commit the result.\n", len(drift), listDrift(drift))
		}
		return 0, "kubeconform-schemas: every loopzero.io schema matches its CRD\n", ""
	}

	changed, err := kubeconformschema.Write(crdDir, schemaDir)
	if err != nil {
		return 5, "", fmt.Sprintf("loopzero-ops kubeconform-schemas: %v\n", err)
	}
	if len(changed) == 0 {
		return 0, "kubeconform-schemas: already in step with the CRDs\n", ""
	}
	return 0, fmt.Sprintf("kubeconform-schemas: %d schema(s) regenerated:\n%s", len(changed), listDrift(changed)), ""
}

func listDrift(drift []kubeconformschema.Drift) string {
	var b strings.Builder
	for _, d := range drift {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	return b.String()
}
