// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/kubeconformschema"
)

const subcmdCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  group: runtime.getloopscale.com
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
	if err := os.WriteFile(filepath.Join(crdDir, "runtime.getloopscale.com_gizmos.yaml"), []byte(subcmdCRD), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(schemaDir, "gizmo-runtime.getloopscale.com-v1.json")
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
	if !strings.Contains(out.String(), "missing gizmo-runtime.getloopscale.com-v1.json") {
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

// thirdPartyChart writes a wrapper chart under root whose pinned dependency
// ships one CRD of a group LoopRuntime does not own.
func thirdPartyChart(t *testing.T, root string) (chartRel, schemaFile string) {
	t.Helper()
	chartRel = filepath.Join("infra", "helm", "wrapper")
	crd := strings.NewReplacer("runtime.getloopscale.com", "example.com", "Gizmo", "Doohickey").Replace(subcmdCRD)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "upstream/crds/doohickeys.yaml", Mode: 0o644, Size: int64(len(crd)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(crd)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, chartRel, "charts"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := "dependencies:\n- name: upstream\n  repository: https://example.invalid\n  version: 2.0.0\n"
	if err := os.WriteFile(filepath.Join(root, chartRel, "Chart.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, chartRel, "charts", "upstream-2.0.0.tgz"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return chartRel, filepath.Join(root, kubeconformschema.DefaultSchemaDir, "doohickey-example.com-v1.json")
}

func TestKubeconformSchemasWritesTheSchemasAChartDependencyShips(t *testing.T) {
	root, loopruntimeSchema := schemaRepo(t)
	chart, schemaFile := thirdPartyChart(t, root)
	var out, errOut bytes.Buffer

	code := runKubeconformSchemas([]string{"--root", root, "--chart", chart}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if _, err := os.Stat(schemaFile); err != nil {
		t.Fatalf("third-party schema not written: %v", err)
	}
	if _, err := os.Stat(loopruntimeSchema); !os.IsNotExist(err) {
		t.Fatalf("--chart touched the runtime.getloopscale.com schemas: %v", err)
	}
}

func TestKubeconformSchemasCheckOfAChartFailsUntilItsSchemasAreWritten(t *testing.T) {
	root, _ := schemaRepo(t)
	chart, _ := thirdPartyChart(t, root)
	var out, errOut bytes.Buffer

	if code := runKubeconformSchemas([]string{"--check", "--root", root, "--chart", chart}, &out, &errOut); code != 1 {
		t.Fatalf("check before write: exit %d, want 1; stderr %q", code, errOut.String())
	}
	if code := runKubeconformSchemas([]string{"--root", root, "--chart", chart}, &out, &errOut); code != 0 {
		t.Fatalf("write: exit %d, stderr %q", code, errOut.String())
	}
	if code := runKubeconformSchemas([]string{"--check", "--root", root, "--chart", chart}, &out, &errOut); code != 0 {
		t.Fatalf("check after write: exit %d, stderr %q", code, errOut.String())
	}
}

func TestKubeconformSchemasOfAnUnbuiltChartCannotReachAVerdict(t *testing.T) {
	root, _ := schemaRepo(t)
	chart, _ := thirdPartyChart(t, root)
	if err := os.Remove(filepath.Join(root, chart, "charts", "upstream-2.0.0.tgz")); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer

	code := runKubeconformSchemas([]string{"--check", "--root", root, "--chart", chart}, &out, &errOut)

	if code != 5 || !strings.Contains(errOut.String(), "helm dependency build") {
		t.Fatalf("exit %d stderr %q, want 5 naming the build step", code, errOut.String())
	}
}
