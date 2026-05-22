// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package clibundlesize

import (
	"os"
	"path/filepath"
	"testing"
)

func writeJS(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestUnderCapPasses(t *testing.T) {
	root := t.TempDir()
	writeJS(t, filepath.Join(root, "cli", "dist"), "index.js", 1000)
	o := Check(Config{Root: root, MaxBytes: 4096})
	if !o.Pass || o.TotalBytes != 1000 {
		t.Errorf("expected pass with 1000 bytes, got %+v", o)
	}
}

func TestOverCapFails(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "cli", "dist")
	writeJS(t, dist, "a.js", 3000)
	writeJS(t, dist, "b.js", 2000) // total 5000 > 4096
	o := Check(Config{Root: root, MaxBytes: 4096})
	if o.Pass {
		t.Errorf("expected fail with 5000 > 4096, got %+v", o)
	}
}

func TestExactlyAtCapPasses(t *testing.T) {
	root := t.TempDir()
	writeJS(t, filepath.Join(root, "cli", "dist"), "index.js", 4096)
	o := Check(Config{Root: root, MaxBytes: 4096})
	if !o.Pass {
		t.Errorf("expected pass at exact cap (<=), got %+v", o)
	}
}

func TestNonJSFilesIgnored(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "cli", "dist")
	writeJS(t, dist, "index.js", 1000)
	// a large .map / .d.ts must NOT count
	if err := os.WriteFile(filepath.Join(dist, "index.js.map"), make([]byte, 9000), 0o644); err != nil {
		t.Fatalf("write map: %v", err)
	}
	o := Check(Config{Root: root, MaxBytes: 4096})
	if !o.Pass || o.TotalBytes != 1000 {
		t.Errorf("only .js should count, got %+v", o)
	}
}

func TestNestedJSCounted(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "cli", "dist")
	writeJS(t, dist, "index.js", 1000)
	writeJS(t, filepath.Join(dist, "commands"), "deploy.js", 1500)
	o := Check(Config{Root: root, MaxBytes: 4096})
	if o.TotalBytes != 2500 {
		t.Errorf("nested .js should be summed, got %d", o.TotalBytes)
	}
}

func TestMissingDistSkips(t *testing.T) {
	root := t.TempDir() // no cli/dist
	o := Check(Config{Root: root})
	if !o.Pass || !o.Skipped {
		t.Errorf("missing dist should skip+pass, got %+v", o)
	}
}

func TestDefaultMaxBytesApplied(t *testing.T) {
	root := t.TempDir()
	writeJS(t, filepath.Join(root, "cli", "dist"), "index.js", 1000)
	o := Check(Config{Root: root}) // default 4 MiB cap
	if !o.Pass || o.MaxBytes != DefaultMaxBytes {
		t.Errorf("expected default cap applied, got %+v", o)
	}
}
