// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package billinglayers refuses a billing chain whose layers stop having an owner.
//
// # O achado, medido
//
// Em 2026-08-11 mediram-se CINCO camadas independentes onde a ausência de configuração de
// cobrança é tolerada — o monte do Secret, o seed, o agregador de custo, o portão de custo
// e o reporte de uso. Todas falhando para o MESMO lado — não cobrar, não barrar, não
// verificar — e nenhuma dizendo isso. A camada 3 anuncia a própria morte com um
// `logger.Info("cost aggregator disabled")`: o mesmo nível de log do boot bem-sucedido.
//
// Cada escolha é defensável isolada. Juntas formam um sistema que, diante de qualquer
// configuração faltante, cobra menos e barra menos — e informa isso a ninguém. **A direção
// do erro é o achado**: um sistema de cobrança que erra sempre para o mesmo lado não é
// tolerante, tem viés (B-038).
//
// # O que este gate verifica, e o que ele NÃO verifica
//
// Ele NÃO conserta as camadas — isso é dos itens B-033 e B-034. Ele garante que a lista
// delas seja **enumerável, com dono, e impossível de encolher em silêncio**:
//
//	guarda declarada que não resolve      →  REPROVA
//	dívida `open:` cujo item foi fechado  →  REPROVA   ← a catraca
//	menos camadas que o piso              →  REPROVA
//	arquivo com insumo e sem declaração   →  REPROVA
//	repositório irmão ausente             →  PARCIAL, nomeado — nunca verde silencioso
//
// # A âncora é DADO, e a razão é concreta
//
// Ela mora no próprio registro, e não como literais neste arquivo. Escrita em Go, ela
// faria este arquivo referenciar insumo de cobrança — e o gate reprovaria a si mesmo.
// Como dado, ela vira revisável em vez de escondida numa constante.
//
// A escolha da âncora foi MEDIDA antes de ser feita: heurística por nome
// (`billing|stripe|metering|cost`) casa **142** arquivos — ruído, não controle, e a corrida
// armamentista que o `publishhygiene` recusou na metade de IP. Por referência a insumo,
// **21** (16 aqui, 5 no irmão).
//
// # Limites, ditos em voz alta
//
// Uma camada que degrade **sem tocar em nenhum insumo conhecido** passa despercebida.
// Aconteceu durante esta própria fatia: o portão de custo (`api/internal/server/router.go`)
// não citava nenhum insumo da âncora inicial, e só entrou quando o identificador do
// enforcer foi acrescentado ao registro. A âncora é uma lista, e listas envelhecem.
//
// E este parágrafo já reprovou este arquivo uma vez: a versão anterior nomeava aquele
// identificador por extenso, o que fez o gate encontrar a si mesmo. Tirar a âncora do
// código não basta se a PROSA a reintroduz — e declarar este arquivo como referência
// resolveria por fora, deixando um ponto cego exatamente onde a trava é auditada.
//
// E uma guarda `test:` é verificada por EXISTÊNCIA, não por conteúdo: o gate não sabe se
// aquele teste afirma o invariante. Distinguir exigiria análise semântica de teste, que
// nada neste repositório faz.
//
// A varredura de superfície cobre ESTE repositório apenas. As camadas do irmão são
// verificadas por declaração — os arquivos existem, a guarda resolve —, mas um arquivo NOVO
// lá que referencie um insumo não é detectado aqui. Isso é deliberado: varrer o irmão
// faria uma mudança feita por outra pessoa, noutro repositório, travar o push deste. Um
// gate que reprova por causa do trabalho alheio é desligado, e levaria junto a proteção
// que dava.
//
// Decisão registrada em
// `.claude/knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-18-billing-layers-registry.md`
// (T2, pendente de ratificação).
package billinglayers

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ErrUndeclaredSurface means a file references a billing input without being declared.
var ErrUndeclaredSurface = errors.New("arquivo referencia insumo de cobrança e não está declarado")

// ErrBelowFloor means the registry shrank below its declared minimum.
var ErrBelowFloor = errors.New("menos camadas que o piso declarado")

// ErrNothingScanned means the scan reached no file.
var ErrNothingScanned = errors.New("nenhum arquivo varrido — não achar nada não é o mesmo que não achar nada errado")

// RegistryRelPath is where the declared registry lives.
const RegistryRelPath = "infra/quality/billing-layers.txt"

// Config drives the check.
type Config struct {
	RepoRoot string
	// SiblingRoot is `../loopzero-cloud`. Empty or absent → PARTIAL, never a silent green.
	SiblingRoot string
}

// BrokenGuard is one layer whose declared protection does not hold.
type BrokenGuard struct {
	Layer string
	Why   string
}

