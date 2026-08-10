# BACKLOG — loopzero (engine)

Registro de trabalho de manutenção **deste repositório**: uma linha por hipótese, com dono e critério
de fechamento. **Ids são monotônicos, nunca reusados, nunca renumerados** — um item morto guarda o
número dele para sempre, porque o número é o rastro.

Um item aqui é **hipótese, não compromisso**. Ele nasce com evidência fraca ou nenhuma por desenho;
provar ou matar é trabalho do `/discover`.

## Divergência declarada do `cycle-backlog.md`

A regra desenha **um registro único no umbrella**, cobrindo os repositórios do ecossistema, e o
`/backlog-init` recusa dentro de um repo governado — *"a per-repo backlog is the fragmentation the
single registry exists to prevent"*.

**Este arquivo diverge disso**, seguindo as duas decisões de owner que já criaram registros per-repo
em `loopzero-cloud` (2026-08-06) e `loopzero-db` (2026-08-08). Duas razões o sustentam:

1. O `/backlog-init` continua **bloqueado no umbrella** — `loopzero-platform/` não tem `CHANGELOG.md`, e o
   Step 0.3 do skill falha. Verificado em 2026-08-09, um dia depois do registro do `loopzero-db` apontar o
   mesmo bloqueio.
2. `knowledge-base-location.md` estabelece que **consumidores não compartilham knowledge-base**: cada
   projeto é dono do próprio `ROADMAP.md` e do próprio `.claude/knowledge-base/`. Um backlog local é
   coerente com essa autonomia; o que ela proíbe é um artefato de ciclo referenciar outro repositório.

O custo da divergência é real e fica dito: **um achado que cruze repositórios não tem onde morar
aqui.** O `B-002` deste arquivo já esbarra nisso — metade da evidência dele vive em `loopzero-cloud`, e o
que está registrado aqui é só a metade que é deste repo. Quando o umbrella ganhar `CHANGELOG.md`, este
arquivo deve ser reavaliado — migrado ou mantido com escopo explícito.

## Fronteira com os planos

Os dois existem e **não são intercambiáveis**:

| | vai para |
|---|---|
| trabalho já com escopo, decisões e tarefas | **`.claude/knowledge-base/plans/{slug}-plan.md`** (via `/to-plan`) |
| hipótese de manutenção, ainda sem medição classificada | **este arquivo** (`B-NNN`) |

Um item que precisa ser **medido antes de virar escopo** nasce aqui e sobe depois. `raw → planned` é
proibido: nada chega a um plano sem passar pela medição do `/discover`.

## Como um item chega

| Produtor | Comando | Chega como |
|---|---|---|
| Humano (hipótese) | `/backlog-item {slug}` | `status: raw` |
| Medição (varredura com evidência) | `/discover --sweep engine-go` | `status: triaged` · `evidence: <ponteiro>` |

Schema, transições de status, gates G1–G5, vereditos e rollback: `.claude/rules/cycle-backlog.md`. Este
arquivo é **dado**; o contrato vive na regra.

## Roteamento de domínio

Este repositório inteiro é **um** domínio registrado do ecossistema. Não há taxonomia de pilares aqui,
porque não há ambiguidade a resolver:

| Domínio | Superfície | Especialista |
|---|---|---|
| `engine-go` | `loopzero` — módulos Go (`api/`, `operators/`, `pkg/`, `cmd/`, `tools/`), Helm charts (`infra/helm/`, `charts/`), helmfile, k3d, `infra/tests/` | `.claude/agents/engine-go.md` |

Um item que abranja dois domínios **é dois itens** (gate G3). Itens dos outros 7 domínios do
ecossistema (`control-plane`, `data-plane-ts`, `loopzero-db`, `infra-terraform`, `contracts-auth`,
`frontend-dashboard`, `platform-cli`) vivem nos repos irmãos e **não são registrados aqui**, por
consequência do escopo per-repo declarado acima.

## Items

## B-001 — Os quatro clusters da plataforma têm nomes que mentem sobre o que são   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
sponsor: CTO
tier: T2+ (`.claude/rules/pre-release-governance.md`) — toca operações e o caminho crítico de deploy; ADR obrigatório
evidence: `.claude/knowledge-base/discoveries/opportunities/cluster-topology-naming-opportunity.md` — inventário classificado medido em 2026-08-09 sobre a árvore `26ba1340`, com lista de padrões congelada antes da execução e classificador determinístico (duas execuções byte-idênticas). **2.569 ocorrências-alvo vivas** (não 2.024, e não as 2.990 da primeira contagem — uma emenda datada aos caminhos histórico-imutáveis, derivada de `docs/README.md:16`, reclassificou 421 delas: a contagem do briefing perde identidade em forma nua — contexto kubectl derivado, chave de kubeconfig, env var, alvo de Taskfile, argumento de script). **14.346 ambíguas**, das quais **3.257 vivas em código executável**, 1.614 em `infra/` por 287 arquivos. Artefatos em `.claude/knowledge-base/discoveries/snapshots/cluster-topology-naming-*`. Controle negativo (`build`) validou o instrumento e pegou uma contaminação cruzada no classificador antes de ela virar número neste registro.
why_now: o gatilho já estava registrado neste repo — `.claude/knowledge-base/plans/helm-topology-index-plan.md:198-200` deixou `mgmt`→`core` fora de escopo *"deliberadamente"*, com a razão **"renomear antes significa renomear duas vezes"**. Por isso os quatro renames vão juntos. E a janela é agora: **os clusters AWS ainda não existem** (`AM5 — [ ]` em `loopzero-workspace/ROADMAP_AWS.md:79`), então o custo hoje é renomear dev loop k3d, helmfiles, ArgoCD apps e secrets. Depois, são quatro clusters vivos a migrar.
status: triaged
blocked_by: **decisão de sponsor sobre o mapa da metade `cell`** (D2 da opportunity). A medição refutou `loopzero-workload` → `cell-1` / `loopzero-runtime-2` → `cell-2`: `infra/helmfile/scripts/bootstrap-cell-cc.sh:20-22` mostra que `cell-1` e `cell-2` **já existem** como ids de cell, e que `cell-default` + `cell-1` apontam para o mesmo cluster `loopzero-workload` (relação 2:1). Renomear o cluster para `cell-1` faz o nome do cluster coincidir com o de uma das duas cells que rodam nele — reproduz uma camada acima o defeito que este item existe para remover. As metades `mgmt`→`core` e `vault`→`secrets` **não dependem** desta decisão e estão prontas para `/to-plan`. Ver também `docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md` (**PROPOSED**, T3), que este ADR deve emendar em vez de competir.
dod:
  - `git grep -n "loopzero-core\|core-cluster\|loopzero-workload\|workload-cluster\|loopzero-runtime\|runtime-cluster"` retorna zero fora de `CHANGELOG.md`, ADRs e documentos históricos — que **preservam** os nomes antigos, porque descrevem o passado
  - `loopzero-build` intocado: 657 hits/253 arquivos e `build-cluster` 122/69 antes e depois
  - nenhum falso positivo renomeado — pacote `runtime` do Go, `runtimeClassName`, paths do OpenBao e vocabulário genérico do Kubernetes inalterados, provado pela suíte compilando e os testes passando
  - todos os gates de qualidade passam (`task quality:gates`)
  - `infra/tests/snapshots/helmfile-*.yaml` e `docs/architecture/helm-topology.md` **regenerados** pelo gate, com diff contendo só renomeação
  - **o ambiente sobe do zero com os nomes novos**, pelo caminho que `.claude/rules/cleanroom-up-live-test-mandate.md` exige — rename de cluster que nunca subiu não está verificado
  - ADR escrito (com as alternativas rejeitadas: `control`, `platform`, `ops`, reciclar `mgmt`, `bao`) e `CHANGELOG.md` atualizado sob `[Unreleased] § Changed`

