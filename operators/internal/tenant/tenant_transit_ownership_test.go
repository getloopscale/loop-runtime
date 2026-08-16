// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"strings"
	"testing"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/vault"
)

// O provisionamento de chave Transit por tenant é **api-side** — decidido em
// `docs/wiki/decisions/2026-08-12-tenant-transit-key-ownership.md` (T3, ACCEPTED em
// parte), item `B-042`.
//
// A evidência que decidiu: o caminho do `api` está ligado e roda (`api/cmd/main.go:162`
// constrói o `pkg/vault.TransitClient`, que cria a chave preguiçosamente no primeiro 404
// de encrypt, `transit_client.go:391`), e o caminho do operator **nunca teve
// implementador de produção** — os únicos `EnsureTenantKey` da árvore eram dois mocks de
// teste, e `TenantReconciler.VaultClient` não era atribuído em nenhum ponto de
// construção. Dez semanas de no-op silencioso.
//
// Estes testes travam a decisão nos dois sentidos: o que saiu não pode voltar por
// distração, e o que ficou não pode sair por arrumação.

// A costura de PROVISIONAMENTO saiu. Uma reintrodução acidental — alguém "religando" o
// passo por achar que o no-op era esquecimento — recria a divergência que tornava o
// fail-fast do operator inócuo: o operator nomeia a chave `tenant-<id>` e o `api` nomeia
// `loopzero-tenants-<id>`, então o operator garantiria uma chave que ninguém usa.
func TestTransitKeyProvisioning_IsNotAnOperatorReconcileStep(t *testing.T) {
	r := &TenantReconciler{}
	for _, passo := range r.provisioningSteps(context.Background(), &loopzerov1.Tenant{}) {
		nome := passo.condType
		if strings.Contains(nome, "TransitKey") {
			t.Fatalf(
				"o passo %q voltou à lista de reconcile do tenant-controller.\n\n"+
					"O provisionamento é api-side (ADR 2026-08-12-tenant-transit-key-ownership, "+
					"B-042). O operator nomeia a chave `tenant-<id>` e o api nomeia "+
					"`loopzero-tenants-<id>` — um passo aqui garante uma chave que ninguém usa.",
				nome,
			)
		}
	}
}

// O SHREDDING ficou, e ficou de propósito. `DeleteTenantKey` é o primitivo de
// right-to-erasure: apagar a chave de um tenant torna todo ciphertext dele
// permanentemente irrecuperável. O ADR recusou deletá-lo junto — a propriedade dele
// depende do ciclo de vida de deleção de tenant, que a fatia não olhou (`B-075`).
//
// Sem este teste, a próxima limpeza de "interface com um método só" apaga o primitivo de
// conformidade porque a outra metade dela morreu.
func TestTransitShredding_SurvivesTheProvisioningRemoval(t *testing.T) {
	// Um tipo que implementa APENAS o shredding tem de satisfazer a interface. Se
	// alguém reintroduzir EnsureTenantKey no contrato, isto para de compilar — que é
	// exatamente o sinal que se quer, no momento em que se quer.
	var _ vault.TenantTransitClient = shreddingOnly{}
	_ = t
}

type shreddingOnly struct{}

func (shreddingOnly) DeleteTenantKey(context.Context, string) error { return nil }

// A armadilha que a remoção do passo criava, e que só apareceria num deployment que
// ligue o shredder: `requiredProvisioningConditions` acrescentava `TransitKeyAvailable`
// sempre que `VaultClient != nil`, e o `VaultClient` **continua existindo** — ele carrega
// a metade que ficou (`DeleteTenantKey`, `B-075`).
//
// Com o passo removido, ninguém escreve a condition. Exigi-la faria todo Tenant esperar
// para sempre por um passo que não roda mais — e o sintoma apareceria só onde o shredder
// fosse ligado, que é o ambiente onde ninguém está olhando para o controller de tenant.
//
// A asserção é sobre ESTA condition, e não sobre "toda condition exigida tem um passo que
// a escreve". A forma geral parece mais forte e é falsa: `QuotaEnforced` é exigida e
// escrita por `tenant_build.go:54`, dentro do passo cujo `condType` é
// `BuildNamespaceReady` — um passo escreve mais de uma condition, então `condType` não é
// o conjunto do que ele produz. Afirmar a forma geral reprovaria por uma razão que não
// existe, e é assim que um teste ensina a ser ignorado.
func TestTenantActivation_DoesNotRequireTheConditionWhoseWriterWasRemoved(t *testing.T) {
	comShredder := &TenantReconciler{VaultClient: shreddingOnly{}}

	for _, exigida := range comShredder.requiredProvisioningConditions() {
		if strings.Contains(exigida, "TransitKey") {
			t.Fatalf(
				"a activation exige %q, e o único passo que a escrevia foi removido em "+
					"2026-08-16 (provisionamento é api-side, B-042).\n\n"+
					"Todo Tenant num deployment com o shredder ligado ficaria em "+
					"Provisioning para sempre, esperando um passo que não roda.",
				exigida,
			)
		}
	}
}
