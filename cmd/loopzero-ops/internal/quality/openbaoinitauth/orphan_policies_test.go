// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package openbaoinitauth

import (
	"strings"
	"testing"
)

const roleQueCarrega = `    write_auth_role "k8s-core" "api" \
      "loopzero-api" \
      "loopzero-system" \
      "loopzero-api" \
      "24h"
`

func TestPolicyCarregadaPorUmRolePassa(t *testing.T) {
	r := `write_policy "loopzero-api" "/tmp/x.hcl"` + "\n" + roleQueCarrega
	if f := checarPoliciesOrfas(r); len(f) != 0 {
		t.Errorf("falhas = %v; a policy e' carregada pelo role", f)
	}
}

// O defeito MEDIDO (B-098): duas policies declaradas que nenhum role carregava.
func TestPolicyOrfaEAcusadaPeloNome(t *testing.T) {
	r := `write_policy "loopzero-api" "/tmp/a.hcl"
write_policy "loopzero-api-read" "/tmp/b.hcl"` + "\n" + roleQueCarrega
	f := checarPoliciesOrfas(r)
	if len(f) != 1 || !strings.Contains(f[0], "loopzero-api-read") {
		t.Fatalf("falhas = %v; a orfa precisa ser NOMEADA — um gate que nao diz QUAL forca uma cacada", f)
	}
	if strings.Contains(f[0], `loopzero-api,`) || strings.Contains(f[0], `: loopzero-api `) {
		t.Errorf("falhas = %v; a policy carregada nao pode ser acusada", f)
	}
}

// Um role pode carregar varias policies separadas por virgula.
func TestListaDePoliciesSeparadaPorVirgula(t *testing.T) {
	r := `write_policy "a" "/tmp/a.hcl"
write_policy "b" "/tmp/b.hcl"
    write_auth_role "k8s-core" "r" \
      "sa" \
      "ns" \
      "a,b" \
      "24h"
`
	if f := checarPoliciesOrfas(r); len(f) != 0 {
		t.Errorf("falhas = %v; as duas estao na lista do role", f)
	}
}

// Verde sobre nada e' a forma `PASS — 0 routes` (B-040): sem policy alcancada, o gate nao
// verificou coisa alguma.
func TestRenderSemPolicyEFalha(t *testing.T) {
	if f := checarPoliciesOrfas("echo nada"); len(f) != 1 || !strings.Contains(f[0], "nao alcancou nada") {
		t.Errorf("falhas = %v; want a falha de varredura vazia", f)
	}
}

// Se a assinatura do helper mudar, o parser se desalinha e leria o argumento errado — marcando
// policies vivas como orfas. Reportar e' obrigatorio; adivinhar, nao.
func TestAssinaturaCurtaEReportadaEmVezDeMalLida(t *testing.T) {
	r := `write_policy "a" "/tmp/a.hcl"
    write_auth_role "k8s-core" "r" \
      "sa"
`
	f := checarPoliciesOrfas(r)
	var achou bool
	for _, x := range f {
		if strings.Contains(x, "assinatura do helper mudou") {
			achou = true
		}
	}
	if !achou {
		t.Errorf("falhas = %v; a mudanca de assinatura precisa ser reportada, nao absorvida", f)
	}
}
