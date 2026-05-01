# Cloud SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo`
- `Critério de aprovação`
- `Evidência esperada`
- `Notas LoopZero`

No contexto de uma PaaS, o módulo `cloud/` precisa ser avaliado tanto como serviço de identidade/comercial quanto como experiência de produto.

Regra transversal de validação:

- testes unitários e integração do serviço são necessários;
- para auth, billing, webhook e sessão, isso ainda não basta sem exercício com dependências reais ou parity alta, porque os riscos críticos estão em flows externos e comportamento de produto ponta a ponta.

## Gate CLO-G01: Identity And Billing Separation

`Objetivo`

Evitar que autenticação, autorização e cobrança se contaminem sem fronteiras claras.

`Critério de aprovação`

- sessão/auth funcionam sem depender da camada de cobrança para validar identidade;
- billing decide capabilities comerciais, não autenticação básica;
- autorização de rotas é aplicada no boundary adequado.

`Evidência esperada`

- pacotes separados para `auth/` e `billing/`;
- middleware JWT distinto de handlers comerciais.

`Notas LoopZero`

- esse gate é fundamental para não transformar cobrança em pré-requisito estrutural de login.

## Gate CLO-G02: Session Token Discipline

`Objetivo`

Emitir e validar sessões do dashboard com semântica explícita, curta e auditável.

`Critério de aprovação`

- sessão emitida para o dashboard tem audience e issuer explícitos;
- TTL e rotação são claros;
- token de sessão não é confundido com token do engine ou com token do provedor OIDC.

`Evidência esperada`

- uso do `pkg/jwt` com session tokens HMAC;
- exchange dedicado de sessão;
- testes de auth/session.

`Notas LoopZero`

- esse gate é importante porque o dashboard opera com semântica distinta da CLI.

## Gate CLO-G03: Correct OAuth Device Flow Semantics

`Objetivo`

Preservar experiência de autenticação compatível com CLI-first product.

`Critério de aprovação`

- device authorization e token polling seguem semântica do flow;
- login/consent browser flows são separados do polling device flow;
- dashboard e CLI não disputam o mesmo contrato de sessão.

`Evidência esperada`

- `routes/device.go`;
- `consent/handler.go`;
- Hydra client e testes.

`Notas LoopZero`

- o LoopZero é CLI-first; por isso o device flow é parte central da experiência do produto.

## Gate CLO-G04: Billing As Product Capability Layer

`Objetivo`

Tratar billing como governança de capacidade do produto, não só cobrança financeira.

`Critério de aprovação`

- plano tem relação explícita com limites/capabilities;
- provisionamento comercial atualiza estado consumível pelo restante da plataforma;
- mudanças de plano são auditáveis e reproduzíveis.

`Evidência esperada`

- `billing/plans.go`, `provisioner.go`;
- integração com enforcement no restante da plataforma;
- testes de billing/enforcement.

`Notas LoopZero`

- em PaaS, cobrança sem governança de capability não resolve o problema de produto.

## Gate CLO-G05: Webhook Integrity And Replay Safety

`Objetivo`

Receber eventos externos de cobrança sem spoofing nem efeitos duplicados indevidos.

`Critério de aprovação`

- webhook verifica assinatura;
- processamento tolera retries legítimos do provedor;
- efeito do webhook é idempotente ou reconciliável.

`Evidência esperada`

- `billing/webhook.go`;
- uso de segredo de assinatura;
- testes cobrindo webhook handling.

`Notas LoopZero`

- Stripe webhooks tornam esse gate obrigatório.

## Gate CLO-G06: Metering Is Eventually Correct

`Objetivo`

Tratar medição como pipeline reconciliável, não como incremento oportunista sem controle.

`Critério de aprovação`

- tracking, reporting, scheduling e reconciliation são explicitamente separados;
- atraso, replay e reprocessamento não corrompem o estado;
- billing não depende de evento efêmero único.

`Evidência esperada`

- `metering/tracker.go`, `reporter.go`, `scheduler.go`, `reconciler.go`;
- testes de metering.

`Notas LoopZero`

- em uma PaaS, uso medido raramente é estritamente síncrono com a ação do usuário.

## Gate CLO-G07: Dashboard Is Consumer, Not Rule Engine

`Objetivo`

Impedir que a UI se torne a verdadeira fonte das regras do produto.

`Critério de aprovação`

- dashboard consome contratos do backend;
- decisões de permissão, plano e estado vivem no backend adequado;
- a UI reflete o mesmo modelo mental de CLI e API.

