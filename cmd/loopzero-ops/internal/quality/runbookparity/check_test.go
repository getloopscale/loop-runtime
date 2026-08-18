// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package runbookparity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

func arvore(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const runbookOK = "O SA correto é `loopzero-workflow-submitter` (bound to a Role).\n" +
	"Do **NOT** mint `loopzero-build-sa` here — ele é o executor.\n" +
	"```bash\nkubectl -n argo create token loopzero-workflow-submitter --duration=720h\n```\n"

// O DEFEITO MEDIDO em 2026-08-18: a mensagem de erro mandava rotacionar
// `loopzero-build-sa`, e o runbook diz explicitamente para NÃO fazer isso — o SA errado
// autentica e depois é proibido, trocando um 401 legível por um 403 que parece falha nova.
func TestMensagemQueContradizORunbookReprova(t *testing.T) {
	root := arvore(t, map[string]string{
		"docs/operations/build-cluster-token-rotation.md":  runbookOK,
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"argo step watcher: build cluster auth failed — rotate loopzero-build-sa token\",\n",
	})
	out, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrContradiction) {
		t.Fatalf("mensagem que manda fazer o que o runbook proíbe reprova; err=%v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Named != "loopzero-build-sa" {
		t.Fatalf("o achado precisa NOMEAR o SA errado e o certo: %+v", out.Findings)
	}
	if out.Findings[0].Prescribed != "loopzero-workflow-submitter" {
		t.Fatalf("o SA prescrito precisa vir do runbook: %+v", out.Findings[0])
	}
}

func TestMensagemQueConcordaComORunbookPassa(t *testing.T) {
	root := arvore(t, map[string]string{
		"docs/operations/build-cluster-token-rotation.md":  runbookOK,
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"argo step watcher: build cluster auth failed — rotate loopzero-workflow-submitter token\",\n",
	})
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("mensagem alinhada passa: %v", err)
	}
}

// Ausência é FALHA: sem runbook não há prescrição contra a qual comparar, e reportar
// "sem contradição" seria o verde-sobre-nada de sempre.
func TestRunbookAusenteReprova(t *testing.T) {
	root := arvore(t, map[string]string{
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"rotate loopzero-build-sa token\")\n",
	})
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrRunbookMissing) {
		t.Fatalf("sem runbook não há o que comparar; err=%v", err)
	}
}

func TestZeroMensagensVarridasReprova(t *testing.T) {
	root := arvore(t, map[string]string{
		"docs/operations/build-cluster-token-rotation.md": runbookOK,
	})
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("zero mensagens varridas reprova; err=%v", err)
	}
}

// A população real.
func TestArvoreReal(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot})
	if err != nil {
		for _, f := range out.Findings {
			t.Errorf("%s:%d — manda rotacionar %q, runbook prescreve %q", f.File, f.Line, f.Named, f.Prescribed)
		}
		t.Fatalf("árvore real: %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero mensagens varridas — não achar nada não é o mesmo que não achar nada errado")
	}
}

// A FORMA REAL do runbook, e o gate era cego a ela.
//
// `Do **NOT** mint` termina a linha e o nome do SA vem na SEGUINTE, atrás de um `> ` de
// citação markdown. O casamento parava no `>`, então `forbidden` ficava vazio e a varredura
// da população passava — não porque a árvore estava limpa, mas porque o detector não via.
// Terceira vez que esta armadilha aparece nesta sessão (B-090, B-019, esta).
func TestProibicaoQuebradaEmDUASLinhasComCitacao(t *testing.T) {
	real := "> watch** build Workflows is `loopzero-workflow-submitter` (bound to a Role with\n" +
		"> `workflows: [create, get, list, watch, delete]`). Do **NOT** mint\n" +
		"> `loopzero-build-sa` here — that SA is the workflow *executor*.\n" +
		"```bash\nkubectl -n argo create token loopzero-workflow-submitter --duration=720h\n```\n"
	root := arvore(t, map[string]string{
		"docs/operations/build-cluster-token-rotation.md":  real,
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"build cluster auth failed — rotate loopzero-build-sa token\",\n",
	})
	out, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrContradiction) {
		t.Fatalf("a proibição quebrada em duas linhas precisa ser lida; err=%v (%+v)", err, out.Findings)
	}
}

// Um COMENTÁRIO que cita a mensagem antiga é documentação, não emissão — e o gate casava
// com ele. Foi o próprio comentário que registra este conserto que disparou o falso
// positivo. Mesma armadilha do `publishhygiene`: o texto que descreve o defeito aciona o
// detector. Reescrever o comentário resolveria por fora e deixaria o gate errado.
func TestComentarioQueCitaAMensagemAntigaNaoEAchado(t *testing.T) {
	root := arvore(t, map[string]string{
		"docs/operations/build-cluster-token-rotation.md": runbookOK,
		"api/internal/services/build/argo_step_watcher.go": "" +
			"\t\t// CORRIGIDO: esta mensagem dizia \"rotate loopzero-build-sa token\", e o runbook proíbe.\n" +
			"\t\tlogger.Error(\"build cluster auth failed — rotate loopzero-workflow-submitter token\",\n",
	})
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("comentário que documenta o conserto não é achado: %v", err)
	}
}
