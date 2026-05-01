# CLI Technical Reference

## Objetivo

Esta pasta define a referência técnica do módulo `cli/` e os gates objetivos para considerar a LoopZero CLI em padrão SOTA.

O foco aqui é verificabilidade. Os documentos abaixo não descrevem só intenção; eles descrevem critérios que podem ser auditados por código, testes e comportamento observável.

## Documentos

| Documento | Finalidade |
|---|---|
| [architecture.md](./architecture.md) | Mapa por pastas, fronteiras e domínios da CLI |
| [sota-gates.md](./sota-gates.md) | Gates SOTA claros, com critérios de aprovação e evidências esperadas |
| [research-basis.md](./research-basis.md) | Base de pesquisa usada para justificar os gates |
| [render-benchmark.md](./render-benchmark.md) | Análise do `RENDER_CLI.md` como benchmark de experiência para uma PaaS |

## Resumo Executivo

No LoopZero, a CLI só deve ser considerada SOTA quando:

1. É strict no parsing e previsível para automação.
2. Mantém contrato estável de `--json` e exit codes.
3. Se comporta corretamente em TTY, pipe e `CI=true`.
4. Protege segredos e configuração local com defaults seguros.
5. Normaliza erros remotos em mensagens acionáveis e contratos consistentes.
6. Propaga tracing e correlation através do boundary CLI → API.
7. Tem cobertura de teste suficiente para provar os pontos acima.

## Leitura Recomendada

- Para entender a decomposição atual do módulo: [architecture.md](./architecture.md)
- Para definir critérios de aceitação em mudanças da CLI: [sota-gates.md](./sota-gates.md)
- Para revisar a fundamentação externa e padrões de mercado usados aqui: [research-basis.md](./research-basis.md)
- Para comparar a LoopZero CLI com uma referência de mercado orientada a PaaS: [render-benchmark.md](./render-benchmark.md)
