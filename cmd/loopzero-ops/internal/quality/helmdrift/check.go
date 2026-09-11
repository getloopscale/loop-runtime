// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package helmdrift detects drift between published charts (charts/) and
// bootstrap charts (infra/helm/) by comparing template-file sets. A drift is
// acceptable only if it has an approving ADR (per ADR-0041, gate CH-G16).
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/audit-helm-drift.sh. Pure-Go (os.ReadDir + set diff
// + static registry) — no external CLI (bash used find/comm/awk). Genuine
// pure-logic win per D5.
package helmdrift

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// conceptualPairs maps published-chart → bootstrap-chart when names differ.
var conceptualPairs = map[string]string{
	"loopzero-admission-policies": "admission-policies",
}

// approval is one file's licence to differ between the two charts.
type approval struct {
	ADR    string
	Reason string
}

// approvedDriftKinds registry, keyed "published:bootstrap:KIND".
//
// # Por que a chave e o KIND, e nao o nome do arquivo
//
// Ate o B-110 a chave era o nome do arquivo, e 13 das 21 entradas descreviam algo
// que nao era divergencia: `service.yaml` de um lado contra `svc.yaml` do outro,
// ou um arquivo condicional que NENHUM dos dois lados renderiza sob os values
// padrao. Cada uma dessas entradas era uma linha que alguem lia como "divergencia
// aceita", e um allowlist inflado e um allowlist que ninguem revisa — justamente
// onde as reais moram.
//
// Comparando o que os charts INSTALAM, sobram 11, e o par das politicas de
// admissao cai a ZERO: os dois produzem os mesmos 19 recursos.
//
// Todas as razoes abaixo foram medidas em 2026-08-16 renderizando os tres pares.
var approvedDriftKinds = map[string]approval{
	// --- loopzero-api ---
	"loopzero-api:loopzero-api:ConfigMap":     {ADR: "B-110", Reason: "o chart publicado monta configuracao por ConfigMap; o de bootstrap a entrega por env literal no values do helmfile"},
	"loopzero-api:loopzero-api:NetworkPolicy": {ADR: "B-110", Reason: "politica de rede e do chart publicado; o bootstrap roda em cluster de dev onde a malha nao e segmentada"},
	"loopzero-api:loopzero-api:Secret":        {ADR: "B-110", Reason: "o publicado declara o Secret; no bootstrap ele vem de fora (generate-secrets / ESO), o que e assimetria de PROVENIENCIA e nao de recurso"},
	"loopzero-api:loopzero-api:Certificate":   {ADR: "B-110", Reason: "cert-manager so existe no caminho de bootstrap; o publicado espera TLS terminado na borda"},
	"loopzero-api:loopzero-api:IngressRoute":  {ADR: "B-110", Reason: "IngressRoute e do Traefik do bootstrap; o publicado nao presume ingress"},

	// --- loopzero-cloud ---
	"loopzero-cloud:loopzero-cloud:ConfigMap":           {ADR: "B-110", Reason: "o chart publicado monta a configuracao do painel por ConfigMap; o de bootstrap a entrega como env literal no values do helmfile"},
	"loopzero-cloud:loopzero-cloud:NetworkPolicy":       {ADR: "B-110", Reason: "politica de rede e do chart publicado; o caminho de bootstrap roda em cluster de dev onde a malha nao e segmentada"},
	"loopzero-cloud:loopzero-cloud:Secret":              {ADR: "B-110", Reason: "o publicado declara o Secret do painel; no bootstrap ele e provisionado de fora (generate-secrets / ESO), o que e assimetria de PROVENIENCIA e nao de recurso"},
	"loopzero-cloud:loopzero-cloud:PodDisruptionBudget": {ADR: "B-110", Reason: "PDB pressupoe mais de uma replica; o bootstrap roda replica unica em dev"},
	"loopzero-cloud:loopzero-cloud:ServiceAccount":      {ADR: "B-110", Reason: "o publicado cria a SA; no bootstrap ela vem do helmfile, fora do chart"},
	"loopzero-cloud:loopzero-cloud:IngressRoute":        {ADR: "B-110", Reason: "IngressRoute e do Traefik do caminho de bootstrap; o chart publicado nao presume que exista ingress no cluster de destino"},
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	// Renderer turns a chart into the manifests it installs. Nil means the real
	// `helm template`; a test injects a double so the gate never needs the
	// toolchain it audits in order to run its own suite.
	Renderer Renderer
}

// Drift describes one chart-pair drift.
type Drift struct {
	Pair            string
	Intentional     bool
	ADR             string
	OnlyInPublished []string
	OnlyInBootstrap []string
	// Unapproved lists the drifted files no per-file approval covers. These are
	// what fail the gate — the named files, not the pair.
	Unapproved []string
}

// Outcome captures the result.
type Outcome struct {
	Pass   bool
	Drifts []Drift
	// RenderErrors carries a failed `helm template`, verbatim.
	//
	// A render that failed and a chart that produces nothing are different facts:
	// treating the first as an empty set would report total drift as though it
	// had been measured. So it fails the gate and says why.
	RenderErrors []string
}

// Check audits all loopzero-* published charts + conceptual pairs for drift.
func Check(cfg Config) (*Outcome, error) {
	outcome := &Outcome{Pass: true}
	r := cfg.Renderer
	if r == nil {
		r = helmRenderer{}
	}

	// Same-name overlaps: charts/loopzero-*/ that also exist in infra/helm/.
	chartsDir := filepath.Join(cfg.RepoRoot, "charts")
	entries, err := os.ReadDir(chartsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read charts/: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "loopzero-") {
			continue
		}
		name := e.Name()
		if dirExists(filepath.Join(cfg.RepoRoot, "infra/helm", name)) {
			auditPair(r, cfg.RepoRoot, name, name, outcome)
		}
	}

	// Conceptual pairs (different names).
	// Deterministic order for stable output.
	var pubNames []string
	for p := range conceptualPairs {
		pubNames = append(pubNames, p)
	}
	sort.Strings(pubNames)
	for _, pub := range pubNames {
		auditPair(r, cfg.RepoRoot, pub, conceptualPairs[pub], outcome)
	}

	return outcome, nil
}

