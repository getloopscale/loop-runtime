// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package alertrunbooks implements the T5.8 gate: every PrometheusRule alert
// must carry a runbook_url (or legacy runbook:) annotation so Alertmanager can
// link operators to recovery docs.
//
// Plan T5.3 of docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/check_alert_runbooks.sh (which embedded a python3
// heredoc). Pure-Go (file read + regexp) — no external CLI. (The "helm" in the
// rules-dir path is a directory name, not a helm invocation.)
package alertrunbooks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultRulesDir holds the PrometheusRule manifests, relative to repo root.
//
// Mantido para compatibilidade: e' o diretorio historico e segue sendo o primeiro da
// lista. Quem varre e' `DefaultRulesDirs`.
const DefaultRulesDir = "infra/helm/monitoring/manifests"

// DefaultRulesDirs sao TODOS os diretorios onde alertas de fato vivem (B-164).
//
// Ate' 2026-08-19 o gate varria apenas o primeiro. Medido nessa data: a arvore tem **233**
// alertas e ele olhava **110** — 47%. E os dois de `admission-policies` sao justamente onde
// vivem 22 das 23 anotacoes `runbook_url`, entao a metade nao varrida concentrava quase
// toda a evidencia que este gate existe para conferir.
//
// `charts/` nao e' artefato de bootstrap: sao os charts OCI publicados que o ArgoCD entrega
// aos tenants.
var DefaultRulesDirs = []string{
	"infra/helm/monitoring/manifests",
	"charts/loopzero-monitoring/templates",
	"infra/helm/admission-policies/templates",
	"charts/loopzero-admission-policies/templates",
}

// runbookLookahead is the per-alert-block window scanned for a runbook
// annotation (matches the bash `blk[:1500]`).
const runbookLookahead = 1500

var (
	alertSplitRe = regexp.MustCompile(`\n\s*- alert:`)
	alertNameRe  = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)`)
)

// DefaultExceptionList are pre-T5.8-baseline alerts permitted without a runbook.
var DefaultExceptionList = []string{
	"PulumiOrphanStackDetected", "GiteaAuthLatencyHigh", "ArgoWorkflowQuotaExceeded",
	"HPAAtMaxReplicas", "PVCPendingSustained", "TenantReconcileBacklog",
	"BuildFailureRateHigh", "LoopZeroQueueSlow", "LoopZeroDeployFailureRate",
	"LoopZeroLockContention", "LoopZeroSyncSlow", "LoopZeroBuildQueueSlow",
	"LoopZeroTempoStorageHigh", "LoopZeroCollectorExportErrors",
}

// Config drives the gate.
type Config struct {
	Root          string
	RulesDir      string   // um unico diretorio (compat); vazio usa RulesDirs
	RulesDirs     []string // default DefaultRulesDirs (relativos a Root)
	ExceptionList []string // default DefaultExceptionList
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Violations []string // "<file>:<alertName>"
	// LinksMortos sao anotacoes de runbook que APONTAM PARA O NADA e que o gate
	// consegue provar sem rede: caminho deste repositorio que nao existe em disco,
	// ou URL do GitHub numa org diferente da identidade canonica do repo.
	//
	// Medido em 2026-08-19 (B-161): das 7 URLs da arvore, SEIS nao levavam a nada, e
	// todas as sete contavam como satisfeitas. Um alerta que dispara as 3 da manha
	// mandando o operador para um 404 e' pior que um alerta sem link — o link promete.
	LinksMortos []LinkMorto
	// LinksNaoVerificaveis conta as URLs externas. Um gate hermetico nao pode dizer
	// se `https://docs.loopzero.example/runbooks/x` esta viva; afirmar qualquer um dos dois
	// seria inventar. Contar e' honesto, e o numero e' o que faz alguem conferir.
	LinksNaoVerificaveis int
	// DirsVarridos conta quantos diretorios de regras existiram de fato. Zero e' falha:
	// e' a diferenca entre "nenhum alerta sem runbook" e "nenhum alerta olhado".
	DirsVarridos int
	// LinksConferidos conta TODAS as anotacoes examinadas, inclusive as saudaveis.
	//
	// Existe porque a varredura de populacao somava mortos + nao-verificaveis e tratava
	// zero como "nao alcancou nada" — quando zero e' tambem o resultado de **todos os
	// links estarem bons**. Confundir as duas coisas e' a forma exata que este gate existe
	// para combater, e ela estava no teste que devia prova-lo.
	LinksConferidos int
}

