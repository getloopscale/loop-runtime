# ADR — Unificar cell e cluster em 1:1, aposentando `cell-default`

**Data:** 2026-08-09
**Status:** **ACCEPTED** — retroactively ratified 2026-09-06 by sponsor (see § Retroactive Ratification at end)
**Tier:** T3 (`.claude/rules/pre-release-governance.md`) — schema, placement e identidade de cluster
**Item:** `BACKLOG.md` § B-005
**Medição:** `.claude/knowledge-base/discoveries/opportunities/cell-cluster-unification-opportunity.md`

> Este documento existe para que a reunião decida sobre **material medido**, não sobre intuição. Ele
> não decide — ele instrui a decisão, e nomeia explicitamente a pergunta que sobra.

## Contexto

O CTO decidiu em 2026-08-09 eliminar a relação 2:1 entre cell e cluster, para que `cell-N` possa nomear
um cluster sem ambiguidade — o que destrava a metade `cell` do `B-001`. A medição confirmou que a
classificação T3 se sustenta, e mostrou **por quê**: o obstáculo não é a linha na tabela `cells`, é uma
sentinela num trigger.

## O que está medido

### `cell-default` não é um valor — é uma sentinela

`api/internal/db/migrations/000001_initial_schema.up.sql:66-74`:

```sql
CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id = 'cell-default' OR NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN NEW.cell_id := 'cell-default'; END IF;
    END IF;
    RETURN NEW;
END;
$$;
```

Ela significa **"ainda não resolvido, herde do tenant"**, é tratada como equivalente a `NULL`, e é também
o valor de último recurso quando o tenant não tem cell. Armada em **4 tabelas**:
`000001_initial_schema.up.sql:456` e `:460`, `000004_preview_environments.up.sql:91`,
`000005_release_app_attestations.up.sql:80`.

### A restrição que amarra as duas pontas

**Cinco colunas** declaram `cell_id TEXT NOT NULL DEFAULT 'cell-default'` —
`000001:96`, `000001:133`, `000001:169`, `000004:39`, `000005:31` — mais `tenants.cell_id` em
`000011_add_cell_id.up.sql:9`. E o catálogo impõe `CHECK (cell_id ~ '^cell-[a-z0-9-]+$')`
(`000006_cells.up.sql:40`), então qualquer id novo continua tendo de começar com `cell-`.

### O caminho já foi julgado descartável uma vez

`api/internal/placement/engine.go:15-19` registra `CHG-2026-04-30-DELETE-SINGLE-CELL`: `DefaultEngine`,
`DefaultCellID`, `Engine`, `ExtractRegion`, `ConstraintEngine` e `CellLabelQuerier` foram deletados,
*"All were unused in production paths"*, deixando explícito que *"the cell-default row in
tenants.cell_id remains as the SQL DEFAULT — this is data, not code"*. A constante voltou em junho
(`cells.go:5-12`, plano `2026-06-10-j22-layer2-code-review-fixes` T1.1) só para o broker do dev-public.
Hoje são **5 sítios não-teste** que a referenciam.

### O catálogo diz que só uma cell admite tráfego

`000006_cells.up.sql:114-129`: `cell-default` é *"Backwards-compat default … Pre-multi-cell"* (`active`),
`cell-1` é *"Canonical first cell"* (`active`), e `cell-2` está em **`maintenance`** — *"Physically
provisioned but not admitting traffic"*.

### `cell-default` e `cell-1` são a mesma máquina sob dois nomes

Medido em 2026-08-10, três fontes independentes:

1. `infra/helmfile/environments/dev.yaml:76,79` — *"In dev: runtime cluster maps to cell-1"*, e
   `runtimeCellId: cell-1`. O cluster das aplicações se declara `cell-1`.
2. `000006_cells.up.sql:120-123` — `cell-1` é *"Canonical first cell. Maps to runtime-1 … cluster"*;
   `cell-default` é o valor que o `DEFAULT` da coluna grava. O mesmo cluster, um nome por camada.
