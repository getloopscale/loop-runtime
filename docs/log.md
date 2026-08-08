# Log

## 2026-08-06
* **Update**: Árvore convertida para Open Knowledge Format v0.2 in-place. Todo concept
  recebeu `type` no frontmatter; `created_at`/`updated_at`/`updated_by` passaram a
  `generated: { by, at }`; `sources` em lista de strings virou entradas com `resource`;
  o `status` de workflow do ciclo foi preservado como `workflow_status` e `status` passou
  a usar o vocabulário OKF `draft|stable|deprecated`. Wikilinks `[[…]]` e menções a
  caminhos `.md` viraram links markdown, que é o que forma o grafo que um agente percorre.
  Nenhum corpo de documento foi reescrito — a conversão é aditiva.
* **Validation**: `okf-validate docs` → 245 concepts, **0 erros, Conformant** (exit 0).
  Com `--strict` o producer gate falha (exit 1) por conectividade: 31 links quebrados e
  56 orphans.
* **Boundary**: os 31 links quebrados são **pré-existentes** — o mesmo validador contra
  `HEAD` antes da conversão já apontava 31. A conversão não introduziu nenhum; ela reduziu
  orphans de 171 para 56. Os links quebrados restantes apontam para fora deste bundle
  (`.claude/knowledge-base/…`, `.claude/rules/…`, `../CLAUDE.md`) e são relações reais do
  repositório: o OKF resolve o grafo dentro de um bundle, e esses alvos moram em outro.
  Removê-los destruiria conhecimento correto, então ficam registrados aqui em vez de
  silenciados. Os 56 orphans são documentos que o próprio material não relaciona a nenhum
  outro; ligá-los exigiria inventar relações que o texto não afirma.
