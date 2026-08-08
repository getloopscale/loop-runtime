# Log

## 2026-08-06
* **Update**: Árvore convertida para Open Knowledge Format v0.2 in-place. Todo concept
  recebeu `type` no frontmatter; `created_at`/`updated_at`/`updated_by` passaram a
  `generated: { by, at }`; `sources` em lista de strings virou entradas com `resource`;
  o `status` de workflow do ciclo foi preservado como `workflow_status` e `status` passou
  a usar o vocabulário OKF `draft|stable|deprecated`. Wikilinks `[[…]]` e menções a
  caminhos `.md` viraram links markdown, que é o que forma o grafo que um agente percorre.
  Nenhum corpo de documento foi reescrito — a conversão é aditiva.
* **Validation**: `okf-validate knowledge-base` → 110 concepts, **0 erros, 0 warnings,
  0 links quebrados, Conformant** (exit 0). Com `--strict` o gate falha (exit 1) apenas
  por 46 orphans, reduzidos de 110 — nenhum dos 110 documentos tinha um único link
  markdown antes da conversão.
* **Boundary**: os 46 orphans restantes são documentos que o próprio material não
  relaciona a nenhum outro (rodadas de auditoria e roadmap datadas, sem referência
  cruzada no texto). Ligá-los exigiria inventar relações que o texto não afirma, então
  ficam registrados aqui.
