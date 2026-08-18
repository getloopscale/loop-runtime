// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package runbookparity refuses an error message that tells the operator to do what the
// runbook forbids.
//
// # O defeito, medido
//
// Em 2026-08-18, seguindo o runbook `docs/operations/build-cluster-token-rotation.md` para
// consertar um erro que disparava a cada 2 segundos no droplet, encontrei a contradição:
//
//	api/.../argo_step_watcher.go  logava  "build cluster auth failed — rotate loopzero-build-sa token"
//	o runbook diz                          "Do NOT mint `loopzero-build-sa` here"
//
// E o runbook explica a consequência: `loopzero-build-sa` é o **executor** do workflow, ligado só a
// `workflowtaskresults: [create, patch]`. O token dele **autentica** (some o 401) e é
// **proibido** de listar workflows (403). *"Minting the wrong SA turns a 401 into a silent 403
// that looks like a fresh failure."*
//
// Ou seja: a mensagem de erro conduzia o operador a trocar um defeito legível por um mudo. O SA
// correto é `loopzero-workflow-submitter`.
//
// # O que este gate verifica
//
// Que o ServiceAccount **nomeado na mensagem** seja o mesmo que o runbook **prescreve**. É
// hermético — lê a árvore, não o cluster — e nasce de um defeito real, não de uma hipótese.
//
// # O limite, dito em voz alta
//
// Ele cobre **esta** família: mensagens de rotação de token que nomeiam um ServiceAccount. Uma
// mensagem que contradiga o runbook de outra forma — porta errada, namespace errado, comando
// errado — passa. Prometer paridade geral entre runbook e código seria a superestimação que
// este ecossistema já mede em vários itens.
package runbookparity

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrContradiction means a message names a ServiceAccount the runbook forbids.
var ErrContradiction = errors.New("mensagem de erro contradiz o runbook")

// ErrRunbookMissing means the prescribing runbook is absent.
//
// Sem prescrição não há com o que comparar, e reportar "sem contradição" seria verde sobre
// nada.
var ErrRunbookMissing = errors.New("runbook ausente — sem prescrição não há paridade a verificar")

// ErrNothingScanned means no rotation message was found.
var ErrNothingScanned = errors.New("nenhuma mensagem de rotação varrida — não achar nada não é o mesmo que não achar nada errado")

// runbookRel is the prescribing document.
const runbookRel = "docs/operations/build-cluster-token-rotation.md"

var (
	// rotateMsgRE captura o SA que a mensagem manda rotacionar.
	rotateMsgRE = regexp.MustCompile(`rotate\s+([a-z0-9-]+)\s+token`)
	// prescribedRE captura o SA que o runbook manda mintar.
	prescribedRE = regexp.MustCompile(`create\s+token\s+([a-z0-9-]+)`)
	// forbiddenRE captura o SA que o runbook proíbe explicitamente.
	forbiddenRE = regexp.MustCompile("NOT\\*{0,2}\\s+mint\\s+`?([a-z0-9-]+)`?")
)

// normalize strips markdown blockquote prefixes and collapses whitespace, so a prescription
// broken across lines is read as one sentence.
func normalize(md string) string {
	var b strings.Builder
	for _, line := range strings.Split(md, "\n") {
		b.WriteString(strings.TrimPrefix(strings.TrimSpace(line), "> "))
		b.WriteString(" ")
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Finding is one message contradicting the runbook.
type Finding struct {
	File       string
	Line       int
	Named      string
	Prescribed string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned  int
	Findings []Finding
	Reason   string
}

// Check compares every rotation message against the runbook's prescription.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}

	body, err := os.ReadFile(filepath.Clean(filepath.Join(root, runbookRel)))
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrRunbookMissing, runbookRel)
	}
	// Normaliza ANTES de casar: no markdown real a proibição quebra em duas linhas e a
	// segunda começa com o `> ` da citação, então `\s+` não alcança o nome do SA. A
	// primeira versão deste gate era cega exatamente a isso, e a varredura da população
	// passava por cegueira em vez de limpeza — a armadilha do B-090, terceira vez na sessão.
	flat := normalize(string(body))
	prescribed := ""
	if m := prescribedRE.FindStringSubmatch(flat); m != nil {
		prescribed = m[1]
	}
	forbidden := map[string]bool{}
	for _, m := range forbiddenRE.FindAllStringSubmatch(flat, -1) {
		forbidden[m[1]] = true
	}
	if prescribed == "" {
		return out, fmt.Errorf("%w: %s não prescreve nenhum `create token <sa>`", ErrRunbookMissing, runbookRel)
	}

	err = filepath.Walk(filepath.Join(root, "api"), func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil //nolint:nilerr // arquivo ilegível não é achado deste gate
		}
		// #nosec G122 -- `p` vem do próprio Walk sobre `api/`, raiz derivada de constante, e
		// é apenas LIDO. Não há escrita nem execução; o pior caso de um symlink é ler um .go
		// que não é nosso, e o gate reporta a linha, não a segue.
		f, oerr := os.Open(filepath.Clean(p))
		if oerr != nil {
			return nil //nolint:nilerr
		}
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		n := 0
		for sc.Scan() {
			n++
			linha := sc.Text()
			// Comentário é documentação, não emissão. O comentário que registra ESTE
			// conserto cita a mensagem antiga, e a primeira versão do gate casou com ele —
			// a armadilha do `publishhygiene`, onde o texto que descreve o defeito aciona o
			// detector. Reescrever o comentário resolveria por fora e deixaria o gate errado.
			if strings.HasPrefix(strings.TrimSpace(linha), "//") {
				continue
			}
			m := rotateMsgRE.FindStringSubmatch(linha)
			if m == nil {
				continue
			}
			out.Scanned++
			named := m[1]
			if named == prescribed {
				continue
			}
			// Só é contradição quando o runbook PROÍBE o nomeado. Um SA que o runbook
			// simplesmente não menciona pode ser de outro procedimento.
			if !forbidden[named] {
				continue
			}
			rel, _ := filepath.Rel(root, p)
			out.Findings = append(out.Findings, Finding{File: rel, Line: n, Named: named, Prescribed: prescribed})
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("varrendo api/: %w", err)
	}

	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}
	if len(out.Findings) > 0 {
		f := out.Findings[0]
		return out, fmt.Errorf("%w: %s:%d manda rotacionar %q, e o runbook proíbe exatamente isso — o SA correto é %q. O errado autentica e é PROIBIDO, trocando um 401 legível por um 403 mudo",
			ErrContradiction, f.File, f.Line, f.Named, f.Prescribed)
	}
	out.Reason = fmt.Sprintf("%d mensagem(ns) de rotação varrida(s), todas nomeando o ServiceAccount que o runbook prescreve (%s)", out.Scanned, prescribed)
	return out, nil
}
