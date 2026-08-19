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
const DefaultRulesDir = "infra/helm/monitoring/manifests"

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
	RulesDir      string   // default DefaultRulesDir (relative to Root)
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
	rulesDir := cfg.RulesDir
	if rulesDir == "" {
		rulesDir = DefaultRulesDir
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
	dir := filepath.Join(root, rulesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out // missing rules dir → nothing to check
	}
	for _, e := range entries {
		// bash filter: filename contains "prometheusrule" AND ends ".yaml".
		if e.IsDir() || !strings.Contains(e.Name(), "prometheusrule") || !strings.HasSuffix(e.Name(), ".yaml") {
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
	if len(out.Violations) > 0 || len(out.LinksMortos) > 0 {
		out.Pass = false
	}
	return out
}

// orgCanonica e' a identidade deste repositorio, tirada do caminho de modulo dos
// `go.mod` (`github.com/getloopscale/loopzero/...`) e nao do remote git: o remote varia por
// checkout e por alias de ssh, o caminho de modulo nao.
const orgCanonica = "getloopscale"

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
		url := u[1]
		b := blobRe.FindStringSubmatch(url)
		if b == nil {
			out.LinksNaoVerificaveis++
			continue
		}
		org, caminho := b[1], b[3]
		if org != orgCanonica {
			out.LinksMortos = append(out.LinksMortos, LinkMorto{
				Arquivo: arquivo, Alerta: alerta, URL: url,
				Motivo: "aponta para a org " + org + ", e a deste repositorio e' " + orgCanonica,
			})
			continue
		}
		if _, err := os.Stat(filepath.Join(root, caminho)); err != nil {
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
