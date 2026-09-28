// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package kubeconformschema

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gadgetCRD is a third-party CRD, of a group LoopZero does not own.
var gadgetCRD = strings.NewReplacer("loopzero.io", "example.com", "Widget", "Gadget", "widgets", "gadgets").Replace(widgetCRD)

const upstreamLock = "dependencies:\n- name: upstream\n  repository: https://example.invalid/charts\n  version: 1.2.3\n" +
	"digest: sha256:0\ngenerated: \"2026-01-01T00:00:00Z\"\n"

// tgz builds a chart archive holding files, keyed by their path in the archive.
func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// wrapperChart writes a chart whose Chart.lock pins upstream 1.2.3 and, when
// archive is non-nil, the built dependency archive beside it.
func wrapperChart(t *testing.T, archive []byte) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "Chart.lock"), upstreamLock)
	if archive != nil {
		if err := os.MkdirAll(filepath.Join(dir, "charts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "charts", "upstream-1.2.3.tgz"), archive, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGenerateFromChartReadsTheCRDsThePinnedDependencyShips(t *testing.T) {
	chart := wrapperChart(t, tgz(t, map[string]string{
		"upstream/Chart.yaml":                  "name: upstream\n",
		"upstream/crds/gadgets.yaml":           gadgetCRD,
		"upstream/templates/deployment.yaml":   "kind: Deployment\n",
		"upstream/charts/nested/crds/any.yaml": widgetCRD,
	}))

	schemas, err := GenerateFromChart(chart)

	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) != 1 || schemas[0].Name != "gadget-example.com-v1.json" || schemas[0].Group != "example.com" {
		t.Fatalf("schemas = %+v, want only the dependency's own gadget CRD", schemas)
	}
}

func TestGenerateFromChartIsTheSameTransformAsTheCRDDirectory(t *testing.T) {
	chart := wrapperChart(t, tgz(t, map[string]string{"upstream/crds/gadgets.yaml": gadgetCRD}))
	want, err := FromCRD([]byte(gadgetCRD))
	if err != nil {
		t.Fatal(err)
	}

	got, err := GenerateFromChart(chart)

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !bytes.Equal(got[0].Content, want[0].Content) {
		t.Fatal("a chart CRD must produce the byte-identical schema its file would")
	}
}

func TestGenerateFromChartNamesTheBuildStepWhenTheDependencyIsNotBuilt(t *testing.T) {
	chart := wrapperChart(t, nil)

	_, err := GenerateFromChart(chart)

	if err == nil || !strings.Contains(err.Error(), "helm dependency build") || !strings.Contains(err.Error(), "upstream-1.2.3.tgz") {
		t.Fatalf("err = %v, want the missing archive and the command that builds it", err)
	}
}

func TestGenerateFromChartRefusesADependencyThatShipsNoCRD(t *testing.T) {
	chart := wrapperChart(t, tgz(t, map[string]string{"upstream/Chart.yaml": "name: upstream\n"}))

	if _, err := GenerateFromChart(chart); err == nil {
		t.Fatal("a comparison against no CRD would pass every time; it must be an error")
	}
}

func TestGenerateFromChartRefusesAChartWithNoLock(t *testing.T) {
	if _, err := GenerateFromChart(t.TempDir()); err == nil {
		t.Fatal("a chart with no Chart.lock pins no version to generate from")
	}
}

func TestWriteSchemasLeavesOtherGroupsUntouched(t *testing.T) {
	f := newFixture(t)
	if _, err := writeFromCRDs(f.crdDir, f.schemaDir); err != nil {
		t.Fatal(err)
	}
	chart := wrapperChart(t, tgz(t, map[string]string{"upstream/crds/gadgets.yaml": gadgetCRD}))
	want, err := GenerateFromChart(chart)
	if err != nil {
		t.Fatal(err)
	}

	changed, err := WriteSchemas(want, f.schemaDir)

	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].Name != "gadget-example.com-v1.json" {
		t.Fatalf("changed = %v, want only the gadget schema written", changed)
	}
	if drift, err := diffFromCRDs(f.crdDir, f.schemaDir); err != nil || len(drift) != 0 {
		t.Fatalf("loopzero.io drift after a third-party write = %v, %v", drift, err)
	}
}