**O mapa.** `mgmt`/`loopzero-core` → **`core`** (é o produto, não gestão). `workload` + `runtime` como
cluster → **`cell`**, com `loopzero-workload` → `cell-1` e `loopzero-runtime-2` → `cell-2`, alinhando a infra ao
vocabulário que o código Go já usa em 137 arquivos (tabela `cells`, `cellID`, `maxPerCell`,
`K8sCellStore`). `vault`/`loopzero-secrets`/`secrets-system` → **`secrets`** (o cluster roda OpenBao, não Vault:
`infra/helmfile/secrets.helmfile.yaml.gotmpl` usa o chart `openbao-helm`, appVersion `v2.5.4`, binário
`bao`). `build`/`loopzero-build` **não muda** — já diz o que é e numera bem. O futuro cluster de CAPI nasce
como **`fleet`**: registrar no ADR, sem trabalho de rename. **`mgmt` fica em quarentena** — não recicla
para o cluster de CAPI, ainda que o CAPI chame o dele de *management cluster*; um nome que acabou de
mudar de dono fica ambíguo em todo log, runbook e ticket antigo por meses.

**As fases.** *0* — inventário classificado, **nenhuma edição**, separando identificador de cluster de
palavra genérica com caminho e linha, e submetendo a classificação a revisão antes de editar; é o que
torna o resto seguro, e é o que o `/discover` mede para levar este item a `triaged`. *1* — `mgmt` →
`core`, o maior volume mas o mais limpo, primeiro para calibrar o processo. *2* — `workload` +
`runtime` → `cell-1`/`cell-2`, a mais delicada por causa dos 353 arquivos Go, incluindo
`runtime.helmfile.yaml.gotmpl` → `cell.helmfile.yaml.gotmpl` (ele serve as duas) e o secret
`loopzero-runtime-kubeconfig` passando a ser por cell. *3* — `vault` → `secrets`, **com sub-tarefa própria**
para `secrets-system` → `secrets-system`: é mudança de **recurso vivo**, não de texto — o namespace
precisa ser recriado, as cargas migradas, e o `ClusterSecretStore` `openbao-transit` aponta para
`openbao-transit.secrets-system.svc:8200`; o rename de texto sozinho deixa referências apontando para um
namespace que não existe mais. *4* — ADR + documentação.

**A armadilha.** Substituição global cega destrói o repositório: 91,2% dos hits são falso positivo. Não
rodar `sed` global em `mgmt`, `runtime`, `workload` ou `vault`. Não editar snapshots nem documentos
gerados à mão — um snapshot editado passa no teste e mente sobre o sistema. Não juntar este rename com
outra mudança: neste volume, um commit que renomeia e "de passagem" corrige outra coisa é irrevisável.
Se a Fase 2 encontrar um uso de `runtime` que seja **ao mesmo tempo** nome de cluster e termo Go no
mesmo arquivo, isso é decisão de desenho, não detalhe de substituição — parar e levar ao sponsor.

**Resultado da medição (2026-08-09).** Dos três critérios de falsificação escritos antes de medir, **dois
dispararam**: o modelo de duas populações não fecha (3.257 ocorrências vivas em código não classificadas
por uma lista escrita às cegas), e o alvo `cell-1`/`cell-2` já está ocupado. O critério 1 não disparou —
o nome é escrito ~3.000 vezes, não derivado de poucas declarações. **O problema é real e está
confirmado; o mapa da metade `cell` foi refutado.** Correções aos números herdados: o alvo é 2.990, não
2.024; e o DoD-1 fecha em **147 arquivos**, não em zero, porque 42% dos 351 que o grep acusa hoje são
histórico-imutável que o próprio DoD manda preservar.

> Origem: análise de topologia feita no `loopzero-capsim` em 2026-08-08/09 durante o desenho da solução de
> CAPI — `../loopzero-capsim/docs/capi-cell-lifecycle-solution-design.md`.
> Registrado 2026-08-09 por `/backlog-item` (slug: `cluster-topology-naming`).

## B-002 — O cluster `mgmt` é SPOF de tudo, e renomeá-lo para `core` não muda isso   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
evidence: `.claude/knowledge-base/discoveries/opportunities/core-cluster-spof-opportunity.md` — medido em 2026-08-09 sobre a árvore `26ba1340`, rastreando cada função até o **sítio de chamada**, não até a linha de helmfile. As 4 funções **param** juntas, sem janela de degradação: login (`api/internal/config/config.go:257`, JWKS do Hydra que roda no `mgmt`), deploy e rollback (`config.go:377` Gitea + `config.go:381` ArgoCD, ambos no `mgmt`), build (a iniciação parte do `loopzero-api`; um build em voo continua no cluster de build). **Mas o plano de dados sobrevive:** Traefik, cert-manager, Argo Rollouts, KEDA e ESO vivem na cell (`runtime.helmfile.yaml.gotmpl:98`, `:76`, `:148`, `:123`, `:108`), e nenhum caminho de dados chama de volta o `mgmt`. A metade Model-B **continua não medida** — o código vive em `loopzero-cloud` e `loopzero-traefik-mcp`, fora deste registro.
why_now: o `B-001` renomeia este cluster para `core` justamente porque o nome `mgmt` faz todo mundo raciocinar sobre ele como infraestrutura de suporte descartável. O rename corrige o **raciocínio**; não corrige o **raio de explosão**. Registrar agora é o que impede que o rename seja lido como se o problema tivesse sido resolvido — o item existe para que a diferença fique explícita depois que `core` for o nome.
status: triaged
depends_on: B-001 apenas para vocabulário (o item continua válido com qualquer um dos dois nomes); tecnicamente independente — não bloqueia nem é bloqueado pelo rename
dod:
  - existe uma medição do raio de explosão real: para cada uma das 4 funções (login, deploy, build, rollback), o que degrada e o que para completamente quando o cluster fica indisponível
  - a dependência por-requisição dos produtos Model-B está confirmada ou refutada **com evidência**, e a fronteira de repositório dessa evidência está dita
  - o resultado é registrado onde uma decisão de arquitetura possa consumi-lo — não apenas neste bloco