// LinkMorto e' uma anotacao de runbook provada inutil sem sair da maquina.
type LinkMorto struct {
	Arquivo string
	Alerta  string
	URL     string
	Motivo  string
}

// Check scans PrometheusRule YAMLs for alerts missing a runbook annotation.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	dirs := cfg.RulesDirs
	if len(dirs) == 0 {
		if cfg.RulesDir != "" {
			dirs = []string{cfg.RulesDir}
		} else {
			dirs = DefaultRulesDirs
		}
	}
	exc := cfg.ExceptionList
	if exc == nil {
		exc = DefaultExceptionList
	}
	excepted := make(map[string]struct{}, len(exc))
	for _, e := range exc {
		excepted[e] = struct{}{}
	}

	out := &Outcome{Pass: true}
	for _, rulesDir := range dirs {
		dir := filepath.Join(root, rulesDir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Diretorio ausente e' tolerado — as arvores de teste tem so' um deles — mas
			// NAO em silencio: `DirsVarridos` conta quantos existiram, e zero e' falha.
			continue
		}
		out.DirsVarridos++
		varrerDiretorio(root, dir, entries, excepted, out)
	}
	if out.DirsVarridos == 0 {
		out.Pass = false
		out.Violations = append(out.Violations,
			"NENHUM diretorio de regras encontrado — varredura que nao alcanca nada nao e' verde")
	}
	if len(out.Violations) > 0 || len(out.LinksMortos) > 0 {
		out.Pass = false
	}
	return out
}

func varrerDiretorio(root, dir string, entries []os.DirEntry, excepted map[string]struct{}, out *Outcome) {
	for _, e := range entries {
		// O filtro historico exigia "prometheusrule" no nome — herdado do script bash que
		// este gate substituiu. Medido em 2026-08-19 (B-164): os alertas de
		// `admission-policies` vivem em `*-alerts.yaml` (`sigstore-storage-alerts.yaml`,
		// `tenant-namespace-alerts.yaml`, …), entao ampliar o escopo de diretorios nao
		// bastava: o filtro de NOME continuava descartando 37 alertas e 22 das 23
		// anotacoes `runbook_url` da arvore.
		//
		// Aceita as duas formas. O que decide e' o arquivo declarar alerta, e ambos os
		// nomes sao convencao viva do repositorio.
		nome := e.Name()
		relevante := strings.Contains(nome, "prometheusrule") || strings.Contains(nome, "alert")
		if e.IsDir() || !relevante || !strings.HasSuffix(nome, ".yaml") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		for _, name := range missingRunbookAlerts(string(data), excepted) {
			out.Violations = append(out.Violations, e.Name()+":"+name)
		}
		conferirLinks(root, e.Name(), string(data), out)
	}
}

// repoCanonico e' o NOME deste repositorio, tirado do caminho de modulo dos `go.mod`
// (`github.com/getloopscale/loopzero/...`) e nao do remote git — o remote varia por checkout e
// por alias de ssh, o caminho de modulo nao.
//
// A comparacao e' pelo REPO e nao pela ORG, e isso e' correcao de um erro que este mesmo
// arquivo carregou por algumas horas em 2026-08-19. A versao anterior tratava org
// diferente de `getloopscale` como link morto "por construcao". Medido depois, com credencial
// que enxerga: **`getloopscale/loopzero` existe e tem os arquivos**, tanto quanto
// `getloopscale/loopzero`. Sao dois caminhos para o mesmo repositorio, e a regra teria produzido
// falso positivo assim que o escopo do gate crescesse (B-164).
//
// Como o erro entrou, porque a forma se repete: eu extrai as URLs truncadas, INVENTEI os
// nomes de arquivo que elas pareciam ter (`sigstore-key-rotation.md`,
// `tuf-root-rotation.md`), sondei ESSES nomes pela API e recebi Not Found. Os nomes reais
// eram outros (`sigstore-storage-model.md`, `tuf-root-ceremony.md`) e resolvem. Sondar o
// que eu supus em vez do que o arquivo diz e' fabricacao de evidencia com passo extra.
const repoCanonico = "loopzero"

