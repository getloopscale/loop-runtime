// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package kubeconformschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct{ crdDir, schemaDir string }

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	f := fixture{crdDir: filepath.Join(root, "crds"), schemaDir: filepath.Join(root, "schemas")}
	for _, d := range []string{f.crdDir, f.schemaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(f.crdDir, "loopzero.io_widgets.yaml"), widgetCRD)
	// A third-party schema the generator does not own must survive untouched.
	write(t, filepath.Join(f.schemaDir, "certificate-cert-manager.io-v1.json"), `{"x":1}`)
	return f
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiffReportsNothingAfterWrite(t *testing.T) {
	f := newFixture(t)
	if _, err := Write(f.crdDir, f.schemaDir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	drift, err := Diff(f.crdDir, f.schemaDir)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("drift after Write = %v, want none", drift)
	}
}

func TestDiffReportsACRDChangedWithoutRegeneration(t *testing.T) {
	f := newFixture(t)
	if _, err := Write(f.crdDir, f.schemaDir); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.crdDir, "loopzero.io_widgets.yaml"), strings.Replace(widgetCRD, "maximum: 9", "maximum: 99", 1))

	drift, err := Diff(f.crdDir, f.schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0].Kind != Stale || drift[0].Name != "widget-loopzero.io-v1.json" {
		t.Fatalf("drift = %v, want one stale widget schema", drift)
	}
}

func TestDiffReportsAMissingSchema(t *testing.T) {
	f := newFixture(t)
	drift, err := Diff(f.crdDir, f.schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0].Kind != Missing {
		t.Fatalf("drift = %v, want one missing schema", drift)
	}
}

func TestDiffReportsAnOrphanSchemaOfAnOwnedGroup(t *testing.T) {
	f := newFixture(t)
	if _, err := Write(f.crdDir, f.schemaDir); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(f.schemaDir, "gadget-loopzero.io-v1.json"), `{}`)

	drift, err := Diff(f.crdDir, f.schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0].Kind != Orphan || drift[0].Name != "gadget-loopzero.io-v1.json" {
		t.Fatalf("drift = %v, want one orphan gadget schema", drift)
	}
}

func TestWriteRemovesOrphansAndKeepsThirdPartySchemas(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.schemaDir, "gadget-loopzero.io-v1.json"), `{}`)

	changes, err := Write(f.crdDir, f.schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want the missing widget written and the gadget orphan removed", changes)
	}
	if _, err := os.Stat(filepath.Join(f.schemaDir, "gadget-loopzero.io-v1.json")); !os.IsNotExist(err) {
		t.Fatalf("orphan still present: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(f.schemaDir, "certificate-cert-manager.io-v1.json"))
	if err != nil || string(body) != `{"x":1}` {
		t.Fatalf("third-party schema changed: %q, %v", body, err)
	}
}

func TestDiffRefusesASourceDirectoryWithNoCRD(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.crdDir, "loopzero.io_widgets.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Diff(f.crdDir, f.schemaDir); err == nil {
		t.Fatal("Diff over an empty CRD directory returned no error; it compared nothing")
	}
}

func TestDiffRefusesAMissingSchemaDirectory(t *testing.T) {
	f := newFixture(t)
	if _, err := Diff(f.crdDir, filepath.Join(f.schemaDir, "absent")); err == nil {
		t.Fatal("Diff against an absent schema directory returned no error")
	}
}
