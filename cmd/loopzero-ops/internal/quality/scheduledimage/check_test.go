// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scheduledimage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// arvore monta um repositorio sintetico: um chart com recurso agendado, os values dele,
// e a matriz de publicacao.
func arvore(t *testing.T, chart, template, values, matriz string) string {
	t.Helper()
	root := t.TempDir()
	escreve := func(rel, corpo string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(corpo), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	escreve(filepath.Join("infra/helm", chart, "templates/cron.yaml"), template)
	escreve(filepath.Join("infra/helm", chart, "values.yaml"), values)
	escreve(".github/workflows/release-artifacts.yml", matriz)
	return root
}

const matrizComLoopZeroAPI = `          ALL='[{"image":"loopzero-api","dockerfile":"api/Dockerfile","context":"."}]'` + "\n"

const cronTemplate = `apiVersion: argoproj.io/v1alpha1
kind: CronWorkflow
spec:
  templates:
    - container:
        image: "{{ .Values.img.repository }}:{{ .Values.img.tag }}"
`

func TestImagemNossaComCaminhoDePublicacaoPassa(t *testing.T) {
	root := arvore(t, "c1", cronTemplate,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: ghcr.io/getloopscale/loopzero-api\n  tag: develop\n",
		matrizComLoopZeroAPI)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("imagem publicada passa: %v (%+v)", err, out.Findings)
	}
	if out.Scanned != 1 {
		t.Fatalf("scanned=%d", out.Scanned)
	}
}

// O defeito do B-019: agendamento apontando para imagem NOSSA que ninguem publica.
func TestImagemNossaSemCaminhoDePublicacaoReprova(t *testing.T) {
	root := arvore(t, "c1", cronTemplate,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: registry.digitalocean.com/loopzero-system/loopzero-ops\n  tag: v0.1.0\n",
		matrizComLoopZeroAPI)
	out, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}})
	if !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("imagem sem caminho reprova; err=%v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Image == "" {
		t.Fatalf("o achado precisa NOMEAR a imagem; got %+v", out.Findings)
	}
}

// Imagem de terceiro nao e nossa para publicar — e sao 6 das 7 desta arvore.
func TestImagemDeTerceiroNaoEResponsabilidadeNossa(t *testing.T) {
	for _, img := range []string{"curlimages/curl", "bitnami/kubectl", "quay.io/openbao/openbao", "ghcr.io/openbao/openbao-snapshot-agent"} {
		root := arvore(t, "c1", cronTemplate,
			"crons:\n  a:\n    enabled: true\nimg:\n  repository: "+img+"\n  tag: v1\n",
			matrizComLoopZeroAPI)
		if _, err := Check(Config{RepoRoot: root}); err != nil {
			t.Fatalf("%s e de terceiro: %v", img, err)
		}
	}
}

// Imagem literal no template (sem passar por values) tambem e julgada.
func TestImagemLiteralNoTemplateEJulgada(t *testing.T) {
	lit := "kind: CronJob\nspec:\n  template:\n    spec:\n      containers:\n        - image: registry.digitalocean.com/loopzero-system/loopzero-ops:v0.1.0\n"
	root := arvore(t, "c1", lit, "crons:\n  a:\n    enabled: true\n", matrizComLoopZeroAPI)
	if _, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}}); !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("literal tambem reprova; err=%v", err)
	}
}

// Um chart SEM recurso agendado nao e varrido — o gate mede agendamento, nao imagem.
func TestChartSemAgendamentoNaoEVarrido(t *testing.T) {
	semCron := "kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - image: registry.digitalocean.com/loopzero-system/loopzero-ops:v0.1.0\n"
	root := arvore(t, "c1", semCron, "x: 1\n", matrizComLoopZeroAPI)
	out, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("sem agendamento nao ha o que varrer, e zero varrido reprova; err=%v (%+v)", err, out)
	}
}

// Ausencia e FALHA, nunca "sem violacao".
func TestMatrizAusenteReprova(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "infra/helm/c1/templates/cron.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, []byte(cronTemplate), 0o600)
	if _, err := Check(Config{RepoRoot: root}); !errors.Is(err, ErrMatrixUnreadable) {
		t.Fatalf("sem a matriz nao da para saber o que e publicado; err=%v", err)
	}
}

// A populacao real (rules/testing.md § 4.2).
func TestArvoreReal(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot})
	if err != nil {
		for _, f := range out.Findings {
			t.Errorf("%s — %s: %s", f.Chart, f.Image, f.Why)
		}
		t.Fatalf("arvore real: %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero manifestos agendados varridos — nao achar nada nao e o mesmo que nao achar nada errado")
	}
}

