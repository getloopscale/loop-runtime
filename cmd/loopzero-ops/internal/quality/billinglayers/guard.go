// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinglayers

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// openStatuses são os estados em que um item ainda é dívida com dono.
//
// `planned` conta como ABERTO, e a escolha foi revista durante a revisão desta fatia. O
// primeiro desenho o excluía com o argumento de que a camada segue descoberta enquanto o
// plano não vira código — verdade, e irrelevante para o que a catraca existe para pegar:
// FECHAMENTO sem cobertura. Um item planejado não está fechado.
//
// O custo de excluí-lo era concreto: o gate travaria TODO push do repositório justamente
// na janela em que alguém implementa o conserto, o que é desproporcional ao risco e é o
// jeito conhecido de uma trava ser desligada — a mesma razão que decidiu o B-127 e o B-143.
// `approved` was added to the contract on 2026-09-04 and belongs here for the same
// reason `planned` does, only more so: an approved item is a commitment somebody
// with the authority made, so it is MORE owned than a hypothesis, not less. It was
// missing until 2026-09-05, when moving B-033 to `approved` took the
// cost-aggregator layer's guard out from under it and failed the chain at step 8.
//
// This map is the sixth reader of `status:` in this ecosystem and the first one in
// Go. The kit pins the other five against each other, and all five are Python and
// all five live in the kit — so the invariant was pinned one repository short.
// `guard_open_statuses_test.go` closes that from this side.
var openStatuses = map[string]bool{
	"raw": true, "triaged": true, "approved": true, "planned": true,
}

var statusRE = regexp.MustCompile(`(?m)^status:\s*(\S+)`)

// GuardResolver answers whether a declared guard actually guards.
//
// Recebe as raízes como campos, e não lê `os.Getenv` nem assume o diretório corrente:
// um colaborador que lê o processo não é substituível num teste.
type GuardResolver struct {
	repoRoot    string
	siblingRoot string
	backlogPath string
}

// NewGuardResolver builds the resolver used by Check and by the tests alike.
func NewGuardResolver(repoRoot, siblingRoot, backlogPath string) *GuardResolver {
	return &GuardResolver{repoRoot: repoRoot, siblingRoot: siblingRoot, backlogPath: backlogPath}
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

// resolveDebt é a CATRACA.
//
// Uma dívida só guarda enquanto tiver dono ativo. No instante em que o item vira
// `shipped` ou `killed`, a camada ficou descoberta — e é aí que o gate reprova, obrigando
// a trocar a guarda por `gate:` ou `test:`. Sem esta cláusula, `open:` seria um tapete:
// fechar o item silenciaria a camada com o registro ainda afirmando que alguém cuida dela.
func (g *GuardResolver) resolveDebt(item string) error {
	corpo, err := os.ReadFile(filepath.Clean(g.backlogPath))
	if err != nil {
		// EC-5: presumir "aberto" seria decidir a favor da dívida com base em nada.
		return fmt.Errorf("%w: não consegui ler %s para conferir %s — não achar não é o mesmo que estar em ordem",
			ErrGuardUnresolved, g.backlogPath, item)
	}
	bloco, ok := itemBlock(string(corpo), item)
	if !ok {
		return fmt.Errorf("%w: o item %s não existe no backlog — uma dívida sem item não tem dono", ErrGuardUnresolved, item)
	}
	m := statusRE.FindStringSubmatch(bloco)
	if m == nil {
		return fmt.Errorf("%w: o item %s não declara status", ErrGuardUnresolved, item)
	}
	if !openStatuses[m[1]] {
		return fmt.Errorf("%w: o item %s está %q — o item saiu dos estados abertos e a camada ficou descoberta; troque a guarda por `gate:` ou `test:` no registro",
			ErrGuardUnresolved, item, m[1])
	}
	return nil
}

// itemBlock extracts one `## B-NNN` block.
func itemBlock(corpo, item string) (string, bool) {
	marca := "## " + item + " "
	i := strings.Index(corpo, marca)
	if i < 0 {
		return "", false
	}
	resto := corpo[i:]
	if j := strings.Index(resto[1:], "\n## B-"); j >= 0 {
		return resto[:j+1], true
	}
	return resto, true
}