3. `docs/runbooks/bootstrap-regression-matrix.md:180` (auditoria LIVE de 2026-05-01) — a tabela `cells`
   tinha três linhas `active`, mas existiam ClusterCredential CRs para **duas**. A terceira identidade
   nunca teve máquina.

É o mesmo defeito que motivou a renomeação dos clusters — "duas palavras, mesmo cluster" —, uma camada
abaixo. Isso **muda o custo da Opção A**: aposentar `cell-default` não é só trocar uma sentinela, é
fundir duas identidades numa só. A fusão tem ordem obrigatória (`cell-default`+`cell-1` → `cell-0`
**antes** de `cell-2` → `cell-1`), e uma substituição textual ingênua faria as duas ao mesmo tempo,
colapsando identidades distintas sem deixar rastro.

### A migração é um conjunto de UPDATEs, não uma cirurgia de esquema

`git grep "REFERENCES cells"` em `api/internal/db/migrations` não devolve nada: `cell_id` é `TEXT` livre
em todas as 6 tabelas que o declaram (`000001` ×4, `000004`, `000005`), sem chave estrangeira para
`cells(cell_id)`. Renomear não exige `ON UPDATE CASCADE` nem janela de indisponibilidade — mas também
significa que **nada no banco impede um `cell_id` órfão**, que é o que torna a escolha da sentinela uma
decisão de integridade e não de estilo.

## A pergunta que a reunião decide

Aposentar `cell-default` exige escolher **quem passa a portar o sentido de "ainda não resolvido"**, e
**o que acontece quando o tenant não tem cell** — hoje as duas respostas são a mesma palavra.

### Opção A — `NULL` é o único sinal de "não resolvido"

O trigger **já aceita** `NULL` no primeiro ramo (`OR NEW.cell_id IS NULL`). Pela ordem documentada do
Postgres, um trigger `BEFORE INSERT` roda **antes** da checagem de `NOT NULL`, então um `INSERT` que
passe `cell_id = NULL` explicitamente é resolvido pelo trigger e satisfaz a restrição depois.

- **Migração:** remover `DEFAULT 'cell-default'` das 6 colunas; simplificar o trigger para checar só
  `IS NULL`; decidir o último recurso (abaixo).
- **A favor:** nenhum vocabulário novo; `NULL` é o jeito que SQL já tem de dizer "ausente"; o trigger
  não muda de forma, encolhe.
- **Contra:** depende da ordem `BEFORE trigger` → `NOT NULL`, que é comportamento documentado mas
  **precisa ser confirmado por um INSERT real na reunião** — não há banco de pé para provar aqui, e
  afirmar sem provar é o que esta medição existe para evitar.

### Opção B — uma sentinela explícita nova (ex.: `cell-unassigned`)

- **A favor:** o `CHECK (cell_id ~ '^cell-[a-z0-9-]+$')` continua satisfeito sem tocar em `NOT NULL`;
  a migração é um `UPDATE` de valor, sem mudança de restrição.
- **Contra:** troca uma sentinela por outra. O defeito que o `B-001` corrige é justamente uma palavra
  com dois sentidos, e esta opção o preserva sob um nome novo — `cell-unassigned` continua sendo uma
  linha na tabela `cells` que não é uma cell.

### A sub-decisão que nenhuma das duas resolve sozinha

`IF NEW.cell_id IS NULL THEN NEW.cell_id := 'cell-default'; END IF;` — o que fazer quando o tenant não
tem cell. Hoje isso **silenciosamente** grava a sentinela e a linha segue. `.claude/rules/error-handling.md`
§ 2 diz o oposto: falhar rápido, alto e claro, e nunca usar valor mágico para sinalizar falha.

**Recomendação:** levantar exceção (`RAISE EXCEPTION`) nomeando o tenant. Um build cuja cell não resolve
é um build que não deveria existir; grava-lo com uma sentinela empurra o defeito para o deploy, onde
ele aparece como roteamento errado em vez de como erro de dados.

## Recomendação para a reunião

