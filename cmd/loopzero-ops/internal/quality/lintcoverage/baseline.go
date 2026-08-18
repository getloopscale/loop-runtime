// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrBaselineMissing means the frozen counts are absent.
//
// Ausente é FALHA, nunca conjunto vazio implícito: sem o congelado não há como saber o que é
// regressão, e tratar isso como "nenhuma regressão" é o verde-sobre-nada de sempre.
var ErrBaselineMissing = errors.New("linha de base ausente — sem o congelado não há catraca")

// BaselineRelPath is where the frozen counts live.
const BaselineRelPath = "infra/quality/lint-baseline.json"

// Baseline holds the frozen per-module counts, one map per dimension.
//
// Por MÓDULO, e não um total: um número único deixaria `cmd/loopzero-ops` melhorar 10 e `api`
// piorar 10 sem ninguém ver. É o que o baseline de 200 do `lint:strict` fazia — e ele estava
// 200 fora da realidade quando foi medido.
type Baseline struct {
	Note  string         `json:"note,omitempty"`
	Lint  map[string]int `json:"lint"`
	Gofmt map[string]int `json:"gofmt"`
}

// Regression is one module that got worse.
type Regression struct {
	Module    string
	Dimension string
	Was, Now  int
}

// Improvement is one module that got better — reported so the number can actually come down.
type Improvement struct {
	Module   string
	Was, Now int
}

// Verdict is the outcome of comparing one dimension.
type Verdict struct {
	Regressions  []Regression
	Improvements []Improvement
	// Undeclared lists modules with no frozen entry. Um módulo novo entra DECLARADO, senão
	// ele nasce com achados invisíveis — o defeito que esta fatia conserta.
	Undeclared []string
}

const baselineNote = "Contagens congeladas por MODULO de golangci-lint e gofmt, lidas por " +
	"`loopzero-ops quality lint-coverage`. A catraca recusa REGRESSAO: um modulo em 0 que ganhe um " +
	"achado reprova, mesmo com centenas congeladas ao lado. Melhorar passa e aparece na saida, " +
	"para que alguem recongele com `--write`. Um modulo sem entrada aqui e reportado — ele entra " +
	"declarado, nunca em silencio. Ver B-121, B-083 e B-148."

// LoadBaseline reads the frozen counts.
func LoadBaseline(path string) (Baseline, error) {
	var b Baseline
	body, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return b, fmt.Errorf("%w: %s (gere com `loopzero-ops quality lint-coverage --write`)", ErrBaselineMissing, path)
	}
	if err != nil {
		return b, fmt.Errorf("lendo %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return b, fmt.Errorf("linha de base ilegível em %s: %w", path, err)
	}
	if b.Lint == nil {
		b.Lint = map[string]int{}
	}
	if b.Gofmt == nil {
		b.Gofmt = map[string]int{}
	}
	return b, nil
}

// Compare measures the observed counts against one frozen dimension.
func (b Baseline) Compare(observed, frozen map[string]int, dimension string) Verdict {
	var v Verdict
	mods := make([]string, 0, len(observed))
	for m := range observed {
		mods = append(mods, m)
	}
	sort.Strings(mods)

	for _, m := range mods {
		now := observed[m]
		was, ok := frozen[m]
		if !ok {
			v.Undeclared = append(v.Undeclared, m)
			continue
		}
		switch {
		case now > was:
			v.Regressions = append(v.Regressions, Regression{Module: m, Dimension: dimension, Was: was, Now: now})
		case now < was:
			v.Improvements = append(v.Improvements, Improvement{Module: m, Was: was, Now: now})
		}
	}
	return v
}

// WriteBaseline freezes the observed counts.
func WriteBaseline(path string, lint, gofmtCounts map[string]int) error {
	b := Baseline{Note: baselineNote, Lint: lint, Gofmt: gofmtCounts}
	if b.Lint == nil {
		b.Lint = map[string]int{}
	}
	if b.Gofmt == nil {
		b.Gofmt = map[string]int{}
	}
	// MarshalIndent sobre map ordena as chaves, então a saída é determinística — e um diff
	// cuja ordem varia é ruído que ninguém revisa.
	body, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("serializando linha de base: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}
