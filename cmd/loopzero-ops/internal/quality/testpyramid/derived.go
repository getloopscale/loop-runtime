// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testpyramid

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testinventory"
)

// # Por que a pirâmide passou a ser DERIVADA, e por que o veredito é catraca
//
// O gate lia três percentuais escritos em `infra/quality/test-inventory.json`. Medido
// em 2026-08-17: o arquivo listava 710 arquivos, **335 não existiam**, e **649**
// arquivos `_test.go` do disco não estavam nele — ele descrevia 375 de 988, **62%
// cego**, e a pirâmide de 99,4% unit que declarava vinha dessa amostra, que exclui a
// suíte `infra/tests/e2e` inteira.
//
// Derivada da árvore, a pirâmide real é outra:
//
//	              declarado    derivado
//	arquivos            710         988
//	unit              99,4%       73,5%
//	integração         0,4%       15,7%
//	e2e                0,1%       10,8%
//
// # E aqui está a decisão que a medição forçou
//
// O alvo do ADR D7 é `e2e ≤ 5%`. Ele foi escolhido quando o arquivo afirmava 0,1% —
// isto é, **calibrado contra ficção**. O número real é 10,8%, e 107 arquivos e2e num
// produto cujo objeto É implantar não é obviamente um defeito.
//
// Fazer o gate reprovar hoje contra o alvo do ADR criaria uma trava que o repositório
// não consegue satisfazer, e uma trava que não pode ficar verde é desligada na
// primeira semana — o mesmo raciocínio que recusou `HARD` no `B-127` e a enforcement
// total no `B-119`.
//
// Então o veredito é **catraca**: a linha de base congela o que foi MEDIDO, e o gate
// reprova quando um índice piora. A distância até o alvo do ADR é **reportada**, e a
// pergunta "10,8% de e2e é certo?" virou item próprio — ela é do sponsor, não minha.
//
// Isto não é "ajustar o número até passar": é separar *não regredir* (verificável
// hoje) de *alcançar o alvo* (decisão que ninguém tomou contra dados reais).

// Baseline is the frozen measured pyramid.
type Baseline struct {
	MeasuredAt          string  `json:"measured_at"`
	TotalFiles          int     `json:"total_files"`
	RatioUnitPct        float64 `json:"ratio_unit_pct"`
	RatioIntegrationPct float64 `json:"ratio_integration_pct"`
	RatioE2EPct         float64 `json:"ratio_e2e_pct"`
	Note                string  `json:"note"`
}

// ErrBaselineMissing means the frozen baseline is absent.
//
// Ausência é FALHA, nunca skip: sem linha de base a catraca não compara nada, e um
// gate que não compara nada reportando verde é o defeito que este item removeu.
var ErrBaselineMissing = errors.New("linha de base da pirâmide ausente — sem ela a catraca não compara nada, e um veredito sobre nada não é um veredito (rode com --write)")

// ErrRegressed means a ratio moved the wrong way past the baseline.
var ErrRegressed = errors.New("a pirâmide regrediu em relação à linha de base medida")

// tolerancePct absorve o ruído de um arquivo entrando ou saindo. Com ~988 arquivos,
// um arquivo vale ~0,1 ponto; 0,5 dá folga para meia dúzia sem deixar passar uma
// mudança de forma.
const tolerancePct = 0.5

// DerivedOutcome reports both verdicts: what blocks and what is merely reported.
type DerivedOutcome struct {
	Deferred bool
	Total    int
	UnitPct  float64
	IntegPct float64
	E2EPct   float64
	// Regressions bloqueiam.
	Regressions []string
	// AspirationGaps são a distância até o alvo do ADR — reportados, não bloqueantes,
	// porque o alvo nunca foi medido contra a árvore real.
	AspirationGaps []string
}

// Pass reports whether nothing regressed.
func (o DerivedOutcome) Pass() bool { return len(o.Regressions) == 0 }

// LoadBaseline reads the frozen measurement.
func LoadBaseline(path string) (Baseline, error) {
	var b Baseline
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return b, fmt.Errorf("%w: %s", ErrBaselineMissing, path)
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return b, nil
}

// WriteBaseline freezes the current measurement.
func WriteBaseline(path string, inv testinventory.Inventory, measuredAt, note string) error {
	u, i, e := inv.Ratios()
	b := Baseline{
		MeasuredAt:          measuredAt,
		TotalFiles:          inv.Total(),
		RatioUnitPct:        round1(u),
		RatioIntegrationPct: round1(i),
		RatioE2EPct:         round1(e),
		Note:                note,
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// CheckDerived derives the pyramid from the tree and ratchets it against the frozen
// baseline, reporting the distance to the ADR aspiration separately.
func CheckDerived(repoRoot, baselinePath string, thresholds Thresholds) (DerivedOutcome, error) {
	var out DerivedOutcome

	inv, err := testinventory.Derive(repoRoot)
	if err != nil {
		return out, err
	}
	base, err := LoadBaseline(baselinePath)
	if err != nil {
		return out, err
	}

	u, i, e := inv.Ratios()
	out.Total, out.UnitPct, out.IntegPct, out.E2EPct = inv.Total(), round1(u), round1(i), round1(e)

	// Catraca: unit só pode SUBIR, integração e e2e só podem DESCER.
	if out.UnitPct < base.RatioUnitPct-tolerancePct {
		out.Regressions = append(out.Regressions, fmt.Sprintf(
			"unit caiu de %.1f%% (linha de base) para %.1f%%", base.RatioUnitPct, out.UnitPct))
	}
	if out.IntegPct > base.RatioIntegrationPct+tolerancePct {
		out.Regressions = append(out.Regressions, fmt.Sprintf(
			"integração subiu de %.1f%% para %.1f%%", base.RatioIntegrationPct, out.IntegPct))
	}
	if out.E2EPct > base.RatioE2EPct+tolerancePct {
		out.Regressions = append(out.Regressions, fmt.Sprintf(
			"e2e subiu de %.1f%% para %.1f%%", base.RatioE2EPct, out.E2EPct))
	}

	// A distância até o alvo do ADR — reportada, nunca bloqueante enquanto o alvo não
	// tiver sido medido contra a árvore real.
	if out.UnitPct < thresholds.UnitMinPct {
		out.AspirationGaps = append(out.AspirationGaps, fmt.Sprintf(
			"unit %.1f%% < alvo %.1f%% (ADR D7)", out.UnitPct, thresholds.UnitMinPct))
	}
	if out.IntegPct > thresholds.IntegrationMaxPct {
		out.AspirationGaps = append(out.AspirationGaps, fmt.Sprintf(
			"integração %.1f%% > alvo %.1f%% (ADR D7)", out.IntegPct, thresholds.IntegrationMaxPct))
	}
	if out.E2EPct > thresholds.E2EMaxPct {
		out.AspirationGaps = append(out.AspirationGaps, fmt.Sprintf(
			"e2e %.1f%% > alvo %.1f%% (ADR D7)", out.E2EPct, thresholds.E2EMaxPct))
	}

	if !out.Pass() {
		return out, ErrRegressed
	}
	return out, nil
}
