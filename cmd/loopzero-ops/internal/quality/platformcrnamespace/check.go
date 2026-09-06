// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package platformcrnamespace recusa manifesto versionado que aplique um CR de plataforma
// (`loopzero.io` Tenant ou Environment) fora de `loopzero-system`.
//
// B-095. Dois fatos tornam a colocacao load-bearing, e nenhum dos dois e' obvio lendo o manifesto:
//
//  1. O cache do tenant-operator e' restrito a `loopzero-system`
//     (`operators/cmd/tenant-operator/main.go`). Um CR aplicado noutro namespace fica INVISIVEL
//     ao controller — nao da erro, nao aparece em log, simplesmente nunca reconcilia.
//  2. A politica de admissao `loopzero-tenant-namespace-policy` casa CREATE e UPDATE e valida
//     `namespace == "loopzero-system"`. Remover finalizer e' um UPDATE: um CR fora do lugar, com a
//     politica ligada, fica IMPOSSIVEL DE APAGAR.
//
// O primeiro faz o desvio passar despercebido; o segundo faz ele custar caro depois. Por isso a
// verificacao e' de gate e nao de revisao.
package platformcrnamespace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ErrForaDoNamespace indica manifesto aplicando CR de plataforma fora de `loopzero-system`.
var ErrForaDoNamespace = errors.New("CR de plataforma aplicado fora de loopzero-system")

// ErrNadaVarrido indica que a varredura nao alcancou nenhum manifesto — verde sobre nada.
var ErrNadaVarrido = errors.New("nenhum manifesto de CR de plataforma alcancado")

// NamespaceCanonico e' o unico namespace onde Tenant e Environment podem viver.
const NamespaceCanonico = "loopzero-system"

// DirsPadrao sao as arvores varridas quando Config.Dirs vem vazia.
var DirsPadrao = []string{"infra", "charts", "cmd"}

// Config parametriza a varredura.
type Config struct {
	RepoRoot string
	Dirs     []string
}

// Ocorrencia e' um bloco de manifesto que declara um CR de plataforma.
type Ocorrencia struct {
	Arquivo   string
	Linha     int
	Kind      string
	Namespace string
}

// Output relata o que a varredura viu. `Conferidas` existe para distinguir "nada errado" de
// "nao alcancou nada" — sem ele, zero violacoes e zero cobertura sao o mesmo verde.
type Output struct {
	Conferidas []Ocorrencia
	Violacoes  []Ocorrencia
	Arquivos   int
}

var (
	reKind      = regexp.MustCompile(`^\s*kind:\s*(Tenant|Environment)\s*$`)
	reNamespace = regexp.MustCompile(`^\s*namespace:\s*["']?([A-Za-z0-9_.${}:-]+)["']?\s*$`)
	reAPILoopZero   = regexp.MustCompile(`^\s*apiVersion:\s*loopzero\.io/`)
)

// Check varre as arvores e devolve as violacoes.
func Check(cfg Config) (Output, error) {
	var out Output
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = DirsPadrao
	}

	for _, d := range dirs {
		raiz := filepath.Join(cfg.RepoRoot, filepath.FromSlash(d))
		if _, err := os.Stat(raiz); err != nil {
			continue
		}
		err := filepath.WalkDir(raiz, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err //nolint:wrapcheck // erro de travessia sobe cru de proposito
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".yaml", ".yml", ".sh":
			default:
				return nil
			}
			// #nosec G304,G122 -- `p` vem do WalkDir sobre as arvores declaradas em Config.Dirs,
			// sob o proprio repositorio; nao ha entrada externa neste caminho.
			b, rerr := os.ReadFile(filepath.Clean(p))
			if rerr != nil {
				return nil
			}
			rel, _ := filepath.Rel(cfg.RepoRoot, p)
			out.Arquivos++
			out.Conferidas = append(out.Conferidas, ocorrenciasEm(string(b), filepath.ToSlash(rel))...)
			return nil
		})
		if err != nil {
			return out, fmt.Errorf("varrendo %s: %w", d, err)
		}
	}

	if len(out.Conferidas) == 0 {
		return out, fmt.Errorf("%w: %d arquivo(s) lido(s)", ErrNadaVarrido, out.Arquivos)
	}

	for _, o := range out.Conferidas {
		if !namespaceAceito(o.Namespace) {
			out.Violacoes = append(out.Violacoes, o)
		}
	}
	sort.Slice(out.Violacoes, func(i, j int) bool {
		if out.Violacoes[i].Arquivo != out.Violacoes[j].Arquivo {
			return out.Violacoes[i].Arquivo < out.Violacoes[j].Arquivo
		}
		return out.Violacoes[i].Linha < out.Violacoes[j].Linha
	})

	if len(out.Violacoes) > 0 {
		var b strings.Builder
		for _, v := range out.Violacoes {
			fmt.Fprintf(&b, "\n  %s:%d — %s em %q", v.Arquivo, v.Linha, v.Kind, v.Namespace)
		}
		return out, fmt.Errorf("%w (esperado %q):%s", ErrForaDoNamespace, NamespaceCanonico, b.String())
	}
	return out, nil
}

// namespaceAceito aceita o canonico e tambem uma variavel de shell — o valor dela e' decidido em
// tempo de execucao, e cravar aqui um julgamento sobre o que ela vale seria adivinhar. Os seeds
// que a usam declaram `loopzero-system` como default, e isso e' verificavel no proprio script.
func namespaceAceito(ns string) bool {
	if ns == NamespaceCanonico {
		return true
	}
	return strings.HasPrefix(ns, "$")
}

// ocorrenciasEm acha blocos `kind: Tenant|Environment` sob `apiVersion: loopzero.io/` e o namespace
// que os acompanha. Deliberadamente textual: os alvos incluem heredoc dentro de script de shell,
// que nenhum parser de YAML le sem antes executar o shell.
func ocorrenciasEm(conteudo, arquivo string) []Ocorrencia {
	linhas := strings.Split(conteudo, "\n")
	var out []Ocorrencia
	for i, l := range linhas {
		m := reKind.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		// A ancora e' o `apiVersion: loopzero.io/` imediatamente acima — sem ela, `kind: Environment`
		// casaria com qualquer CRD homonimo de outro grupo.
		if i == 0 || !reAPILoopZero.MatchString(linhas[i-1]) {
			continue
		}
		ns := ""
		for j := i + 1; j < len(linhas) && j <= i+8; j++ {
			if nm := reNamespace.FindStringSubmatch(linhas[j]); nm != nil {
				ns = nm[1]
				break
			}
		}
		out = append(out, Ocorrencia{Arquivo: arquivo, Linha: i + 1, Kind: m[1], Namespace: ns})
	}
	return out
}
