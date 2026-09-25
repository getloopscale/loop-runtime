// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

// A costura de planos deste pacote.
//
// `operators/internal/tenant` e' o lugar onde mais de um plano da plataforma se
// toca, e o agrupamento e' defensavel: o agregado Tenant E' a unidade certa —
// um tenant precisa mesmo de repositorio, de projeto de imagens e de bucket. O
// que faltava era a costura dizendo QUAL plano cada arquivo serve (B-060).
//
// Sem ela a informacao existe apenas como conhecimento tribal, e conhecimento
// tribal envelhece sem avisar: a evidencia que registrou o B-060 em 2026-08-11
// afirmava quatro planos, e cinco dias depois o provisionamento de Transit saiu
// do operator (`e19e6dea5`, provisionamento e' api-side por ADR) sem que nada
// no pacote registrasse a mudanca. Uma declaracao verificada contra o codigo
// nao pode envelhecer em silencio — `planes_test.go` reprova quando diverge.
//
// A declaracao NAO e' uma interface por plano. A forma que a DoD do B-060
// propunha copiar — os provisioners `gitea`/`argocd`/`minio`/`harbor` — e'
// fachada estreita sobre um servico EXTERNO, e cada um ja' existe. Repetir a
// forma aqui produziria interfaces com um unico implementador sobre codigo
// interno, que e' o anti-padrao que `rules/architecture.md` e a Regra 11 nomeiam.
// O dano medido nao e' falta de abstracao; e' falta de costura. Esta e' a costura.
type plano string

const (
	// BUILD — o que constroi e guarda artefato: bucket de bundles e projeto de
	// imagens do tenant.
	planoBuild plano = "BUILD"
	// CONTROL — o que declara o estado desejado: repositorio git e o projeto
	// ArgoCD que o sincroniza.
	planoControl plano = "CONTROL"
	// SECRETS — hoje presente apenas pela metade que ficou: o `VaultClient`
	// carrega `DeleteTenantKey` (crypto-shredding, B-075). O provisionamento
	// saiu em 2026-08-16 e nao deve voltar por arrumacao — ver
	// `tenant_transit_ownership_test.go`, que trava a decisao nos dois sentidos.
	planoSecrets plano = "SECRETS"
)

// planoDoCliente diz a que plano cada cliente do `TenantReconciler` pertence.
// As chaves sao nomes de campo da struct; `planes_test.go` verifica que todo
// campo cliente da struct aparece aqui, para que um cliente novo nao entre sem
// plano declarado.
var planoDoCliente = map[string]plano{
	"MinIOProvisioner":  planoBuild,
	"HarborClient":      planoBuild,
	"GitProvisioner":    planoControl,
	"ArgoCDProvisioner": planoControl,
	"VaultClient":       planoSecrets,
}

// planosDoArquivo declara os planos que cada arquivo do pacote pode tocar.
// Medido em 2026-08-18 a partir dos usos reais de `r.<Cliente>`.
//
// Dois arquivos cruzam planos, e cruzam de proposito — a lista diz qual e' o
// motivo, porque "cruza tudo" sem motivo escrito e' o mesmo que nao declarar:
//
//   - `tenant_controller.go` e' o orquestrador: ele carrega os clientes e chama
//     os passos. Cruzar planos e' a funcao dele.
//   - `tenant_deletion.go` e' o desmonte: apagar um tenant exige desfazer o que
//     cada plano criou, entao ele cruza por simetria com a criacao.
//
// Um arquivo sem cliente algum e' logica pura e declara lista vazia — o que
// tambem e' uma afirmacao verificada: se ele passar a chamar um cliente, o
// teste reprova ate' alguem decidir a que plano aquilo pertence.
var planosDoArquivo = map[string][]plano{
	// a propria costura nao chama cliente nenhum
	"planes.go":                {},
	"registry_auth_merge.go":   {},
	"tenant_build.go":          {planoBuild},
	"tenant_controller.go":     {planoBuild, planoControl, planoSecrets},
	"tenant_credentials.go":    {},
	"tenant_deletion.go":       {planoBuild, planoControl},
	"tenant_gitops.go":         {planoControl},
	"tenant_harbor.go":         {planoBuild},
	"tenant_priority.go":       {},
	"misplaced_tenant_scan.go": {},
}

// serve responde se o arquivo declarou o plano dado.
func serve(declarados []plano, p plano) bool {
	for _, d := range declarados {
		if d == p {
			return true
		}
	}
	return false
}