// Outcome reports what was established.
type Outcome struct {
	Layers      int
	Scanned     int
	Broken      []BrokenGuard
	Undeclared  []string
	NotVerified []string
	Partial     bool
	Reason      string
}

// Check reads the registry and verifies it is honest.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	f, err := os.Open(filepath.Clean(filepath.Join(root, RegistryRelPath)))
	if err != nil {
		return out, fmt.Errorf("%w: não consegui ler %s: %w", ErrRegistryMalformed, RegistryRelPath, err)
	}
	reg, perr := ParseRegistry(f)
	_ = f.Close()
	if perr != nil {
		return out, perr
	}
	out.Layers = len(reg.Layers)

	sibling := cfg.SiblingRoot
	if sibling != "" {
		if st, serr := os.Stat(sibling); serr != nil || !st.IsDir() {
			sibling = ""
		}
	}
	resolver := NewGuardResolver(root, sibling, filepath.Join(root, "BACKLOG.md"))

	// EC-4: o piso é o que impede o registro de se auto-silenciar.
	if out.Layers < reg.Floor {
		return out, fmt.Errorf("%w: %d camadas declaradas, piso %d — remover uma camada de verdade exige baixar o piso no mesmo diff",
			ErrBelowFloor, out.Layers, reg.Floor)
	}

	declarados := map[string]bool{RegistryRelPath: true} // EC-1: citar o insumo é a definição dele
	for _, ref := range reg.References {
		declarados[ref.File] = true
	}

	for _, l := range reg.Layers {
		if resolver.Root(l.Repo) == "" {
			out.Partial = true
			out.NotVerified = append(out.NotVerified, l.ID+" ("+l.Repo+")")
			continue
		}
		for _, rel := range l.Files {
			if l.Repo == "loopzero" {
				declarados[rel] = true
			}
			if err := resolver.resolveFile(rel, l.Repo, "arquivo da camada"); err != nil {
				out.Broken = append(out.Broken, BrokenGuard{Layer: l.ID, Why: err.Error()})
			}
		}
		if err := resolver.Resolve(l.Guard, l.Repo); err != nil {
			out.Broken = append(out.Broken, BrokenGuard{Layer: l.ID, Why: err.Error()})
		}
	}

	arquivos, gerr := versionedFiles(root)
	if gerr != nil {
		return out, gerr
	}
	out.Scanned = len(arquivos)
	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}
	out.Undeclared = undeclaredSurfaces(root, arquivos, reg.Anchor, declarados)

	if len(out.Broken) > 0 {
		return out, fmt.Errorf("%w: %s", ErrGuardUnresolved, describe(out.Broken))
	}
	if len(out.Undeclared) > 0 {
		return out, fmt.Errorf("%w (%d): %s — declare como camada, ou como referência com a razão de não ser uma",
			ErrUndeclaredSurface, len(out.Undeclared), strings.Join(out.Undeclared, ", "))
	}

	out.Reason = fmt.Sprintf("%d camada(s) com guarda que resolve, %d arquivo(s) varrido(s)", out.Layers, out.Scanned)
	if out.Partial {
		out.Reason += fmt.Sprintf(" — COBERTURA PARCIAL: %s não verificada(s), repositório irmão ausente",
			strings.Join(out.NotVerified, ", "))
	}
	return out, nil
}

func describe(b []BrokenGuard) string {
	partes := make([]string, 0, len(b))
	for _, x := range b {
		partes = append(partes, x.Layer+": "+x.Why)
	}
	return strings.Join(partes, "; ")
}

// undeclaredSurfaces finds files that touch a billing input without being declared.
func undeclaredSurfaces(root string, arquivos, ancora []string, declarados map[string]bool) []string {
	var out []string
	for _, rel := range arquivos {
		if declarados[rel] || !scannable(rel) {
			continue
		}
		if referencesAnchor(filepath.Join(root, rel), ancora) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// scannable limits the sweep to the kinds of file a layer can live in.
//
// Os testes ficam de fora de propósito: um teste que cita o insumo está exercitando a
// camada, não sendo uma. Incluí-los obrigaria a declarar cada arquivo de teste, e o
// registro viraria uma lista de testes.
func scannable(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, "_test.sh") {
		return false
	}
	for _, ext := range []string{".go", ".yaml", ".yml", ".gotmpl"} {
		if strings.HasSuffix(rel, ext) {
			return true
		}
	}
	return false
}

func referencesAnchor(abs string, ancora []string) bool {
	f, err := os.Open(filepath.Clean(abs))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		linha := sc.Text()
		for _, a := range ancora {
			if strings.Contains(linha, a) {
				return true
			}
		}
	}
	return false
}

// versionedFiles lists what git tracks — what will be published.
func versionedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files em %s: %w", root, err)
	}
	var out []string
	for _, l := range strings.Split(string(body), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}