var runbookURLRe = regexp.MustCompile(`runbook_url:\s*"([^"]+)"`)
var blobRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/blob/[^/]+/(.+)$`)

// conferirLinks prova, SEM REDE, quais anotacoes de runbook nao levam a nada.
//
// So' afirma o que da' para afirmar hermeticamente:
//   - URL de blob do GitHub nesta org  -> confere o caminho em disco
//   - URL de blob do GitHub noutra org -> link morto por construcao (quem tem acesso
//     a este repositorio nao alcanca o outro; medido no B-161 com `gh api`)
//   - qualquer outra                   -> nao-verificavel, e fica CONTADA
func conferirLinks(root, arquivo, conteudo string, out *Outcome) {
	blocos := alertSplitRe.Split(conteudo, -1)
	for _, blk := range blocos[1:] {
		m := alertNameRe.FindStringSubmatch(blk)
		if m == nil {
			continue
		}
		alerta := m[1]
		corpo := blk
		if len(corpo) > runbookLookahead {
			corpo = corpo[:runbookLookahead]
		}
		u := runbookURLRe.FindStringSubmatch(corpo)
		if u == nil {
			continue
		}
		out.LinksConferidos++
		url := u[1]
		b := blobRe.FindStringSubmatch(url)
		if b == nil {
			out.LinksNaoVerificaveis++
			continue
		}
		repo, caminho := b[2], b[3]
		if repo != repoCanonico {
			// Outro repositorio: o caminho nao existe nesta arvore por definicao, e
			// afirmar que ele nao existe NA DE LA' exigiria rede. Contado, nao acusado.
			out.LinksNaoVerificaveis++
			continue
		}
		// O caminho vem do CONTEUDO de um YAML — entrada nao confiavel. Sem tratamento,
		// um `runbook_url` com `../../..` faria este `Stat` perguntar ao disco por um
		// arquivo FORA da arvore, e um que existisse responderia "runbook ok". O gosec
		// apontou (G703) e estava certo.
		//
		// `Clean("/"+caminho)` resolve os `..` contra a RAIZ e nao contra `root`, entao o
		// resultado sempre comeca em `/` e sempre cai dentro de `root` depois do Join —
		// a travessia vira um caminho relativo comum, que simplesmente nao existe.
		//
		// Escrevi tambem uma checagem de contencao aqui e a REMOVI: depois do `Clean` ela
		// nao tinha como disparar. Guarda inalcancavel e' codigo morto que se le' como
		// protecao, e este e' o repositorio errado para deixar uma dessas. Quem prova a
		// neutralizacao e' `TestTravessiaNaoAlcancaArquivoForaDaRaiz`, que poe um arquivo
		// REAL fora e exige que ele nao seja aceito.
		alvo := filepath.Join(root, filepath.Clean("/"+caminho))
		if _, err := os.Stat(alvo); err != nil {
			out.LinksMortos = append(out.LinksMortos, LinkMorto{
				Arquivo: arquivo, Alerta: alerta, URL: url,
				Motivo: caminho + " nao existe neste repositorio",
			})
		}
	}
}

// missingRunbookAlerts returns alert names in content lacking a runbook
// annotation and not in the exception set.
func missingRunbookAlerts(content string, excepted map[string]struct{}) []string {
	var missing []string
	blocks := alertSplitRe.Split(content, -1)
	for _, blk := range blocks[1:] { // [0] is the pre-first-alert preamble
		m := alertNameRe.FindStringSubmatch(blk)
		if m == nil {
			continue
		}
		name := m[1]
		body := blk
		if len(body) > runbookLookahead {
			body = body[:runbookLookahead]
		}
		if strings.Contains(body, "runbook_url") || strings.Contains(body, "runbook:") {
			continue
		}
		if _, ok := excepted[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}
