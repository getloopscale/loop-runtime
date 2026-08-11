// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"fmt"
	"os"
)

// Uma capacidade é algo que um ambiente DECLARA oferecer, e a distância entre declarar e
// ter é o que este arquivo torna visível.
//
// MEDIDO em 2026-08-11: o app-dev serviu por 20h um painel com plano, preço e botão de
// upgrade, enquanto o segredo que verifica pagamentos não existia no cluster. Quatro
// camadas independentes trataram essa ausência como configuração legítima — o monte do
// Secret (`optional: true`), o seed (`SKIP-graceful`), o runtime (declara cobrança olhando
// só a chave de API) e o reporte de uso (percorre zero linhas e devolve nil).
//
// Nenhuma delas está errada isolada: num ambiente hermético, rodar sem credencial de
// cobrança é o comportamento desejado. O que faltava era a pergunta que separa os dois
// casos — *este ambiente prometeu cobrar?* —, e ela precisa ser respondida num lugar só.
// Respondida em quatro, ela é respondida de quatro jeitos, e a quinta camada nasce sem ela.

// Capability nomeia uma capacidade declarável.
//
// É um tipo próprio, e não uma string solta, para que a tabela abaixo e os chamadores não
// possam divergir por um erro de digitação que o compilador aceitaria.
type Capability string

const (
	// CapabilityBilling — o ambiente cobra de verdade: emite sessão de portal, recebe
	// evento de pagamento e reporta consumo.
	CapabilityBilling Capability = "billing"
)

// RequirementPolicy responde se a ausência do insumo de uma capacidade é tolerável.
//
// Um método só, de propósito (ISP): quem consulta quer saber se pode seguir sem o insumo,
// e nada além disso. Quem decide COMO obter o insumo é o seed; quem decide SE ele é
// obrigatório é esta política.
type RequirementPolicy interface {
	Requires(Capability) bool
}

// capabilitiesByEnv é a tabela de declarações — DADO, não `switch`.
//
// Um ambiente novo é uma linha aqui; uma capacidade nova é uma constante acima mais uma
// entrada. Nenhum consumidor muda (OCP). A forma anterior — `if env == "dev-public"`
// espalhado — obrigava a editar cada consumidor, e foi assim que a quinta camada nasceu
// sem a verificação que as outras quatro tinham.
//
// A ausência é deliberadamente NÃO-exigente: um ambiente fora da tabela não promete nada,
// então não deve ser cobrado por não cumprir. É a polaridade oposta à do registro de
// portas (`infra/quality/host-ports.txt`), e por um motivo: lá, o silêncio significa uma
// porta aberta que ninguém revisou; aqui, significa um ambiente que não prometeu cobrar.
var capabilitiesByEnv = map[string][]Capability{
	// dev-public serve app-dev.loopzero.example — o painel público, com plano e preço na tela.
	"dev-public": {CapabilityBilling},
}

// EnvRequirementPolicy resolve a exigência a partir do ambiente do Helmfile.
//
// Reaproveita o mesmo sinal que `tenant_fixture.go:116` já usava para decidir se roda; a
// diferença é que ali a decisão estava embutida no seed, e aqui ela é um colaborador que
// os testes substituem sem variável de ambiente.
type EnvRequirementPolicy struct {
	declared map[Capability]bool
}

// NewEnvRequirementPolicy constrói a política para um ambiente nomeado.
//
// Recebe o ambiente como parâmetro em vez de ler `os.Getenv` internamente: um colaborador
// que lê o processo não é substituível num teste, e testar por variável de ambiente torna
// os testes dependentes de ordem — o que `rules/testing.md § 3` proíbe.
func NewEnvRequirementPolicy(env string) *EnvRequirementPolicy {
	declared := make(map[Capability]bool, 1)
	for _, c := range capabilitiesByEnv[env] {
		declared[c] = true
	}
	return &EnvRequirementPolicy{declared: declared}
}

// PolicyFromEnvironment é a fábrica que a raiz de composição usa.
//
// É o ÚNICO ponto do pacote que lê o processo, mantendo `os.Getenv` fora da lógica.
func PolicyFromEnvironment() *EnvRequirementPolicy {
	return NewEnvRequirementPolicy(os.Getenv("LOOPZERO_HELMFILE_ENV"))
}

// Requires responde se a capacidade foi declarada por este ambiente.
func (p *EnvRequirementPolicy) Requires(c Capability) bool {
	if p == nil {
		return false
	}
	return p.declared[c]
}

// MissingInput traduz a ausência de um insumo no veredito que o ambiente merece.
//
// É o único lugar do repositório que sabe a regra "insumo ausente + capacidade declarada
// = defeito". Os dois seeds de Stripe a consultam; um terceiro consumidor a herda sem
// reimplementá-la, que é a diferença entre corrigir quatro camadas e corrigir o padrão.
//
// `detail` descreve O QUE falta, na linguagem do chamador — o arquivo de credenciais
// ausente, um valor-placeholder. A razão final acrescenta a capacidade e onde corrigir,
// porque uma falha que não diz o que fazer vira uma escavação para quem a encontra.
func (d *Deps) MissingInput(c Capability, detail string) Result {
	if d.RequiresCapability(c) {
		return Result{
			Verdict: FAIL,
			Reason: fmt.Sprintf("%s — este ambiente declara a capacidade %q, e sem esse insumo "+
				"ela não existe. Preencha em ~/.loopzero/dev-credentials.json", detail, c),
		}
	}
	// O ambiente não prometeu nada: pular é o comportamento correto, e é o que a corrente
	// pós-cleanroom espera num tier hermético.
	return Result{Verdict: SKIP, Reason: detail + " (SKIP-graceful)"}
}

// RequiresCapability é o atalho que os seeds usam.
//
// Nil-safe em duas camadas — `Deps` sem política e política nula — porque `Deps` é
// construído em muitos lugares que nada têm a ver com cobrança. Se uma `Deps` vazia
// passasse a exigir, esta mudança quebraria chamadores alheios com um FAIL sem explicação,
// que é o oposto do que ela existe para produzir.
func (d *Deps) RequiresCapability(c Capability) bool {
	if d == nil || d.Policy == nil {
		return false
	}
	return d.Policy.Requires(c)
}