`Evidência esperada`

- páginas do dashboard consumindo APIs;
- ausência de gating crítico só no frontend.

`Notas LoopZero`

- esse gate é importante para manter CLI e dashboard consistentes.

## Gate CLO-G08: Engine Boundary Clarity

`Objetivo`

Manter a separação entre serviço cloud e engine API rigorosa.

`Critério de aprovação`

- o cloud autentica o usuário e chama o engine com boundary apropriado;
- o cloud não reimplementa semântica operacional do engine;
- o proxy preserve segurança e rastreabilidade.

`Evidência esperada`

- proxy `/engine` no `cloud/cmd/main.go`;
- uso de service auth e middleware de sessão;
- ausência de lógica duplicada do engine em `cloud/`.

`Notas LoopZero`

- esse gate protege contra drift entre dashboard e CLI.

## Gate CLO-G09: Same-Origin And Browser Security Correctness

`Objetivo`

Fazer o dashboard operar com postura segura e previsível no browser.

`Critério de aprovação`

- arquitetura same-origin ou cross-origin é explícita;
- cookies, redirects e callback URLs são tratados deliberadamente;
- surface HTTP evita exposição desnecessária.

`Evidência esperada`

- decisões comentadas em `cloud/cmd/main.go`;
- config validada;
- páginas/login callbacks coerentes.

`Notas LoopZero`

- o módulo já explicita decisões de CORS/same-origin; isso precisa continuar governado.

## Gate CLO-G10: Product-Facing Read Models

`Objetivo`

Dar ao usuário web uma visão útil da conta e da plataforma sem exigir montagem manual de estado.

`Critério de aprovação`

- endpoints como `/me` e billing retornam informação orientada a UX;
- páginas principais do dashboard têm estado consistente e acionável;
- o módulo cloud não devolve somente echoes crus de providers externos.

`Evidência esperada`

- `routes/me.go`, `routes/billing.go`;
- páginas `overview`, `projects`, `billing`, `team`, `settings`.

`Notas LoopZero`

- em produto SaaS/PaaS, o valor do dashboard está muito ligado a qualidade desses read models.

## Gate CLO-G11: Observability For Commercial And Identity Flows

`Objetivo`

Tornar login, sessão, billing e metering observáveis como domínios críticos.

`Critério de aprovação`

- tracing e metrics cobrem os fluxos importantes;
- logs permitem correlacionar autenticação, billing e chamadas ao engine;
- health expõe degradação de dependências relevantes.

`Evidência esperada`

- `internal/tracing`;
- `internal/metrics`;
- health endpoint com estado do DB;
- request logging com trace id.

`Notas LoopZero`

- billing/identity outage é incidente de produto, não detalhe secundário.

## Gate CLO-G12: Config And Dependency Clarity

`Objetivo`

Evitar comportamento implícito demais em segredos, URLs e enablement de subsistemas.

`Critério de aprovação`

- config valida campos críticos;
- ausência de dependência opcional desabilita feature explicitamente;
- produção e dev têm contratos discerníveis.

`Evidência esperada`

- `internal/config/config.go`;
- logs explícitos sobre billing/DB enablement;
- testes de config.

`Notas LoopZero`

- o módulo já faz enablement explícito de DB e Stripe; isso é sinal bom.

## Gate CLO-G13: Test Matrix Covers Identity, Billing And Metering

`Objetivo`

Cobrir as áreas onde erro compartilhado gera impacto direto no produto.

`Critério de aprovação`

- device flow, billing, metering, tenant provisioning e config têm testes dedicados;
- session/auth regressions são detectáveis;
- webhook e enforcement têm cobertura direcionada.

`Evidência esperada`

- `cloud/tests/unit/*`;
- testes do frontend quando houver contrato crítico de auth/session.

`Notas LoopZero`

- as áreas mais sensíveis desse módulo não são renderização visual; são identidade, cobrança e reconciliação.

## Ordem De Prioridade

1. `CLO-G01`, `CLO-G02`, `CLO-G04`, `CLO-G05`, `CLO-G08`
2. `CLO-G03`, `CLO-G06`, `CLO-G07`, `CLO-G11`, `CLO-G12`
3. `CLO-G09`, `CLO-G10`, `CLO-G13`

Justificativa:

- primeiro, segurança de identidade, billing e boundary com engine;
- depois, consistência operacional e de produto;
- por fim, refinamentos de UX/browser e cobertura expandida.
