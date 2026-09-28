// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package exposure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_ReadsEveryDeclaredColumn(t *testing.T) {
	decls, malformed := Parse("# comment\n\napp.example.dev /v1/* | reviewed | BFF, tenant scoped\nauth.example.dev - | internal | health only\n")
	if len(malformed) != 0 {
		t.Fatalf("malformed = %v", malformed)
	}
	if len(decls) != 2 {
		t.Fatalf("decls = %+v", decls)
	}
	d := decls[0]
	if d.Site != "app.example.dev" || d.Path != "/v1/*" || d.Class != ClassReviewed || d.Rationale != "BFF, tenant scoped" || d.Line != 3 {
		t.Errorf("first declaration = %+v", d)
	}
	if decls[1].Key() != "auth.example.dev -" {
		t.Errorf("Key = %q", decls[1].Key())
	}
}

func TestParse_RejectsARowWithoutThreeColumns(t *testing.T) {
	_, malformed := Parse("app.example.dev /v1/* | reviewed\n")
	if len(malformed) != 1 || !strings.Contains(malformed[0], "line 1") {
		t.Fatalf("malformed = %v", malformed)
	}
}

func TestParse_RejectsAnUnknownClass(t *testing.T) {
	_, malformed := Parse("app.example.dev /v1/* | public | nope\n")
	if len(malformed) != 1 || !strings.Contains(malformed[0], "public") {
		t.Fatalf("malformed = %v", malformed)
	}
}

func TestParse_RejectsAnEmptyRationale(t *testing.T) {
	_, malformed := Parse("app.example.dev /v1/* | reviewed |   \n")
	if len(malformed) != 1 {
		t.Fatalf("malformed = %v", malformed)
	}
}

func TestParse_RejectsASiteColumnThatIsNotSiteAndPath(t *testing.T) {
	_, malformed := Parse("app.example.dev | reviewed | missing path\napp.example.dev /a /b | reviewed | two paths\n")
	if len(malformed) != 2 {
		t.Fatalf("malformed = %v", malformed)
	}
}

func TestLoad_MissingFileIsAnError(t *testing.T) {
	if _, _, err := Load(filepath.Join(t.TempDir(), "absent.txt")); err == nil {
		t.Fatal("a missing inventory must be an error, not an empty declaration set")
	}
}

func TestLoad_ReadsTheVersionedInventory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "inv.txt")
	if err := os.WriteFile(p, []byte("a.example.dev - | internal | assets\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decls, malformed, err := Load(p)
	if err != nil || len(malformed) != 0 || len(decls) != 1 {
		t.Fatalf("Load = %+v, %v, %v", decls, malformed, err)
	}
}
