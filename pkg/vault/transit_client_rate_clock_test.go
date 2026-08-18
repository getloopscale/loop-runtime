// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"errors"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"
)

// A capacidade de rajada deve ser afirmada sem depender do relógio de parede.
//
// B-014, medido 2026-08-10: `TestTransitClient_RateLimitBurst` reprovou com
// `allowed: got 11 want 10`. Ele faz 15 chamadas HTTP reais e conta quantas passam; com
// `RateLimitPerSec=10` o balde reabastece a 10/s, então bastam as 15 chamadas levarem mais
// de 100ms para um token extra entrar e a contagem observada mudar. O teste reprovava por
// tempo, não por comportamento — e `rules/testing.md § 3` chama isso de bug.
//
// Aqui o tempo é um parâmetro. O limitador é o de produção; o que muda é que ele não
// consulta mais o relógio da máquina.
func TestAcquireRateToken_BurstIsExactAtAFrozenInstant(t *testing.T) {
	c := newRateLimitedClientAtFixedTime(t, 10)

	var allowed, limited int
	for i := 0; i < 15; i++ {
		err := c.acquireRateToken("tn-burst")
		switch {
		case err == nil:
			allowed++
		case errors.Is(err, ErrRateLimited):
			limited++
		default:
			t.Fatalf("erro inesperado: %v", err)
		}
	}
	if allowed != 10 {
		t.Errorf("permitidos: %d, esperava 10 (capacidade da rajada)", allowed)
	}
	if limited != 5 {
		t.Errorf("recusados: %d, esperava 5 (15 menos a rajada)", limited)
	}
}

// O reabastecimento também é comportamento, e também deve ser afirmado sem esperar de
// verdade: avançar o relógio um décimo de segundo devolve exatamente um token a 10/s.
func TestAcquireRateToken_RefillIsDrivenByTheInjectedClock(t *testing.T) {
	c := newRateLimitedClientAtFixedTime(t, 10)

	for i := 0; i < 10; i++ {
		if err := c.acquireRateToken("tn-refill"); err != nil {
			t.Fatalf("a rajada inicial deveria passar, falhou em %d: %v", i, err)
		}
	}
	if err := c.acquireRateToken("tn-refill"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("balde vazio deveria recusar, veio %v", err)
	}

	advanceClock(c, 100*time.Millisecond)

	if err := c.acquireRateToken("tn-refill"); err != nil {
		t.Fatalf("um décimo de segundo a 10/s devolve um token; veio %v", err)
	}
	if err := c.acquireRateToken("tn-refill"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("devolveu UM token, não dois; veio %v", err)
	}
}

// O isolamento por tenant é o que a rajada protege, e ele também não pode depender do
// relógio: um tenant esgotado não pode consumir a capacidade de outro.
func TestAcquireRateToken_IsolationHoldsAtAFrozenInstant(t *testing.T) {
	c := newRateLimitedClientAtFixedTime(t, 5)

	for i := 0; i < 5; i++ {
		if err := c.acquireRateToken("tn-noisy"); err != nil {
			t.Fatalf("rajada do vizinho barulhento deveria passar em %d: %v", i, err)
		}
	}
	if err := c.acquireRateToken("tn-noisy"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("o vizinho barulhento deveria estar esgotado, veio %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := c.acquireRateToken("tn-quiet"); err != nil {
			t.Fatalf("o tenant silencioso tem balde proprio, falhou em %d: %v", i, err)
		}
	}
}

// --- ajudantes ---
//
// O cliente e construido direto, sem servidor HTTP: `acquireRateToken` nao faz I/O, e
// atravessar a rede para exercitar um balde de tokens era justamente o que tornava o teste
// antigo dependente do tempo.

type frozenClock struct{ at time.Time }

func (f *frozenClock) now() time.Time { return f.at }

var clocks = map[*TransitClient]*frozenClock{}

func newRateLimitedClientAtFixedTime(t *testing.T, perSec int) *TransitClient {
	t.Helper()
	cache, err := lru.New[string, *rate.Limiter](64)
	if err != nil {
		t.Fatalf("cache do limitador: %v", err)
	}
	// Instante fixo e arbitrario: o que importa e que ele nao avanca sozinho.
	fc := &frozenClock{at: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	c := &TransitClient{
		rateLimiters: cache,
		rateLimit:    rate.Limit(perSec),
		rateBurst:    perSec,
		now:          fc.now,
	}
	clocks[c] = fc
	t.Cleanup(func() { delete(clocks, c) })
	return c
}

func advanceClock(c *TransitClient, d time.Duration) {
	if fc, ok := clocks[c]; ok {
		fc.at = fc.at.Add(d)
	}
}
