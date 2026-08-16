// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package prodimagedigests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanChartValues closes the gap this gate DOCUMENTED and never checked.
//
// The template scanner deliberately treats `{{ include "loopzero-common.image" . }}`
// as clean, and its own comment says why: *"the violation surfaces at the
// values.yaml level when digest is missing"*. That sentence names, exactly, a
// check that was never written.
//
// Measured 2026-08-16: all SEVEN published charts use the helper, none declares
// `image.digest`, and all seven declare `tag: dev`. The helper prefers digest and
// falls back to the tag, so the effective default reference of every published
// chart is a mutable `loopzero-x:dev` — and the gate reported PASS on 66 files while
// the only three findings were Helm test pods.
//
// The chart does the right thing in the template layer and the wrong thing in the
// values layer, and the gate only looked at one of them.
func TestScanChartValues(t *testing.T) {
	cases := []struct {
		name       string
		values     string
		usesHelper bool
		wantHit    bool
		wantIn     string
	}{
		{
			name:       "helper sem digest e a forma que o gate nao via",
			values:     "image:\n  repository: loopzero-api\n  tag: dev\n",
			usesHelper: true, wantHit: true, wantIn: "digest",
		},
		{
			name:       "digest declarado e imutavel, entao passa",
			values:     "image:\n  repository: loopzero-api\n  digest: sha256:abc123\n",
			usesHelper: true, wantHit: false,
		},
		{
			name: "digest vazio nao conta como declarado — o helper cai na tag",
			// `.Values.image.digest | default ""` trata vazio como ausente, e
			// aceitar a chave presente-porem-vazia seria aceitar a aparencia da
			// correcao no lugar dela.
			values:     "image:\n  repository: loopzero-api\n  digest: \"\"\n  tag: dev\n",
			usesHelper: true, wantHit: true, wantIn: "digest",
		},
		{
			name: "chart que NAO usa o helper nao e julgado aqui",
			// Quem escreve `image:` literal ja e coberto pelo varredor de
			// templates. Julgar duas vezes produziria dois achados para um
			// defeito, e o leitor aprende a somar errado.
			values:     "image:\n  repository: qualquer\n  tag: v1\n",
			usesHelper: false, wantHit: false,
		},
		{
			name:       "sem bloco image nenhum nao e violacao",
			values:     "replicaCount: 2\n",
			usesHelper: true, wantHit: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			chart := filepath.Join(root, "charts", "loopzero-fixture")
			if err := os.MkdirAll(filepath.Join(chart, "templates"), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(filepath.Join(chart, "values.yaml"), []byte(tc.values), 0o600); err != nil {
				t.Fatalf("WriteFile values: %v", err)
			}
			tmpl := "spec:\n  containers:\n    - image: nginx@sha256:deadbeef\n"
			if tc.usesHelper {
				tmpl = "spec:\n  containers:\n    - image: {{ include \"loopzero-common.image\" . }}\n"
			}
			if err := os.WriteFile(filepath.Join(chart, "templates", "deployment.yaml"), []byte(tmpl), 0o600); err != nil {
				t.Fatalf("WriteFile template: %v", err)
			}

			var out Outcome
			if err := scanChartValues(filepath.Join(root, "charts"), &out); err != nil {
				t.Fatalf("scanChartValues: %v", err)
			}

			if tc.wantHit && len(out.Violations) == 0 {
				t.Fatalf("esperava violação, veio nenhuma")
			}
			if !tc.wantHit && len(out.Violations) > 0 {
				t.Fatalf("não esperava violação, vieram %d: %v", len(out.Violations), out.Violations)
			}
			if tc.wantHit && tc.wantIn != "" && !strings.Contains(strings.Join(out.Violations, " "), tc.wantIn) {
				t.Errorf("a violação não nomeia a causa (%q): %v", tc.wantIn, out.Violations)
			}
		})
	}
}

// TestScanChartValues_DiretorioAusenteNaoEhErro keeps the gate usable in a
// checkout that has no `charts/` — same tolerance the path walker already has.
func TestScanChartValues_DiretorioAusenteNaoEhErro(t *testing.T) {
	var out Outcome
	if err := scanChartValues(filepath.Join(t.TempDir(), "nao-existe"), &out); err != nil {
		t.Fatalf("diretório ausente virou erro: %v", err)
	}
	if len(out.Violations) != 0 {
		t.Errorf("diretório ausente produziu violações: %v", out.Violations)
	}
}
