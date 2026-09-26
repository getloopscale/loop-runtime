// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/kubeconformschema"
)

const subcmdCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  group: loopzero.io
  names:
    kind: Gizmo
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        properties:
          spec:
            type: object
        type: object
`

func schemaRepo(t *testing.T) (root, schemaFile string) {
	t.Helper()
	root = t.TempDir()
	crdDir := filepath.Join(root, kubeconformschema.DefaultCRDDir)
	schemaDir := filepath.Join(root, kubeconformschema.DefaultSchemaDir)
	for _, d := range []string{crdDir, schemaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(crdDir, "loopzero.io_gizmos.yaml"), []byte(subcmdCRD), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(schemaDir, "gizmo-loopzero.io-v1.json")
}

func TestKubeconformSchemasWritesTheSchemaAndReportsIt(t *testing.T) {
	root, schemaFile := schemaRepo(t)
	var out, errOut bytes.Buffer

	code := runKubeconformSchemas([]string{"--root", root}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if _, err := os.Stat(schemaFile); err != nil {
		t.Fatalf("schema not written: %v", err)
	}
	if !strings.Contains(out.String(), "missing gizmo-loopzero.io-v1.json") {
		t.Errorf("stdout = %q, want the written schema named", out.String())
	}
}

func TestKubeconformSchemasCheckFailsOnDriftAndWritesNothing(t *testing.T) {
	root, schemaFile := schemaRepo(t)
	var out, errOut bytes.Buffer

	code := runKubeconformSchemas([]string{"--check", "--root", root}, &out, &errOut)

	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr %q", code, errOut.String())
	}
	if _, err := os.Stat(schemaFile); !os.IsNotExist(err) {
		t.Fatalf("--check wrote the schema: %v", err)
	}
}

func TestKubeconformSchemasCheckPassesWhenInStep(t *testing.T) {
	root, _ := schemaRepo(t)
	var out, errOut bytes.Buffer
	if code := runKubeconformSchemas([]string{"--root", root}, &out, &errOut); code != 0 {
		t.Fatalf("write: exit %d, stderr %q", code, errOut.String())
	}
	out.Reset()

	code := runKubeconformSchemas([]string{"--check", "--root", root}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestKubeconformSchemasWithoutCRDsCannotReachAVerdict(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runKubeconformSchemas([]string{"--root", t.TempDir()}, &out, &errOut); code != 5 {
		t.Fatalf("exit %d, want 5; stderr %q", code, errOut.String())
	}
}

func TestKubeconformSchemasRejectsAStrayArgument(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runKubeconformSchemas([]string{"extra"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
}
