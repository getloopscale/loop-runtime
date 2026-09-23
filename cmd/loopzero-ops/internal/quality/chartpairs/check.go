// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package chartpairs mantém honesto o registro dos charts que existem nas DUAS árvores com
// o mesmo `name`.
//
// # O defeito, medido
//
// B-056, 2026-08-18. `infra/helm/` (bootstrap, Helmfile L2) e `charts/` (OCI publicado,
// ArgoCD L3) coexistem por desenho, e dois charts declaram o MESMO `name` nas duas:
// `loopzero-api` e `loopzero-cloud`. O `CLAUDE.md` manda manter o par em paridade — *"when patching
// one, apply the same fix to the other"* — e a única forma de saber de qual se fala é pelo
// caminho.
//
// E o próprio `CLAUDE.md` já tinha envelhecido: afirmava **3** pares, incluindo
// `loopzero-admission-policies`. Medido: `infra/helm/loopzero-admission-policies` **não existe**. O
// contrato mandava aplicar o mesmo conserto num par que não tem outro lado — e diluir a
// paridade com um par inexistente enfraquece os dois que são reais.
//
// # Por que um registro declarado, e não uma lista em prosa
//
// Porque a prosa já drifou uma vez. Um registro que o gate compara com o disco não pode
// afirmar par que não existe nem omitir par que existe. É o mesmo padrão de
// `proxy-exposure.txt` e `billing-layers.txt`, que este repositório já usa pela mesma razão.
//
// # O limite, dito em voz alta
//
// Ele verifica a EXISTÊNCIA do par, não a paridade do conteúdo. Dois charts com o mesmo nome
// e comportamentos divergentes passam aqui — provar que um conserto foi aplicado nos dois
// exige comparar templates, e isso é outro problema.
package chartpairs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrUndeclared means a pair exists on disk and the registry does not name it.
var ErrUndeclared = errors.New("par existe em disco e não está declarado")

// ErrStale means the registry names a pair that no longer exists.
var ErrStale = errors.New("declaração sobreviveu ao par")

// ErrNothingScanned means no chart was found at all — silence, not cleanliness.
var ErrNothingScanned = errors.New("nenhum chart varrido — não achar nada não é o mesmo que não achar nada errado")

// ErrRegistryMissing means the declared registry is absent.
var ErrRegistryMissing = errors.New("registro de pares ausente")

const registroRel = "infra/quality/chart-pairs.txt"

var nameRE = regexp.MustCompile(`(?m)^name:\s*(\S+)`)

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	InfraHelm  int
	Charts     int
	Pairs      []string
	Undeclared []string
	Stale      []string
}

// chartNames reads the `name:` of every Chart.yaml directly under base. An
// absent tree holds no charts and a directory without Chart.yaml is not a
// chart; any other read failure is an error, because it hides a name.
func chartNames(base string) (map[string]bool, error) {
	out := map[string]bool{}
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chart-pairs: read %s: %w", base, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Clean(filepath.Join(base, e.Name(), "Chart.yaml")))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("chart-pairs: read %s/Chart.yaml: %w", e.Name(), err)
		}
		if m := nameRE.FindSubmatch(b); m != nil {
			out[string(m[1])] = true
		}
	}
	return out, nil
}

// Check compares the pairs on disk with the declared registry.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	infra, err := chartNames(filepath.Join(root, "infra", "helm"))
	if err != nil {
		return out, err
	}
	pub, err := chartNames(filepath.Join(root, "charts"))
	if err != nil {
		return out, err
	}
	out.InfraHelm, out.Charts = len(infra), len(pub)

	if out.InfraHelm == 0 && out.Charts == 0 {
		return out, fmt.Errorf("%w: nem infra/helm nem charts/", ErrNothingScanned)
	}

	for n := range infra {
		if pub[n] {
			out.Pairs = append(out.Pairs, n)
		}
	}
	sortStrings(out.Pairs)

	declarado, err := lerRegistro(filepath.Join(root, registroRel))
	if err != nil {
		return out, err
	}

	emDisco := map[string]bool{}
	for _, p := range out.Pairs {
		emDisco[p] = true
		if !declarado[p] {
			out.Undeclared = append(out.Undeclared, p)
		}
	}
	for d := range declarado {
		if !emDisco[d] {
			out.Stale = append(out.Stale, d)
		}
	}
	sortStrings(out.Undeclared)
	sortStrings(out.Stale)

	if len(out.Undeclared) > 0 {
		return out, fmt.Errorf("%w: %s — declare em %s",
			ErrUndeclared, strings.Join(out.Undeclared, ", "), registroRel)
	}
	if len(out.Stale) > 0 {
		return out, fmt.Errorf("%w: %s — o par não existe mais; remova de %s",
			ErrStale, strings.Join(out.Stale, ", "), registroRel)
	}
	return out, nil
}

// lerRegistro extrai os nomes declarados, um por linha antes do `|`.
func lerRegistro(p string) (map[string]bool, error) {
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRegistryMissing, registroRel)
	}
	out := map[string]bool{}
	for _, linha := range strings.Split(string(b), "\n") {
		linha = strings.TrimSpace(linha)
		if linha == "" || strings.HasPrefix(linha, "#") {
			continue
		}
		nome := linha
		if i := strings.Index(linha, "|"); i >= 0 {
			nome = strings.TrimSpace(linha[:i])
		}
		if nome != "" {
			out[nome] = true
		}
	}
	return out, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
