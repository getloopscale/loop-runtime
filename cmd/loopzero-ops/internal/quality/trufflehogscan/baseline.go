// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package trufflehogscan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrBaselineMissing means the adjudicated baseline is absent.
//
// Ausente é FALHA, nunca conjunto vazio implícito (EC-2): ler a ausência como "nada
// adjudicado, nada novo" transformaria um `rm` em bypass do gate.
var ErrBaselineMissing = errors.New("linha de base ausente — não há verde possível sem o conjunto adjudicado")

// BaselineRelPath is where the adjudicated set lives, relative to the repo root.
const BaselineRelPath = "infra/quality/trufflehog-baseline.json"

// defaultBaselineRel is the package-internal alias used by Check and Generate.
const defaultBaselineRel = BaselineRelPath

// Entry is one adjudicated candidate. It carries NO secret material — see Finding.ID.
type Entry struct {
	ID       string `json:"id"`
	Detector string `json:"detector"`
	// File is CONTEXT, not identity. A identidade é (detector, bruto): o mesmo texto
	// em cinco arquivos é UM candidato, porque a adjudicação diz "isto não é segredo"
	// e essa natureza não muda de arquivo para arquivo. O nome do campo diz que é o
	// primeiro lugar onde foi visto — chamá-lo de `file` sugeriria o único.
	File string `json:"first_seen_in"`
}

// baselineNote travels inside the artifact because whoever finds this file next will
// not have this commit's context.
const baselineNote = "Conjunto de candidatos ADJUDICADOS do trufflehog. " +
	"Um candidato fora daqui REPROVA o gate até alguém regravar com `loopzero-ops quality trufflehog-scan --write` — " +
	"e é o diff dessa regravação que é a adjudicação. " +
	"`--write` RECUSA enquanto houver achado vivo: esta lista adjudica \"isto não é segredo\", nunca \"este segredo pode viver aqui\". " +
	"`id` é sha256(detector+bruto) truncado — nenhum material de segredo mora neste arquivo. Ver B-143."

// Baseline is the adjudicated candidate set.
type Baseline struct {
	Note    string  `json:"note,omitempty"`
	Entries []Entry `json:"entries"`
}

// LoadBaseline reads the versioned baseline.
func LoadBaseline(path string) (Baseline, error) {
	var b Baseline
	body, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return b, fmt.Errorf("%w: %s (gere com `loopzero-ops quality trufflehog-scan --write`)", ErrBaselineMissing, path)
	}
	if err != nil {
		return b, fmt.Errorf("lendo %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return b, fmt.Errorf("linha de base ilegível em %s: %w", path, err)
	}
	return b, nil
}

// Unadjudicated returns the findings absent from the baseline.
func (b Baseline) Unadjudicated(found []Finding) []Finding {
	known := b.index()
	seen := map[string]bool{}
	var out []Finding
	for _, f := range found {
		if known[f.ID()] || seen[f.ID()] {
			continue
		}
		seen[f.ID()] = true
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Stale returns baseline entries with no matching finding.
//
// Higiene da catraca — REPORTA, nunca reprova: um segredo removido da árvore não pode
// virar motivo para o gate travar. Um conjunto que só cresce é um conjunto que ninguém
// poda, e é assim que uma linha de base vira carimbo.
func (b Baseline) Stale(found []Finding) []Entry {
	live := map[string]bool{}
	for _, f := range found {
		live[f.ID()] = true
	}
	var out []Entry
	for _, e := range b.Entries {
		if !live[e.ID] {
			out = append(out, e)
		}
	}
	return out
}

func (b Baseline) index() map[string]bool {
	m := make(map[string]bool, len(b.Entries))
	for _, e := range b.Entries {
		m[e.ID] = true
	}
	return m
}

// WriteBaseline records the adjudicated set.
//
// Recusa enquanto houver achado VIVO (EC-1). Sem isso, regravar seria o caminho para
// destravar um push barrado por uma credencial que funciona — e o mecanismo desenhado
// para impedir o B-142 viraria a maneira de repetí-lo, com aparência de processo.
func WriteBaseline(path string, found []Finding) error {
	for _, f := range found {
		if f.Live {
			return fmt.Errorf("%w: %s — a linha de base adjudica \"isto não é segredo\", nunca \"este segredo pode viver aqui\"",
				ErrLiveSecret, f)
		}
	}

	seen := map[string]bool{}
	b := Baseline{Note: baselineNote, Entries: []Entry{}}
	for _, f := range found {
		if seen[f.ID()] {
			continue
		}
		seen[f.ID()] = true
		b.Entries = append(b.Entries, Entry{ID: f.ID(), Detector: f.Detector, File: f.File})
	}
	sort.Slice(b.Entries, func(i, j int) bool {
		if b.Entries[i].File != b.Entries[j].File {
			return b.Entries[i].File < b.Entries[j].File
		}
		if b.Entries[i].Detector != b.Entries[j].Detector {
			return b.Entries[i].Detector < b.Entries[j].Detector
		}
		return b.Entries[i].ID < b.Entries[j].ID
	})

	body, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("serializando linha de base: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}