**Resultado da medição (2026-08-09) — o título deste item está errado e fica corrigido.** Dos dois
critérios de falsificação, **um disparou**: o tráfego dos tenants **não** é afetado. O SPOF é **total e
do plano de controle** — o cluster é ponto único para *mudar* o sistema e irrelevante para *servi-lo*.
Isso é mais forte, não mais fraco, do que a redação original: quatro funções param juntas, sem
degradação parcial e sem janela; e agora se sabe o que **não** precisa ser protegido, o que torna uma
eventual mitigação bem mais barata do que a redação original sugeria. A redação exagerada era o risco
real do item — ela é falsa de um jeito verificável, e a primeira pessoa a verificar descontaria o item
inteiro. Ver D1 da opportunity.

**Próximo passo não é mitigar.** O que está medido é a topologia, não o custo da indisponibilidade.
Quanto tempo o plano de controle pode ficar fora sem consequência para o negócio é decisão de produto
que ninguém tomou, e projetar HA para 55 releases antes dela é dimensionar solução sem requisito.

> Registrado 2026-08-09 por `/backlog-item` (slug: `core-cluster-spof`).

## B-003 — O checker de ponteiros acusa de fabricação todo caminho iniciado por ponto   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: `.claude/skills/discover-confidence/scripts/check_evidence_pointers.py:30-32`. O regex é `\b((?:[A-Za-z0-9_.\-]+/)+[A-Za-z0-9_.\-]+\.[A-Za-z0-9]{1,10}):(\d+)`; o `\b` inicial não casa antes de um ponto, então `.claude/rules/current-constraint.md:26` é extraído como `claude/rules/current-constraint.md:26` — sem o ponto — e não resolve. Reproduzido em 2026-08-09 durante `/discover-confidence` de `B-001`: a opportunity recebeu `hard_caps_triggered: ['fabricated_evidence']` e verdict `INVALID` (49) apontando esse único ponteiro, com o arquivo existindo em disco e a linha 26 dentro dele. Na mesma execução notei que o regex exige ao menos uma `/`, então `Taskfile.yml:403` não é extraído nem verificado. **Medido em 2026-08-09 e reclassificado: não é defeito.** Tornar a barra opcional faz `example.com:8080` e `app-dev.loopzero.example:443` — que aparecem por todo o corpus deste projeto — serem extraídos como ponteiro de código e acusados de fabricação. Exigir a barra troca um falso negativo barato (ponteiro de raiz não verificado) por evitar um falso positivo caro no único cap que o golden rule chama de irrecuperável. É escolha conservadora correta e fica registrada como limite deliberado, não como dívida.
why_now: `fabricated_evidence` é o único cap que `discover-opportunity-golden-rule.md § 1` chama de irrecuperável, porque tudo a jusante trata ponteiro medido como fato. Um falso positivo nele ensina a contornar o gate mais importante do ciclo — e a saída natural (tirar o número de linha) **enfraquece a evidência** para satisfazer o checker. Encontrado ao medir `B-001`, não por auditoria do skill.
status: planned
resolved_in: 6d790d16 — fix + 2 testes (o de regressao e o que tranca o limite deliberado da barra)
dod:
  - um caminho iniciado por ponto (`.claude/rules/x.md:26`) é extraído **com** o ponto e verificado contra o disco
  - existe teste que falha no estado atual, escrito antes da correção
  - `example.com:8080` e `app-dev.loopzero.example:443` continuam **não** sendo extraídos — teste que trava o limite deliberado contra um afrouxamento futuro
  - a opportunity de `B-001` volta a citar `.claude/rules/current-constraint.md:26` com número de linha e o gate passa

> Registrado 2026-08-09 durante `/discover --mode review B-001` (slug: `evidence-pointer-dotpath`).

## B-004 — A fixture "boa" do discover-confidence não passa no layout em que o skill é instalado   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: `.claude/skills/discover-confidence/fixtures/good-opportunity.md` cita `rules/cycle-discover.md:20`, `rules/cycle-discover.md:150`, `skills/discover-confidence/scripts/check_corner_coverage.py:78` e `.../check_evidence_pointers.py:95` — caminhos do layout **standalone**. Neste repo (layout plugin) eles vivem sob `.claude/`, e `conftest.py::_find_project_root` resolve para a raiz do repositório, então nenhum dos quatro existe. Resultado: a fixture que representa uma opportunity **boa** pontua `INVALID` (49) com `hard_caps_triggered: ['fabricated_evidence']`, e `test_run_opportunity_score.py::test_good_opportunity_scores_shippable` falha. Verificado em 2026-08-09 com `git stash` do fix de `B-003` que **a falha é anterior** e independente dele.
why_now: encontrado ao rodar a suíte completa para provar não-regressão do fix de `B-003`. Uma suíte permanentemente vermelha treina o time a ignorar o vermelho — `CLAUDE.md § 7` chama isso de bug de prioridade máxima — e este vermelho fica em **todo repo consumidor** do skill, não só neste.
status: planned
resolved_in: ec0c5f74 — conftest passa a reescrever o prefixo conforme o layout; 36/36 verdes
dod:
  - `pytest .claude/skills/discover-confidence/tests` passa inteira nos dois layouts (standalone e plugin), sem marcar teste como skip
  - a fixture continua citando arquivos **reais** do kit — o teste é de integração e perde o sentido se virar hermético com arquivos sintéticos
  - existe teste que falha no layout plugin antes da correção

> Registrado 2026-08-09 durante `/discover --mode review B-001` (slug: `good-fixture-layout-dependent`).

