// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package stdlibonly_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/stdlibonly"
)

func pacote(t *testing.T, corpo string) string {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "api", "internal", "domain", "types.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(corpo), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// O pacote de vocabulário compartilhado importa SÓ a biblioteca padrão, e isso precisa ser
// verificado em vez de assumido.
//
// B-057, medido 2026-08-18: `api/internal/domain` é importado por **90 arquivos**, e a razão
// de poder sê-lo é justamente não arrastar dependência nenhuma — hoje são 9 imports, todos
// stdlib. Um único import externo ali se propaga para os 90, e nada impedia.
func TestStdlibOnly_CleanPackagePasses(t *testing.T) {
	root := pacote(t, `package domain

import (
	"context"
	"errors"
	"time"
)

var _ = context.Background
var _ = errors.New
var _ = time.Now
`)
	out, err := stdlibonly.Check(stdlibonly.Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("só stdlib deveria passar; veio %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero imports varridos — verde sobre nada")
	}
}

// Import externo é o defeito: ele atravessa para todos os importadores.
func TestStdlibOnly_ExternalImportIsRefused(t *testing.T) {
	root := pacote(t, `package domain

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

var _ = context.Background
var _ = prometheus.NewCounter
`)
	out, err := stdlibonly.Check(stdlibonly.Config{RepoRoot: root})
	if !errors.Is(err, stdlibonly.ErrExternalImport) {
		t.Fatalf("esperava ErrExternalImport, veio %v", err)
	}
	if len(out.External) != 1 || out.External[0] != "github.com/prometheus/client_golang/prometheus" {
		t.Errorf("o achado precisa nomear o import: %v", out.External)
	}
}

// Import de outro pacote DESTE repositório também quebra a propriedade — não é "externo" no
// sentido de terceiro, mas arrasta o mesmo acoplamento para os 90 importadores.
func TestStdlibOnly_InternalImportIsAlsoRefused(t *testing.T) {
	root := pacote(t, `package domain

import "github.com/getloopscale/loopzero/pkg/errors"

var _ = errors.New
`)
	if _, err := stdlibonly.Check(stdlibonly.Config{RepoRoot: root}); !errors.Is(err, stdlibonly.ErrExternalImport) {
		t.Fatalf("import do proprio repo tambem quebra a propriedade; veio %v", err)
	}
}

// Pacote ausente é erro, não silêncio: um rename que mova o diretório deixaria a guarda
// verde para sempre sobre nada — que é a forma `PASS — 0` que este ecossistema já mediu.
func TestStdlibOnly_MissingPackageIsRefused(t *testing.T) {
	if _, err := stdlibonly.Check(stdlibonly.Config{RepoRoot: t.TempDir()}); !errors.Is(err, stdlibonly.ErrNothingScanned) {
		t.Fatal("pacote ausente deveria reprovar, não passar em silêncio")
	}
}

// A população real: hoje são 9 imports, todos stdlib.
func TestStdlibOnly_RealTree(t *testing.T) {
	out, err := stdlibonly.Check(stdlibonly.Config{RepoRoot: filepath.Join("..", "..", "..", "..", "..")})
	if err != nil {
		t.Fatalf("árvore real quebrou a propriedade: %v (externos=%v)", err, out.External)
	}
	if out.Scanned == 0 {
		t.Fatal("zero imports varridos na árvore real")
	}
	t.Logf("população real: %d import(s) em api/internal/domain, todos stdlib", out.Scanned)
}