// auditPair compares template sets for one published↔bootstrap pair and, if
// they drift, records a Drift (intentional if an ADR approves it; else fails).
func auditPair(r Renderer, repoRoot, publishedName, bootstrapName string, outcome *Outcome) {
	pairKey := publishedName + ":" + bootstrapName
	publishedChart := filepath.Join(repoRoot, "charts", publishedName)
	bootstrapChart := filepath.Join(repoRoot, "infra/helm", bootstrapName)

	if !dirExists(publishedChart) && !dirExists(bootstrapChart) {
		return
	}

	// B-110: compara o que os charts INSTALAM, nao os nomes dos arquivos que os
	// declaram. Um `service.yaml` chamado `svc.yaml` do outro lado era reportado
	// como divergencia sem que nada divergisse — medido, 13 das 21 entradas do
	// allowlist descreviam exatamente isso.
	pubKinds, err := renderedKinds(r, publishedChart)
	if err != nil {
		outcome.RenderErrors = append(outcome.RenderErrors, err.Error())
		outcome.Pass = false
		return
	}
	bootKinds, err := renderedKinds(r, bootstrapChart)
	if err != nil {
		outcome.RenderErrors = append(outcome.RenderErrors, err.Error())
		outcome.Pass = false
		return
	}
	onlyPub, onlyBoot := diffKinds(pubKinds, bootKinds)

	if len(onlyPub) == 0 && len(onlyBoot) == 0 {
		return // os dois lados instalam os mesmos tipos de recurso
	}

	d := Drift{
		Pair:            pairKey,
		OnlyInPublished: onlyPub,
		OnlyInBootstrap: onlyBoot,
	}

	// Cada kind divergente carrega a propria aprovacao. Um kind sem aprovacao
	// reprova o par — o par ser "conhecido por divergir" nao e licenca para o
	// proximo recurso divergir em silencio.
	for _, k := range append(append([]string{}, onlyPub...), onlyBoot...) {
		if a, ok := approvedDriftKinds[pairKey+":"+k]; ok {
			d.ADR = a.ADR
			continue
		}
		d.Unapproved = append(d.Unapproved, k)
	}
	d.Intentional = len(d.Unapproved) == 0
	if !d.Intentional {
		outcome.Pass = false
	}
	outcome.Drifts = append(outcome.Drifts, d)
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
