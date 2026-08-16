// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ldflagsymbols

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a throwaway repo: one module, one package, one Dockerfile.
func fixture(t *testing.T, module, pkgDir, pkgSrc, dockerfile string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+module+"\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	if pkgDir != "" {
		full := filepath.Join(root, pkgDir)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(full, "x.go"), []byte(pkgSrc), 0o600); err != nil {
			t.Fatalf("pkg src: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		t.Fatalf("dockerfile: %v", err)
	}
	return root
}

const pkgWithSymbols = `package version

var (
	versionTag    = "dev"
	versionCommit = "unknown"
)
`

// TestCheck_ResolveOCaminhoContraARealidade is the whole point of this gate.
//
// `-X <importpath>.<símbolo>` over a path that does not exist is a NO-OP that
// exits 0. The Go linker does not warn, by design, so the build succeeds and the
// binary keeps its default — measured live on 2026-08-16 as
// `{"version":"dev","built_at":"unknown"}` (B-053).
//
// A gate that matched the spelling of the wrong org would catch that one typo and
// nothing else. The failure that actually costs more is a package MOVE: a
// refactor relocates the symbol, the Dockerfile keeps the old path, and the
// injection silently stops working. Resolving the path against the go.mod and the
// symbol against the AST is what makes the assertion depend on what EXISTS.
func TestCheck_ResolveOCaminhoContraARealidade(t *testing.T) {
	const mod = "github.com/getloopscale/loopzero/api"

	cases := []struct {
		name       string
		dockerfile string
		pkgDir     string
		pkgSrc     string
		wantPass   bool
		wantIn     string
	}{
		{
			name:       "caminho e simbolo existem",
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes/version.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: true,
		},
		{
			name: "org errada — o defeito medido do B-053",
			// Uma letra: getloopscale contra getloopscale. O linker aceita e nao injeta nada.
			dockerfile: "RUN go build -ldflags \"-X github.com/getloopscale/loopzero/api/internal/routes/version.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: false, wantIn: "nenhum módulo",
		},
		{
			name: "pacote movido — a falha que uma refatoracao legitima introduz",
			// O simbolo vive em internal/routes/version; o Dockerfile aponta para o
			// pacote plano que a decomposicao esvaziou.
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			// A razao tem de dizer MOVEU, e nao "falta o simbolo". O diretorio
			// `internal/routes` EXISTE — sobrevive como pai de `version` — e nao
			// guarda pacote Go nenhum. Dizer "o pacote existe, falta a variavel"
			// mandaria o leitor procurar num pacote que nao esta la, que foi a
			// primeira mensagem que escrevi e que este caso reprovou.
			wantPass: false, wantIn: "MOVEU",
		},
		{
			name:       "pacote existe mas o simbolo nao",
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes/version.versionSHA=abc\"\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: false, wantIn: "versionSHA",
		},
		{
			name:       "Dockerfile sem -X nenhum nao e violacao",
			dockerfile: "FROM alpine\nRUN echo oi\n",
			pkgDir:     "internal/routes/version", pkgSrc: pkgWithSymbols,
			wantPass: true,
		},
		{
			name: "simbolo que nao e variavel de topo nao conta como existente",
			// `-X` so escreve em variavel STRING de pacote. Uma constante ou uma
			// funcao com o mesmo nome deixaria o gate verde sobre uma injecao que
			// continua sendo no-op.
			dockerfile: "RUN go build -ldflags \"-X " + mod + "/internal/routes/version.versionTag=v1\"\n",
			pkgDir:     "internal/routes/version",
			pkgSrc:     "package version\n\nconst versionTag = \"dev\"\n",
			wantPass:   false, wantIn: "versionTag",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t, mod, tc.pkgDir, tc.pkgSrc, tc.dockerfile)
			out, err := Check(Config{Root: root})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if out.Pass != tc.wantPass {
				t.Fatalf("Pass = %v, esperado %v (violações: %+v)", out.Pass, tc.wantPass, out.Violations)
			}
			if tc.wantIn != "" {
				joined := ""
				for _, v := range out.Violations {
					joined += v.Reason + " " + v.Symbol + " "
				}
				if !strings.Contains(joined, tc.wantIn) {
					t.Errorf("a violação não nomeia a causa (%q): %+v", tc.wantIn, out.Violations)
				}
			}
		})
	}
}

// TestCheck_ArvoreSemDockerfileEhFalha keeps the invariant every gate in this tree
// shares: "nothing to check" and "everything is clean" are different facts, and
// reporting the second for the first is how a gate goes quiet.
func TestCheck_ArvoreSemDockerfileEhFalha(t *testing.T) {
	if _, err := Check(Config{Root: t.TempDir()}); err == nil {
		t.Fatal("uma árvore sem Dockerfile nenhum foi aceita como limpa")
	}
}
