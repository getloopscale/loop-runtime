// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrGuardUnresolved means a declared guard does not actually guard anything.
var ErrGuardUnresolved = errors.New("guarda declarada não resolve")

// ErrSiblingAbsent means a layer lives in a repository that is not checked out.
//
// NÃO é reprovação: um checkout parcial não pode travar todo push. É reportado como
// cobertura PARCIAL, no contrato de `rules/reference-provenance.md § 4` — o que não pode
// acontecer é o verde silencioso sobre o que não foi olhado.
var ErrSiblingAbsent = errors.New("repositório irmão ausente")

// openStatuses are the states in which a debt still has an owner. `planned`
// and `approved` count as open: the ratchet exists to catch CLOSING without
// coverage, and a planned or approved item is not closed.
var openStatuses = map[string]bool{
	"raw": true, "triaged": true, "approved": true, "planned": true,
}

// terminalStatuses close a debt: an `open:` guard pointing at one fails.
var terminalStatuses = map[string]bool{"shipped": true, "killed": true}

// GuardResolver answers whether a declared guard actually guards.
//
// Recebe as raízes como campos, e não lê `os.Getenv` nem assume o diretório corrente:
// um colaborador que lê o processo não é substituível num teste.
type GuardResolver struct {
	repoRoot    string
	siblingRoot string
	debts       map[string]Debt
}

// NewGuardResolver builds the resolver used by Check and by the tests alike.
// debts is the registry's versioned [debts] ledger.
func NewGuardResolver(repoRoot, siblingRoot string, debts map[string]Debt) *GuardResolver {
	return &GuardResolver{repoRoot: repoRoot, siblingRoot: siblingRoot, debts: debts}
}

// Root returns the filesystem root for a declared repo name.
func (g *GuardResolver) Root(repo string) string {
	if repo == "loopzero" {
		return g.repoRoot
	}
	return g.siblingRoot
}

// Resolve reports whether the guard holds.
func (g *GuardResolver) Resolve(guard Guard, repo string) error {
	switch guard.Kind {
	case GuardGate:
		return g.resolveGate(guard.Target)
	case GuardTest:
		return g.resolveFile(guard.Target, repo, "teste")
	case GuardOpen:
		return g.resolveDebt(guard.Target)
	default:
		return fmt.Errorf("%w: forma %q", ErrGuardUnresolved, guard.Kind)
	}
}

// resolveGate exige que o gate seja RODADO, não apenas que exista.
//
// A verificação é sobre `taskfiles/quality.yml` — o lugar onde um gate passa a ser
// executado — e não sobre a existência do pacote. Um gate implementado e não ligado é o
// padrão "implementado mas não wired" do `CLAUDE.md`, e guarda exatamente nada.
func (g *GuardResolver) resolveGate(nome string) error {
	caminho := filepath.Join(g.repoRoot, "taskfiles", "quality.yml")
	f, err := os.Open(filepath.Clean(caminho))
	if err != nil {
		return fmt.Errorf("%w: não consegui ler %s: %w", ErrGuardUnresolved, caminho, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	alvo := "quality " + nome
	for sc.Scan() {
		if strings.Contains(sc.Text(), alvo) {
			return nil
		}
	}
	return fmt.Errorf("%w: o gate %q não é invocado em taskfiles/quality.yml — um gate que ninguém roda não guarda nada",
		ErrGuardUnresolved, nome)
}

func (g *GuardResolver) resolveFile(rel, repo, oQue string) error {
	raiz := g.Root(repo)
	if raiz == "" {
		return fmt.Errorf("%w: %s em %s", ErrSiblingAbsent, rel, repo)
	}
	if _, err := os.Stat(filepath.Join(raiz, rel)); err != nil {
		return fmt.Errorf("%w: %s %q não existe em %s", ErrGuardUnresolved, oQue, rel, repo)
	}
	return nil
}

// resolveDebt is the RATCHET.
//
// A debt guards only while it has an owner. The moment the registry records
// the item as shipped or killed, the layer is uncovered and the gate fails,
// forcing the guard to become `gate:` or `test:`. Without this clause `open:`
// would be a rug: closing the item would silence the layer while the registry
// still claimed somebody owned it.
func (g *GuardResolver) resolveDebt(item string) error {
	d, ok := g.debts[item]
	if !ok {
		return fmt.Errorf("%w: the debt %s is not declared in the [debts] section of %s — a debt without an item owns nothing",
			ErrGuardUnresolved, item, RegistryRelPath)
	}
	if !openStatuses[d.Status] {
		return fmt.Errorf("%w: the debt %s is %q — it left the open states and the layer is uncovered; swap the guard for `gate:` or `test:` in the registry",
			ErrGuardUnresolved, item, d.Status)
	}
	return nil
}