**Opção A, com falha explícita no último recurso** — mas a ratificação depende de **duas medições que só
a reunião pode fazer**, porque exigem um banco de pé:

1. **Contar.** Quantos tenants em cada cell, e quantas linhas nas 4 tabelas com `cell_id = 'cell-default'`.
   Sem isso a migração não tem tamanho, e este ADR não o inventa.
2. **Provar a ordem.** Um `INSERT` com `cell_id = NULL` numa das 4 tabelas, confirmando que o trigger
   resolve antes de o `NOT NULL` reprovar.

Se (2) falhar, a Opção A cai e a B passa a ser a única, com o custo de vocabulário assumido.

## Alternativas rejeitadas

- **Manter a relação 2:1 e desistir da unificação** — rejeitada: a decisão do CTO já foi tomada, e o
  `B-001` depende dela para nomear o cluster de tenants sem homônimo.
- **Rebaixar para T2 por ser "só uma linha em `cells`"** — rejeitada pela medição: o trigger em 4
  tabelas faz disso mudança de comportamento da camada de dados, não de dados.
- **Renomear `cell-default` para algo melhor sem eliminar a relação 2:1** — rejeitada: não resolve o
  homônimo, só o adia.

## Consequências

- `B-001` continua bloqueado na metade `cell` até esta decisão. As metades `mgmt`→`core` e
  `vault`→`secrets` já foram implementadas e **não dependem** dela.
- A migração é forward-only (`.claude/rules/` + convenção do repo): remover um `DEFAULT` e mudar um
  trigger são passos reversíveis por nova migração, não por `DOWN`.
- `api/internal/placement/cells.go` perde a constante `DefaultCellID` — pela segunda vez, e desta vez
  com o caminho dev-public precisando de substituto explícito, que é o que faltou em abril.

## Referências

- Medição: `.claude/knowledge-base/discoveries/opportunities/cell-cluster-unification-opportunity.md`
- Item: `BACKLOG.md` § B-005
- ADR de nomenclatura que este destrava: `docs/decisions/2026-08-09-cluster-topology-naming-adr.md`
- Ciclo de vida de cluster: `docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md` (PROPOSED)

---

## Retroactive Ratification — 2026-09-06

**Signed by:** sponsor (paulohenriquevn) via `/goal` chat approval on 2026-09-06, in response to B-136 halt (2026-09-04) which measured that:

- The `cell-cluster-unification` code landed in commit `148c04104` on 2026-08-17 — **11 days after** the `governance-gate.py` hook was deleted (`8cb41f660`, 2026-08-06) — while this ADR remained `PROPOSED`.
- 11 of the 11 agents that `pre-release-governance.md § Meeting Flow` named had been deleted in that same commit, so the meeting instructed by this ADR had no executor.
- `.claude/rules/decision-delegation.txt § RETAINED governance` explicitly forbids autonomous delegation of governance changes.

**What is ratified:** the decision described in this document (eliminate the 2:1 cell↔cluster relation, retire the `cell-default` sentinel, adopt 1:1 mapping). The code already implementing it (`148c04104`, `a20ec802c`, `92d1674fc`, `4d7a6b97a`) becomes covered by this ratified ADR.

**What is NOT ratified retroactively:** the *process* by which the code landed without ratification. That gap is separately addressed by B-197 (restore `governance-gate.py` + agents, 2026-09-06 same day) which prevents recurrence.

**Consequence:** B-136 bullet 1 (*"existe registro T3 (retroativo, se for o caso) cobrindo a fusão das identidades de cell e o rename dos clusters"*) is now satisfied. Bullets 2 (measured 2026-08-19) and 3 (`pre-release-governance.md` amendment) remain per B-136 halt records.

**Precedent:** future T3 changes without prior ratification MUST NOT rely on this retroactive path. `governance-gate.py` (restored B-197) now BLOCKS Edit/Write on production paths before any T3 code can land. This ratification closes a specific 30-day HONOR-SYSTEM gap and is not policy going forward.