// A FORMA REAL desta árvore, e o gate era cego a ela.
//
// `cronworkflows.yaml` monta a imagem numa VARIÁVEL de template
// (`{{- $image := printf "%s:%s" .Values.a .Values.b }}`) e só depois a usa
// (`image: {{ $image | quote }}`). A primeira versão deste gate resolvia apenas `.Values`
// inline, então a varredura da população passou — não porque a árvore estava limpa, mas
// porque o detector era cego. É a mesma armadilha do `B-090`: seis fixtures sintéticas
// verdes e a árvore real usando uma sétima forma.
func TestImagemViaVariavelDeTemplateEResolvida(t *testing.T) {
	real := `{{- $image := printf "%s:%s" .Values.loopzeroOpsImage.repository .Values.loopzeroOpsImage.tag }}
kind: CronWorkflow
spec:
  templates:
    - container:
        image: {{ $image | quote }}
`
	root := arvore(t, "c1", real,
		"crons:\n  a:\n    enabled: true\nloopzeroOpsImage:\n  repository: registry.digitalocean.com/loopzero-system/loopzero-ops\n  tag: v0.1.0\n",
		matrizComLoopZeroAPI)
	out, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}})
	if !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("a forma real precisa ser resolvida; err=%v (%+v)", err, out.Findings)
	}
	if len(out.Findings) != 1 || !strings.Contains(out.Findings[0].Image, "loopzero-ops") {
		t.Fatalf("a imagem precisa ser NOMEADA: %+v", out.Findings)
	}
}

// A mesma forma, com imagem que TEM caminho — a fronteira que impede o resolvedor de
// virar um "reprova tudo que passa por variável".
func TestVariavelDeTemplateComImagemPublicadaPassa(t *testing.T) {
	real := `{{- $image := printf "%s:%s" .Values.img.repository .Values.img.tag }}
kind: CronWorkflow
spec:
  templates:
    - container:
        image: {{ $image | quote }}
`
	root := arvore(t, "c1", real,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: ghcr.io/getloopscale/loopzero-api\n  tag: develop\n",
		matrizComLoopZeroAPI)
	if _, err := Check(Config{RepoRoot: root}); err != nil {
		t.Fatalf("imagem publicada via variável passa: %v", err)
	}
}

// A isenção precisa de razão E prazo. Sem os dois ela não declara nada, só cala.
func TestIsencaoPrecisaDeRazaoEPrazo(t *testing.T) {
	root := arvore(t, "c1", cronTemplate,
		"crons:\n  a:\n    enabled: true\nimg:\n  repository: registry.digitalocean.com/loopzero-system/loopzero-ops\n  tag: v1\n",
		matrizComLoopZeroAPI)
	for _, e := range []Exemption{
		{Image: "loopzero-ops", Sunset: "2026-11-18"},
		{Image: "loopzero-ops", Reason: "os crons não executam o binário"},
	} {
		if _, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{e}}); err == nil {
			t.Fatalf("isenção incompleta não pode passar: %+v", e)
		}
	}
	ok := Exemption{Image: "loopzero-ops", Reason: "os crons não executam o binário", Sunset: "2026-11-18"}
	out, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{ok}})
	if err != nil {
		t.Fatalf("isenção completa passa: %v", err)
	}
	if out.Exempt != 1 {
		t.Fatalf("a isenção precisa ser CONTADA para que sua existência apareça; got %d", out.Exempt)
	}
}

// O WorkflowTemplate disparado por evento sofre do MESMO defeito, e a primeira versão deste
// gate era cega a ele: varria só CronWorkflow/CronJob.
//
// Medido em 2026-08-18: `loopzero-ci-workflows` usa a mesma imagem impossível em DUAS superfícies
// — os quatro crons e o `workflowtemplate-quality-gate.yaml`, disparado por PR via o sensor do
// Gitea. Um gate que cobrisse só metade reportaria verde com o defeito idêntico um arquivo ao
// lado — a superestimação de cobertura que este ecossistema já mediu em quatro itens.
//
// Os dois compartilham a propriedade que importa: a execução é AUTÔNOMA, então a falha aparece
// longe de quem a causou.
func TestWorkflowTemplateTambemEJulgado(t *testing.T) {
	wt := `kind: WorkflowTemplate
spec:
  templates:
    - container:
        image: "{{ .Values.img.repository }}:{{ .Values.img.tag }}"
`
	root := arvore(t, "c1", wt,
		"img:\n  repository: registry.digitalocean.com/loopzero-system/loopzero-ops\n  tag: v0.1.0\n",
		matrizComLoopZeroAPI)
	if _, err := Check(Config{RepoRoot: root, Exemptions: []Exemption{}}); !errors.Is(err, ErrNoPublicationPath) {
		t.Fatalf("execução disparada por evento reprova igual; err=%v", err)
	}
}
