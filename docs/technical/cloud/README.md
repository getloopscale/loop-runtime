# Cloud Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `cloud/` e os gates objetivos para considerar o serviço cloud do LoopZero em padrão SOTA no contexto de uma PaaS como a nossa.

No LoopZero, `cloud/` não é só um “site com login”. Ele é a camada de identidade, billing e experiência web do produto, e portanto precisa:

- autenticar usuários e emitir contexto de sessão seguro;
- ligar conta, tenant, plano e capacidades;
- medir uso e reconciliar cobrança sem corromper o modelo do produto;
- expor dashboard coerente com a CLI e com a API do engine.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa do serviço cloud, dashboard e fronteiras do módulo |
| [sota-gates.md](./sota-gates.md) | Gates SOTA claros, com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa oficial e evidências do código atual |
| [paas-benchmark.md](./paas-benchmark.md) | Benchmark do módulo cloud no contexto de uma PaaS como o LoopZero |

## Resumo Executivo

No LoopZero, `cloud/` só deve ser considerado SOTA quando:

1. Separa claramente identidade, sessão, cobrança, metering e UI.
2. Mantém billing como capability de produto, não só integração financeira.
3. Emite e valida tokens/sessões com semântica segura e explícita.
4. Trata webhooks e eventos de cobrança como fluxos idempotentes e auditáveis.
5. Reflete o mesmo modelo mental da CLI e da engine API no dashboard.
6. Tolera assincronia de medição, reconciliação e atualização de plano.
7. Mantém o boundary entre “produto cloud” e “control plane engine” rigorosamente definido.

## Leitura Recomendada

- Para entender a decomposição atual da pasta: [architecture.md](./architecture.md)
- Para definir critérios de aceitação de mudanças em `cloud/`: [sota-gates.md](./sota-gates.md)
- Para revisar a base oficial de OAuth device flow, webhooks e segurança de sessão: [research-basis.md](./research-basis.md)
- Para manter a discussão no contexto de uma PaaS como o LoopZero: [paas-benchmark.md](./paas-benchmark.md)