## B-005 — Unificar cell e cluster em 1:1, aposentando `cell-default`   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: human
sponsor: CTO — decidido 2026-08-09
tier: T3 (`.claude/rules/pre-release-governance.md`) — toca schema, placement e identidade de cluster; ADR obrigatório e reunião **antes** de código
evidence: hoje a relação é **2:1** — `infra/helmfile/scripts/bootstrap-cell-cc.sh:20-22` mostra `cell-default` **e** `cell-1` apontando para o mesmo cluster `loopzero-workload`, com `cell-2` para `loopzero-runtime-2`. Pesquisa de 2026-08-09 sobre o papel do `cell-default`: `api/internal/placement/cells.go:30` o define como constante `DefaultCellID`, e o doc-comment em `cells.go:20-29` é explícito — é *"the canonical single-cluster cell used by dev-public and the standalone tilt-up environment"*, e *"Production multi-cluster paths derive cell identity from CrossClusterTokenReviewer, NOT this constant"*, com um MUST NOT para autorização cross-cell em produção. Ou seja: **não é uma cell de produção, é a cell do caminho de desenvolvimento**. Restrição de schema a respeitar: `api/internal/db/migrations/000006_cells.up.sql:40` impõe `CHECK (cell_id ~ '^cell-[a-z0-9-]+$')`, então qualquer id novo continua tendo de começar com `cell-`. O default da coluna vem de `000011_add_cell_id.up.sql:9` (`DEFAULT 'cell-default'`).
why_now: nasceu da medição do `B-001`. O mapa original do rename (`loopzero-workload` → `cell-1`) foi refutado porque criaria homônimo entre o nome do cluster e o de uma das duas cells que rodam nele. O CTO decidiu em 2026-08-09 pela saída de fundo: **eliminar a relação 2:1** e fazer cell↔cluster 1:1, o que torna `cell-N` um nome não-ambíguo para o cluster e alinha de vez a infra ao vocabulário que o código já usa. Sem isto, a metade `cell` do `B-001` fica bloqueada.
status: triaged
blocks: a metade `cell` do `B-001` (`loopzero-workload`/`loopzero-runtime-2` → `cell-N`). As metades `mgmt`→`core` e `vault`→`secrets` **não** dependem deste item e seguem em planejamento.
**Medição de 2026-08-10 (droplet app-dev), que fecha o primeiro bullet do DoD:** `select cell_id, count(*) from tenants group by cell_id` retorna **`cell-default | 94`** e mais nada — zero tenants em `cell-1`, zero em `cell-2`. E `select cell_id, status from cells` retorna as três como `active`, ou seja, a migration `000007_activate_cell_2` de fato ativou a cell-2. Consequência para o desenho: **a unificação não migra tenant nenhum entre cells** — os 94 estão todos na cell de compatibilidade, e o trabalho é reapontá-los para uma cell nomeada, não redistribuí-los. Isso muda o tamanho da migração de "mover 94 tenants" para "renomear a cell de 94 tenants", que são coisas diferentes na reunião T3.

dod:
  - ~~está medido quantos tenants estão hoje em cada cell~~ — **MEDIDO 2026-08-10: 94 em `cell-default`, 0 nas demais**; falta o que acontece com eles na unificação
  - o caminho dev-public/standalone continua funcionando sem a constante `DefaultCellID`, ou a substituição dela está explícita
  - `CHECK (cell_id ~ '^cell-[a-z0-9-]+$')` continua satisfeito pelos ids finais
  - existe ADR T3, com reunião antes de código, cobrindo schema + placement + ClusterCredential
  - depois disso, `B-001` consegue mapear cluster → `cell-N` sem homônimo

**Por que é `evolve` e não `bug`.** Nada está quebrado: a relação 2:1 funciona. O que se mede é o **custo do status quo** — ele impede que a infra use o vocabulário do domínio sem ambiguidade, e foi esse custo que apareceu ao tentar renomear.

**Resultado da medição (2026-08-09).** Ver `.claude/knowledge-base/discoveries/opportunities/cell-cluster-unification-opportunity.md` (SHIPPABLE 100/100). **A classificação T3 se sustenta, agora com razão medida em vez de precaução.** O critério 1 da falsificação disparou: `cell-default` **não é um valor, é uma sentinela**. `api/internal/db/migrations/000001_initial_schema.up.sql:66-74` define o trigger `populate_cell_id_from_tenant`, que trata `'cell-default'` como equivalente a `NULL` e significando *"ainda não resolvido, herde do tenant"* — e ele está armado em **4 tabelas** (`000001:456`, `000001:460`, `000004:91`, `000005:80`). Aposentá-la é mudar comportamento da camada de dados, não apagar uma linha.

Dois contextos que a medição trouxe: o repositório **já removeu** esse caminho uma vez (`api/internal/placement/engine.go:15-19`, CHG-2026-04-30-DELETE-SINGLE-CELL, *"unused in production paths"*) e a constante voltou em junho só para o broker do dev-public; e o 2:1 **não é só local** — `infra/helmfile/environments/drill.yaml:111-121`, que espelha a topologia de produção, também mapeia `cell-default` e `cell-1` para o mesmo cluster.

**A pauta da reunião T3 é concreta:** quem passa a portar o sentido de "não resolvido". `NULL` já é aceito pelo trigger, mas a coluna é `NOT NULL DEFAULT 'cell-default'` (`000011_add_cell_id.up.sql:9`) — as duas decisões vieram de momentos diferentes e hoje se sustentam mutuamente. E o **primeiro passo do plano é contar**, não editar: quantos tenants por cell, e quantas linhas nas 4 tabelas com `cell_id = 'cell-default'`. Isso não pôde ser medido porque nenhum cluster de tenants está de pé.

**A metade CLUSTER foi entregue em 2026-08-10; a metade CELL continua bloqueada, e agora com custo medido.**
Os clusters passaram a 1:1 com a célula que hospedam — `loopzero-workload`/`loopzero-runtime` → `loopzero-cell-0`,
`loopzero-runtime-2` → `loopzero-cell-1` —, guardado pela mesma prova usada em `B-001`: das **688** ocorrências
genéricas de `workload`/`runtime` congeladas antes da primeira edição, **0** foram renomeadas. Isso
desbloqueia o vocabulário sem tocar em identidade de cell.

