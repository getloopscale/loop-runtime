// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package kubeconformschemasync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/kubeconformschema"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

const gadgetCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  group: loopzero.io
  names:
    kind: Gadget
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              replicas:
                maximum: 3
                type: integer
            type: object
        type: object
`

// repo lays out a repository root with one CRD and its schema regenerated.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	crdDir := filepath.Join(root, kubeconformschema.DefaultCRDDir)
	schemaDir := filepath.Join(root, kubeconformschema.DefaultSchemaDir)
	for _, d := range []string{crdDir, schemaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(crdDir, "loopzero.io_gadgets.yaml"), gadgetCRD)
	if _, err := kubeconformschema.Write(crdDir, schemaDir); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPassesWhenEverySchemaIsRegenerated(t *testing.T) {
	root := repo(t)

	o, err := Check(Config{RepoRoot: root})

	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Verdict() != verdict.Pass || o.Schemas != 1 {
		t.Fatalf("outcome = %+v, want PASS over 1 schema", o)
	}
}

func TestFailsWhenACRDChangedWithoutRegeneration(t *testing.T) {
	root := repo(t)
	crd := filepath.Join(root, kubeconformschema.DefaultCRDDir, "loopzero.io_gadgets.yaml")
	writeFile(t, crd, strings.Replace(gadgetCRD, "maximum: 3", "maximum: 5", 1))

	o, err := Check(Config{RepoRoot: root})

	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Verdict() != verdict.Fail || len(o.Drift) != 1 || o.Drift[0].Kind != kubeconformschema.Stale {
		t.Fatalf("outcome = %+v, want FAIL with one stale schema", o)
	}
}

func TestFailsWhenASchemaIsMissing(t *testing.T) {
	root := repo(t)
	if err := os.Remove(filepath.Join(root, kubeconformschema.DefaultSchemaDir, "gadget-loopzero.io-v1.json")); err != nil {
		t.Fatal(err)
	}

	o, err := Check(Config{RepoRoot: root})

	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Verdict() != verdict.Fail || len(o.Drift) != 1 || o.Drift[0].Kind != kubeconformschema.Missing {
		t.Fatalf("outcome = %+v, want FAIL with one missing schema", o)
	}
}

func TestFailsOnAnOrphanSchemaWithNoCRD(t *testing.T) {
	root := repo(t)
	writeFile(t, filepath.Join(root, kubeconformschema.DefaultSchemaDir, "widget-loopzero.io-v1.json"), `{}`)

	o, err := Check(Config{RepoRoot: root})

	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Verdict() != verdict.Fail || len(o.Drift) != 1 || o.Drift[0].Kind != kubeconformschema.Orphan {
		t.Fatalf("outcome = %+v, want FAIL with one orphan schema", o)
	}
}

func TestCannotMeasureWithoutTheCRDSource(t *testing.T) {
	root := t.TempDir()

	if _, err := Check(Config{RepoRoot: root}); err == nil {
		t.Fatal("Check with no CRD directory returned no error; it compared nothing")
	}
}
