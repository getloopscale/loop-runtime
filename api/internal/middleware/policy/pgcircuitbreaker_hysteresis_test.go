// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// B-054: o `api` roda DOIS portoes sobre o mesmo pool. O que de fato barra requisicao e' este
// middleware, e ele decide por percentual instantaneo, sem histerese. O outro —
// `buildplane/admission.PGPoolBreaker` — tem histerese (high/low water), documenta a oscilacao
// como a razao de existir... e nunca e' consultado: `Allow()` nao tem chamador de producao.
//
// Ou seja: o instrumento que resolveu a oscilacao esta desligado, e o que esta ligado tem o
// problema. Este teste mede o problema no caminho LIGADO.
//
// Por que oscilar importa: perto da linha de corte, o pool sobe e desce a cada requisicao que
// devolve conexao. Sem histerese o portao abre e fecha junto, e o cliente ve 503 alternando com
// 200 — o pior dos dois mundos, porque nem protege de forma estavel nem serve de forma estavel.
func TestBreakerNaoOscilaEmTornoDaLinhaDeCorte(t *testing.T) {
	// O pool oscila um ponto em torno do limite: 90, 89, 90, 89...
	amostras := []int32{90, 89, 90, 89, 90, 89}
	var i int
	gate := &fakeGate{tripped: false, amostras: amostras, idx: &i}

	cb := NewPGCircuitBreaker(PGCircuitBreakerConfig{Gate: gate})
	h := cb.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var transicoes int
	anterior := -1
	for range amostras {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", nil))
		atual := rec.Code
		if anterior != -1 && atual != anterior {
			transicoes++
		}
		anterior = atual
	}

	// Com histerese, o portao dispara em 90 e so' rearma abaixo de 75: UMA transicao no maximo.
	// Sem ela, ele alterna a cada amostra — cinco.
	if transicoes > 1 {
		t.Errorf("o portao alternou %d vez(es) sobre um pool que oscila UM ponto na linha de corte; "+
			"com histerese seria no maximo 1. O cliente ve 503 e 200 alternando, o que nao protege "+
			"nem serve de forma estavel (B-054)", transicoes)
	}
}

// fakeGate reproduz a histerese high/low water do `admission.PGPoolBreaker` sobre uma sequencia
// de amostras, para que este teste meça o COMPORTAMENTO do portao e nao a implementacao dele.
type fakeGate struct {
	tripped  bool
	amostras []int32
	idx      *int
}

func (g *fakeGate) Allow() bool {
	pct := g.amostras[*g.idx%len(g.amostras)]
	*g.idx++
	if g.tripped {
		if pct < 75 {
			g.tripped = false
		}
	} else if pct >= 90 {
		g.tripped = true
	}
	return !g.tripped
}