A metade cell **não** foi aplicada, e a medição de hoje explica por quê — ela é pior do que "renomear
uma sentinela". Três fontes independentes mostram que **`cell-default` e `cell-1` são a mesma máquina
sob dois nomes**: `infra/helmfile/environments/dev.yaml:76,79` (`runtimeCellId: cell-1`, com o comentário
*"runtime cluster maps to cell-1"*), o seed em `000006_cells.up.sql:120-123` (*"Canonical first cell.
Maps to runtime-1"*) e a auditoria LIVE de 2026-05-01 em `docs/runbooks/bootstrap-regression-matrix.md:180`
(três cells `active`, ClusterCredential CRs para **duas**). Ou seja, é o mesmo defeito "duas palavras,
mesmo cluster" do `B-001`, uma camada abaixo — e a unificação é uma **fusão de identidades**, não um
rename.

Isso impõe uma **ordem obrigatória**: `cell-default` + `cell-1` → `cell-0` **antes** de `cell-2` → `cell-1`.
Uma substituição textual ingênua faria as duas ao mesmo tempo e colapsaria identidades distintas sem
deixar rastro — o motivo pelo qual esta metade não foi aplicada junto com a dos clusters. Medição que
barateia a migração: `git grep "REFERENCES cells"` em `api/internal/db/migrations` **não devolve nada**
— `cell_id` é `TEXT` livre nas 6 tabelas que o declaram, sem chave estrangeira, então renomear é um
conjunto de `UPDATE`s e não exige janela de indisponibilidade. Ambas as medições estão no ADR
`docs/decisions/2026-08-09-cell-cluster-unification-adr.md`, que é a pauta da reunião T3.

**Inconsistências de nome encontradas de passagem, e o que foi feito com cada uma:**

| O que | Estado |
|---|---|
| `task clusters:recreate` derivava o nome do cluster do rótulo, não do config — apagava cluster inexistente e criava outro, anunciando sucesso | **corrigido** (`taskfiles/k3d.yml`) |
| `kubeContexts.mgmt` sobreviveu à fase `mgmt`→`core` do `B-001` | **corrigido** → `kubeContexts.core` |
| `kubeconfig.runtime` alimentava `CELL_0_KUBECONFIG` — chave e variável diziam coisas diferentes | **corrigido** → `kubeconfig.cell0` |
| `secrets.runtimeKubeconfig` idem, pego pelo teste de contrato do chart | **corrigido** → `secrets.cell0Kubeconfig` |
| ponto de autenticação do OpenBao ainda em `auth/k8s-mgmt` | **corrigido** → `auth/k8s-core` (o ADR já é ACEITO para `core`) |
| `auth/k8s-cell-default`, `auth/k8s-cell-1`, `auth/k8s-cell-2` | **anotado, não tocado** — dependem desta fusão |
| output de stack Pulumi `runtimeKubeconfig` | **anotado, não tocado** — contrato externo vivo, e o Pulumi é legado AS-IS até o corte AWS |

> Registrado 2026-08-09 a partir da decisão do CTO sobre D2 da opportunity de `B-001` (slug: `cell-cluster-unification`).

## B-006 — O oráculo do conjunto de gates vive num arquivo com data no nome, e é mutado a cada gate   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: `cmd/loopzero-ops/quality_registry_test.go:700` fixa o oráculo do `TestOracle_MatchesFrozenBaseline` em `docs/operations/baselines/2026-07-02-m9-gateset.before`, e o doc-comment em `:697-698` instrui *"Re-freeze the baseline via `loopzero-ops quality --list` when gates are consciously added/wired"*. Mas `docs/README.md:16` classifica `operations/baselines/` como **audit trail — "History. Preserved indefinitely. Never edited to match present state"**. Encontrado em 2026-08-09 ao adicionar o gate `cluster-rename-guard` (B-001 T0.1): o teste falhou com `got 116 lines, baseline 115`, e a correção que ele mesmo instrui exige editar um registro que a regra manda preservar.
why_now: as duas regras se contradizem hoje, e a contradição foi exercida — não é teórica. O nome do arquivo (`2026-07-02-...before`) afirma ser o retrato do conjunto de gates naquela data; ele já não é, porque cada gate novo o reescreve. Quem ler `.before` esperando o estado de julho encontra o de agosto, e a data no nome torna isso especialmente enganoso.
status: planned
resolved_in: o oráculo passou para `cmd/loopzero-ops/testdata/gateset.frozen`, que declara ser vivo; o registro datado voltou ao conteúdo do commit 46c0a72d (70 gates); e a ausência do oráculo deixou de ser skip — virou falha, porque mover o caminho fez o teste passar por não rodar
dod:
  - o oráculo do conjunto de gates vive num caminho que declara ser vivo, não num com data no nome
  - `docs/operations/baselines/2026-07-02-m9-gateset.before` volta a conter o que continha em 2026-07-02, ou é explicitamente redesignado
  - `TestOracle_MatchesFrozenBaseline` continua pegando um gate des-ligado em silêncio — a proteção não pode ser perdida na mudança

> Registrado 2026-08-09 durante a implementação de `B-001` T1.1 (slug: `gateset-oracle-in-dated-baseline`).

## B-007 — 4,3 MB de snapshot de helmfile que ninguém gera nem consome   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: `infra/tests/snapshots/helmfile-drill-phase1.yaml` (2,1 MB) e `helmfile-standalone-multi-phase1.yaml` (2,2 MB). Medido em 2026-08-09: `git grep -l "infra/tests/snapshots"` retorna **apenas documentos históricos** (`docs/plans/completed/2026-05-20-*`, `docs/scripts-audit/2026-05-2*`, `docs/wiki/*`) — nenhum teste, gate, task ou script os lê, e nenhum os regenera. As duas menções em `cmd/loopzero-ops/internal/quality/{anonymousauth,danglingdata}/check.go` são padrões genéricos de exclusão de diretório (`"/snapshots/"`), não consumo destes arquivos.
why_now: encontrado ao executar o T4.1 do plano de rename, cujo DoD manda "regenerar os snapshots, com diff só de renomeação". Não há por onde regenerar, e não há quem consuma — então o critério, como escrito, não é executável. Eles foram corretamente deixados intocados pelo rename (são artefato gerado, e editá-los à mão faria o arquivo passar no teste mentindo sobre o sistema), mas isso deixa neles nomes de cluster que já não existem.
status: planned
resolved_in: removidos. A medição fechou a decisão do DoD: o plano que os criou (`docs/plans/completed/2026-05-20-infra-standardization-and-secrets-plan.md:686-700`) previa **cinco** snapshots `-template.yaml` mais um teste comparador; o que existe são **dois** arquivos `-phase1` e nenhum teste. Eram artefato de uma comparação pontual da Fase 1 em maio de 2026, nunca ligada a nada — 4 commits os tocaram, o último uma varredura de deleção em massa. `git log` preserva o conteúdo.
dod:
  - está decidido se os dois arquivos têm consumidor: ou o comando que os regenera é encontrado e documentado, ou eles são removidos
  - se removidos, `git log` preserva o conteúdo e a remoção cita esta medição
  - o DoD-5 do plano de rename é corrigido para refletir a decisão

> Registrado 2026-08-09 durante a execução de `B-001` T4.1 (slug: `orphaned-helmfile-snapshots`).

## B-008 — `task test:unit` está vermelho por drift entre repositórios   [x]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: `task test:unit` sai **201**. Um único teste falha: `api/internal/contract/dashboard_consumer_test.go:159` — *"dashboard/src calls 1 engine path(s) not in testdata/dashboard-consumed-paths.txt (add to the list + engine.yaml): [/api/v1/auth/whoami]"*. O teste caminha por `dashboardSrcDir = "../../../../loopzero-cloud/dashboard/src"` (`:32`), ou seja, lê o **repo irmão**. E o dashboard de fato passou a chamar o caminho: `../loopzero-cloud/dashboard/src/hooks/settings/use-whoami.ts:22` faz `engineFetch<Whoami>('/api/v1/auth/whoami')`. A lista deste repo tem 13 entradas e não inclui essa. Medido em 2026-08-09.
why_now: encontrado ao rodar o checklist de pré-conclusão do próprio repo (`CLAUDE.md § Pre-completion checklist`) depois do rename de clusters. **Não é do rename**: `git diff 26ba1340..HEAD --name-only` não toca `api/internal/contract/` nem o `testdata/`, e o dashboard vive em `loopzero-cloud`, repositório que esta sessão não abriu. É drift pré-existente que estava mascarado porque `task test:unit` não vinha sendo rodado inteiro.
status: raw
dod:
  - está decidido se `/api/v1/auth/whoami` **deve** ser consumido pelo dashboard — a lista é contrato, não formalidade, e adicionar a linha sem responder isso transforma o gate em carimbo
  - se deve: a entrada existe em `testdata/dashboard-consumed-paths.txt` **e** em `engine.yaml`, como a própria mensagem do teste exige
  - `task test:unit` volta a sair 0

**Por que não corrigi na hora.** Acrescentar `/api/v1/auth/whoami` à lista faria o vermelho sumir em uma linha. Mas a lista é um **contrato de consumo entre dois repositórios**, e a pergunta que ela existe para forçar é se aquele caminho deve mesmo ser consumido dali — decisão que não é minha e que o commit de uma linha esconderia. É o mesmo padrão do `B-006`: o barato faz o gate calar, não acertar.

> Registrado 2026-08-09 ao rodar o checklist pós-rename (slug: `dashboard-consumed-paths-drift`).

**RESOLVIDO em 2026-08-10 — era falso positivo do próprio guarda, não drift.** O dashboard **não chama** `/api/v1/auth/whoami` em nenhum código de produção. Os três hits no repo irmão eram um mock (`src/lib/mocks/handlers.ts:91`), um teste (`src/build/sw-runtime-caching.test.ts:119`) e, o que sobrevivia ao filtro, **um comentário**: `src/main.tsx:41` documenta em prosa uma medição de 2026-08-07 listando sete requisições que escapavam do interceptador, e cita a rota. `isDashboardTestFixture` já excluía mock e teste **por arquivo**, mas o extrator rodava `enginePrefixedRE.FindAllString` sobre o arquivo inteiro, sem remover comentários — então prosa virava chamada.

Corrigido com `stripComments` aplicado antes do regex, e teste de mesa cobrindo os cinco casos (chamada real, comentário de linha, bloco, bloco multilinha, código depois de bloco). **Adicionar a rota à lista teria sido pior que o falso positivo**: afirmaria um consumo que não existe, e o contrato passaria a mentir na direção que ninguém verifica. O comentário que descreve um bug não é o bug.

## B-009 — `task lint` está vermelho em 11 achados, nenhum deles novo   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: `task lint` sai **201** com 11 achados, todos em `operators/`: 2 de `gosec G706` (log injection via taint analysis, em `operators/internal/reconcilehelpers/pprof.go:34` e `operators/internal/tracing/tracing.go:99`) e o restante de `staticcheck ST1000` (pacote sem comentário de pacote, em `operators/internal/environment/*.go` e vizinhos). **Nenhum dos 11 arquivos foi tocado nesta sessão** — `comm -12` entre a lista de arquivos acusados e `git diff --name-only 26ba1340..HEAD` sai vazia. Medido em 2026-08-09.
why_now: encontrado no mesmo checklist que revelou o `B-008`. Vale registrar junto porque os dois contam a mesma história: dois comandos do `CLAUDE.md § Pre-completion checklist` — `task test:unit` e `task lint` — estão vermelhos há tempo indeterminado, e nenhum dos dois vermelhos é recente. Uma checklist que ninguém roda inteira deixa de ser checklist.
status: raw
dod:
  - os 2 achados de `gosec G706` estão corrigidos ou allowlistados com sunset, conforme `code-quality-allowlist.txt`
  - os achados de `ST1000` estão resolvidos — é comentário de pacote, o custo é baixo
  - `task lint` volta a sair 0
  - está decidido o que impede os dois vermelhos de voltarem: hoje nada os pega antes de alguém rodar o checklist à mão

**Nota sobre severidade.** `G706` é o único dos dois que merece leitura, não correção mecânica: log injection depende de a entrada ser realmente controlável por terceiro. Allowlistar sem ler é o mesmo erro que o `B-008` descreve — fazer o gate calar em vez de acertar.

> Registrado 2026-08-09 ao rodar o checklist pós-rename (slug: `lint-red-preexisting`).

## B-010 — A borda do app-dev alcançava o loopzero-cloud por um port-forward que reiniciava 1,8×/min   [ ]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: discover-live-test
evidence: medido no droplet `loopzero-e2e-runner` (165.227.121.20) em 2026-08-10. `https://app-dev.loopzero.example/engine/api/v1/health` devolveu **502** na primeira sonda e **401 AUTH_REQUIRED** (correto) minutos depois — intermitência, não quebra. Causa: `kubectl --context k3d-loopzero-mgmt -n loopzero-system get svc loopzero-cloud` era `ClusterIP`, então a borda Caddy só o alcançava por `kubectl port-forward`. A unit `loopzero-cloud-edge-13002.service` marcava **`NRestarts=126499`** com 43.476 eventos de journal em 24h — ~1,8 reinícios por minuto, e cada um é uma janela de 502 em `/engine/*`, que carrega deploy, whoami, listar projetos e status de build. O irmão `loopzero-api` nunca teve o problema: é `NodePort` publicado pelo serverlb do k3d (`docker port k3d-loopzero-mgmt-serverlb` → `30001 → 13001`), mapeamento docker comum. A unit irmã `loopzero-cloud-dataplane-authz.service` marcava `NRestarts=3`, o que isola o defeito a esta.
why_now: encontrado ao diagnosticar o app-dev sob o mandato de deixá-lo 100% funcional. Não aparecia como "quebrado" em nenhuma verificação de pod — os 5 clusters estavam saudáveis e o dashboard devolvia 200. A intermitência só aparece sondando a superfície que os usuários usam, repetidamente.
status: planned
resolved_in: 05b3ae08 — chart passa a suportar `service.nodePort` (condicional) e dev-public fixa 30002, escolhida porque o serverlb já a publica como host 3002 e estava livre. Aplicado no vivo por `kubectl patch`, Caddy repontado de `localhost:13002` para `localhost:3002` (9 referências), unit parada e desabilitada.
dod:
  - `loopzero-cloud` renderiza como NodePort 30002 em dev-public — travado por `infra/tests/charts/loopzero_cloud_edge_nodeport_test.go`
  - o template continua servindo ClusterIP sem emitir `nodePort` — travado pelo segundo teste do mesmo arquivo
  - 10 sondas consecutivas em `/engine/api/v1/health` devolvem 401, não 502 — **verificado 10/10 em 2026-08-10**
  - a unit `loopzero-cloud-edge-13002.service` está parada e desabilitada — **verificado**

**Por que o chart e não só o patch.** O `kubectl patch` sozinho seria revertido no próximo `helm upgrade` — e aí seria **pior que antes**, porque o port-forward que servia de rede de proteção já não existe. A correção durável não é zelo: é o que impede o conserto de virar uma armadilha.

> Registrado 2026-08-10 durante o diagnóstico do app-dev (slug: `app-dev-edge-portforward`).

## B-011 — O reconciliador do loopzero-cloud reiniciava o deployment a cada 5 minutos, sempre   [ ]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: discover-live-test
evidence: medido no droplet `loopzero-e2e-runner` em 2026-08-10. `kubectl get rs -l app=loopzero-cloud` mostrava **4 ReplicaSets em 19 minutos** (19m, 14m, 8m59s, 3m48s), e o diff entre dois consecutivos tinha **uma única diferença**: a anotação `kubectl.kubernetes.io/restartedAt` — a assinatura de `kubectl rollout restart`. A origem é `loopzero-cloud-reconcile.timer` (a cada 5 min) → `~/.local/bin/loopzero-cloud-reconcile.sh`, que chamava `rollout restart deploy/loopzero-cloud` **incondicionalmente**. O efeito não ficava no loopzero-cloud: `kubectl port-forward` se prende a um POD, então cada rollout matava os forwards da borda — `loopzero-cloud-edge-13002.service` com `NRestarts=126499` e `loopzero-pf-loopzero-cloud-bff.service` com `72163`. Cada morte é uma janela de 502 em `/engine/*`. Este é o **causador** do `B-010`.
why_now: encontrado ao perseguir a intermitência do `B-010` até a origem, em vez de parar no sintoma. Nenhuma verificação de pod acusaria: os pods estavam todos `Running`, só que sempre recém-nascidos.
status: planned
resolved_in: `loopzero-workspace@526f608` — o script passa a comparar o digest da imagem local com o do pod **antes** de reiniciar, e dispensa o rollout quando são iguais. Ele já calculava `before`/`after` para decidir `changed`; só comparava depois de reiniciar, quando a informação já não servia. Digest ausente de qualquer lado reinicia, deliberadamente o comportamento antigo. Aplicado também no fonte real do droplet (`/home/paulo/loopzero-ops/dev/loopzero-cloud/reconcile.sh`).
dod:
  - o log diz `BFF: imagem em dia (sha256:...)` quando nada mudou — **verificado 2026-08-10**
  - o pod não troca de nome entre duas execuções do reconcile — **verificado: mesmo pod, 11 ReplicaSets antes e depois**
  - as duas units de port-forward do loopzero-cloud estão paradas e desabilitadas — **verificado**

## B-012 — A imagem do MCP do loopzero-promptly nunca constrói os pacotes que ela executa   [ ]

domain: data-plane-ts
repo: loopzero-promptly
suggested_mode: bug
source: discover-live-test
evidence: `docker ps` no droplet mostra `loopzeropromptly_mcp` em `Restarting` — crash loop. `docker logs` dá `ERR_MODULE_NOT_FOUND` para `file:///app/node_modules/@loopzero/promptly-template/dist/index.js`. Causa: `loopzero-promptly/Dockerfile.mcp` faz `COPY . .` e vai direto a `pnpm deploy --filter ./packages/mcp --prod /app`, **sem nenhum passo de build**. `packages/template` e `packages/sdk` declaram `exports: {".": {"default": "./dist/index.js"}}`, então resolvem para um `dist/` que nada produz; os outros três exportam de `./src/*.ts` e são resolvidos pelo tsx em runtime, que é por que a falha atinge só esses dois. **O Dockerfile irmão já estava certo:** `loopzero-promptly/Dockerfile:61` tem `RUN pnpm run build` com o comentário explicando *"O build roda ANTES do `pnpm deploy` porque este stage ainda tem as devDependencies"* — a segunda imagem copiou o padrão e perdeu o passo.
why_now: encontrado na varredura de containers do droplet sob o mandato de deixá-lo funcional.
severity: **baixa — o container é órfão, e isso está medido.** Eu havia afirmado que ele era "a única superfície ainda degradada"; **estava errado, e a medição corrige**. Ele (a) não publica porta nenhuma (`docker port loopzeropromptly_mcp` vazio), (b) não é destino de rota no Traefik da borda MCP — os 10 `url:` do `dynamic.yml` apontam para `host.docker.internal:180XX`, `loopzero-mcp-gateway`, `loopzero-cloud`, `kratos` e `dashboard`, nenhum para ele — e (c) **não está no mapa de upstreams do gateway MCP**, que declara só `{"memory": ..., "rag": ...}`. A superfície pública `/v1/promptly/*` é servida pelo `loopzeropromptly_app` em `host.docker.internal:18083`, que está `Up (healthy)` e responde 401 como as demais. O crash-loop não degrada nada que alguém consuma.
status: planned
resolved_in: `loopzero-promptly@aa681dc` — `Dockerfile.mcp` ganha `RUN pnpm -r build` antes do `pnpm deploy`, espelhando o que `Dockerfile:61` já fazia na imagem irmã.
verification: **provado por execução no droplet em 2026-08-10.** A construção local falhou duas vezes por `ECONNRESET` no registry (rede desta máquina, não a mudança), então clonei o repo no droplet, apliquei o mesmo `Dockerfile.mcp` e construí lá: `EXIT=0`, e `docker run` confirmou que `/app/node_modules/@loopzero/promptly-template/dist/index.js` **existe** na imagem — o arquivo exato que faltava. Recriando o container com ela, `loopzeropromptly_mcp` ficou **`Up (healthy)`**.
resolution_live: **resolvido no droplet em 2026-08-10.** `docker-compose.deploy.yml` ganhou `pull_policy: never` **apenas** para `promptly_mcp`, com comentário datado, a razão e o gatilho de remoção no próprio arquivo. Verificado: o container fica `Up (healthy)` **e sobrevive a uma execução do reconciliador** — antes voltava a `Restarting` em menos de 45s. Rodando a imagem construída de `loopzero-promptly@aa681dc`, ou seja, o código corrigido.
pin_sunset: **devolver a `pull_policy: always` assim que `loopzero-promptly@aa681dc` chegar em `develop` e o CI publicar.** Enquanto o pin existir, esta máquina não acompanha novas publicações do promptly-mcp. Registrado aqui e no arquivo.
was_blocked_by: **a objeção que eu mesmo levantei, e revi.** `docker-compose.deploy.yml:141` declara `pull_policy: always` para o serviço, então o `compose up -d` do reconciliador puxa a imagem ainda quebrada do GHCR e o container volta a `Restarting` — verificado: voltou em menos de 45s. A correção só se fixa quando o CI publicar a imagem, o que exige a promoção `workspace → develop` por PR. **Deliberadamente não contornei**: mascarar o timer ou trocar a `pull_policy` trocaria um crash-loop visível por uma imagem velha em silêncio, que é o incidente que os comentários do próprio reconciliador registram ("o BFF serviu o dia inteiro a revisao de um PR antigo").
dod:
  - `Dockerfile.mcp` constrói os pacotes de workspace antes do `pnpm deploy`
  - a imagem construída resolve `@loopzero/promptly-template` sem `ERR_MODULE_NOT_FOUND`
  - `loopzeropromptly_mcp` fica `Up (healthy)` no droplet, não `Restarting`

> B-011 e B-012 registrados 2026-08-10 durante o diagnóstico do app-dev.

## B-013 — O nó agente da cell-2 está NotReady desde 2026-07-23 por token não autorizado   [ ]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: discover-live-test
evidence: medido no droplet em 2026-08-10. `kubectl --context k3d-loopzero-runtime-2 get nodes` mostra `k3d-loopzero-runtime-2-agent-0` **`NotReady`**, com `Kubelet stopped posting node status` e `lastHeartbeatTime = 2026-07-23T14:36:39Z` — 18 dias parado. O container Docker está **`Up 2 semanas`**: o processo vive, o k3s dentro dele não. O log daquele dia registra a morte: `Shutdown request received: failed to start networking: unable to initialize network policy controller: error getting node subnet: failed to find interface with specified node ip` — deriva de IP na rede Docker. Reiniciei o container (2026-08-10 03:15): o k3s **inicia** (`Starting k3s agent v1.32.13+k3s1`) e então trava em `Waiting to retrieve agent configuration; server is not ready: failed to retrieve configuration from server: **not authorized**` — o token do agente já não bate com o do servidor. Consequência colateral: os 6 pods em `Terminating` há 18-28 dias estão todos nesse nó, e um nó NotReady nunca confirma deleção; todos têm substituto `Running` no nó servidor, então não há degradação de serviço por causa deles.
why_now: encontrado ao perseguir os 6 pods `Terminating` até a origem, em vez de forçar a remoção deles. Forçar teria escondido o nó morto.
status: raw
severity: baixa — `select cell_id, count(*) from tenants` retorna 94 em `cell-default` e zero em `cell-2`.
status_live: **RESOLVIDO em 2026-08-10.** Token e CA batiam, o servidor estava `readyz=ok` e não registrava recusa; a senha de nó é guardada como `hash`, então não dava para alinhar por patch. Em vez de seguir escavando internals num nó com zero tenants, apliquei o caminho canônico para agente que não re-registra: `k3d node delete` do agente morto + `k3d node create` do substituto. O novo (`k3d-agent-0-0`) subiu **`Ready`**. Removido também o objeto Node órfão — lápide sem container — e, aí sim, os 6 pods `Terminating` foram removidos com `--force`: **com a causa raiz corrigida, forçar já não escondia nada**, que era a objeção original. Resultado: `loopzero-runtime-2` com 2 nós `Ready` e **0 pods problemáticos** de 14.
dod:
  - `k3d-loopzero-runtime-2-agent-0` volta a `Ready` e o kubelet reporta heartbeat atual
  - os 6 pods `Terminating` desaparecem sozinhos, sem `--force` — se sumirem só à força, o nó não foi consertado
  - está decidido se o k3d ganha um passo canônico de re-registro de agente, ou se o caminho é recriar o nó

**Por que não improvisei.** `infra/scripts/local/k3d-cross-cluster-heal.sh` cobre H1–H7 — rede, `/etc/hosts`, RBAC, Secrets de kubeconfig — e **nenhum** trata token de nó. Sem caminho canônico, recriar um nó num cluster vivo para restaurar capacidade que ninguém usa é trocar um defeito conhecido por um risco desconhecido. Com o impacto medido em zero tenants, registrar é a decisão mais defensável que agir.

> Registrado 2026-08-10 durante o diagnóstico do app-dev (slug: `cell2-agent-node-unauthorized`).


## B-014 — `TestTransitClient_RateLimitBurst` reprova por tempo, não por comportamento   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-10 durante a suíte completa do módulo `pkg`. `pkg/vault/transit_client_test.go:622-625` reprovou com `allowed: got 11 want 10 (burst capacity)` e `rate limited: got 4 want 5 (N - burst)` — o balde deixou passar uma requisição a mais do que a capacidade de rajada. Reexecutado isoladamente **5 vezes seguidas: 5 PASS**. O arquivo do teste não contém a palavra `cell` nenhuma vez (`grep -c cell` = 0), então não tem relação com a renomeação que estava em curso; o gatilho é a suíte inteira rodando em paralelo, que muda o tempo de parede entre a criação do balde e a rajada.
why_now: apareceu ao rodar a suíte completa dos quatro módulos de uma vez, o que raramente é feito — a execução por pacote esconde a corrida. Um teste que reprova conforme a carga da máquina reprova em CI compartilhada, e o custo é a suíte inteira perder credibilidade: quem vê vermelho intermitente passa a reexecutar em vez de investigar, e o próximo defeito real vira ruído.
status: raw
severity: média — não há defeito de produto, mas `.claude/rules/testing.md` § 3 é explícito: *"Tests MUST be deterministic. Flaky tests are bugs — fix or delete."*
dod:
  - o teste afirma a capacidade de rajada sem depender do relógio de parede — relógio injetado, ou asserção sobre a decisão do limitador em vez da contagem observada
  - roda 50 vezes seguidas com `-count=50` em verde, e também com a suíte completa do módulo em paralelo
  - se a conclusão for que a rajada não é testável de forma determinística nesta forma, o teste é removido e o que ele protegia fica dito explicitamente

> Registrado 2026-08-10 ao rodar a suíte completa durante a unificação cell↔cluster (`B-005`).

## B-015 — Dois charts apontam para versões que não existem mais no upstream   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-live-test
evidence: medido no droplet app-dev em 2026-08-10, durante a reconstrução total. Ao vendorizar as dependências de todos os charts, dois falharam contra o upstream: `infra/helm/k6-operator/` — *"can't get a valid version for 1 subchart(s): \"k6-operator\" (repository https://grafana.github.io/helm-charts, version 3.10.0)"* — e `infra/helm/keda-http-addon/` — *"http-add-on chart not found in repo https://kedacore.github.io/charts"*. Os outros 16 charts com dependências vendorizaram sem erro. Nenhum dos dois está no caminho de `task setup` (a instalação completou com eles falhando), então o impacto hoje é potencial, não atual.
why_now: apareceu porque a reconstrução a partir de clone limpo é a única situação que exercita `helm dependency build` em todos os charts de uma vez — o `B-015` é filho direto do defeito que o `9918556b` corrigiu. Enquanto ninguém instalava do zero, as duas quebras ficaram invisíveis. Elas mordem no dia em que alguém precisar de k6 ou do KEDA HTTP add-on, que é exatamente um dia em que já se está resolvendo outro problema.
status: raw
severity: baixa — nada instalado hoje depende deles; alta no dia em que depender, porque a falha aparece no meio de uma instalação e não no momento em que a versão sumiu do upstream.
dod:
  - cada um dos dois tem versão (ou nome de chart) válida contra o repositório upstream atual, ou está declarado como removido com a razão
  - `task validate:helm-deps` termina com `0 com upstream indisponível`
  - está decidido se um chart cuja dependência remota some deve reprovar a instalação ou apenas avisar — hoje avisa, e a razão está escrita na própria task

> Registrado 2026-08-10 durante a reconstrução total do app-dev (`B-001`, `B-005`).
