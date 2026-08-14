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
status: shipped
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
status: planned
resolved_in: os 11 achados de `operators/` estao em ZERO. Os 2 `gosec G706` viraram VALIDACAO NA FRONTEIRA — `LOOPZERO_PPROF_ADDR` e `OTEL_EXPORTER_OTLP_ENDPOINT` passam por `net.SplitHostPort` e uma variavel malformada agora falha alto em vez de ser logada e usada; a taint analysis do gosec nao enxerga validacao (tentei logar a string crua e depois os componentes validados — acusa identico), entao ficou `//nolint:gosec` com a razao escrita, que e a convencao local (`operators/internal/constants/labels.go` ja a usa). Os `ST1000` viraram comentarios de pacote reais: `reconcilehelpers` dizia `Package shared` (nome anterior ao split de binarios), `platform` tinha comentario de TIPO colado na clausula, e `environment` nao tinha nenhum. Mais QF1008 (campo embutido redundante) e a funcao morta `safeName`. `golangci-lint run ./...` em `operators` sai **0 issues**.
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

## B-013 — O nó agente da cell-2 está NotReady desde 2026-07-23 por token não autorizado   [x]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: discover-live-test
evidence: medido no droplet em 2026-08-10. `kubectl --context k3d-loopzero-runtime-2 get nodes` mostra `k3d-loopzero-runtime-2-agent-0` **`NotReady`**, com `Kubelet stopped posting node status` e `lastHeartbeatTime = 2026-07-23T14:36:39Z` — 18 dias parado. O container Docker está **`Up 2 semanas`**: o processo vive, o k3s dentro dele não. O log daquele dia registra a morte: `Shutdown request received: failed to start networking: unable to initialize network policy controller: error getting node subnet: failed to find interface with specified node ip` — deriva de IP na rede Docker. Reiniciei o container (2026-08-10 03:15): o k3s **inicia** (`Starting k3s agent v1.32.13+k3s1`) e então trava em `Waiting to retrieve agent configuration; server is not ready: failed to retrieve configuration from server: **not authorized**` — o token do agente já não bate com o do servidor. Consequência colateral: os 6 pods em `Terminating` há 18-28 dias estão todos nesse nó, e um nó NotReady nunca confirma deleção; todos têm substituto `Running` no nó servidor, então não há degradação de serviço por causa deles.
why_now: encontrado ao perseguir os 6 pods `Terminating` até a origem, em vez de forçar a remoção deles. Forçar teria escondido o nó morto.
status: shipped
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

## B-016 — O token que puxa as imagens de plataforma do GHCR está inválido   [ ]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: discover-live-test
evidence: medido no droplet app-dev em 2026-08-10, após a reconstrução total. Quatro componentes do cluster `loopzero-core` ficam em `ImagePullBackOff`: `loopzero-api`, `loopzero-cloud`, `loopzero-app-operator` e `loopzero-platform-operator` (13 pods problemáticos de 69; os clusters `loopzero-build`, `loopzero-cell-0` e `loopzero-cell-1` estão 100% limpos). O `dev-public` os puxa de `ghcr.io/getloopscale/loopzero-api:develop` com o `imagePullSecret` `ghcr-pull`, cuja fonte canônica é `~/.loopzero/ghcr-pull-token.txt` (`taskfiles/infra.yml:985,993`). O arquivo existe, tem 40 bytes e data de **2026-06-19**; testado direto contra o registro devolve **`HTTP 401`** para `develop`, `latest` e `main`. O Secret no cluster carrega o mesmo token (usuário `getloopscale`) e falha igual. `docker pull` sem credencial devolve `403`.
why_now: a condição é anterior à reconstrução — o achado de 2026-08-09 já registrava GHCR inacessível a partir do droplet. O que mudou é que o cache local de imagens mascarava o problema: os pods rodavam com camadas puxadas meses atrás. `k3d cluster delete` levou o cache junto, e a dependência ficou exposta. **Não é regressão do rename**; é uma dependência que só aparece quando o ambiente sobe do zero, que é exatamente o que o mandato de cleanroom existe para forçar.
status: triaged
severity: alta — sem isto, `app-dev.loopzero.example` não volta a servir. É o único item que separa a plataforma reconstruída de estar funcional.
**RESOLVIDO em 2026-08-10 — e a afirmação original estava errada.** Eu havia escrito que não existia caminho técnico sem a credencial. Existe: `loopzero-api` e os quatro operators têm Dockerfile **neste** repositório, e o `loopzero-cloud` tem no repo irmão. As seis imagens foram construídas localmente e publicadas no registro do k3d, e `LOOPZERO_PLATFORM_IMAGE_PREFIX` (commit `6b4669dd`, padrão inalterado no GHCR) passou a permitir apontar o ambiente para elas. O token do GHCR continua inválido — o que mudou é que ele deixou de ser condição para o ambiente subir.
dod:
  - `~/.loopzero/ghcr-pull-token.txt` contém um token que devolve `HTTP 200` para `ghcr.io/v2/getloopscale/loopzero-api/manifests/develop`
  - `task images:ghcr:pullsecret` reprovisiona o Secret nos 4 clusters e os 4 deployments saem de `ImagePullBackOff`
  - está decidido se o dev-public continua dependendo de imagem publicada ou se ganha um caminho de build local — hoje `task images:build` produz os operators no registry k3d, mas **não** `loopzero-api` nem `loopzero-cloud`, então o caminho local não existe inteiro
  - se as imagens não estiverem publicadas no GHCR (o `401` não distingue ausente de não-autorizado), isso fica dito, porque muda o conserto de "renovar token" para "publicar imagem"

> Registrado 2026-08-10 durante a reconstrução total do app-dev (`B-001`, `B-005`).

> **Balas 3 e 4 fechadas em 2026-08-14, medindo o lado da publicação — e as duas responderam ao
> contrário do que o item supunha.**
>
> **Bala 4 — as imagens ESTÃO publicadas.** O `401` é token, não ausência. `release-artifacts.yml`
> (gatilho `push` em `develop` e tag `v*`) chama `build-publish.yml` via `workflow_call`, e a última
> execução com sucesso é de **2026-08-09**, com cinco jobs verdes: `loopzero-api` e os quatro operators.
> Isto mantém o conserto em "renovar token" e **valida o pedido do PAT** em vez de o redirecionar.
>
> Duas leituras minhas antes disso estavam erradas, pela mesma razão: **saída truncada lida como
> completa.** `gh run list --workflow=build-publish.yml` veio vazio e eu li como "nunca roda" —
> execução chamada por `workflow_call` aparece sob o **chamador**, não sob o chamado. E o grep que
> procurava o chamador foi cortado em `head -3`, escondendo `release-artifacts.yml:103`.
>
> **Bala 3 — o caminho local existe, e o texto desta bala sobre ele estava errado.** `task
> images:build` não produz os operators: é composto de três imagens de **ferramenta**
> (`clone-runner`, `syft-scanner`, `cosign-signer`). Quem constrói aplicação localmente é o
> **Tiltfile** — cinco `docker_build`: `loopzero-api` e os quatro operators. `loopzero-cloud` não tem caminho
> local aqui, porque nasce no repositório irmão. O caminho local cobre **5 de 6** imagens, e a lacuna
> passa a ser nomeada em vez de suposta.
>
> A decisão que a bala pede — depender de imagem publicada ou adotar build local — fica com o
> patrocinador, agora com as duas opções medidas.

> **Re-medido no droplet 2026-08-14, com acesso SSH concedido pelo patrocinador.** A causa está
> **viva**; o sintoma é que não.
>
> Os cinco clusters estão limpos — **158 pods, zero fora de `Running`**. Medir só isso teria matado o
> item. A causa, medida direto: `~/.loopzero/ghcr-pull-token.txt` continua com **40 bytes** e data de
> **2026-06-19**, e devolve `HTTP 401` em `develop`, `latest` e `main`. O Secret `ghcr-pull` do
> `loopzero-core` carrega **o mesmo token** — confirmado por hash, sem revelar o valor. O pacote é privado
> (401 também sem credencial).
>
> Por que o sintoma sumiu: os pods rodam de camadas já baixadas. O defeito reaparece **no próximo
> cleanroom**, que é exatamente o que o item previu.
>
> **Bloqueado em credencial que só o patrocinador emite** — um PAT do GitHub com `read:packages`. Nada
> mais no item depende de mim.
>
> Nota de método: minha primeira sonda do Secret leu os campos `username`/`password` e concluiu "Secret
> em branco". Aquele `dockerconfigjson` só tem o campo `auth` (base64 de `user:pass`) — eu quase
> reportei um achado falso a partir de um campo que não existe naquele formato.

## B-017 — O `Dockerfile.local` do loopzero-cloud tem dois defeitos que o impedem de construir   [x]

domain: control-plane
repo: loopzero-cloud
suggested_mode: bug
source: discover-live-test
evidence: medido em 2026-08-10 ao reconstruir o app-dev sem acesso ao GHCR. `loopzero-cloud/Dockerfile.local` é o caminho documentado para quando o `gomod_token` falta (criado em 2026-06-27 por Red Team #95 P0, pela mesma razão), e **não funciona**. Dois defeitos independentes: (1) injeta um `replace` citando `getloopscale/loopzero-contracts`, mas o `go.mod` declara `getloopscale/loopzero-contracts` — o `replace` não casa e o Go cai no fetch remoto, falhando em `git ls-remote` sem credencial; (2) compila `go build -o /cloud-api ./cmd/main.go` — **um arquivo**, não o pacote —, então os arquivos irmãos de `package main` ficam de fora e o build morre com `undefined: newTrustDashboardKeyResolver` e outros. Corrigidos os dois numa cópia de build, a imagem constrói e sobe.
why_now: o escape hatch existe justamente para o dia em que o token falta. Nesse dia ele não funcionou, e os dois defeitos são de tipos que nenhum build verde pegaria: o (1) só aparece sem credencial (com ela, o fetch remoto funciona e mascara o replace morto), e o (2) só aparece quando `main.go` passa a depender de um irmão — o que aconteceu depois que o arquivo foi escrito.
status: shipped
severity: alta — é o caminho de recuperação, e estava quebrado exatamente no cenário para o qual foi criado.

> **CORREÇÃO 2026-08-14 — a auditoria adversarial marcou este fechamento com ressalva, e tinha razão.**
>
> Os dois defeitos originais estão mortos e as três guardas são reais — ela verificou por mutante. Mas
> a terceira bala pede verificação que **constrói**, *"senão o próximo apodrecimento é invisível
> igual"*, e o entregue é regex sobre o texto do Dockerfile, com o próprio doc-comment admitindo
> *"este guarda é estático"*. As três fixam três erros **passados**: alvo de build, caminho do módulo,
> staging do contracts. Nenhuma vê a classe.
>
> **A classe, nomeada pela auditoria:** um diretório de código novo no topo do módulo que o `./cmd`
> passe a importar e o Dockerfile nunca copie. A imagem falha a construir por pacote ausente — a mesma
> forma dos defeitos que as guardas fixam, e invisível do mesmo jeito.
>
> **Fechada com uma guarda que DERIVA em vez de repetir** (`loopzero-cloud`, commit `4ec23e61`):
> `go list -deps ./cmd` diz exatamente quais pacotes o binário alcança, então o conjunto exigido vem do
> compilador e não de alguém lembrar de atualizar uma lista. Medido: o binário alcança `cmd` e
> `internal`; `tests/` tem código Go e está corretamente fora da imagem, então não é exigido.
>
> **Ela não é um build, e isso está escrito nela.** Um build de verdade precisa de Docker, e fingir
> que uma checagem estática é um build seria a substituição de que este item inteiro trata. A bala fica
> **parcialmente** cumprida — a classe está fechada, a verificação por construção não —, e isso fica
> dito em vez de arredondado.
>
> A primeira versão **SKIPava**: o `go list` rodava com `cwd` em `internal/ci`, onde `./cmd` não
> existe, e o teste saía `ok`. Um skip que lê como aprovação é o modo de falha do próprio item. Agora
> roda na raiz do módulo e falha alto se não puder rodar.
dod:
  - o `replace` injetado cita o caminho de módulo atual
  - o build compila o pacote (`./cmd`), não um arquivo
  - existe verificação que constrói por este Dockerfile **sem** o token disponível, senão o próximo apodrecimento é invisível igual

**RESOLVIDO NO REPOSITÓRIO em 2026-08-10** — `loopzero-cloud@1dfb3513`. Os dois defeitos corrigidos no arquivo de verdade, não mais numa cópia de build: o `replace` passou a citar o caminho de módulo atual, e o build passou a compilar o pacote (`./cmd`) em vez de um arquivo.

**Provado por execução, no cenário para o qual o arquivo existe**: clone limpo do commit publicado, `loopzero-contracts` na `v0.4.0`, e `docker build -f loopzero-cloud/Dockerfile.local` **sem segredo algum** → OK. Antes do conserto o mesmo comando falhava em `git ls-remote` (defeito 1) e, passado esse, em `undefined: newTrustDashboardKeyResolver` (defeito 2).

Guarda: `internal/ci/dockerfile_local_test.go` compara o `replace` injetado com o que o `go.mod` **declara** — em vez de fixar a string, que é como o defeito entrou — e reprova qualquer alvo de build terminado em `.go`. RED antes de GREEN nos dois.

> **Verificado fechado 2026-08-14**, no `loopzero-cloud` (`../loopzero-cloud`, clonado). Os dois defeitos que o
> item nomeia não existem mais no `Dockerfile.local`: o build é `go build … ./cmd` — o **pacote**, não
> o arquivo — e não há mais `go mod edit -replace` citando a organização errada; os contratos são
> encenados no contexto de build. Consertado pelo commit `1dfb3513` daquele repositório
> (*"o caminho de recuperacao sem token volta a construir"*).
>
> As menções a `getloopscale` que sobram são comentário e `LABEL` de imagem, não `replace`.
>
> O campo `status` seguia `raw` com o checkbox já em `[x]` — a mesma deriva que esta sessão mediu em
> outros seis itens, aqui numa variação que o verificador ainda não cobre: **checkbox contra campo**,
> em vez de nota-de-fechamento contra campo.

## B-018 — A borda alcança Kratos e Hydra por encaminhamento de porta que não sobrevive a um rebuild   [x]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-live-test
evidence: medido em 2026-08-10. `auth-dev.loopzero.example` devolvia `502` com Kratos e Hydra **saudáveis** no cluster: a borda encaminha para `localhost:14433` e `localhost:14444`, e os serviços `ory-kratos-public` / `ory-hydra-public` são `ClusterIP` — alcançáveis apenas por `kubectl port-forward`. Nenhuma unidade systemd os gerencia (diferente de `loopzero-cloud-dataplane-authz.service`), então eram processos manuais que morreram junto com os clusters. Restaurados em laço de reinício, `auth-dev` voltou a `200` e o fluxo de login a `303`.
why_now: é o mesmo defeito do `B-010` — encaminhamento de porta como transporte em vez de ferramenta de depuração — noutro serviço, e sobreviveu porque ninguém havia destruído os clusters desde então. O `loopzero-cloud` ganhou NodePort; Kratos e Hydra não.
status: shipped
severity: média — o login funciona hoje, mas por um processo que ninguém supervisiona e que qualquer reinício derruba.
dod:
  - Kratos e Hydra são alcançados pela borda sem `kubectl port-forward`, ou o encaminhamento é uma unidade supervisionada e declarada como as irmãs
  - a escolha vale para os dois, e não só para o que quebrou primeiro
  - derrubar e recriar os clusters não exige intervenção manual para o login voltar

**RESOLVIDO em 2026-08-10** — `loopzero@ddf6e3a4`. Kratos e Hydra ganharam pontes NodePort com porta FIXA (`30005`/`30006`), o `core-cluster.yaml` publica `14433:30005` e `14444:30006`, e o Caddyfile já apontava para essas portas. As pontes são aditivas: os Services ClusterIP do chart seguem intactos.

**Prova de que o encaminhamento saiu do caminho**: `ps -eo cmd | grep -c "^kubectl port-forward"` devolve **0**, as portas 14433/14444 são mantidas por **`docker-proxy`** (o loadbalancer do k3d), e `auth-dev/health/alive` responde **200** com os fluxos de login e registro em **303**.

Aplicado sem recriar cluster (`k3d cluster edit --port-add`), então não houve regressão nas demais portas publicadas. Guarda: `infra/tests/charts/ory_edge_nodeport_test.go` trava as três pontas juntas — Service, mapeamento e Caddyfile —, porque foi a divergência entre elas que produziu o 502 com tudo saudável.

> Registrado 2026-08-10 durante a reconstrução total do app-dev.

## B-019 — Os quatro CronWorkflows executam uma imagem cujo registro foi deletado   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-live-test
evidence: medido em 2026-08-10, ao perseguir o último pod vermelho da reconstrução. `drill-watchdog-*` ficava em `ImagePullBackOff` no cluster de build; o container falho é `main=registry.digitalocean.com/loopzero-system/loopzero-ops:v0.1.0`, declarado em `infra/helm/loopzero-ci-workflows/values.yaml:11`. **Não é credencial**: o `doctl` está autenticado (`paulohenriquevn@gmail.com`) e `doctl registry get` responde **`404 registry does not exist`** — o registro DigitalOcean inteiro foi deletado. Ninguém consegue puxar essa imagem, com token ou sem. Os **quatro** crons do chart (`drillWatchdog` a cada 6h, `weeklySoak`, `coverageReport`, `cosignDeadline`) executam a mesma imagem, então os quatro só podem falhar — o watchdog apenas dispara com mais frequência e por isso apareceu primeiro.
why_now: enquanto o registro existiu, isto era invisível. A reconstrução total expôs porque destruir os clusters apagou o cache de imagens, e nenhum `imagePullSecret` resolve um registro que não existe. Um agendamento que produz pod vermelho a cada disparo é pior que automação ausente: treina quem olha o cluster a ignorar vermelho — foi exatamente esse ruído que fez este ser o último item investigado.
status: raw
severity: média — nenhum dos quatro serve o app-dev, mas `cosignDeadline` verifica o prazo do corte keyless do cosign e `coverageReport` alimenta a cobertura; ambos silenciosamente não rodam há tanto tempo quanto o registro não existe.
dod:
  - `loopzero-ops` está publicada num registro alcançável, ou os crons executam uma imagem que existe
  - `ciCronsEnabled` volta a `true` em `standalone-multi`, ou está registrado por que ele fica desligado lá em definitivo
  - está medido desde quando os quatro não rodam — e o que se perdeu nesse intervalo, em especial o prazo do cosign
  - existe verificação que reprova quando um manifesto agendado referencia imagem impossível de puxar, senão o próximo apodrecimento é igualmente silencioso

> Registrado 2026-08-10 ao fechar o `B-018`. Mitigado no mesmo commit: `standalone-multi` deixou de agendá-los (`ciCronsEnabled: false`), e os ambientes com alvo real — `drill`, `standalone`, `dev`, `prod` — seguem com os quatro.


## B-020 — O host `cloud-dev` era um segundo nome para o mesmo painel   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
evidence: perguntado pelo sponsor em 2026-08-10 — *"por que estamos expondo o cloud-dev?"*. A medição respondeu que ele tinha duas metades, e nenhuma se sustentava. A **carga** era o webhook do GitHub, que não pode carregar JWT (o GitHub assina o corpo com HMAC-SHA256) e por isso ia direto ao `loopzero-api` sem passar pelo proxy do painel — mas o webhook é registrado contra `app-dev` (`cmd/loopzero-ops/internal/seed/github_webhook_fixture.go:44`), e o bloco `app-dev` já tem o mesmo desvio direto. Nada entregava lá. A **compatibilidade** era um 301 para clientes antigos, que não existem: a plataforma ainda não foi lançada. O que sobrava era um host público servindo redirecionamento, declarado como `dashboardOrigin` e `publicCloudUrl` do ambiente, autorizado em `corsAllowedOrigins` e `allowedReturnUrls`, e recebendo o `Host` reescrito de seis rotas da borda.
why_now: a pergunta surgiu ao ler o desenho do sistema, e o desenho só o listava porque ele respondeu 301 na sondagem — eu o havia incluído sem saber a razão de existir. É o mesmo defeito que motivou a renomeação dos clusters e a unificação das cells — "duas palavras, mesma coisa" —, agora na camada de identidade, com o agravante de autorizar em CORS e em URL de retorno um host que existia só para redirecionar.
status: shipped
dod:
  - ~~o host sai da borda, do DNS e do ambiente~~ — **feito** (`d201168a`)
  - ~~as seis reescritas de `Host` e o `connect-src` do CSP acompanham~~ — **feito**
  - ~~o gate deixa de exigir o nome antigo e passa a reprovar seu retorno~~ — **feito**: `dev-public-env` reprova se `cloud-dev` reaparecer em CORS ou em URL de retorno, com teste próprio

**Dois achados de passagem, ambos consertados no mesmo trabalho:**

1. **`GITHUB_WEBHOOK_SECRET` não está semeado** — o log do `loopzero-api` diz *"github webhook: GITHUB_WEBHOOK_SECRET not set, webhook disabled"*, e `POST /api/v1/webhooks/github` devolve 404 enquanto `POST /api/v1/health` devolve 405 (rota existe, método errado). O seed `5-seed-github-app-dev` falhou com `namespaces "loopzero-system" not found` porque rodou antes do namespace existir. **Auto-deploy a partir do GitHub está morto até a cadeia de seeds ser reexecutada** — e minha verificação anterior não pegou porque sondei `/engine/*` e login, não o webhook. Rastreado no `B-021`.

2. **O conserto do `B-010` vivia só no servidor.** O Caddyfile canônico deste repositório seguia apontando para `localhost:13002` em nove lugares — a porta do `kubectl port-forward` que o B-010 aposentou. Sincronizar o arquivo canônico por cima do servidor **reverteu o conserto**, e `/engine/*` foi de 401 a 502 na hora. Corrigido em `69b08c91`, com guarda que reprova a string antiga: um conserto que vive só no servidor é uma correção esperando o próximo `git pull` para ser desfeita.

> Registrado e fechado em 2026-08-10, a partir de uma pergunta do sponsor.

## B-021 — O webhook do GitHub está desligado: falta o segredo HMAC   [ ]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: discover-live-test
evidence: medido em 2026-08-10 ao investigar o `cloud-dev`. `POST /api/v1/webhooks/github` devolve **404** por `app-dev` e direto no `loopzero-api` (`localhost:13001`), enquanto `POST /api/v1/health` devolve **405** — ou seja, a rota do webhook nem chega a ser registrada, não é erro de roteamento. O log do `loopzero-api` confirma: *"github webhook: GITHUB_WEBHOOK_SECRET not set, webhook disabled (RepositoryHandler still active)"*. A causa está em `api/cmd/setup_github.go:152-158`: sem o segredo, `WebhookHandler` fica nil e o roteador não registra a rota. O seed que provisiona o segredo (`5-seed-github-app-dev`) reprovou com `namespaces "loopzero-system" not found` — ele rodou antes do namespace existir, durante a reconstrução.
why_now: enquanto o segredo existiu no cluster, isto era invisível. A reconstrução total o apagou junto com tudo, e o seed que o recria falhou por ordem. **A consequência é que auto-deploy a partir do GitHub não funciona** — um `git push` no repositório de um cliente não dispara build nenhum. Não apareceu antes porque as sondagens de verificação cobriam `/engine/*` e os fluxos de login, e o webhook é a única superfície que não se testa com um GET.
status: triaged
severity: alta — é uma jornada inteira do produto (conectar repositório → push → deploy) que está morta, e ela falha em silêncio: o GitHub recebe 404 e o usuário não vê erro nenhum no painel.

> **CORREÇÃO 2026-08-14 — a auditoria adversarial de fechamento reprovou este item, e tinha razão.**
> O commit que o fechou (`876ad663`) toca **apenas** `BACKLOG.md` e `CHANGELOG.md`: zero código, zero
> teste, zero mudança de ordem. A evidência era um host que ninguém pode re-executar, e as duas balas
> que **tinham** artefato possível não ganharam nenhum.
>
> **Bala da ordem dos seeds — fechada com artefato.** A causa medida é de ordem, não de código: numa
> sala limpa o seed rodou antes de `loopzero-system` existir, falhou no pré-requisito, o segredo do webhook
> nunca foi gravado, o handler ficou `nil`, a rota nunca foi registrada — e o único sintoma foi um
> `404` no log de entrega do **GitHub**. A cadeia inteira silenciosa. `github_app_dev.go` passou a
> garantir o namespace antes de gravar, reusando o `ensureNamespace` que o `broker.go` já usava, com
> asserção estrutural em `namespace_precondition_test.go` — estrutural porque o helper é no-op contra
> o cliente falso dos testes, e isso está escrito no teste em vez de escondido.
>
> Medindo para consertar, apareceu a classe: **sete** seeds gravam Secret e **um** garantia o
> namespace. Os cinco restantes viraram o `B-108`, em vez de serem consertados por analogia — a
> pressuposição é a mesma, mas se a posição de cada um na cadeia já a garante ninguém mediu, e
> adivinhar é o que produziu este item.
>
> **Bala da verificação da rota — fechada com artefato.** O `router_wiring_test.go` injeta
> `&stubGitHubWebhookHandler{}`, sempre não-nil: **um teste que sempre fornece a dependência nunca vê o
> que acontece sem ela.** `webhook_route_registration_test.go` fixa a cadeia pelos dois lados —
> handler `nil` ⇒ rota ausente, handler presente ⇒ rota alcançada — e reprova com o registro removido
> **e** com o caminho renomeado, os dois verificados por mutante.
>
> **Bala do push real — continua aberta, e é honesto dizer.** Ela pede um push no repositório de
> fixture disparando um build ponta a ponta. Isso exige o droplet e um push de verdade; não há
> artefato de repositório que a substitua, e fingir que os dois testes acima a cobrem seria repetir o
> defeito que esta correção conserta.

dod:
  - a cadeia de seeds pós-cleanroom completa sem falhas de pré-requisito, ou a ordem dela garante que o namespace exista antes do seed que depende dele
  - `POST /api/v1/webhooks/github` deixa de devolver 404 — com assinatura inválida deve devolver 401, não 404
  - um push real no repositório de fixture dispara um build, medido ponta a ponta
  - existe verificação que reprova quando a rota do webhook não está registrada, senão o próximo apagamento é igualmente silencioso

> Registrado 2026-08-10 ao investigar por que o `cloud-dev` existia.

> **Re-medido no droplet 2026-08-14.** Vivo e inalterado: `POST /api/v1/webhooks/github` → **404**,
> `POST /api/v1/health` → **405**. A rota não é registrada.
>
> E a medição foi além do que o item registrava, encontrando **o mecanismo do silêncio**. Os dois
> secrets de que o webhook depende estão **ausentes** — `loopzero-github-webhook` e `loopzero-api-github` —, e
> as duas referências são `optional: true`: a variável (`secretKeyRef … optional: true`) e o `envFrom`.
> É por isso que o pod sobe normal, a rota não existe, e nada reclama em lugar nenhum.
>
> Isso divide o item em duas metades com donos diferentes. **A credencial** (segredo HMAC e chave
> privada do GitHub App, que precisam casar com o lado do GitHub) só o patrocinador provê. **A
> invisibilidade** é defeito próprio e independente da credencial: uma superfície de segurança que se
> desliga sozinha e deixa o `git push` do cliente sem disparar build nenhum deveria ser alta e não
> muda; hoje ela é `optional: true` em silêncio. Vale item próprio.

> **Consertado no droplet 2026-08-14 — e NÃO precisou de credencial nenhuma.**
>
> Eu tinha reportado o item como bloqueado no patrocinador. Estava errado, e o erro foi meu: sondei o
> `~/.loopzero/dev-credentials.json` procurando a chave `private_key`, nome que vem do **doc-comment** do
> seed (`github_app_dev.go:22`) e está impreciso. O código lê `app_private_key` — `creds.go:21` — que é
> exatamente o nome que o arquivo tem. As credenciais estavam lá o tempo todo.
>
> A causa era a que o próprio item registrava: o seed rodou antes de o namespace existir. Hoje
> `loopzero-system` tem `3d22h`, e o seed é idempotente. Sequência: `--verify` reprovava por
> `loopzero-api-github not found`; `--dry-run` dizia *"would apply loopzero-api-github + loopzero-github-webhook"*;
> aplicado, `--verify` passou com *"both Secrets present with data matching creds"*.
>
> **O rollout é parte do conserto, não detalhe.** O `loopzero-api` lê o segredo só no arranque — a rota
> continuou 404 depois de os Secrets existirem, e só voltou após `rollout restart`. É a mesma armadilha
> que o `CLAUDE.md` registra para o Kratos.
>
> Provado ponta a ponta, com o segredo comparado por HMAC e nunca impresso:
>
> | requisição | antes | depois |
> |---|---|---|
> | `POST /webhooks/github` sem assinatura | **404** (rota inexistente) | **401** |
> | assinatura **válida** (`X-Hub-Signature-256`) | — | **200** |
> | assinatura **inválida** | — | **403** |
> | `POST /health` (controle) | 405 | 405 |
>
> O binário usado foi construído da árvore atual e copiado: o que estava no droplet era de
> **2026-06-03**, e o checkout local está **61 commits** atrás. Não usei nenhum dos dois.
>
> O `B-104` — a invisibilidade que permitiu isto passar despercebido — **continua aberto**: o desenho
> que fez o pod subir calado com os dois secrets ausentes não mudou.

## B-022 — O caminho do motor depende do painel estar de pé   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-live-test
evidence: medido em 2026-08-10, por acidente. Ao sincronizar o Caddyfile canônico por cima do servidor, o `loopzero-cloud` ficou sem upstream alcançável — e **`/engine/*` e `/v1/*` devolveram 502 juntos**, embora o `loopzero-api` estivesse saudável o tempo todo (`curl http://localhost:13001/api/v1/health` → 200 durante toda a janela). A razão está em `loopzero-cloud/cmd/main.go:1678`: `r.Route("/engine")` tira o prefixo, aplica `requireJWT` e repassa ao motor. Ou seja, o painel é o proxy autenticador do motor, e cair leva os dois caminhos junto.
why_now: o arranjo é deliberado e tem valor real — `r.Use(requireJWT)` no grupo `/engine` é o que mantém o `loopzero-api` **nunca exposto diretamente**: quem autentica é o plano de controle, e o motor só recebe o que já passou por ele. O que não está registrado é o custo: o CLI, que fala com o motor e não com o painel, fica sem deploy quando um componente de dashboard cai. Nenhum incidente até hoje veio disso — a medição veio de um acidente meu, não de uma queda real.
status: raw
severity: baixa hoje, alta no dia em que o `loopzero-cloud` cair sozinho em produção. O `B-002` registra que o cluster central é SPOF; este item é mais estreito e mais provável: não é o cluster que cai, é **um** processo, e ele carrega uma jornada (deploy) que não é dele.
dod:
  - está decidido se o caminho do motor mantém a dependência do painel ou ganha uma rota própria autenticada — e a decisão está num ADR, porque muda a fronteira de autenticação
  - se mantiver: existe medição de quanto tempo o `loopzero-cloud` ficou indisponível no último trimestre, para a decisão ser sobre risco medido e não sobre desenho no quadro
  - se mudar: o `loopzero-api` continua sem ser exposto diretamente sem autenticação — trocar o proxy por uma rota aberta seria piorar em nome de disponibilidade
  - a sonda `loopzero-ops leak live` distingue "o motor está fora" de "o painel está fora", que hoje ela não faz: ambos aparecem como 502

**Uma coisa que este item NÃO propõe.** Remover o `requireJWT` do grupo `/engine` para o CLI falar direto com o motor. Isso trocaria um problema de disponibilidade por um de segurança, e é a direção errada: a fronteira única de autenticação é o que há de mais valioso no arranjo atual.

> Registrado 2026-08-10, medido por acidente durante a remoção do `cloud-dev`.

## B-023 — Um `helmfile sync` sem as variáveis grava marcador por cima de credencial   [x]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: human
evidence: reportado pelo sponsor em 2026-08-10 — o login com Google devolvia `Erro 401: invalid_client`, e o Google estava certo: o cliente não existia. Medido no cluster, `ory-kratos-config` tinha `client_id: REPLACE_VIA_PULUMI` para **Google e GitHub**. A causa é `infra/helm/ory-kratos/values.yaml.gotmpl:104-113`: o chart lê `LOOPZERO_GOOGLE_CLIENT_ID` do ambiente com `| default "REPLACE_VIA_PULUMI"`, então um `helmfile sync` sem a variável exportada grava o MARCADOR por cima da credencial boa — **e termina com sucesso**. Fui eu que causei, rodando `task cloud:setup` três vezes hoje sem exportá-las.
why_now: é a mesma classe do bug do Resend que o `CLAUDE.md` documenta — configuração que aceita um placeholder como se fosse válido. O `default` existe por uma razão legítima (o chart precisa renderizar em ambiente sem credencial); o defeito é ninguém verificar depois se o que ficou no cluster é credencial ou marcador. O modo de falha é caro e silencioso: nada reprova, e só aparece quando uma pessoa tenta entrar.
status: shipped
dod:
  - ~~as credenciais reais voltam ao cluster~~ — **feito** via `loopzero-ops seed patch-kratos` (7 correções); o fluxo redireciona para `accounts.google.com` com o `client_id` real
  - ~~existe verificação que reprova marcador sobrevivente~~ — **feito**: `loopzero-ops leak live` lê a configuração de identidade e reprova `REPLACE_VIA_*`, `CHANGEME`, `TODO_SET_ME`
  - falta decidir se `task cloud:setup` deve **recusar** rodar sem as variáveis em vez de gravar o marcador — hoje ele grava e reporta sucesso, que é o comportamento que causou isto

**Um achado de passagem.** O `loopzero-ops seed patch-kratos` falhou primeiro com `configmaps "ory-kratos-config" not found`, embora o ConfigMap existisse: o contexto padrão do kubectl estava em `k3d-loopzero-secrets`, deixado pelo `k3d:secrets:create`. O seed herda o contexto atual em vez de fixar o cluster que ele opera — e a mensagem culpa o ConfigMap em vez do contexto. Registrado como `B-024`.

## B-024 — Os seeds herdam o contexto do kubectl e culpam o recurso errado   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-live-test
evidence: medido em 2026-08-10. `loopzero-ops seed patch-kratos` reprovou com `get configmap loopzero-auth/ory-kratos-config: configmaps "ory-kratos-config" not found` — e o ConfigMap existia. O contexto padrão do kubectl estava em `k3d-loopzero-secrets` (deixado por `task k3d:secrets:create`), e o seed operou o cluster errado. Trocado o contexto para `k3d-loopzero-core`, o mesmo comando passou com 7 correções.
why_now: o `k3d:secrets:create` passou a fazer parte do caminho canônico há poucas semanas, e ele muda o contexto ativo como efeito colateral. Qualquer seed rodado depois dele opera o cluster de segredos sem avisar. A mensagem de erro é o que torna isto caro: ela nomeia o recurso ausente, não o cluster errado — quem a lê procura o ConfigMap, que está lá.
status: raw
severity: média — não corrompe nada (o cluster errado simplesmente não tem os recursos), mas manda quem depura para o lugar errado, e o custo é tempo em cima de um sistema já quebrado.
dod:
  - os seeds fixam o cluster que operam em vez de herdar o contexto ativo, ou falham dizendo qual contexto usaram
  - a mensagem de recurso-não-encontrado nomeia o contexto, para quem lê não procurar o recurso certo no cluster errado
  - está decidido se `k3d:secrets:create` deve restaurar o contexto anterior ao terminar

> Registrado 2026-08-10 ao restaurar o login com Google.

## B-025 — Três hosts públicos apontam para o vazio   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-live-test
evidence: medido em 2026-08-10 ao revalidar a topologia. `app.loopzero.example`, `auth.loopzero.example` e `cloud.loopzero.example` — **sem** o sufixo `-dev` — têm bloco próprio no Caddyfile e não servem nada: `app` devolve **502** porque encaminha para `localhost:5173`, um servidor Vite de desenvolvimento que não está rodando (`ss -ltn` na 5173 devolve zero sockets); `auth` e `cloud` devolvem **404**. Os três estão declarados em `infra/quality/proxy-exposure.txt`, e a declaração de `app.loopzero.example /sessions/whoami` diz literalmente *"now consumer-less, delete after the new bundle"*. O `setup-dev-public-dns.sh` só gerencia `auth-dev` e `app-dev`, então nem DNS eles têm por parte deste repositório.
why_now: apareceu ao medir para onde a borda encaminha, e não por sondagem de host — a sonda `loopzero-ops leak live` só cobre `app-dev` e `auth-dev`, então estes três nunca foram medidos. O `app.loopzero.example` é o mais relevante: ele encaminha para um servidor de **desenvolvimento** (Vite), e o Red Team já fechou dois achados sobre justamente isso (loopzero-cloud#66 e #141, sobre o bundle de produção e o bloqueio de caminhos de dev-server). Um bloco vivo apontando para o Vite reabre a mesma superfície se alguém subir a 5173 na máquina.
status: raw
severity: média — hoje não servem nada, então não vazam. O risco é o bloco existir e alguém subir o Vite: aí `app.loopzero.example` passa a servir código-fonte na internet, que é exatamente o que o #66 fechou.
dod:
  - está decidido se os três hosts são removidos ou se voltam a servir — e a decisão vale para os três, não só para o que devolve 502
  - se removidos: saem do Caddyfile e do inventário juntos, e o gate `proxy-exposure` continua PASS
  - se mantidos: a sonda `loopzero-ops leak live` passa a cobri-los, porque hoje três hosts públicos não são medidos por nada
  - a declaração que diz "delete after the new bundle" é resolvida em vez de continuar descrevendo uma intenção de meses atrás

> Registrado 2026-08-10 durante a revalidação completa da topologia.

## B-026 — As células divergem: a plataforma só era instalada na primeira   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
evidence: perguntado pelo sponsor em 2026-08-10 ao ler o desenho da topologia — *"as cells deveriam ter configurações espelho, por que uma cell está configurada diferente da outra?"*. Medido: `loopzero-cell-0` com **8** componentes de plataforma e `loopzero-cell-1` com **4** — faltavam KEDA (escala por demanda), `static-assets`, `cloudflared`, `policy-controller` e `kube-state-metrics`. As duas tasks de bootstrap instalam o **mesmo** conjunto de 5 releases, então a divergência não vinha dali: vinha de `infra:install:cell`, que sincroniza as 28 releases do `cell.helmfile.yaml.gotmpl` com `--kube-context {{.CELL_0_CONTEXT}}` **fixo**, e de `infra:install`, que a chamava **uma vez só**.
why_now: a pergunta veio de olhar o desenho, não de um incidente — e é o tipo de divergência que não produz erro nenhum até produzir um difícil. Um tenant fixado na `cell-1` recebia menos plataforma, e **ninguém escolhe a célula**: o roteamento é bin-packing, então a célula é detalhe de alocação. Duas células com capacidades diferentes tornam o comportamento do produto dependente de onde o tenant caiu — sem nada avisando.
status: shipped
severity: alta — não pela divergência de hoje (a `cell-1` tem zero tenants), mas pelo segundo efeito: `task clusters:recreate -- cell-1` chamava a mesma task e instalava na `cell-0`. Recriar uma célula mexia no cluster errado, sem erro.
dod:
  - ~~o contexto da célula é parâmetro e não valor fixo~~ — **feito**, e `required`: um padrão silencioso recriaria o defeito na primeira chamada que esquecesse de passá-lo
  - ~~`infra:install` instala em todas as células~~ — **feito**, uma chamada por célula
  - ~~`clusters:recreate` opera a célula que foi pedida~~ — **feito**, exporta `LOOPZERO_CELL_CONTEXT`
  - ~~existe verificação que reprova o contexto fixo voltando~~ — **feito**: o guarda mira a causa (contexto fixo), não o sintoma (divergência observada)
  - falta aplicar na `cell-1` do app-dev — o conserto está no repositório, e o cluster ainda tem os 4 componentes

**Duas notas de honestidade.** O guarda nasceu com um falso positivo próprio: varria texto e contava o comentário que explica o conserto como se fosse a diretiva. Corrigido para ignorar comentários — um guarda que reprova a documentação da correção treina o time a desligá-lo. E a primeira versão do `required` quebrou o YAML: a mensagem tinha dois-pontos, e o gate de qualidade pegou antes do push.

> Registrado e fechado em 2026-08-10, a partir de uma pergunta do sponsor sobre o desenho.

## B-027 — O diagnóstico do CLI manda verificar um endereço que não existe   [ ]

domain: platform-cli
repo: loopzero-cli
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-10 ao verificar por onde o CLI fala com o motor. `src/commands/doctor.ts:124` — quando a checagem de rede falha, a ação sugerida é *"verify https://api.loopzero.example is accessible"*. Esse endereço **não resolve em DNS** (verificado com `getent hosts`, junto com `api-dev.loopzero.example`, que também não). O próprio repositório já sabe: `src/config/constants.ts:34` marca `https://api-dev.loopzero.example` como *"stale value … does NOT resolve in DNS — issue #130 lock 2026-06-21"*. A correção de junho passou pelo valor padrão e não pela mensagem de diagnóstico.
why_now: a mensagem só aparece para quem **já está com problema de rede** — e manda a pessoa investigar um endereço inexistente, que vai falhar de novo e confirmar o diagnóstico errado. É o pior lugar possível para uma informação obsoleta: o caminho de depuração de quem já está travado. O endereço canônico hoje é `https://app-dev.loopzero.example/engine`, documentado três linhas acima no mesmo repositório.
status: raw
severity: baixa em impacto de sistema, alta em custo humano — ninguém perde dado, mas quem seguir a sugestão perde tempo e conclui a coisa errada sobre a própria rede.
dod:
  - a ação sugerida cita o endereço que o CLI realmente usa, e não um literal fixo — idealmente derivado de `getDefaultApiUrl()`, que é quem sabe para onde a requisição foi
  - `grep -rn "api-dev.loopzero.example\|api.loopzero.example" src/` devolve zero fora de comentários que expliquem por que o valor foi aposentado
  - existe verificação que reprova endereço fixo em mensagem de diagnóstico — a lição é que a correção de junho consertou o valor e esqueceu a mensagem

> Registrado 2026-08-10 ao medir se o CLI passa pelo painel (`B-022`).

## B-028 — 17 portas alcançáveis da internet fora de qualquer lista   [ ]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: human
sponsor: diretriz de 2026-08-10 — *"SEGURANÇA SEMPRE É CRÍTICO E PRIORIDADE P0. TEMOS QUE TER SOMENTE UM ENDEREÇO EXPOSTO."*
severity: **P0** — por diretriz do sponsor, e sustentada por medição.
evidence: medido em 2026-08-10, **da internet** (não do droplet). 39 portas escutam em `0.0.0.0`; o firewall `loopzero-host-firewall` bloqueia 20; sobram **19**, das quais 2 estão na `ALLOW_EXTERNAL` com justificativa medida. As outras **17 não estão em lista nenhuma**. Testado de fora com conexão TCP: `14433`, `14444`, `30082`, `18084`, `30090` e `30094` responderam **ABERTA**. Identificado o que serve cada uma via `docker ps`: `30082` é o **ArgoCD** (entregador GitOps), `30095` é o **cluster de segredos**, `18083`–`18090` são cinco produtos do plano de dados servindo direto, contornando a borda, e `14433`/`14444` são Kratos e Hydra **sem TLS** — o mesmo serviço que a borda já publica em HTTPS.
why_now: a lista `PROTECT` do firewall foi escrita quando os clusters se chamavam `mgmt`, `workload`, `runtime-2` e `vault` — os comentários dela ainda citam esses nomes. Ela cobre as portas que existiam então; tudo que subiu depois (as pontes NodePort de Kratos e Hydra que criei hoje, os produtos do plano de dados, o cluster de segredos) nasceu fora da lista. **Uma lista escrita à mão degrada sozinha**, e esta degradou em silêncio: nada reprova quando uma porta nova aparece sem entrada.
status: raw
blocked_by: nada técnico. O que exige decisão é a política — quais portas ficam abertas e com que justificativa —, e a diretriz do sponsor já a define: **uma superfície exposta**.
dod:
  - o firewall passa a fechar **por padrão** e abrir por exceção nomeada, em vez do inverso: hoje uma porta ausente da lista fica ABERTA, que é a polaridade errada para uma regra de segurança
  - as 17 portas sem lista são classificadas — fecha, ou entra na `ALLOW_EXTERNAL` com o consumidor nomeado e medido, como as duas que já estão lá
  - o `loopzero-host-firewall.sh` passa a viver num repositório versionado: hoje ele existe **só em `/home/paulo/loopzero-ops/dev/`**, fora de qualquer git, e é o mesmo padrão do `B-010` — um controle que vive só no servidor some no próximo servidor
  - existe verificação que reprova porta escutando em `0.0.0.0` sem entrada em `PROTECT` nem em `ALLOW_EXTERNAL` — sem ela, a lista degrada de novo pelo mesmo caminho

**O que NÃO deve ser feito, e o autor do firewall já documentou.** As portas `8787` e `18084` foram fechadas uma vez e quebraram dois gates de CI (`dev-version-drift.yml` do loopzero-rag e do loopzero-lens, que sondam o droplet **de um runner do GitHub**). O autor reabriu com a medição registrada no próprio script: *"o gate é CONTROLE — ele é o que detecta deploy"*. Fechar sem substituir o caminho de verificação troca uma exposição por uma cegueira.

**O crédito onde é devido.** O `loopzero-host-firewall` existe, está habilitado, e a `ALLOW_EXTERNAL` documenta cada exceção com evidência de quando e como foi medida. O defeito não é ausência de controle — é que o controle é uma lista de negação escrita à mão, e listas de negação envelhecem para o lado inseguro.

> Registrado 2026-08-10, a partir da diretriz de superfície única do sponsor.

## B-029 — Dois caminhos para o mesmo endpoint do Stripe   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
evidence: medido em 2026-08-11 ao mapear a separação de domínios. O `loopzero-cloud` é o dono declarado do faturamento (`cycle-backlog.md` roteia `control-plane` para ele, e ele tem `internal/billing/` com 5+ arquivos), mas **os dois repositórios falam com o Stripe, por caminhos diferentes**: o `loopzero-cloud` monta requisições HTTP à mão contra `https://api.stripe.com/v1` (`internal/billing/stripe.go:28`), enquanto o `loopzero` importa a biblioteca oficial `github.com/stripe/stripe-go/v82` e abre sessão do portal em `api/internal/services/billing/stripe_portal.go`. São duas implementações da mesma integração, em domínios diferentes. Além disso, **21 arquivos do engine** conhecem limites de plano e 6 mencionam Stripe.
why_now: apareceu ao responder se a separação de domínios está clara. A tabela de roteamento diz que faturamento é `control-plane`; o código diz que é dos dois. Enquanto ninguém precisa mudar a integração, o custo é zero — no dia em que a API do Stripe mudar, ou uma chave rotacionar, são dois lugares a acertar, em repositórios diferentes, com o risco de acertar um só.
status: raw
severity: média — nada quebrado hoje. O risco é de divergência silenciosa: já aconteceu com este par (o `CLAUDE.md` documenta o bug do `Idempotency-Key` com janela de 1h no `cloud/internal/billing/stripe.go`, e o da ordem de webhooks no mesmo arquivo).
dod:
  - está decidido qual repositório é dono da integração com o Stripe, e o outro consome por contrato em vez de reimplementar
  - se o engine precisa medir consumo (legítimo — `metering/` é dele), isso está separado de conhecer PREÇO (não é dele)
  - as duas implementações não divergem em silêncio: ou existe uma só, ou existe verificação que compara o comportamento das duas

**A distinção que importa.** Medir consumo é do engine — ele é quem sabe quantos builds rodaram. Saber quanto custa é do plano de controle. Hoje o engine faz as duas coisas, e é a segunda que atravessa a fronteira.

**MEDIDO com precisão em 2026-08-11** (a evidência acima era de varredura; esta é de leitura dos dois arquivos). A sobreposição é **exata e nominal**: os dois implementam `CreatePortalSession` para o mesmo endpoint do Stripe.

| | `loopzero-cloud` (dono declarado) | `loopzero` (engine) |
|---|---|---|
| como fala | HTTP à mão contra `https://api.stripe.com/v1` | biblioteca oficial `stripe-go/v82` |
| o que implementa | `CreateCustomer`, `CreateSubscription`, `CreateCheckoutSession`, **`CreatePortalSession`**, `ReportUsage` | **`CreateSession`** do portal |
| está ligado? | sim | sim — `setup_server.go` + `routes/billing.go` |

**Por que o engine tem isso, na palavra do próprio código** (`api/internal/routes/billing.go:7-16`): *"Wires GET /api/v1/billing and POST /api/v1/billing/portal so the CLI's `loopzero billing` and `loopzero billing portal` commands actually have an endpoint to call. Pre-T1.6 the CLI hit silent 404 because the routes were declared in the spec but no handler existed."*

Isso reenquadra o item. **Não foi decisão de domínio — foi resposta a um 404.** O CLI fala com o engine, a rota existia na especificação sem implementação, e alguém a implementou onde o CLI já batia, em vez de no dono do assunto. O caminho mais curto na hora, e a duplicação como consequência não intencional.

E há uma sutileza que decide o conserto: o engine **não** conhece preço nem plano do Stripe — ele cria uma sessão de portal a partir de um `stripe_customer_id` que já existe. Isso é bem menos grave que "dois donos do faturamento": é **um caminho de leitura duplicado**, não duas fontes de verdade sobre cobrança. O que atravessa a fronteira é a credencial do Stripe estar em dois processos, não a regra de negócio.

## B-030 — Uma dependência central não pertence a domínio nenhum   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
evidence: medido em 2026-08-11. `api/go.mod:19` declara `github.com/getloopscale/loopzero-forge v0.6.0`, e **18 arquivos** do engine a importam — incluindo `api/internal/argo/workflow_builder.go`, que constrói os workflows de build (o coração do caminho de deploy). O `loopzero-forge` **não aparece na tabela de domínios** do `cycle-backlog.md` (zero menções), **não tem especialista** em `agents/`, e **não tem checkout no disco** (`loopzero-platform/loopzero-forge` não existe).
why_now: a pergunta sobre separação de domínios expôs a lacuna. Todo repositório que o ecossistema mantém tem domínio e especialista — este tem 18 arquivos dependendo dele e nenhum dos dois. Um achado nele não teria para onde ser roteado: `route_domain.py` recusaria, e a regra de intake (`G1`) bloquearia o item.
status: shipped
severity: média — a dependência funciona (versão fixada, build verde). O custo aparece quando ela precisar mudar: ninguém é dono, ninguém tem o código à mão, e o caminho de manutenção do ecossistema não a alcança.
dod:
  - `loopzero-forge` tem domínio e especialista, ou está declarado explicitamente como dependência de terceiros fora do ecossistema — as duas respostas são aceitáveis, o silêncio não
  - se for do ecossistema: tem checkout, como os outros 15
  - a tabela de domínios passa a ser verificável contra os `go.mod` — hoje ela lista repositórios, e nada reprova quando um repositório importado não está nela

**RESOLVIDO em 2026-08-11**, depois que o sponsor clonou o repositório. Medido o que ele é antes de
decidir onde encaixa: biblioteca pura (54 arquivos Go, **64** testes — mais teste que código), nenhum
serviço (o único `main.go` é fixture em `testdata/`), importa **zero** irmãos e é importado só pelo
engine. Isso é a posição do `loopzero-contracts`: base do grafo.

Entrou em **`contracts-auth`** por essa posição, e não por assunto. O nome do domínio fala de
contratos e autenticação; o que ele guarda é *o que todo mundo importa e que não importa ninguém* — e
é essa propriedade que decide o roteamento, porque é ela que determina o raio de explosão de uma
mudança. A razão ficou escrita na própria regra, para a escolha não parecer arbitrária depois.

Verificado ponta a ponta: `route_domain.py loopzero-forge` devolve `domain: contracts-auth`,
`agent: agents/contracts-auth.md`. O especialista foi estendido — roteamento sem competência seria
meio conserto: ele não mencionava o forge, e agora explica o que a biblioteca faz e por que uma
quebra de compatibilidade nela aparece no primeiro deploy de cliente, não como erro de compilação.

**E a lacuna que o deixou invisível foi fechada**, que é o que impede o próximo:
`TestTodoRepoImportadoTemDominio` varre todo `go.mod` do repositório e reprova qualquer `loopzero-*`
importado sem domínio na tabela. RED antes de GREEN — ele nomeou o `loopzero-forge` antes da declaração
existir. A fonte de verdade passou a ser **o que o código importa**; uma tabela escrita à mão
envelhece, uma verificada contra o código não.

> Ambos registrados 2026-08-11, ao medir a separação de domínios a pedido do sponsor.

## B-031 — A mesma capacidade exige `owner` num lado e nada no outro   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
severity: **alta** — não é vazamento (a rota está autenticada), mas é escalonamento horizontal: qualquer membro autenticado de um time abre o portal de cobrança dele, onde se vê fatura, meio de pagamento e histórico. O `loopzero-cloud` já decidiu que isso é privilégio de `owner`; o engine oferece o mesmo pela porta ao lado, sem a exigência.
evidence: medido em 2026-08-11 ao planejar o `B-029`. As duas implementações da sessão de portal do Stripe exigem autorização **diferente** para a mesma capacidade. `loopzero-cloud/cmd/main.go:1572` monta `r.With(auth.RequireRole(tenantProvisioner, "owner")).Get("/portal", billingHandler.Portal)`. O engine, em `api/internal/server/router.go:520`, monta `r.With(rateLimitMiddlewares...).Post("/api/v1/billing/portal", cfg.BillingHandler.Portal)` — **rate-limit e nada mais**. O engine **tem** o middleware: `api/internal/middleware/roles.go:26` define `RequireRole(minRole)` com a hierarquia `owner > admin > member`, e as rotas vizinhas o usam (`router.go:512` aplica `middleware.RequireAdmin` em `tenants/bulk-delete`). A de portal simplesmente não o aplica.
why_now: apareceu ao medir a viabilidade de unificar as duas implementações — a comparação lado a lado expôs a divergência que nenhuma leitura de um lado só mostraria. É o custo típico de capacidade duplicada: as duas cópias divergem, e a que diverge para menos rigor é a que ninguém percebe.
status: shipped
dod:
  - `POST /api/v1/billing/portal` no engine exige o mesmo papel que o `loopzero-cloud` exige, ou está registrado por que a assimetria é deliberada
  - existe verificação que compara autorização entre implementações duplicadas da mesma capacidade — enquanto houver duas, elas não podem divergir em silêncio
  - se o `B-029` for resolvido eliminando uma das implementações, este item fecha junto; se não for, ele fecha sozinho, porque a assimetria é defeito por si

**RESOLVIDO em 2026-08-11** — `router.go:520` passou a montar a rota com `middleware.RequireRole("owner")`, alinhando com o `loopzero-cloud`. As hierarquias já eram idênticas (`owner: 3`), então alinhar foi aplicar o middleware que já existia.

**O primeiro teste que escrevi era teatro**, e registrar isso vale mais que o conserto: ele montava `RequireRole("owner")(h.Portal)` **dentro do teste** e verificava o 403 — e passava, com a rota real seguindo aberta. Provava que o middleware funciona, que ninguém duvidava, e nada sobre a rota. Um teste que constrói a própria proteção não testa a ausência dela. O guarda que ficou lê a **montagem** em `router.go`, porque é ali que a autorização vive.

> Registrado 2026-08-11 durante a fase de planejamento do `B-029`.

## B-032 — O portal do CLI devolve uma URL construída com o id do cliente   [x]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: discover-review
severity: **alta** — a URL entregue ao usuário não é uma sessão emitida pelo Stripe, e o identificador do cliente vai no endereço. Não é vazamento de credencial, mas é identificador de cobrança em texto claro numa URL, que trafega em histórico de navegador, log de proxy e barra de endereços.
evidence: medido em 2026-08-11. `api/internal/routes/billing.go:245` define o caminho sem credencial: `url := "https://billing.stripe.com/p/login/" + customerID // legacy fallback`. Ele só é substituído por uma sessão real quando `h.portalCreator != nil`, o que exige `STRIPE_SECRET_KEY`. **No app-dev o fallback está ativo**: o deployment monta o Secret `loopzero-api-stripe` com `optional: true`, e `kubectl get secret loopzero-api-stripe` devolve **NotFound**. Ou seja, hoje `loopzero billing portal` entrega ao usuário uma URL montada com o `cus_...` dele.
why_now: apareceu ao medir se o engine realmente precisa da credencial do Stripe (fase de planejamento do `B-029`). A resposta é que ele **não precisa** — e é exatamente por isso que o problema existe: o fallback foi escrito para que dev e testes funcionassem sem chave, e virou o comportamento de produção do ambiente porque o Secret nunca foi semeado. O comentário no código diz *"otherwise preserve legacy placeholder behavior so local dev + tests without a Stripe key keep working"* — a intenção era local, o efeito é público.
status: shipped
dod:
  - `loopzero billing portal` devolve uma sessão emitida pelo Stripe, ou falha dizendo que o faturamento não está configurado — nunca uma URL construída
  - o fallback, se mantido, é explicitamente restrito a ambiente de desenvolvimento e não pode ser alcançado quando o ambiente é público
  - existe verificação que reprova URL de provedor de pagamento montada por concatenação de identificador
  - está medido se essa URL alguma vez chegou a um usuário real, e o que ela expõe a quem a receba

**Relação com o `B-029`.** Este achado **reduz** o item pai: se o engine não tem a chave do Stripe hoje, a "credencial em dois processos" é potencial e não atual — o custo real da duplicação passa a ser a divergência de comportamento entre as duas implementações, não a superfície de credencial. E confirma a direção: o engine não deveria minar sessão de portal.

**RESOLVIDO em 2026-08-11.** O caminho que construía a URL foi removido; sem emissor configurado o engine devolve `503 BILLING_PORTAL_UNAVAILABLE` dizendo onde abrir. Pior em conveniência, melhor em honestidade — uma URL que *parece* funcionar esconde o problema até alguém investigar, e foi assim que ela sobreviveu.

**Provado no artefato em execução**, não deduzido: extraído o binário da imagem implantada no app-dev, a string `p/login/` aparece **0** vezes e `BILLING_PORTAL_UNAVAILABLE` aparece. Um teste existente afirmava que a URL construída era resposta válida; foi reescrito para provar o que deveria desde o início — com emissor, a resposta é a que o emissor deu.

> Registrado 2026-08-11 durante o planejamento do `B-029`.

---

## Fechamento do B-029 — 2026-08-11

**RESOLVIDO**, com escopo bem menor e direção diferente do que o item registrava. O ciclo completo
está em `.claude/knowledge-base/discoveries/opportunities/stripe-portal-duplication-opportunity.md`,
`.claude/knowledge-base/plans/stripe-portal-unification-plan.md` e
`docs/decisions/2026-08-11-stripe-portal-ownership-adr.md`.

**Três refutações, cada uma por leitura do código e não por confiança na leitura anterior:**

1. *"O faturamento tem dois donos."* Não. O engine não conhece preço, plano nem catálogo — a
   sobreposição era de **um verbo**.
2. *"O engine repassa ao loopzero-cloud."* Inverteria a dependência: o fluxo é `cloud → engine` e não
   existe inverso. Seria o `B-022` agravado.
3. *"As implementações são equivalentes."* Não. O cloud **cria** o cliente Stripe quando falta; o
   engine **exige** que exista. O cloud é um passo antes, não uma cópia.

**O achado que decidiu**, e que não estava no item: o engine devolvia uma URL **construída** com o id
de cobrança do cliente quando não tinha a chave — e não tinha, no app-dev. Virou `B-032`, fechado.

**O que ficou:** o `loopzero-cloud` é o único emissor. O engine perdeu a implementação
(`services/billing/stripe_portal.go` removido), a dependência (`stripe-go` fora do `go.mod`) e ganhou
`RequireRole("owner")` na rota, alinhando com o cloud (`B-031`, fechado). O injetor
`WithPortalSessionCreator` **fica** — é a costura testável do handler; o que saiu foi quem falava com
o Stripe.

**Três guardas, cada um mirando a causa:** a rota exige papel (lê a montagem em `router.go`, não um
handler); o engine não importa `stripe-go`; e sem emissor o handler falha em vez de construir URL.
Provado no artefato em execução: extraído o binário da imagem implantada, `p/login/` aparece **0**
vezes e `BILLING_PORTAL_UNAVAILABLE` aparece.

### Validação ponta a ponta do B-029 — 2026-08-11

O fechamento anterior provou que o engine **deixou de emitir**; faltava provar que o `loopzero-cloud`
**emite**. Feito contra o Stripe real, com a credencial do app-dev.

**O que faltava no ambiente, e foi semeado pelo caminho canônico.** Nenhum Secret de Stripe existia:
`loopzero-stripe-api-keys` e `loopzero-stripe-webhook` são montados como `optional: true` e estavam ausentes.
`loopzero-ops seed stripe-api-keys` aplicou o primeiro a partir de `~/.loopzero/dev-credentials.json`, e o
`loopzero-cloud` reiniciou registrando **`"billing enabled"`** — antes disso ele rodava sem faturamento e
nenhum teste teria significado.

**Os quatro passos, exercitados contra `api.stripe.com`:**

| passo | resultado |
|---|---|
| a chave é aceita | `GET /v1/account` → **200** |
| criar cliente (o que `EnsureStripeCustomer` faz) | `cus_V3ItDFoWUMpMbM` |
| emitir sessão de portal | `billing.stripe.com/p/session/test_YWNjdF8xU1g0…` |
| limpeza | cliente removido → **200** |

**A prova que fecha o `B-032`, lado a lado:**

| | endereço |
|---|---|
| sessão **emitida** pelo Stripe | `…/p/session/test_YWNjdF8xU1g0aExBMFp3c3` — opaca |
| URL **construída**, que foi removida | `…/p/login/cus_V3ItDFoWUMpMbM` — com o id de cobrança dentro |

A sessão emitida **não carrega** o `cus_...`, verificado por comparação de string e não por leitura.
É a diferença entre um endereço que o Stripe autoriza e um que qualquer um monta sabendo o id.

**O que esta validação NÃO prova.** Não exercitei o fluxo pelo navegador com sessão de usuário: não
há emissor de JWT de usuário no ferramental, e a única identidade no Kratos é a do sponsor. O que foi
provado é a cadeia que o `loopzero-cloud` executa — chave válida, cliente criado, sessão emitida, e a
sessão sem o identificador. O passo que falta é humano: abrir o painel logado e clicar.

**Estado após a validação:** sonda 11/11, 158/158 pods, painel 200, login 303, e as duas rotas de
portal em 401 sem credencial — o engine porque agora exige `owner`, o cloud porque sempre exigiu.

### O passo humano, feito — 2026-08-11

Faltava a única coisa que nenhum ferramental prova: um usuário logado clicando. Feito no Chrome,
contra `app-dev`, com a sessão do sponsor.

**O que aconteceu ao clicar em `Manage Plan`:**

| | observado |
|---|---|
| quem atendeu | `loopzero-cloud`, `GET /v1/billing/portal` às 10:28:17 |
| o engine foi tocado? | **zero** ocorrências de `billing/portal` no `loopzero-api` |
| para onde o navegador foi | `billing.stripe.com/p/session/test_YWNjdF8x…` — **200** |
| a URL carrega o `cus_`? | **não** — verificado por comparação de string |
| o portal carregou de verdade? | sim, com o titular real: nome + e-mail do sponsor |
| o retorno funciona? | sim — "Voltar para LoopZero" leva a `app-dev.loopzero.example/billing` |

O portal exibir o **titular certo** é o detalhe que fecha a cadeia: significa que o `loopzero-cloud`
resolveu o cliente a partir do tenant autenticado, e não de um identificador vindo do pedido. Uma
sessão emitida para o cliente errado teria carregado igual e parecido sucesso.

**A duplicação, medida no lugar onde ela importava.** Antes, dois componentes sabiam falar com o
Stripe e um deles montava o endereço à mão. Agora o clique atravessa um caminho só, e o engine —
que ainda tem a rota, agora exigindo `owner` — não participa.

O B-029 está fechado por observação, não por argumento.

## B-033 — O teto de custo está desligado, e o portão passa direto quando isso acontece   [ ]

domain: control-plane
repo: loopzero-cloud
suggested_mode: live-test
source: human
evidence: |
  Medido no app-dev em 2026-08-11, contra o cluster core.
  - `loopzero-cloud` registra no boot: `"cost aggregator disabled (LOOPZERO_PROMETHEUS_URL not set)"`
  - a variável não existe no Deployment (`env` do `loopzero-cloud`, nenhuma `PROM`/`COST`)
  - `cost_counters`: **0 linhas**
  - `api/internal/server/router.go:250` diz, no próprio comentário: *"Nil-safe: when
    CostEnforcer is unset … Middleware is pass-through"*
why_now: o agregador é a ÚNICA fonte de `cost_counters`, e o portão que lê essa tabela
  degrada para "passa" quando ela está vazia. As três métricas de custo — minutos de build,
  egresso, puxadas de registro — não têm teto no ambiente que serve o painel público.
status: raw
dod:
  - `cost_counters` recebendo linhas no app-dev, com data recente
  - um teste que prove o portão NEGANDO ao estourar, e não só permitindo
  - a degradação silenciosa deixa de ser silenciosa: quando o agregador não sobe, isso
    aparece como estado do sistema, não como uma linha de log no boot

## B-034 — A cobrança por uso não tem para onde reportar   [ ]

domain: control-plane
repo: loopzero-cloud
suggested_mode: review
source: human
evidence: |
  Medido no banco do app-dev em 2026-08-11.
  - `usage_records`: **0 linhas** (o escritor existe — `api/internal/db/store/metering_wrapper.go`)
  - `subscriptions`: 1 linha — `plano=starter`, `cliente=cus_V3JXfDOr…`,
    `assinatura=NULO`, `item=NULO`
  - `internal/metering/reporter.go:62` seleciona `COALESCE(s.stripe_subscription_item_id, '')`
why_now: o item de assinatura é o endereço para onde o consumo é enviado no Stripe. Sem ele,
  a metade "quanto cobrar" da cobrança não fecha, mesmo que a medição comece a produzir
  linhas amanhã. O cliente existe no Stripe; a assinatura, não.
status: raw
dod:
  - uma conta paga no app-dev com `stripe_subscription_item_id` preenchido
  - o reporter enviando ao menos um registro, provado pelo lado do Stripe
  - o caso "assinatura sem item" falha alto em vez de percorrer zero linhas em silêncio

## B-035 — O segredo que verifica pagamentos não existia no ambiente   [ ]

> Registered 2026-08-11 by `/backlog-item` (slug: `billing-fail-closed`).

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: human
evidence: |
  Medido no app-dev em 2026-08-11:
  - `loopzero-stripe-webhook` **ausente** do namespace `loopzero-system`
  - o Stripe, do outro lado, JÁ tinha o endpoint configurado: `we_1Tr1l5A0ZwssSKrVmQm8t67e`,
    com o `whsec` canônico (`seed stripe-webhook-endpoint --verify` → PASS)
  - `~/.loopzero/dev-credentials.json` no droplet tem `stripe.webhook_secret` (38 chars)
  - `seed stripe-webhook-secret` (sem `--verify`) → **PASS, criou o Secret**
why_now: o passo canônico funciona e nunca rodou nesta reconstrução. Enquanto isso, o Stripe
  enviava eventos para um endpoint cujo destinatário não conseguia verificar a assinatura —
  pagamento confirmado, upgrade de plano, falha de cobrança, todos descartados em silêncio.
  Semear à mão fechou o sintoma hoje; o item é a causa: por que a corrente não cobriu.
status: raw
dod:
  - `loopzero-stripe-webhook` presente após uma execução limpa da corrente pós-cleanroom
  - um evento real do Stripe processado no app-dev, provado pelo lado do Stripe e pelo log
  - a ausência do Secret deixa de ser um estado que o sistema aceita ao subir

## B-036 — Os seeds de cobrança pulam sem credencial e a corrente reporta verde   [ ]

> Registered 2026-08-11 by `/backlog-item` (slug: `billing-fail-closed`).

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
evidence: |
  `cmd/loopzero-ops/internal/seed/stripe_webhook_secret.go:79` e `stripe_api_keys.go:101`
  devolvem, ambos, `Verdict: SKIP, Reason: "dev-credentials.json absent (SKIP-graceful)"`.
  Reproduzido: na máquina local, sem o arquivo, `seed stripe-webhook-secret --verify`
  imprime `SKIP` e sai bem-sucedido.
why_now: `SKIP-graceful` é a política certa para um seed opcional e a errada para a credencial
  que autoriza cobrar. O resultado é uma corrente que passa inteira num ambiente que não
  consegue faturar nem verificar pagamento — e um verde que afirma o contrário.
status: raw
dod:
  - um ambiente que declara cobrança e não tem credencial FALHA, em vez de pular
  - o teste que prove isso falha antes da correção
  - a distinção fica explícita: opcional por design ≠ ausente por acidente

## B-037 — O painel monta os dois segredos de cobrança como opcionais   [ ]

> Registered 2026-08-11 by `/backlog-item` (slug: `billing-fail-closed`).

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: human
evidence: |
  `infra/helm/loopzero-cloud/templates/deployment.yaml:123-132` e o irmão publicado
  `charts/loopzero-cloud/templates/deployment.yaml:84-85` marcam `loopzero-stripe-webhook` e
  `loopzero-stripe-api-keys` com `optional: true`. Medido: o pod subiu por 20h com os dois
  ausentes, `Running`, sem um único evento que dissesse que a cobrança estava morta.
why_now: `optional: true` é o que transforma "não temos credencial" em "subiu normalmente".
  É a terceira camada do mesmo padrão, e a mais silenciosa: as outras duas ao menos deixam
  uma linha de log. Esta não deixa nada.
status: raw
dod:
  - um ambiente que declara cobrança não sobe sem as credenciais, ou sobe declarando-se
    sem cobrança de forma visível — nunca as duas coisas ao mesmo tempo
  - os dois charts irmãos corrigidos juntos (a regra dos 3 charts sobrepostos)
  - provado no app-dev, removendo o Secret e observando o que o sistema diz

## B-038 — Nenhuma camada de cobrança pode degradar em silêncio   [ ]

> Registered 2026-08-11 by `/backlog-item` (slug: `billing-fail-closed`).

domain: engine-go
repo: loopzero
suggested_mode: review
source: human
evidence: |
  Cinco camadas independentes, todas medidas em 2026-08-11, todas falhando para o MESMO
  lado — não cobrar, não barrar, não verificar — e nenhuma delas dizendo isso:
  | camada | como degrada | item |
  |---|---|---|
  | monte do Secret | `optional: true` → sobe sem credencial | B-037 |
  | seed | `SKIP-graceful` → corrente verde sem credencial | B-036 |
  | agregador de custo | `LOOPZERO_PROMETHEUS_URL` ausente → desligado | B-033 |
  | portão de custo | nil-safe → *pass-through* | B-033 |
  | reporte de uso | assinatura sem item → zero linhas percorridas | B-034 |
why_now: cada uma dessas escolhas é defensável isolada. Juntas formam um sistema que, diante
  de qualquer configuração faltante, cobra menos e barra menos — e informa isso a ninguém.
  A direção do erro é o achado: um sistema de cobrança que erra sempre para o mesmo lado
  não está sendo tolerante, está tendo um viés.
status: raw
dod:
  - uma verificação em `loopzero-ops` que enumere as camadas de cobrança e reprove quando
    qualquer uma esteja degradada — no espírito do registro fechado-por-padrão de portas
  - a verificação falha hoje, contra o app-dev, e passa depois de B-033/034/035/036/037
  - uma camada de cobrança nova nasce dentro dessa verificação, ou ela não sobe

### B-034..B-037 fechados — 2026-08-11

**O desenho.** As quatro camadas degradavam para o mesmo lado, e cada uma isolada era
defensável. O que faltava não era correção em quatro lugares: era a pergunta que separa os
casos — *este ambiente prometeu cobrar?* — respondida num lugar só. `Capability` +
`RequirementPolicy` + `Deps.MissingInput`: interface de um método, tabela em vez de
`switch`, injeção na raiz de composição, nil-safe.

**O achado que ligou dois itens.** O reporter não era silencioso — registrava WARN e
esperava o preenchimento do item da assinatura. Mas o **único** escritor desse campo é o
tratador de eventos do Stripe (`webhook.go:299,315`), e o webhook recusava todo evento por
falta do segredo. A espera "transitória" era permanente, e os dois estados produziam o
mesmo aviso de hora em hora. Por isso a correção não foi falhar de imediato — isso quebraria
a assinatura recém-criada — e sim decidir pela **idade**: até 24h é espera, além disso é
travamento.

**A prova, no app-dev, removendo o segredo e observando:**

| | o que o sistema dizia |
|---|---|
| antes | `INFO "billing enabled"` — com metade morta |
| agora | `ERROR "billing NOT enabled — cobrança configurada pela metade — falta o segredo que verifica os eventos de pagamento (webhook)"` |

Segredo restaurado, pod novo, `billing enabled` legítimo. 158/158 pods, sonda 11/11,
painel 200, login 303.

**O que a segurança nunca foi.** `VerifySignature` recusa segredo vazio — falha fechado,
corretamente. Ninguém jamais pôde forjar um evento de pagamento. O que se perdeu foi a
verdade: o sistema afirmava funcionar enquanto todo evento legítimo era recusado, e é a
afirmação falsa que impede alguém de agir.

**Dívida fechada de passagem:** o `Dockerfile.local` do `loopzero-cloud` — o B-017 — já estava
correto no repositório; era a cópia no droplet que carregava o remendo velho. Descartada.

**Dívida nova, medida:** não existe checkout canônico do `loopzero-cloud` no droplet. Ele só
existe em dois diretórios ad-hoc criados por sessões anteriores (`.buildmeta-cloud`,
`.b017-prova`), e um deles é um clone de refspec restrito onde `git switch workspace` falha.
Registrado como B-039.

## B-039 — O painel não tem checkout canônico no droplet   [ ]

domain: engine-go
repo: loopzero
suggested_mode: live-test
source: human
evidence: |
  Medido em 2026-08-11 ao implantar o conserto de cobrança:
  - `~/loopzero-cloud` no droplet é um clone do **loopzero-workspace**, não do loopzero-cloud
  - o código real só existe em `~/.buildmeta-cloud/loopzero-cloud` e `~/.b017-prova/loopzero-cloud`,
    ambos criados ad-hoc por sessões anteriores
  - o primeiro tem refspec restrito: `git fetch origin workspace` popula só `FETCH_HEAD`,
    `origin/workspace` não existe, e `git switch workspace` falha com `invalid reference`
why_now: implantar um conserto no painel exigiu descobrir onde o código mora, corrigir o
  refspec e desfazer um remendo local — três passos que não estão em runbook nenhum. A
  próxima pessoa que precisar implantar às pressas paga a mesma descoberta.
status: raw
dod:
  - um caminho canônico e documentado de onde o painel é construído no droplet
  - `git fetch && git switch workspace` funciona ali sem intervenção
  - os diretórios ad-hoc removidos ou promovidos, não ambos coexistindo

## B-040 — O gate que garante autenticação em toda rota pública passa enumerando zero rotas   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-11 (`9f4a4a62`) e **reproduzido**. `routeauthcoverage/check.go:27` fixa `routerPath = "api/internal/server/router.go"` e percorre o arquivo por linha; `check.go:151` reprova **apenas** quando `len(violations) > 0`, e `quality_gates_authz.go:315-329` só **imprime** `outcome.PassCount`, nunca o assere. Reproduzido reescrevendo `router.go` na forma pós-refatoração numa árvore temporária (marcadores `middleware.Auth` e `registerProtectedRoutes` presentes, registrações movidas para `Register()`): `route-auth-coverage: PASS — 0 routes`, exit 0. Hoje enumera 42 rotas e passa legitimamente. Duas das três guardas já existem — arquivo ausente reprova (`router.go not found`), arquivo stub reprova (`could not locate the middleware.Auth chi.Router`); falta só a contagem mínima. Issue #285.
why_now: este é o **pré-requisito do B-041**. A separação de `routes` por domínio produz exatamente a única forma que passa vazia — ela desligaria a invariante de cobertura de auth sobre toda a superfície pública **no mesmo commit que a justifica**, e o gate ficaria verde.
status: planned
resolved_in: cmd/loopzero-ops/internal/quality/routeauthcoverage/check.go — ErrNoRoutesEnumerated: enumerar zero rotas passou a ser erro tipado, nao PASS. Fatia slice-1.
severity: alta — nada está quebrado hoje, mas o defeito arma exatamente na mudança que o repositório quer fazer, e o modo de falha é silencioso e verde.
dod:
  - `route-auth-coverage` reprova quando enumera zero rotas (piso de contagem mínima)
  - existe teste que prova o piso: uma árvore com marcadores e sem registrações **falha**
  - o upgrade para varredura AST (que sobrevive à movimentação) fica registrado como trabalho seguinte, não como pré-requisito — o piso fecha este buraco sozinho

> Registrado 2026-08-11 pela auditoria de arquitetura (`/loop-codebase-architect`). Evidência reproduzida pela thread principal, não herdada de agente.

## B-041 — `routes` é o pior módulo de 148, e é onde se procura a resposta a "o que é secret?"   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-11. `api/internal/routes` pontua coesão **1/5** (a pior de 148 módulos): 43 arquivos de produção, 11.255 LOC de produção, **263 símbolos exportados contra um limiar de 20 (13×)**. Os nomes de arquivo já carregam o domínio (`secrets.go` 846L, `environments.go` 676L, `builds.go` 628L, `domains*.go` ×5) mas nenhum subpacote existe — o agrupamento por domínio é convenção que o compilador não enforça. Diferente de `db/store`, cuja planura **é** ratificada por ADR, nenhum registro justifica este pacote. **Contrapeso medido:** das 41 funções não-exportadas nos 43 arquivos, exatamente **duas** são referenciadas por mais de 3 arquivos — `writeLoopZeroError` (27) e `writeJSON` (24) — e ambas estão dentro de um arquivo de domínio (`projects.go:489` e `:473`).
why_now: `api/internal` decompõe por **camada** (`services` 29k · `routes` 27k · `db` 22k), e **10 dos 16 domínios têm arquivo de produção nos três ao mesmo tempo**. Mexer no domínio build significa abrir os três. E a correção é barata por duas medições: o vocabulário de domínio **já está publicado e congelado** (`server/router.go` registra 15 famílias de recurso que a Regra Inquebrável 5 impede de derivar), e a decomposição **já foi feita uma camada abaixo** — `services/` tem 9 contextos, zero arquivos soltos, DAG congelado pelo gate AST `servicesacyclic`. Não é introduzir decomposição; é propagar uma que já existe e já foi ratificada.
status: planned
resolved_in: .claude/knowledge-base/plans/slice-5-routes-transport-plan.md — 3 de 5 balas. (1) as duas funcoes de transporte sairam de projects.go para api/internal/routes/httpio, com 399 chamadas re-apontadas e o compilador verificando cada uma; (2) gate novo routes-acyclic recusa subpacote nao registrado E aresta entre irmaos, criado ANTES do primeiro arquivo mover por exigencia literal do item; (3) B-040 ja fechado na fatia slice-1. NAO ENTREGUE: o isomorfismo de `ls routes/` com as familias do router, e o router seguir a unica superficie — ambos exigem mover 43 arquivos e a superficie exportada (263 nao reproduz; ver B-080), T3, registrado como B-080 com as medicoes.
severity: alta — é o achado central da auditoria e a resposta direta à pergunta que a originou.
dod:
  - `ls api/internal/routes/` fica isomórfico à lista de famílias de `server/router.go` — se divergir, alguém adicionou handler sem decidir a família
  - as duas funções de transporte saem do arquivo de domínio para um pacote próprio
  - `server/router.go` continua sendo o único lugar onde a superfície inteira é lida
  - existe gate `routesacyclic` (clone do `servicesacyclic`) recusando subpacote não registrado
  - B-040 fechado **antes** do primeiro arquivo mover

> Registrado 2026-08-11 pela auditoria de arquitetura. Escopo já medido: fatia-piloto de 3 arquivos de produção + 4 testes + 1 pacote novo, T1. O plano detalhado (waves 1-2) sai por `/to-plan` — este item autoriza, não descreve.

## B-042 — Ninguém decidiu se o provisionamento de chave Transit é do `api` ou do operator   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido e verificado à mão em 2026-08-11. `vault.TenantTransitClient` (`operators/internal/vault/client.go:31`) **não tem implementador de produção** — os únicos `EnsureTenantKey` em toda a árvore são dois mocks de teste. No único site de construção do reconciler (`operators/internal/tenant/tenant_controller.go:486-499`) o campo `VaultClient` **não é atribuído**, então `tenant_vault.go:33` curto-circuita em todo reconcile. **Correção importante:** as chaves *são* criadas — preguiçosamente pelo `api`, no primeiro 404 de encrypt (`pkg/vault/transit_client.go:274`). O que não existe é o fail-fast antecipado. E o alarme que o EC-15 promete não tem consumidor: zero `PrometheusRule` referencia `TransitKeyAvailable` ou `VaultQuotaExceeded`. Issue #280.
why_now: **este item bloqueia o B-043.** Mover `pkg/vault` para dentro de `api/internal/` exclui o uso operator-side; se o provisionamento deve ser do operator, o movimento é errado e o trabalho é ligar o implementador. Não dá para fazer os dois. E o estado atual não é neutro: uma falha de policy ou quota aparece no primeiro `POST /secrets` do usuário em vez de o Tenant não chegar a ACTIVE.
status: planned
resolved_in: docs/wiki/decisions/2026-08-12-tenant-transit-key-ownership.md — api-side. A seam do operator e a condition TransitKeyAvailable sao deletadas DEPOIS de a policy loopzero-api do chart ganhar transit/keys/loopzero-tenants-* (B-072)
severity: alta — não pelo defeito (não há perda de dado), mas porque é uma decisão de arquitetura pendente que trava a única fronteira estrutural nova disponível.
dod:
  - está decidido, com ADR, se o provisionamento por tenant é api-side ou operator-side
  - se api-side: `TenantTransitClient`, `reconcileTransitKey` e a condition `TransitKeyAvailable` são deletados (mudança de contrato de CRD — T3)
  - se operator-side: existe implementador concreto atribuído em `SetupWithManager`, e o `PrometheusRule` que o EC-15 promete passa a existir
  - qualquer que seja: existe teste de integração que constrói o componente **pelo caminho de produção**, não injetando mock no struct — foi o mock direto no struct que deixou isto verde

> Registrado 2026-08-11 pela auditoria de arquitetura. Classificado **T3** por `pre-release-governance.md`: um dos desfechos deleta status condition de CRD.

## B-043 — O domínio de secrets mora dentro do kernel que todo mundo compartilha, com um consumidor só   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-11 lendo os importadores reais de cada pacote de `pkg/`. O kernel tem 24.300 LOC e **6.226 (25,6%) têm exatamente um módulo consumidor**: `pkg/vault` 3.663 LOC → só `api`; `pkg/shellrun` 1.241 → só `cmd/loopzero-ops`; `pkg/pulumi` 993 → só `cmd/loopzero-ops`; `pkg/events` 329 → só `api`. `pkg/vault` **é** o domínio de secrets, e é o maior pacote de consumidor único do kernel. Some-se `pkg/` não usar `internal/` — as três únicas árvores `internal/` de primeira ordem são `api`, `operators` e `cmd/loopzero-ops` — então todo o kernel, inclusive o cliente de secrets, é API pública.
why_now: é a **única fronteira nova que o compilador consegue recusar**. Movido para `api/internal/secrets/vault`, a regra `internal/` do Go torna impossível `operators` e `cmd/loopzero-ops` importarem o cliente de secrets — sem gate, sem convenção. Dos cinco planos, só DEVELOPER tem fronteira estrutural hoje; esta adiciona exatamente uma. Custo medido: 4 arquivos consumidores, todos em `api`.
status: planned
blocked_by: B-075 — e o bloqueio foi CONFIRMADO por medição em 2026-08-12, não assumido. Se o shredding vier a ser operator-side, `operators` precisará do cliente de vault, e mover `pkg/vault` para `api/internal/secrets/` tornaria isso impossível pela regra `internal/` do Go — que é exatamente o ganho que este item busca. A DoD deste item manda decidir antes e CANCELAR, não adiar, se o desfecho for operator-side. Medido: `pkg/vault` e importado por 4 arquivos, todos em `api` (3 em `api/cmd`, 1 em `api/internal/adapters`), e ZERO arquivos de `operators` o importam — o custo do movimento nao mudou, so a permissao para faze-lo.
resolved_in: PARCIAL — .claude/knowledge-base/plans/slice-7-kernel-single-consumer-plan.md entregou a bala 3: `pkg/events` (329 LOC) foi para `api/internal/events` e `pkg/shellrun` + `pkg/pulumi` (2.234) para `cmd/loopzero-ops/internal/`, com 17 imports re-apontados e o kernel encolhendo 2.563 linhas. A fronteira e ESTRUTURAL — um teste compila a violacao a partir de um modulo estrangeiro e exige `use of internal package ... not allowed`.
  A premissa do item foi CORRIGIDA por medicao: `shellrun` tem DOIS consumidores (`cmd/loopzero-ops` e `pkg/pulumi/archive`), nao um — por isso os dois viajaram juntos, senao o acoplamento renasceria publico no destino.
  BLOQUEADAS: as balas do `pkg/vault` e da delecao do adapter, ambas dependentes do `B-075`. A clausula de cancelamento do item fala do vault — mover `pkg/vault` para `api/internal/` tornaria impossivel o operator importa-lo, e nao se decidiu de quem e o apagamento. Medido que nenhum dos tres movidos toca o vault e o vault nao toca nenhum deles, entao a bala 3 nao herdava esse bloqueio.
severity: média — nada quebra hoje; o ganho é converter uma convenção em impossibilidade.
dod:
  - B-042 decidido **antes** — se o desfecho for operator-side, este item é cancelado, não adiado
  - `pkg/vault` mora em `api/internal/secrets/`, e um pacote de teste sob `operators/` importando o novo caminho **não compila** (controle negativo — é a única asserção que testa a afirmação)
  - `events`, `shellrun` e `pulumi` saem do kernel para junto do seu único consumidor
  - `api/internal/adapters/vault_transit_adapter.go` é **deletado**, não movido: o próprio doc dele diz que as assinaturas batem byte a byte e só os nomes de método diferem

> Registrado 2026-08-11 pela auditoria de arquitetura. **T2 automático** (`pkg/` + credenciais).

## B-044 — Em qual cluster um chart aterrissa é uma flag de linha de comando, não uma declaração   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `grep -rn kubeContext infra/helmfile/` devolve **um único hit, e é uma values key não relacionada**. Nenhum dos quatro helmfiles de plano (`core`, `build`, `cell`, `secrets`) declara `kubeContext`. O cluster vem do chamador — `taskfiles/infra.yml:197` e `:259` passam `--kube-context {{.BUILD_CONTEXT}}` / `{{.CELL_CTX}}`. Portanto `helmfile -f build.helmfile.yaml.gotmpl --kube-context k3d-loopzero-core sync` **funciona hoje**, e nada — nem helmfile, nem chart, nem gate — o rejeita.
why_now: a auditoria mediu que a separação de planos é real e vive **inteiramente em `infra/`** (5 clusters, 4 helmfiles com forma de plano). Para CORE, BUILD, WORKLOAD e SECRETS, a resposta a *"o que impede uma violação?"* é hoje **"o operador digitou a variável certa"**. Amarrar o contexto dentro do arquivo converte um sucesso silencioso em conflito de declaração, e é **o maior benefício por arquivo tocado de toda a auditoria**: 4 arquivos, 4 planos.
status: shipped
resolved_in: .claude/knowledge-base/plans/slice-3-infra-boundaries-plan.md — os 4 helmfiles declaram kubeContext (conserta o default) + gate novo helmfile-context-parity (obtem o conflito). CORRECAO POR MEDICAO: a premissa original deste item era falsa. Testado contra helmfile 1.1.6, um --kube-context divergente NAO produz erro de conflito — ele sobrescreve o declarado em silencio. A promessa nao e obtenivel do helmfile; veio do gate. Ver MEET-ASYNC-AMENDMENT-2026-08-12-003 Amendment 1.
severity: alta — quatro dos cinco planos dependem de disciplina de digitação para não misturar cluster.
dod:
  - cada um dos quatro helmfiles declara o `kubeContext` do seu plano
  - um `--kube-context` divergente produz **erro de conflito**, não sincronização silenciosa no cluster errado
  - a semântica de `kubeContext` por release foi verificada contra o binário Helmfile instalado **antes** do commit — uma amarração errada quebra toda instalação

> Registrado 2026-08-11 pela auditoria de arquitetura. **T2 automático** (Helm + multi-cluster).

> **Verificado fechado 2026-08-14.** Os **quatro** helmfiles declaram `kubeContext` — `build`,
> `secrets`, `cell` e `core`, um cada — e o gate `helmfile-context-parity` está ligado e passa:
> *"4 invocations compared, 0 divergence(s), 0 helmfile(s) declaring no context"*.
>
> **A bala 2 foi cumprida por mecanismo diferente do que ela descreve, e isso vale dizer.** Ela pedia
> que um `--kube-context` divergente produzisse **erro de conflito**. Medido: `helmfile template` com
> contexto divergente sai **0 e não reclama** — mas ele não conecta, então não é o teste que decide, e
> o que decidiria (`sync`/`apply`) é operação mutante num ambiente compartilhado.
>
> O que protege hoje é mais forte que a bala: o gate impede a divergência **no ponto de invocação**,
> antes de qualquer conexão, em vez de depender de o helmfile detectá-la depois. Fecho por prevenção
> verificada, não por detecção presumida.

## B-045 — Quatro gates reprovam hoje em conteúdo real do repositório, e nenhum deles é executado   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11 com binário recém-compilado (`go build -o /tmp/loopzero-ops ./cmd/loopzero-ops`), **não** o `./loopzero-ops` da raiz. **27 dos 117 gates** reportam wiring `-`/`diff`; 17 não são referenciados por taskfile nenhum. Dos 10 que rodam sem argumento e sem credencial, **quatro reprovam sobre conteúdo real**: `helm-drift` (exit 1, drift sem ADR nomeando `vap-tenant-cr-shape.yaml`), `clusterimagepolicy-mode` (exit 1, `mode: enforce` em `infra/helm/admission-policies/values.yaml:36` **sem nenhum ADR/MEET referenciando o cutover de deny-flip**), `alert-runbooks` (**33** violações), `migration-test-coverage` (**43** de 59 migrations sem teste). E a causa-raiz não é esquecimento: `cmd/loopzero-ops/quality_gate_wiring_test.go:110` e `:113` classificam `helm-drift` e `clusterimagepolicy-mode` como `operatorRun` — *"need live state, a ceremony, or a cadence"* — quando ambos são verificações herméticas que rodam em sandbox. **A isenção documentou o buraco como decisão.** Os outros seis se comportam como a classificação prevê (parametrizados, dormentes, ou dependentes de credencial) — o `-` deles está correto.
why_now: a auditoria mediu que as fronteiras de plano deste repositório são sustentadas **por gates, não por estrutura**. Isso é escolha legítima — mas só funciona se os gates rodarem. O par `helm-drift` (reprova, desligado) × `helm-overlap-parity` (roda, mede outra dimensão, passa) é o caso exemplar: o gate certo existe, funciona, reprova, e ninguém executa. O `clusterimagepolicy-mode` é o mais desconfortável: diz que uma mudança viva de postura de segurança está ativa sem o registro de governança que o próprio repositório exige.
status: shipped
resolved_in: cmd/loopzero-ops/quality_gate_wiring_test.go — wiringReason virou value object com invariante (operatorRun DEVE nomear o insumo externo). O invariante encontrou 4 classificacoes erradas, nao as 2 que a auditoria vira. Fatia slice-1.
severity: alta — não pelos quatro achados isolados, mas porque o mecanismo de defesa do repositório tem uma camada que ninguém verifica.
dod:
  - a classificação `operatorRun` é **provada, não aceita**: para cada gate assim marcado, o teste o roda contra árvore hermética e **falha se ele concluir sem exigir estado externo**
  - `helm-drift` e `clusterimagepolicy-mode` reclassificados e ligados (ou a isenção justificada por escrito com o motivo medido)
  - `security-context` para de sair 0 imprimindo `NON-CONFORMANT without ADR: 2` — ou reprova, ou é `advisory` declarado
  - está decidido, com ADR, se `clusterImagePolicy.mode: enforce` é a postura desejada
  - `infra/quality/environment-lock-baseline.txt` (1529 bytes, 21 caminhos de `api/internal/routes`, **zero leitores em código** — as 5 referências são documentação) é removido ou ganha consumidor; um baseline órfão parece contrato

> Registrado 2026-08-11 pela auditoria de arquitetura. Relacionado a `B-006` (o oráculo do conjunto de gates), que trata do *arquivo* do oráculo — este trata de *quais gates rodam*. Correção postada no issue #255, com pedido de reabertura.

> **Correção 2026-08-11, ao planejar a fatia 1.** O item **superestimava**. `cmd/loopzero-ops/quality_gate_wiring_test.go` já é um ledger de isenções com sete categorias, e três dos achados já estavam nele, medidos e justificados desde 2026-08-06:
> - `alert-runbooks` e `migration-test-coverage` estão como `failingGate` — *"funciona, e reprova num achado real; conserte o achado, depois ligue"* — com contagem registrada e um argumento que a auditoria não tinha: **as URLs de runbook que os alertas conformes já carregam devolvem 404**, então adicionar 33 links mortos compraria gate verde e nada mais.
> - `security-context` **não** sai 0 imprimindo violação: sai 0 em modo relatório e **1 com `--gate`** (verificado: exit=1, `argo-events` e `loopzero-ci-workflows`). A auditoria rodou sem a flag e leu errado.
>
> **O que sobra, e é real:** `helm-drift` e `clusterimagepolicy-mode` estão classificados `operatorRun` (*"needs live cluster state"*) e são **puramente herméticos** — zero `kubectl`/`exec.Command`/`client-go` nos dois pacotes; leem apenas `charts/`, `infra/helm/` e `values.yaml`. A categoria `operatorRun` não tem prova: é a única das sete que ninguém verifica. Mais a baseline órfã. O escopo do item passa a ser esse.


> **2026-08-12 (fatia `slice-13`):** duas balas medidas, uma fechada.
>
> **Fechada — a baseline órfã.** `infra/quality/environment-lock-baseline.txt` foi **removida**.
> Medido antes: zero referências em `*.go`/`*.yml`/`*.md`/`*.sh` (as 5 de documentação que o item
> registra já não existiam), nenhum gate `environment-lock` no inventário, e **2 dos 21 caminhos**
> já ausentes — `api/internal/routes/tenants.go` e seu teste viraram subpacote na fatia 8.
>
> **Medida, não fechada — a classificação `operatorRun`.** A bala pede prová-la em vez de aceitá-la.
> O desenho atual argumenta (`quality_gate_wiring_test.go:48-58`) que detectar é inviável porque uma
> sonda **estática** produz falso-positivo. Correto para sonda estática — mas uma sonda
> **comportamental** (rodar o gate com o insumo ausente) discrimina, e eu rodei as cinco:
>
> | gate | com o insumo ausente | prova? |
> |---|---|---|
> | `drill-creds-isolation` | `ERROR: neither DRILL_DO_TOKEN nor DIGITALOCEAN_TOKEN...` | **sim** |
> | `runtime-slo` | não aceita `--root` (CLI própria) | inconclusivo |
> | `spotcheck-dimension` | não aceita `--root` (CLI própria) | inconclusivo |
> | `cosign-cutover` | `pre-GA — marker absent. Check is dormant.` — **conclui** | **não** |
> | `service-auth-rotation` | `no rotation marker — check is dormant` — **conclui** | **não** |
>
> Os dois últimos citam como insumo externo *"the wall-clock date"*, mas o marcador que eles comparam
> vive **dentro** do repositório (`.github/v1.0-ga-date.txt`), e sem ele concluem sozinhos. Isso
> sugere duas espécies distintas de "estado externo" — o que pode faltar (provável por ausência) e o
> ambiente que está sempre presente (improvável por ausência) —, e a classificação atual as trata
> igual. Fechar isto é a próxima fatia, e exige decidir se as duas ratchets de data continuam
> `operatorRun`.

> **2026-08-12 — duas correções a medições MINHAS, no mesmo dia.**
>
> (a) Afirmei que `loopzero-ci-workflows` não tinha contêiner e que a classificação seria falso positivo.
> **Errado**: meu grep procurou `containers:` no plural e o Argo usa `container:` no singular.
> `cronworkflows.yaml` e `workflowtemplate-quality-gate.yaml` declaram contêineres reais. São **dois
> achados reais**, não um e um falso positivo.
>
> (b) Afirmei que o `security-context` "sai 0 imprimindo NON-CONFORMANT". Ele sai **1 com `--gate`**
> — verificado agora: `--gate` → exit 1, sem a flag → exit 0. O modo sem flag é **relatório**, e a
> auditoria que gerou esta bala rodou sem a flag e leu errado. A linha `:1240` deste próprio item já
> registrava isso, e eu repeti o erro sem ler.
>
> **O que a bala 3 realmente pede, então, é wiring**: `grep -c security-context taskfiles/quality.yml`
> → **0**. O gate sabe reprovar e ninguém o chama. Ligá-lo com `--gate` reprova os dois charts hoje,
> então exige antes endurecer `argo-events` (wrapper de chart upstream) e `loopzero-ci-workflows`
> (nosso), ou registrar exceção por ADR — que é T2 sobre workload em execução.

> **2026-08-12 — as cinco balas fechadas.**
>
> **1. `operatorRun` provado.** O desenho anterior argumentava que detectar era inviável porque uma
> sonda **estática** dá falso-positivo — correto, e é por isso que a prova é **comportamental**: roda
> o gate com o insumo ausente e exige que ele não conclua. Ela não inspeciona nada; observa. Provada
> por mutante: rotular `alert-runbooks` (hermético) como `operatorRun` faz o teste reprovar nomeando
> a razão — a mesma falsidade que `helm-drift` e `clusterimagepolicy-mode` carregaram.
>
> A medição também separou **duas espécies** de estado externo, que a classificação tratava igual: o
> **ausentável** (arquivo, variável — provável por ausência) e o **ambiente** (o relógio — não se
> tira o calendário de um processo). `cosign-cutover` e `service-auth-rotation` são ambiente e agora
> se declaram como tal por construtor próprio, em vez de se esconderem entre os provados.
>
> **3. `security-context` ligado**, com `--gate`. Os dois charts ganharam exceção **datada** pelo
> mecanismo que o próprio gate desenhou (`ADR-securitycontext-exception.md` por chart), cada uma com
> condição de saída: `argo-events` é wrapper de chart upstream, e endurecê-lo às cegas produz
> garantia falsa quando o esquema de valores muda; `loopzero-ci-workflows` é nosso e tem contêineres
> reais, mas `readOnlyRootFilesystem: true` quebra o `loopzero-ops`, que escreve — fechar exige
> `emptyDir` em `/tmp` e validação LIVE dos quatro CronWorkflows, que é T2 com reunião.
>
> **4. ADR do deny-flip** escrito, e o gate ligado. **5. baseline órfã** removida.
> **2. `helm-drift` e `clusterimagepolicy-mode`** ligados.

> **Verificado fechado 2026-08-14.** As três balas, medidas contra o código e não contra o CHANGELOG:
>
> A classificação `operatorRun` é **provada**: `cmd/loopzero-ops/quality_gate_wiring_test.go` roda cada gate
> assim marcado contra árvore hermética e reprova se ele concluir sem exigir estado externo.
>
> `helm-drift` e `clusterimagepolicy-mode` estão **ligados** — uma invocação cada em `quality:gates`.
>
> E o `security-context` deixou de sair 0 imprimindo violação: ele roda **com `--gate`**, e o próprio
> comentário do Taskfile registra por que a auditoria original concluiu o contrário — ela rodou **sem**
> a flag, em modo relatório. Hoje sai `✅ 0 NON-CONFORMANT charts without ADR exception`.

## B-046 — A cerca fail-closed do Tenant CR existe no chart de dev e não no publicado   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `charts/loopzero-admission-policies/templates/` é **subconjunto estrito** de `infra/helm/admission-policies/templates/`: 18 contra 20, nada exclusivo do lado publicado. Os dois ausentes são `cosign-version-config.yaml` e **`vap-tenant-cr-shape.yaml`** — uma `ValidatingAdmissionPolicy` com `failurePolicy: Fail` cujo próprio cabeçalho declara por que existe: *"loopzero has zero admission webhooks on its own CRDs today … so a hand-authored `kubectl apply` of a malformed Tenant bypasses the REST-layer validation — the exact class of the M2 legacy-namespace incident"*. O chart de bootstrap é instalado por `cell.helmfile:169` e `core.helmfile:753`; o publicado é dependência declarada de `charts/loopzero-platform` (`Chart.yaml:58-61`), o caminho de self-host / ArgoCD L3. Issue #282.
why_now: **uma fronteira de segurança que existe em dev e não em produção é pior que uma que não existe em lugar nenhum — porque faz o teste de dev passar.** E o allowlist do `helm-drift` (`helmdrift/check.go:29-32`) aprova **pares de chart inteiros**, então um ADR aprova em bloco toda divergência presente e futura daquele par: foi assim que os dois `ExternalSecret` que carregam as chaves de admin de RAG e Lens também sumiram do radar.
status: planned
resolved_in: .claude/knowledge-base/plans/slice-3-infra-boundaries-plan.md — vap-tenant-cr-shape.yaml e cosign-version-config.yaml copiados para charts/loopzero-admission-policies, e o allowlist do helm-drift re-chaveado de PAR para PAR:ARQUIVO. O gate passou a verde e foi LIGADO em taskfiles/quality.yml (estava isento como failingGate).
severity: alta — assimetria de fronteira de segurança entre superfícies de instalação. **Incerteza declarada:** não verifiquei qual caminho a produção usa hoje; se instala por Helmfile L2, o impacto é potencial (self-host e qualquer migração para L3), não ativo.
dod:
  - está decidido se `vap-tenant-cr-shape` vale para toda instalação ou é deliberadamente bootstrap-only — e, se for o segundo, isso está **escrito no chart publicado**, não inferido do silêncio
  - o allowlist do `helm-drift` passa a aprovar **por arquivo com justificativa**, não por par inteiro
  - `helm-drift` ligado (ver B-045) — sem isso a decisão não fica protegida

> Registrado 2026-08-11 pela auditoria de arquitetura. **T2 automático** (Helm + fronteira de segurança).

> **Medido 2026-08-14 — duas balas de três, e a terceira tem dono.**
>
> **Fechadas:** a decisão está **escrita no chart publicado**, não inferida do silêncio —
> `charts/loopzero-admission-policies/values.yaml` e o chart de bootstrap carregam o mesmo bloco
> explicando que a política é ligada onde os Tenants vivem e desligada nas cells porque o CRD não
> existe lá, citando `B-077` e o ADR `2026-08-13-tenant-cr-fence-placement`. E o `helm-drift` está
> ligado.
>
> **Aberta:** o allowlist do `helm-drift` ainda aprova **por par**, não por arquivo com justificativa —
> a própria saída do gate diz: *"21 aprovações herdadas do allowlist antigo por PAR, cuja razão por
> arquivo nunca foi medida"*, e aponta o **`B-076`**. Essa bala é do `B-076`, não deste item.

## B-047 — A task que viola a fronteira SECRETS/CORE está shipada, e o gate que a nomeia valida o violador   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `CLAUDE.md` diz que instalar OpenBao no cluster de gestão é *"FORBIDDEN in every tier — it is a topology violation, not a shortcut"*, e `.claude/rules/cleanroom-up-live-test-mandate.md` repete como PROIBIDO em qualquer tier. A task existe em `taskfiles/k3d.yml:230` e instala a stack OpenBao inteira no core via quatro syncs `--kube-context k3d-loopzero-core`. E o gate que leva a fronteira no nome — `openbaobootstrapincore/check.go:67,:94` — **não a proíbe**: suas asserções verificam que a task *existe* e *aponta para core*. Ele **falha se a task violadora for removida**.
why_now: é um teste de conformidade **para** a fronteira que ele deveria defender. Enquanto ele existir nessa forma, remover a task proibida quebra o `quality:gates` — ou seja, a regra e o gate se contradizem, e o gate ganha.
status: shipped
resolved_in: docs/wiki/decisions/2026-08-12-openbao-in-core-rule-vs-task.md — a regra vence. A task sai, o gate e invertido, e os runbooks que a apresentam como caminho valido sao corrigidos. Na implementacao: cleanroom_mandate_openbao_gate_test.go:63-69 EXIGE que o mandate documente o atalho como escape hatch — precisa ser invertido junto
severity: alta — a contradição está shipada e é exercível por qualquer um que leia o Taskfile em vez da regra.
dod:
  - está decidido, com ADR, qual das duas é verdadeira: a regra que proíbe ou a task que oferece
  - o gate passa a reprovar a violação em vez de validar sua forma
  - a task é removida, ou a regra passa a declarar a exceção com o motivo

> Registrado 2026-08-11 pela auditoria de arquitetura. **T3 — ADR obrigatório**: uma regra inquebrável e uma task shipada se contradizem, e decidir qual vale é decisão de arquitetura. Deve ser **o último** passo de qualquer sequência que o toque.

> **Cumprido 2026-08-12** pela fatia `slice-12`. O ADR `2026-08-12-openbao-in-core-rule-vs-task.md`
> estava escrito desde 2026-08-12 e **integralmente não implementado** — o item foi marcado
> `planned` tendo entregue só a decisão, que é a forma inversa do defeito que ele registra: antes
> havia regra sem cumprimento, depois decisão sem cumprimento.
>
> As três consequências que o ADR nomeia: a task removida (56 linhas, junto com o irmão
> `status-in-core`, que só existia para observar o deployment proibido); o gate invertido — e
> invertido **por efeito**, não por nome, senão a proibição seria evadível renomeando; os dois
> runbooks corrigidos. A asserção do `cleanroom_mandate_openbao_gate_test.go` foi mantida e sua
> **mensagem** invertida: ela pedia que o mandate documentasse um *escape hatch*, e o mandate
> documenta uma proibição — os dois liam a mesma string com propósitos opostos, como o ADR já
> tinha medido.
>
> RED medido antes: o gate novo achou **5** violações na árvore (o alvo + 4 linhas de efeito).
>
> **A revisão independente, antes do commit, achou quatro HIGH e todos foram corrigidos.** (a) Eu
> corrigi só o bloco de comando do runbook de cutover; a prosa acima dele continuava **recomendando**
> a opção proibida e oferecendo um override por linha de comando que nenhum gate pega — a seção foi
> reescrita e o override está dito. (b) Um comentário retido dizia ao operador que não precisava de
> `--kube-context`, o que ficou ativamente errado depois da troca do comando. (c) A checagem "por
> efeito" era evadível por seis formas, três delas o estilo dominante do próprio repositório
> (variável de template, continuação de linha, forma com `=`); as cinco baratas foram fechadas com
> fixture, o gate passou a varrer `taskfiles/*.yml` em vez de um arquivo, e as duas que exigem
> dataflow estão **declaradas** no gate em vez de implicadas inexistentes. (d) `quality_registry.go`
> e `gateset.frozen` ainda descreviam o gate antigo — o ADR tinha nomeado o segundo e eu não o
> atualizei.
>
> **E a remoção destravou o `B-044`:** o `helmfile-context-parity` estava desligado porque esta task
> produzia 4 divergências, e o teste de wiring dizia *"wiring follows that removal"*. Removida a
> task, ele foi a 0 divergências e **foi ligado** — é ele que cobre o que a checagem por linha não
> alcança. A asserção de reconciliação que escrevi na fatia 3 pegou, no caminho, um comentário meu
> que continha a string da flag e desalinhava a contagem em 1.

> **Verificado fechado 2026-08-14.** As três balas: a decisão está no ADR
> `2026-08-12-openbao-in-core-rule-vs-task` (a **regra** vence, não a task); a task foi **removida** —
> `grep -c bootstrap-in-core taskfiles/k3d.yml` devolve **0**; e o gate deixou de validar a forma da
> violação e passou a **proibi-la**: `openbao-bootstrap-in-core-task` está ligado e sai
> *"PASS — no mgmt-cluster OpenBao shortcut (5144 lines scanned)"*.

## B-048 — O chart guarda-chuva colapsa os quatro planos num único release Helm   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `charts/loopzero-platform` declara como dependências os componentes de control plane, build plane e workload plane e os instala **num único release**, enquanto o caminho de desenvolvimento gasta **quatro helmfiles separados** construindo exatamente essa separação. `charts/loopzero-platform/profiles/dev.yaml:53` liga `loopzero-admission-policies` por condição; o mesmo padrão vale para os demais. Nenhum documento em `infra/` ou `charts/` mapeia chart → plano: `docs/architecture/helm-topology.md:24` **diz explicitamente** que o lado `charts/` não tem essa atribuição.
why_now: a separação de planos que a auditoria mediu como real (5 clusters, 4 helmfiles) **desaparece no caminho publicado**. Quem instalar pelo chart guarda-chuva recebe os três planos num cluster, e nada no chart diz que isso é diferente do que o desenvolvimento faz. Junto com o B-046, é o segundo caso em que a superfície publicada é mais fraca que a de dev.
status: planned
resolved_in: docs/wiki/decisions/2026-08-12-umbrella-chart-plane-separation.md — e composicao single-cluster deliberada, e isso passa a estar escrito no chart. A pergunta que fica (o chart deveria existir?) e o B-073
severity: média — hoje o caminho publicado é pouco usado; o risco é ele virar o caminho de produção sem que a diferença esteja documentada.
dod:
  - está decidido se o chart guarda-chuva deve preservar a separação de planos ou se ele é deliberadamente um modo single-cluster
  - se deliberado: está escrito no chart, com o que se perde
  - existe um índice chart → plano em `infra/` — hoje não existe nenhum `.md` em `infra/*.md`, `infra/helm/*.md` nem `infra/helmfile/*.md`

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-049 — O sink do DAG de domínio importa a camada de persistência e re-exporta os tipos dela   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11, severidade ancorada no limiar `consensus` `infra_in_domain = 0`. `services/deploycontracts` se documenta como *"the pure sink of the services acyclic DAG"* e está registrado `layer=domain`. Mas `shared_contracts.go:207` expõe `WithLock(..., fn func(tx db.DBTX) error) error`; `delivery.go:37` expõe `Activate(ctx, build *db.Build, ...)`; `:55` expõe `TargetRelease *db.Release`; `manifest_builder.go:23` expõe `Apps []db.BuildAppSpec`; e `:11` importa `api/internal/manifests` (`layer=application`). **Os 10 pacotes que dependem do sink dependem transitivamente da persistência.** O `servicesacyclic` não vê isso porque só inspeciona arestas *entre os oito subpacotes de services*. O tipo `db.DBTX` (`db/query_executor.go:53`, interface moldada por strings SQL) aparece em interface (`routes/secrets.go:44`, `:670`, `:795`), domínio (`shared_contracts.go:207`) e aplicação (`services/release` 28 refs, `services/build` 12). O furo é de **uma string**: a cerca MA-7 (`dbgeneratedboundary`) proíbe `api/internal/db/generated`, não `api/internal/db` — e os 2 arquivos que exploram o furo estão ambos na superfície de secrets.
why_now: é o vazamento de camada **medido** — o único ponto onde a auditoria recomendou mexer em camadas, tendo recusado Clean/Hexagonal por atacado duas vezes. Alargar a cerca de `db/generated` para `db` é literalmente uma string, e é o que impede o furo de crescer.
status: planned
resolved_in: .claude/knowledge-base/plans/slice-4-layer-leak-plan.md — 3 de 3 balas TOCADAS, com uma ressalva escrita: a bala 1 (`dbgeneratedboundary` cobre `api/internal/db`) esta coberta em `server` e `routes`; `services` (51) e `cmd` (18) seguem fora, medidos no B-079. Dizer 3/3 sem essa ressalva superdeclararia. (1) o sink parou de se declarar puro e o doc-comment nomeia as 4 referencias; (2) gate novo domain-sink-purity trava o conjunto medido e reprova a quinta; (3) a cerca dbgeneratedboundary alarga para api/internal/db em api/internal/server, o unico dos quatro diretorios medido em ZERO importadores. FECHADO NA FATIA slice-6: `db.DBTX` saiu da camada de interface. A recusa da fatia slice-4 media QUANTOS pontos tocavam WithLock (34) e nao O QUE a camada de interface usava — medido depois: DOIS arquivos, UM simbolo. O arquivo de porta (query_executor.go, 72 linhas, nenhuma entidade) virou o pacote api/internal/db/dbtx, e `db` re-exporta por TYPE ALIAS, entao nenhuma das 34 assinaturas mudou. `routes` chegou a zero importadores de persistencia e entrou na cerca. E a premissa da DoD de que alargar a cerca era "literalmente uma string" foi MEDIDA E DESMENTIDA: 43 arquivos.
severity: alta — o sink é o ponto de maior fan-in do DAG; o que vaza por ele vaza para dez pacotes.
dod:
  - `dbgeneratedboundary` cobre `api/internal/db`, não só `api/internal/db/generated`
  - `deploycontracts` deixa de expor tipos de `db` nas assinaturas públicas, ou a etiqueta `layer=domain` dele é corrigida para o que ele de fato é
  - `db.DBTX` deixa de ser tipo de parâmetro na camada de interface

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-050 — O CR `Application` do ArgoCD tem três escritores em dois runtimes   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: ciclo conceitual traçado ponta a ponta em 2026-08-11 e confirmado pelo gate de qualidade da fase 2. `api/internal/k8s/tenant_cr_manager.go:147` escreve `Tenant.Spec.Environments` → o operator lê a slice em `operators/internal/tenant/tenant_gitops.go:87` e cria o `Application` em `:333` → `api/internal/argocd/sync_client.go:190` faz sync e `:351` refresh → `api/internal/k8s/tenant_evictor.go:49` deleta `argoproj.io/applications` → o próximo reconcile recria. **Nenhum dos dois módulos Go importa o outro** (medido: 0 arquivos) — o laço fecha pelo API server do Kubernetes, invisível ao compilador. O `CLAUDE.md` já cataloga o sintoma como padrão sistêmico conhecido: *"Orphan ArgoCD Application … status Unknown forever"*.
why_now: Go proíbe ciclo de import entre pacotes em tempo de compilação, então a ausência de ciclos **não é mérito da arquitetura** — é garantia do toolchain. Este é o único lugar onde um ciclo real conseguiu existir, e ele existe porque atravessa um runtime que o compilador não vê. O sintoma já é conhecido e tratado como operacional; a causa não está registrada em lugar nenhum.
status: planned
resolved_in: docs/wiki/decisions/2026-08-12-argocd-application-ownership.md — o dono do spec e o operator; o api opera (/sync) e despeja. O caminho que continua descoberto e o B-074
severity: média — o efeito observado (Application órfã em `Unknown`) já tem contorno manual documentado; o custo é recorrente e o diagnóstico é caro por não haver dono declarado do recurso.
dod:
  - está declarado quem é o **dono** do CR `Application` — um escritor, os outros pedem
  - a deleção órfã que o `CLAUDE.md` documenta como padrão conhecido tem correção ou dono, em vez de contorno manual entre execuções de teste
  - se a propriedade não puder ser unificada, a razão está escrita junto ao código dos três escritores

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-051 — Uma regra de segurança vive em dois módulos e as duas cópias já divergiram   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-11. A lista de prefixos de env reservados para injeção da plataforma existe duas vezes: `api/internal/services/deploycontracts/build_plan.go:23` tem `LOOPZERO_`, `KUBERNETES_`, `AWS_`, `DO_`, **`GOOGLE_`**; `tools/loopzero-dockerfile-lint/main.go:84` tem as mesmas menos `GOOGLE_`. O comentário do primeiro diz literalmente *"mirrors tools/loopzero-dockerfile-lint/main.go"*, e o atribui a *"T2.5 … closes find_sec_004 MEDIUM"* — é correção de segurança aplicada num caminho e não no outro. Nenhum dos 117 gates menciona o par (`grep -rl reservedEnvKeyPrefixes cmd/loopzero-ops/internal/quality/` → vazio). **Exploitabilidade verificada e declarada: latente.** Não existe injeção de `GOOGLE_*` em env de workload; as únicas ocorrências são `LOOPZERO_GOOGLE_CLIENT_ID/SECRET` no Kratos, que já caem sob `LOOPZERO_`. Issue #283.
why_now: `tools/loopzero-dockerfile-lint` é módulo Go próprio e **não está no `go.work`** — não há caminho de import possível entre ele e `api/`, então manter a regra única exigiria dependência declarada ou gate. Não existe nenhuma das duas: sobrou um comentário, que é convenção que ninguém executa. Uma das duas listas já está errada e nada declara qual é a fonte de verdade.
status: planned
resolved_in: cmd/loopzero-ops/internal/quality/envprefixparity/ — gate AST que compara reservedEnvKeyPrefixes entre os dois sitios como CONJUNTOS. Entra reprovando, por decisao do dono sobre a direcao (B-069/B-070). Fatia slice-1.
severity: média — hoje não explorável, mas o defeito arma sozinho: no dia em que um prefixo novo passar a ser injetado, a segunda lista não acompanha e a descoberta é por incidente.
dod:
  - existe **uma** fonte de verdade importável pelos dois lados, ou um gate de paridade que faz AST-parse das duas declarações e exige conjuntos idênticos
  - está declarado qual das duas listas está correta — hoje as duas são afirmativas e discordam
  - `tools/loopzero-dockerfile-lint/loopzero-dockerfile-lint` (binário ELF de 3,2 MB versionado no git) é removido do controle de versão — o irmão `gen-service-auth-ed25519/` tem só fonte, o que sugere que a assimetria foi acidental

> Registrado 2026-08-11 pela auditoria de arquitetura.

> **2026-08-12 (fatia `slice-11`, `B-070`):** o gate que este item criou saiu do estado desligado.
> A divergência `GOOGLE_` que o mantinha mudo passou a **isenção datada** (sunset 2026-11-09) em
> `cmd/loopzero-ops/internal/quality/envprefixparity/allowlist.txt`, e o gate entrou em
> `task quality:gates`. A decisão de direção continua **aberta e sua** — o dado A2 segue invisível
> daqui. O que mudou é que a lacuna agora tem prazo e aparece impressa em toda execução verde, em
> vez de invisível por o gate inteiro estar desligado.

## B-052 — O `CLAUDE.md` aponta para cinco contratos que não existem   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `CLAUDE.md:206` declara nove contratos vivendo em `.claude/rules/`; **quatro existem**. Ausentes: `workflow.md` (citado em `:193` como o gate de 95% da Regra 11), `loopzero-design.md` (citado em `:159` como o contrato de *single-status-writer*), `tdd-workflow.md`, `cli-output.md` (o contrato da Regra 3), `design-principles.md`. Nenhum existe em outro lugar do workspace. Também ausente `../.claude/rules/umbrella-scope.md`, citado em `:17` como *"the workspace contract"*, e `.claude/rules/auto-docs.md` (`:215`). O inverso confirma a causa: **36 arquivos existem em `.claude/rules/` e 20+ nunca são mencionados** — todos do cycle-kit. Não é arquivo perdido; é troca de conjunto sem atualizar o índice. Mesma classe: **`ADR-0024` não existe** em lugar nenhum, e é citado como a autoridade que obriga toda migração para `UpdateStatusWithRetry` — por comentário de produção *e* pelo texto de uma métrica Prometheus. Issue #281.
why_now: a auditoria mediu o invariante *single-status-writer* aplicado em **2 de 8 controllers**. O contrato que definiria "aplicado" — `loopzero-design.md` — é um dos que não existem. A regra não está sendo violada por descuido: ela não está disponível para consulta.
status: planned
resolved_in: cmd/loopzero-ops/internal/quality/docsrefs/ — gate que resolve referencias normativas (.claude/rules/*.md e ADR-NNNN) com allowlist datado. Fatia slice-1.
severity: média — não quebra execução, mas todo agente e toda pessoa que seguir a tabela "Locked rules — read before the matching work" bate em arquivo inexistente exatamente antes do trabalho que a regra deveria governar.
dod:
  - cada contrato ausente foi **restaurado ou reapontado** — decidido caso a caso, porque parte do conteúdo migrou (o gate de 95% aparece hoje em `plan-confidence-golden-rule.md`) e recriar por adivinhação produziria documentação inventada, que é pior que a ausência
  - existe gate: toda referência `.claude/rules/*.md` no `CLAUDE.md` resolve
  - o mesmo cheque cobre referências `ADR-NNNN` em comentários de código Go — mesma classe, mesmo custo
  - as três divergências menores da mesma passada: `112` gates declarados contra **125** subpacotes; clusters chamados `{mgmt,build,workload,runtime-2}` contra `{core,build,cell-0,cell-1}`+`secrets` no disco; `pkg/correlation` ausente da lista

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-053 — `/api/v1/version` responde `"dev"` em toda imagem construída   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-11 e confirmado por experimento próprio fora do repositório. `api/go.mod:1` declara o módulo `github.com/getloopscale/loopzero/api`; `api/Dockerfile:82-84` injeta `-X github.com/getloopscale/loopzero/api/internal/routes.versionTag=...` — **org errada**. O doc-comment tinha o caminho certo, então os dois discordavam e quem constrói a imagem é o Dockerfile. **Atualizado em 2026-08-12**: a fatia slice-8 moveu a família para `api/internal/routes/version`, então o caminho correto passou a ser `github.com/getloopscale/loopzero/api/internal/routes/version.versionTag` — e por um momento **nenhum lugar do repositório carregou o caminho certo**, porque o doc-comment ficou apontando para o pacote antigo. Corrigido no mesmo movimento: `api/internal/routes/version/version.go` é de novo a referência única e correta, e é dela que o conserto do Dockerfile deve copiar. O erro do Dockerfile continua sendo o de **org** (`getloopscale`), não o de caminho. Experimento: caminho de import correto injeta o valor; caminho de org errada **sai com 0** e o default `"dev"` sobrevive. O linker não avisa, e nenhum gate cobre (`grep -rl "versionTag\|ldflags" cmd/loopzero-ops/internal/quality/` → vazio). Issue #284.
why_now: qualquer diagnóstico que dependa de "qual versão está rodando" — triagem de incidente, confirmação de rollout, verificação de fix em app-dev — lê `dev` e não distingue duas imagens diferentes. E há um segundo efeito: mover `version.go` para outro pacote **quebra a injeção de novo, em silêncio**, mesmo depois de corrigir o typo — o que torna isto pré-requisito do B-041.
status: triaged
severity: média — não corrompe nada, mas apaga a única forma de saber o que está no ar.
dod:
  - `api/Dockerfile:82-84` usa o caminho de módulo real
  - existe gate hermético que extrai todo `-X <path>.<símbolo>` dos Dockerfiles e valida que o path bate com o `module` do `go.mod` correspondente **e** que o símbolo existe naquele pacote via AST
  - está verificado que nenhum consumidor faz string-match em `"dev"` — a correção muda o valor retornado

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-054 — O `api` roda dois circuit breakers escritos independentemente sobre o mesmo pool Postgres   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `middleware/pgcircuitbreaker.go` é um portão percentual sem histerese; `admission/pg_breaker.go` é high/low-water atômico, e seu próprio doc cita **o modo de falha por flapping que o outro ainda tem**. Os dois são construídos no mesmo processo — `server/router.go:72` e `api/cmd/main.go:468`. E `pkg/circuitbreaker`, que existe no kernel compartilhado e é usado pelos operators, é importado por `api/` **zero vezes**.
why_now: duas implementações do mesmo conceito no mesmo processo divergem em silêncio — uma já corrigiu um defeito que a outra tem, e nada força a segunda a aprender. É o mesmo padrão do B-051, uma camada acima.
status: triaged
severity: média — nenhum incidente conhecido; o custo é comportamento de proteção inconsistente sobre o mesmo recurso.
dod:
  - existe **um** breaker sobre o pool Postgres, ou está escrito por que dois são necessários e o que cada um protege
  - se a resposta for "os dois medem coisas diferentes": os nomes passam a dizer isso (o achado sugere `PGPoolAdmissionGate` para o de saturação), porque hoje ambos se chamam circuit breaker e não são a mesma coisa
  - fica decidido se `pkg/circuitbreaker` deve ser a primitiva compartilhada — **sem forçar**: o achado registra o contra-argumento de que os dois do `api` são medidores de saturação de pool, não contadores de falha, e podem não caber no `Execute`

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-055 — O invariante de escritor único de status vale em 2 de 8 controllers, e o helper que o repara não tem chamador   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11 e conferido pelo gate de qualidade da fase 3. O merge-patch a partir de snapshot base — o padrão que o `CLAUDE.md:145-160` descreve como o contrato — está aplicado em **2 dos 8 controllers de CRD**. E `environment_controller.go:142` registra `DeferReadinessSummary`, que é um **segundo** escritor de status (`readiness.go:59`, `_ = c.Status().Update(ctx, latest)`), no mesmo passo em que o comentário do próprio arquivo (`:166`) diz tê-lo removido; há outros `Status().Update` crus em `:320` e em `app/application_controller.go:235`. Separadamente, `conditions.UpdateStatusWithRetry` (`operators/internal/conditions/retry.go:85`, 165 LOC) tem **zero chamadores de produção** — 24 referências, todas em `_test.go` — e seu contador Prometheus só incrementa em `metrics_test.go:38`. O ADR que o manda migrar **não existe** (ver B-052).
why_now: o `MEET-2026-07-02-002` estabelece a decisão do merge-patch e a pré-condição de escritor único, e sua própria linha de risco diz *"a future 2nd status-writer must revisit"* — que é exatamente o defeito medido. A cobertura de 2 em 8 é extrapolação heurística: a reunião tratava do Environment e não mandatou os oito.
status: shipped
severity: alta — perda de escrita de status sob concorrência é o modo de falha que o merge-patch existe para evitar, e ele está garantido em um quarto dos controllers.
dod:
  - está decidido se o invariante vale para os oito controllers ou só para os que a reunião cobriu — e isso está escrito
  - `DeferReadinessSummary` deixa de ser um segundo escritor, ou o comentário que diz tê-lo removido é corrigido
  - `UpdateStatusWithRetry` ganha chamadores ou é deletado — um helper com ADR inexistente, métrica que nunca incrementa e zero uso é dívida que parece cobertura

> Registrado 2026-08-11 pela auditoria de arquitetura. Uma das três instâncias do padrão sistêmico *declarado + testado + nunca ligado* (ver B-042 e B-045).
>
> **Fechado 2026-08-12** pela fatia `slice-10-status-writer-contract`, ADR
> `docs/wiki/decisions/2026-08-12-status-writer-contract.md`, ata `MEET-ASYNC-AMENDMENT-2026-08-12-006`.
> As três balas: (1) decidido e escrito — o invariante vale para os oito, e a regra que perdeu foi
> apagada porque citava um ADR inexistente; (2) `DeferReadinessSummary` deixou de usar o padrão
> proibido e parou de descartar o erro; (3) `UpdateStatusWithRetry` deletado com o contador morto e
> as 8 citações a `ADR-0024`.
>
> **Três correções à evidência acima.** O helper é registrado em **cinco** controllers, não em um:
> `clustercredential:97`, `gitrepositorybinding:85`, `platformbootstrap:105`, `environment:149`,
> `preview:180` — o item cita `:142`, que é `:149`. E a medição de que *"o comentário do próprio
> arquivo diz tê-lo removido"* não se reproduziu: o doc-comment descreve o helper como ativo e
> nomeia os cinco consumidores (errando só um caminho, corrigido aqui). E *"24 referências, todas em
> `_test.go`"* subconta: havia 7 fora de teste (5 em `retry.go`, 2 em `metrics.go`).
>
> A migração dos 6 controllers restantes (**25** chamadas; 27 no repositório todo) segue aberta em
> **B-085**. O `B-086` mudou de assunto: a revisão desta fatia mediu que o gate `docs-contract-refs`
> **já varre Go** — eu havia afirmado o contrário sem medir —, e o achado real é outro.
>
> **A revisão independente rodou antes do commit e encontrou um HIGH de corretude** que teria
> passado: o merge-patch simples carrega o array de conditions inteiro (RFC 7386 trata array como
> escalar), então apagaria a escrita de outro ator. Corrigido com
> `MergeFromWithOptimisticLock` e um teste que encena a corrida — mesma lição da fatia slice-9.

## B-056 — Dois charts diferentes têm o mesmo nome, em duas árvores com ciclos de vida distintos   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `infra/helm/loopzero-api/Chart.yaml:2` (v0.1.0) e `charts/loopzero-api/Chart.yaml:2` (v0.2.0) declaram **o mesmo `name`**; o mesmo vale para `loopzero-cloud`. São charts diferentes, com templates diferentes e ciclos de vida diferentes (bootstrap Helmfile L2 × publicado OCI L3), e nenhum é superconjunto do outro. Renomear é preservador de comportamento: o nome do release vem da chave `name:` do helmfile (`core.helmfile.yaml.gotmpl:481`), não do diretório do chart. Some-se: `loopzero-common` é dependência de 9 dos 13 charts publicados e de **zero** charts de bootstrap.
why_now: o `CLAUDE.md:171` manda manter os pares em paridade — *"when patching one, apply the same fix to the other"* — e a única forma de saber de qual se fala é pelo caminho. Um nome ambíguo num contrato que exige edição sincronizada é armadilha de leitura, e o B-046 é o caso em que ela já custou.
status: triaged
severity: média — não quebra instalação; torna o contrato de paridade difícil de discutir e de auditar.
dod:
  - os nomes deixam de colidir, ou existe convenção escrita que diz como se refere a cada um
  - o nome do release instalado não muda (verificado contra a chave `name:` do helmfile)

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-057 — Três pacotes chamados `domain` significam três coisas, e o registro de portas não é um modelo de domínio   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-11. Existem `pkg/domain` (vocabulário de produto), `api/internal/domain` (camada DIP) e `api/internal/services/domain` (domínios **DNS**). A colisão forçou `package domainsvc` (`services/domain/domain_provisioner.go:4`) e `package placementsvc` — as **únicas duas** divergências entre nome de diretório e nome de pacote em toda a árvore Go. E `api/internal/domain` não é modelo de domínio: `types.go` (21 KB) declara `DomainStore`(:87), `TeamStore`(:147), `AuditStore`(:184), `SecretStore`(:205), `ProjectStore`(:335) e `TenantCRManager`(:353) — seis contextos sem relação num arquivo. As portas são anuladas na prática por **49 alias re-exports** em `routes`, e cada porta de consumidor único tem como único consumidor a própria linha de alias. **Positivo medido:** o pacote importa somente a stdlib — a disciplina de camada que falta em outros lugares é demonstrável nele.
why_now: a duplicação da palavra em dois níveis é consequência direta do topo em forma de camada (B-041); resolver ali torna o rename aqui quase mecânico. E o `types.go` único é o que faz uma porta de secrets e uma porta de billing mudarem no mesmo arquivo.
status: triaged
severity: média — não quebra nada; custa navegação e força dois nomes de pacote que mentem sobre o diretório.
dod:
  - `api/internal/domain` passa a ter nome que diz o que ele é (o alvo medido é `ports`), com um arquivo por contexto
  - os 49 alias re-exports são deletados — hoje eles fazem a porta e o consumidor serem a mesma linha
  - `domainsvc` e `placementsvc` voltam a ter nome de pacote igual ao do diretório
  - a propriedade de importar só stdlib é preservada, e verificada

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-058 — Três pacotes admitem membro pela tecnologia ou pelo padrão, não pela responsabilidade   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-11. **`api/internal/k8s`** (coesão 3) agrupa três domínios pela biblioteca cliente que compartilham: tenancy (`tenant_cr_manager.go` 322L, `tenant_evictor.go` 333L), workload (`application_cr_manager.go`, `preview_cr_manager.go`) e secrets (`secret_k8s_manager.go` 371L). **`api/internal/adapters`** (coesão 3) é nomeado pelo padrão, então qualquer tipo-ponte qualifica — o próprio doc (`adapters.go:4`) é o critério de admissão, e por isso **não pode recusar nada**: 11 arquivos, 50 exports, cobrindo build, tenancy, release, secrets e SSE. Um deles, `vault_transit_adapter.go:9`, declara *"Signatures match byte-for-byte; only the method names differ"* — é ponte de código interno para código interno. **`services/platform`** (coesão 3) reúne 8 concerns de faxina sem par compartilhado; a propriedade comum real é "roda no binário controller", e o `servicesacyclic` agora defende esse agrupamento.
why_now: são os três pacotes cuja regra de admissão não consegue dizer não. Enquanto ela for "usa client-go" / "é um adapter" / "é um loop de fundo", todo arquivo novo cabe — e a auditoria mediu que já cabem domínios diferentes em cada um.
status: triaged
severity: média — nenhum defeito de comportamento; é a superfície por onde a mistura de domínios entra sem ninguém decidir.
dod:
  - `k8s` encolhe para plumbing real (cliente tipado, resolução de env), com tenancy, workload e secrets em casas próprias
  - `adapters` é dissolvido — cada arquivo vai para junto da porta que satisfaz — ou ganha critério de admissão que consiga recusar
  - `services/platform` ou ganha nome que diga a propriedade real (roda no controller), ou é dividido por concern
  - `vault_transit_adapter.go` é **deletado**, não movido (as assinaturas já batem)

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-059 — O transporte carrega o schema de payload de cada domínio e faz body-snooping   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `middleware` mistura cinco concerns (autenticação, auditoria, rate limit, observabilidade, portões de política) e `audit.go:24` declara `projectIDBodyKeys = []string{"project_id","project","projectId"}` — com o comentário em `:21-23` explicando *"builds use project_id, domains use project"* — e faz buffer de até **64 KiB** do corpo da requisição para enriquecer `resource_id`. Isso é conhecimento por domínio dentro do transporte. **Refutação registrada:** a hipótese inicial de que o `CostEnforcer` misturava política em transporte **não se sustentou** — ele delega a decisão de cap para `pkg/enforcement` em `cost_enforcement.go:90`. O que sobra dele é uma heurística de negócio no caminho de cópia (`:154`, "1,5× do cap ⇒ downgrade de plano").
why_now: o `audit.go` sabe, hoje, como cada domínio nomeia o próprio identificador de projeto — três grafias, mantidas à mão. Todo domínio novo que use uma quarta grafia perde `resource_id` no log de auditoria, em silêncio.
status: triaged
severity: média — o dado de auditoria degrada sem erro, que é o pior modo para dado de auditoria.
dod:
  - o identificador de recurso chega ao middleware de auditoria **declarado**, não adivinhado do corpo
  - o body-snooping de 64 KiB é removido
  - existe verificação de que toda rota mutante continua gravando linha de auditoria com `resource_id` — hoje nenhum gate assere isso, e o achado não é validável por máquina sem ela
  - `middleware` fica agrupado pelos cinco concerns em vez de um pacote plano

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-060 — O único lugar onde os quatro planos se encontram é uma pasta de arquivos irmãos   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-11. `operators/internal/tenant` (coesão 3) é **o único lugar do codebase onde os quatro planos se tocam**, e se tocam sem costura: `tenant_build.go` (571L) + `tenant_harbor.go` (395L) para BUILD, `tenant_gitops.go` (439L) para CONTROL, `tenant_vault.go` (75L) para SECRETS, mais os recursos de tenant em `reconcilehelpers` (`limitrange.go:76`, `networkpolicy.go:34`) que são compartilhados pelos quatro binários. O tipo `loopzerov1.Tenant` é referenciado de `operators/internal/{tenant,environment,app,preview,reconcilehelpers}` — a tenancy cruza **3 dos 4 binários**. Pontuou 3 e não menos porque o agregado Tenant **é** a unidade certa: um tenant realmente precisa de todos os quatro.
why_now: se a separação de domínio for feita só no `api` (B-041) e não aqui, os planos ficam nomeáveis de um lado da plataforma e anônimos do outro. O molde já existe no próprio repositório: os provisioners `gitea`/`argocd`/`minio`/`harbor` são facades estreitas por serviço externo, medidas como corretamente aplicadas.
status: triaged
severity: média — o agrupamento é defensável; o que falta é a costura que diga qual plano cada arquivo serve.
dod:
  - existe interface de provisioner por plano em `operators/internal/tenant`, copiando a forma que os provisioners externos já usam
  - `reconcilehelpers` deixa de carregar política de tenant compartilhada pelos quatro binários sem dono declarado (ver B-061)
  - o reconciler continua sendo um só — a divisão é de costura, não de binário (o split por perfil operacional dos 4 binários foi medido como deliberado e **não** deve ser desfeito)

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-061 — `reconcilehelpers` guarda três assuntos sem relação, e o doc dele ainda chama o pacote de `shared`   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `operators/internal/reconcilehelpers` pontuou coesão **2/5** e reúne três concerns sem par: bootstrap de binário, templating de recursos de tenant (`limitrange.go:76 ApplyTenantLimitRange`, `networkpolicy.go:34 ApplyTenantNetworkPolicies`) e roteamento de cell. É o **único** pacote com nome da família "helpers" em toda a árvore Go — o limiar `consensus` `generic_folder_names = 0` é respeitado em 148 módulos e 730k LOC, e esta é a única exceção real. E o doc do pacote ainda o chama de `shared`, um nome que ele não tem mais: `go doc` renderiza um nome que não existe.
why_now: é o pacote que os quatro binários de operator compartilham, então o que entrar nele é imposto aos quatro. Um nome que não consegue recusar nada, num pacote com esse alcance, é onde a próxima mistura entra.
status: triaged
severity: baixa — nenhum defeito de comportamento; é higiene com alcance grande.
dod:
  - os três assuntos ficam em pacotes que dizem o que guardam
  - o doc do pacote deixa de nomear um pacote que não existe
  - a política de tenant (LimitRange, NetworkPolicy) tem dono declarado — hoje mora no pacote compartilhado por todos

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-062 — O vocabulário do plano de build está espalhado por quinze pacotes, incluindo o sink de domínio   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-11. Um grep por `BuildKit|cosign|WorkflowSubmitter|argoproj…Workflow` bate em **15 pacotes distintos** de `api/internal` — incluindo o sink de domínio: `services/deploycontracts/shared_contracts.go:89` declara `NoCache bool // Skip BuildKit layer cache (--no-cache)`. Do lado da infra o plano é explícito (`build.helmfile`, `build-cluster.yaml`, `charts/loopzero-build-cwts`); do lado do código não há endereço. Os quatro pacotes api-side que de fato são build (`argo`, `source`, `shellgen`, `admission`) somam 10 importadores de produção.
why_now: é a metade de código do plano que a pergunta original nomeia em segundo lugar. E o custo do gate depende disto: uma verificação que confine o vocabulário de build precisaria hoje enumerar **15 prefixos**; depois de aninhar os quatro pacotes, **3** — a diferença entre um gate mantenível e um que ninguém mantém.
status: triaged
severity: média — nada quebrado; é a razão de "o que é build?" não ter resposta no código.
dod:
  - os quatro pacotes api-side de build vivem sob um pai que diz o plano
  - existe gate `buildplanevocabulary`: os literais de build só aparecem sob os prefixos declarados
  - o sink de domínio deixa de declarar campo com semântica de BuildKit, ou está escrito por que ele precisa

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-063 — Um módulo de infraestrutura depende de uma suíte de testes, e `tools/` está fora do workspace   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11 lendo cada `go.mod`. `infra/scripts` (`layer=infrastructure`) declara dependência `require` em `infra/tests` (`layer=test`) — `infra/scripts/go.mod:5`, consumida por `seed_e2e.go:26` que importa `infra/tests/seed`. É a **única aresta em direção a teste** no grafo dos 8 módulos do workspace, que no mais é acíclico e com a direção correta (`pkg` na base, `api` importando `operators/api` e não o contrário). Separadamente: `tools/gen-service-auth-ed25519` e `tools/loopzero-dockerfile-lint` têm `go.mod` próprios e **não estão no `go.work`** — vivem na árvore, fora do workspace que o build cobre.
why_now: o `seed` é usado por produção de fixtures e por teste, e hoje mora do lado de teste — então o módulo de scripts arrasta a suíte inteira. E `tools/` fora do workspace é a razão estrutural de o B-051 existir: não há caminho de import possível para compartilhar a regra.
status: triaged
severity: baixa — compila e funciona; é direção de dependência que contraria a própria etiqueta de camada.
dod:
  - o pacote `seed` mora onde a direção de dependência não inverte, ou a etiqueta `layer` de um dos dois é corrigida
  - está decidido se `tools/` entra no `go.work` — a decisão destrava o B-051 e é pré-requisito dele se a saída for fonte única compartilhada

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-064 — O control plane alcança o plano de secrets por um nome DNS reescrito em tempo de resolução   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `infra/helm/loopzero-api/values.yaml:118` aponta por padrão para um nome DNS **do mesmo cluster**, e a topologia de cluster separado mantém esse literal e o **reescreve no CoreDNS** (`values/dev-public/loopzero-api.yaml.gotmpl:69-76`). Ou seja: nem o código nem o chart conseguem distinguir se estão falando com um OpenBao no mesmo cluster ou no cluster `loopzero-secrets` — a diferença vive na resolução de nomes. Junto: um `role_id` de AppRole do plano de secrets está **commitado dentro do values file do control plane** (`values/dev-public/loopzero-api.yaml.gotmpl:77`). O `secret_id` está corretamente fora — **não é comprometimento**, é observação de fronteira.
why_now: o `CLAUDE.md` classifica instalar OpenBao no core como violação de topologia em qualquer tier (ver B-047), mas o valor padrão do chart aponta para o mesmo cluster. As duas coisas não podem estar certas ao mesmo tempo, e hoje o que decide qual vale é uma reescrita de DNS que não aparece em nenhum dos dois lugares.
status: triaged
severity: média — funciona hoje; o custo é que a fronteira mais forte da plataforma em infra é invisível no chart e no código.
dod:
  - o endereço do plano de secrets é explícito no chart em vez de depender de reescrita de resolução
  - está decidido se o `role_id` deve viver no values do control plane ou vir junto do `secret_id`
  - a topologia de cluster separado é legível a partir do chart, sem precisar do CoreDNS para descobri-la

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-065 — Um contexto delimitado vazio está pré-aprovado no DAG congelado   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `api/internal/services/billing` é um diretório **vazio e não rastreado pelo git** — não existe num clone novo. Mas `servicesacyclic/check.go:87` o registra como contexto delimitado, folha pura, no DAG congelado. A explicação não é descuido: billing foi extraído para `../loopzero-cloud` no M4 (`CLAUDE.md:14`); o código mudou de repositório e o registro no gate ficou. Resultado: o domínio de billing hoje está em três lugares — a lógica no `loopzero-cloud`, a rota `GET /api/v1/billing` ainda servida daqui (`server/router.go:519`), e um contexto fantasma no meio.
why_now: o `servicesacyclic` existe para que um subpacote **novo e não registrado** reprove, forçando decisão consciente. Um contexto vazio pré-registrado **gasta essa decisão adiantado**: alguém pode criar `services/billing` e começar a importar para lá sem que o gate force ninguém a pensar.
status: triaged
severity: baixa — sem impacto hoje; é uma folha do gate emprestada a código que não existe mais aqui.
dod:
  - a entrada `billing` sai do DAG congelado, ou existe código que a justifique
  - está escrito onde o domínio de billing mora, já que a rota continua sendo servida deste repositório

> Registrado 2026-08-11 pela auditoria de arquitetura. Este item é também o exemplo medido do limite de escopo: a auditoria cobriu **um** repositório, e billing é um domínio que não cabe em um.

## B-066 — Três suítes de invariante são testes Go comuns, invisíveis ao oráculo de gates   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `opencostgate`, `phase2gate` e `phase3gate` vivem sob `cmd/loopzero-ops/internal/quality/` e contêm apenas `doc.go` + dois `_test.go` cada — **zero entradas no `gateRegistry`**, importados por nada. Portanto `loopzero-ops quality --list`, que o `CLAUDE.md` designa como **o oráculo** do inventário de gates, não consegue enumerá-los. Eles rodam como testes Go comuns ou não rodam; de qualquer forma, quem consultar o oráculo não sabe que existem.
why_now: o repositório tem um mecanismo bom para esse problema — o `quality_gate_wiring_test.go` exige que todo gate registrado esteja ligado ou isento. Ele não alcança estes três porque eles nunca foram registrados. É a terceira instância do padrão *declarado, testado, nunca ligado* (as outras duas: B-042 e B-055).
status: triaged
severity: baixa — as asserções podem estar rodando via `go test`; o defeito é o inventário mentir por omissão.
dod:
  - as três suítes são registradas como gates, ou movidas para fora de `internal/quality/` para não parecerem gates
  - o oráculo `--list` volta a enumerar tudo que vive ali

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-067 — 43 migrations sem teste e 33 alertas sem runbook, medidos por gates que ninguém executa   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11 rodando os gates desligados. `migration-test-coverage` reprova com **43 violações** — migrations `000017`..`000059` sem `api/tests/unit/migration_{NNNNNN,NNN}*_test.go` correspondente, de **59** migrations no total. `alert-runbooks` reprova com **33 violações** — alertas sem `runbook_url`, incluindo `LoopZeroUnsignedReleasePushed`, `LoopZeroTenantDeletionStuck`, `LoopZeroSecretESOSyncFailing`, `LoopZeroAttestationSaveFailing`. Os dois gates existem, funcionam e não estão ligados (ver B-045).
why_now: são dois números que ninguém tinha porque o instrumento que os produz não roda. Migração sem teste é irreversível por desenho (o repositório é forward-only); alerta sem runbook acorda alguém às 3h sem dizer o que fazer — e três dos 33 são de assinatura de release e de vazamento de secret.
status: triaged
severity: média — dívida acumulada, não defeito ativo. **Deliberadamente separado do B-045**: ligar os gates e pagar a dívida são trabalhos diferentes, e ligá-los antes de ter plano transformaria 76 achados legítimos num muro que alguém desliga na sexta-feira.
dod:
  - existe decisão sobre o piso: quais migrations e quais alertas precisam de cobertura, e quais têm exceção **registrada com prazo**
  - os alertas de segurança (`LoopZeroUnsignedReleasePushed`, `LoopZeroSecretESOSyncFailing`, `LoopZeroAttestationSaveFailing`) têm runbook — estes não entram em exceção
  - só depois disso os dois gates são ligados no `quality:gates`

> Registrado 2026-08-11 pela auditoria de arquitetura.

> **Correção 2026-08-11, ao planejar a fatia 1.** Os dois números **já estavam no registro** desde 2026-08-06, em `quality_gate_wiring_test.go`, com rationale melhor do que o deste item: 42 migrations (hoje 43 — uma nova entrou) e 33 alertas, ambos classificados `failingGate`, e a observação decisiva de que os runbooks existentes **devolvem 404**, então o gate mede presença de string, não existência de runbook. O item continua válido como *dívida a pagar*, mas deixa de ser descoberta: a descoberta é anterior e melhor documentada.


## B-068 — Um ponto de variação de Strategy que não varia mais, mantido por construir e descartar um objeto   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-11. `api/internal/argo/languages.go:231` — `dockerfileStepTemplateName` **constrói um container descartável só para ler o nome dele**, mantendo um ponto de variação que as três implementações de `LanguageStrategy` já resolvem igual: todas retornam `generate-dockerfile`. O restante do padrão foi medido como **corretamente aplicado**: lookup por registry, sem `if app.Build ==` no orquestrador, interface coesa de 8 métodos, variação real entre estratégias de linguagem e de Dockerfile do usuário.
why_now: é o resíduo de uma variação que existiu — o `UserDockerfileStrategy` emitia `dockerfile-lint`. Enquanto o ponto de variação ficar, todo leitor precisa checar se ainda varia, e a construção descartável esconde que não.
status: triaged
severity: baixa — custo de leitura, sem defeito de comportamento.
dod:
  - o nome do template vem de onde varia, ou a variação é removida junto com a construção descartável
  - se a variação deve voltar (por exemplo, se o `dockerfile-lint` for reintroduzido), isso está escrito no lugar em vez de mantido por um objeto descartado

> Registrado 2026-08-11 pela auditoria de arquitetura.

## B-069 — A mesma regra de env reservada descarta em silêncio de um lado e mata o build do outro   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-11 ao planejar o `B-051`. As duas implementações da regra `find_sec_004` não diferem só na lista de prefixos — diferem na **severidade**. Lado API (`api/internal/services/deploycontracts/build_plan.go:120-124`): chave reservada gera `slog.Warn` e `continue` — a env é **descartada em silêncio** e o deploy prossegue. Lado linter (`tools/loopzero-dockerfile-lint/main.go:296` → `:158-159`): `SeverityReject` vira `os.Exit(1)` — o **build falha**. E o linter está no caminho de build real: `api/internal/argo/languages.go:36` e `workflow_builder.go:408` emitem o passo `dockerfile-lint` para `UserDockerfileStrategy`.
why_now: o gate `env-prefix-parity` (`B-051`) compara os dois **conjuntos de prefixos**. Conjunto igual não é regra igual — ele passaria verde com esta divergência intacta, e um gate verde é lido como *"a regra está unificada"*. Sem este item registrado, o `B-051` fecharia deixando para trás a metade que importa: qual das duas severidades é a correta.
status: triaged
severity: média — nenhuma quebra nova; o custo é que o usuário recebe tratamentos opostos para a mesma violação, dependendo de por qual porta ele entrou.
dod:
  - está decidido qual severidade é a correta para a regra — descarte silencioso ou rejeição
  - a decisão vale para as duas portas, ou está escrito por que elas devem divergir
  - o `env-prefix-parity` declara no doc-comment que prova conjuntos, não semântica — para verde não ser lido como mais do que é

> Registrado 2026-08-11 pelo Challenger do `MEET-ASYNC-AMENDMENT-2026-08-11-001`, ao verificar o desenho do gate do `B-051`.

> **2026-08-12 (fatia `slice-11`, `B-070`):** o eixo da **caixa** foi decidido e passou a ser
> vigiado pelo gate. Este item — o eixo da **severidade** — segue aberto, e pela mesma razão do eixo
> do conjunto: tornar o descarte silencioso da API explícito muda o resultado de deploys que hoje
> sucedem. Continua sendo a única das três divergências em que nenhum lado é obviamente o certo.

## B-070 — `ENV aws_region` já quebra o build hoje, e a API aceitaria a mesma chave   [x]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido **ao vivo** em 2026-08-11 executando o binário real (`GOWORK=off go run .` em `tools/loopzero-dockerfile-lint`) contra um Dockerfile com `ENV aws_region=us-east-1`: `{"rule":"RESERVED_ENV_KEY","severity":"REJECT",...}` e `exit status 1`. A causa é sensibilidade a maiúsculas divergente: o linter compara `strings.HasPrefix(strings.ToUpper(key), p)` (`main.go:491`), a API compara `k[:len(p)] == p` (`build_plan.go:36`), e o comentário em `:32-33` **documenta** a escolha da API como *"exact-prefix and case-sensitive"*. O lado do linter usa `ToUpper` **sem comentário**, e `git log -S` sobre o símbolo devolve um único commit que não separa a decisão.
why_now: esta divergência é **ativa**, não latente — diferente do `GOOGLE_` do `B-051`, que é potencial. Um tenant que escreva `ENV aws_region` em minúsculas tem o build rejeitado hoje, com uma mensagem que nomeia prefixos em maiúsculas (`LOOPZERO_/KUBERNETES_/AWS_/DO_`) e não explica que o casamento ignora caixa. Se houver tenants nesse estado, existe quebra que ninguém atribuiu a esta regra.
status: shipped
severity: média (rebaixada de alta em 2026-08-12) — a premissa de 'já rejeita build de terceiro hoje' NAO se sustentou: a revisão da fatia `slice-11` mediu que o linter nao e invocado, porque o caminho BYO-Dockerfile foi removido como superficie de RCE (`api/internal/argo/languages.go:138-143`, `UserDockerfileStrategy` com zero definicoes). A medicao original era do binario ISOLADO. Resta divergencia real entre duas implementacoes da mesma regra, uma delas sem chamador.
dod:
  - está decidido se o casamento ignora caixa ou não — e os dois lados passam a concordar
  - a mensagem de rejeição nomeia a regra de casamento, não só a lista de prefixos
  - a lista hard-coded em prosa na mensagem (`main.go:300-301`) deixa de poder divergir do slice
  - existe medição de quantos Dockerfiles de tenant estão nesse estado antes de a decisão ser aplicada (dado A2 do MEET-ASYNC-AMENDMENT-2026-08-11-001)

> Registrado 2026-08-11 pelo Challenger do `MEET-ASYNC-AMENDMENT-2026-08-11-001`. Medido ao vivo, não inferido.
>
> **Fechado 2026-08-12** pela fatia `slice-11-reserved-env-case`, ADR
> `docs/wiki/decisions/2026-08-12-reserved-env-case-matching.md`, ata
> `MEET-ASYNC-AMENDMENT-2026-08-12-007`. Binário real medido antes e depois: `ENV aws_region` saía
> **1** e passa a sair **0**; `ENV AWS_REGION` continua saindo **1**.
>
> As quatro balas: (1) decidido — casamento sensível a caixa, muda o linter, porque a colisão que a
> rejeição previne não é possível no Linux e a direção só relaxa; (2) e (3) a mensagem nomeia a regra
> de casamento e deriva a lista do slice; (4) **respondida, não cumprida** — a contagem de
> Dockerfiles de tenant nesse estado não é visível deste repositório, e para **este** eixo ela não
> altera a decisão, porque relaxar não quebra caso nenhum. Continua bloqueante para o eixo do
> conjunto.
>
> **Duas correções de premissa, achadas pela revisão independente antes do commit.** (a) O linter
> **não está** no caminho de build: as duas citações que eu usava como prova são *comentários*, e o
> código logo abaixo diz o oposto. Severidade rebaixada, e o CHANGELOG deixou de prometer um
> conserto que não pode acontecer. (b) Existe uma **terceira** implementação da regra —
> `api/internal/source/validator_env.go:46`, conjunto diferente (`DYLD_`/`KUBERNETES_`/`LOOPZERO_`), e
> **essa está ligada**. O gate afirmava paridade sobre 2 de 3; o texto dele foi corrigido e a
> terceira virou item próprio.
>
> **Uma correção ao próprio plano, achada pelo RED:** eu havia previsto que `AWS_region` deixaria de
> casar. Errado — ele *começa* com `AWS_`, e a API o reserva também (`k[:4] == "AWS_"`). Asserir o
> contrário abriria divergência **nova**. O que muda é a caixa do prefixo, não a do sufixo.
>
> Efeito colateral no `B-051`: o gate que ele criou saiu do estado desligado. A divergência
> `GOOGLE_` virou isenção datada (2026-11-09) em vez de manter os três eixos sem guarda.

## B-071 — `task test:unit` está vermelho na `workspace`, e a causa é um par que se desencontrou   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-11 ao rodar a validação de integração da fatia `slice-1-tripwires`. `task test:unit` sai 1 com **4 reprovações**, todas em `api/tests/unit`: `TestMigration006CellsSeedRows`, `TestMigration007ActivatesOnlyCell2`, `TestMigration007AppendsAuditNote`, `TestMigration007ReferencesADRAndPlan`. A mensagem é `seed row missing "'cell-0'"`. Bissecção: em `fbb24643` (2026-08-10, *"funde as três identidades de célula"*) o teste e a migration **concordavam** — os dois falavam `cell-0`/`cell-1`. Depois disso, `75af13c7` (*"migracoes historicas voltam ao original; so a 000061 transforma"*) reescreveu `000006_cells.up.sql` de volta para `cell-1`/`cell-2`/`cell-default` (12 inserções, 12 remoções) e **não tocou o teste**, que continua exigindo a forma nova (`git diff fbb24643 HEAD -- api/tests/unit/migration_006_cells_test.go` é vazio).
why_now: apareceu porque a fatia `slice-1-tripwires` tem `task test:unit` verde na Global DoD e **não pôde marcá-la** — o vermelho é anterior e alheio. Enquanto ele durar, toda fatia seguinte herda um sinal que não distingue "eu quebrei" de "já estava assim", que é exatamente o que a baseline pré-migração do plano existe para evitar. E o vermelho é de **teste de migration**, num repositório com migrations forward-only: o par teste↔migration é o único lugar que documenta qual forma é a correta.
status: planned
severity: alta — não pelo defeito em si (nenhuma das duas formas está em produção por causa deste teste), mas porque uma suíte vermelha por motivo alheio é como uma suíte deixa de ser lida.
resolved_in: os 4 testes de migracao estao verdes, e a causa era UMA. As migracoes `000006` e `000007` sao historia imutavel e falam de `cell-default`/`cell-1`/`cell-2`; a `000061` (cell_identity_unification) depois fundiu tudo em `cell-0`/`cell-1`. Os testes foram reescritos com a identidade NOVA e continuaram lendo os arquivos ANTIGOS. A correcao NAO foi editar a migracao — `docs/README.md` e explicito que reescrever um registro datado para parecer atual destroi a unica prova do que era verdade entao. Foram os testes que voltaram a ler historia, com o porque escrito no arquivo. Bonus: o `TestMigration006CellsSeedRows` listava `'cell-0'` DUAS vezes, o que duplicava a falha e escondia que a terceira entrada nunca fora verificada. `go test ./...` em `api` sai **0 FAILs**.
dod:
  - está decidido qual forma é a correta — `cell-0`/`cell-1` (a fusão de identidades do `B-005`) ou `cell-1`/`cell-2`/`cell-default` (o original restaurado por `75af13c7`)
  - o teste e a migration voltam a concordar, e a decisão está escrita onde a próxima pessoa a encontre
  - `task test:unit` volta a sair 0 na `workspace`
  - se as duas formas devem coexistir por um período, isso está declarado — hoje o par apenas discorda

> Registrado 2026-08-11 durante a validação de integração da fatia `slice-1-tripwires`. Bissectado até o commit; **não** causado por esta fatia, que não toca `api/`.

## B-072 — A policy versionada nega ao `api` a criação de chave que o `api` faz   [x]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-12 ao decidir o `B-042`. `infra/helm/openbao-init/values.yaml:187-201` define a policy `loopzero-api` com **apenas** `transit/encrypt/*` e `transit/decrypt/*` — **nenhuma capacidade em `transit/keys/*`**. Mas o caminho lazy do `api` cria a chave: `pkg/vault/transit_client.go:274` reage ao 404 do encrypt com um POST para `/v1/transit/keys/...` (`:380-392`). **Correção 2026-08-12 (review da fatia 2):** a redação original dizia que *"o único lugar que concede a capacidade é um script de desenvolvimento"*. **Falso.** Há pelo menos dois, e o segundo é operacional: `docs/operations/openbao-transit-cutover-runbook.md:117-133` escreve a policy `loopzero-api-approle` com `path "transit/keys/loopzero-tenants-*" { capabilities = ["create","read","update"] }`. O outro é `infra/scripts/local/vault-bootstrap-dev.sh:429-434`. Isso muda a moldura: **não é ausência de autorização, é drift entre o chart e o runbook** — alguém autorizou em dois lugares e o chart não acompanhou. A correção é a mesma; a descrição errada mandaria a próxima pessoa procurar uma decisão que já existe.
why_now: a auditoria de 2026-08-11 registrou que "as chaves são criadas lazily pelo `api`" e usou isso para baixar a severidade do `B-042`. Isso vale para o ambiente de desenvolvimento; **em qualquer ambiente que carregue as policies do chart, não vale** — a criação é negada por desenho. E o erro tipado não tem tratamento a jusante: `grep ErrTransitKeyCreateFailed api/` devolve zero hits, e em `api/internal/routes/secrets.go:503-510` a negação de policy é indistinguível de timeout de rede para quem chama. O ADR `2026-08-12-tenant-transit-key-ownership` torna isto pré-requisito: a policy vem **antes** da deleção da seam do operator.
status: shipped
severity: alta — é o caminho de escrita de segredo de tenant, e a falha é silenciosa do ponto de vista de quem chama.
dod:
  - a policy `loopzero-api` do chart concede `transit/keys/loopzero-tenants-*` com `create`, `read` **e `update`** — `update` não é opcional: o POST do cliente Go usa HTTP PUT, que o OpenBao aplica com semântica de `update` mesmo quando a chave ainda não existe (nota LIVE-caught em `docs/operations/openbao-transit-cutover-runbook.md:125-131`). A redação original pedia só o path, e teria reproduzido o defeito que aquele runbook registrou
  - o chart e o runbook deixam de divergir — hoje o runbook concede e o chart não
  - `ErrTransitKeyCreateFailed` deixa de ser indistinguível de erro de rede na resposta ao usuário
  - existe verificação de que o nome de chave que o chart autoriza é o mesmo que o código usa — hoje o operator usa `tenant-<id>` e o `api` usa `loopzero-tenants-<id>`

> Registrado 2026-08-12 ao escrever o ADR do `B-042`. Medido, não inferido.

> **NÃO fechado. A fatia `slice-17` avançou e a revisão mostrou que não basta.** ADR
> `docs/wiki/decisions/2026-08-13-transit-key-policy.md`, ata `MEET-ASYNC-AMENDMENT-2026-08-13-004`.
>
> **Dois BLOCKERs medidos pela revisão, antes do commit, e ambos anulam o efeito em runtime:**
>
> 1. O `TransitClient` autentica **exclusivamente por AppRole**, e o chart **não declara AppRole
>    nenhum**. A policy que corrigi está ligada só ao role de k8s-auth, cujo cliente
>    (`pkg/vault.NewClient`) tem **zero** chamadores de produção. As três fontes não são três versões
>    de uma policy: o runbook escreve `loopzero-api-approle`, as outras duas escrevem `loopzero-api`, e são
>    consumidas por **mounts diferentes**. Minha moldura no ADR estava errada.
> 2. `vault-bootstrap-dev.sh` aplica o chart (passo 5) e **sobrescreve a mesma policy** (passo 6-7)
>    com a versão larga, incondicionalmente. Último escritor vence — e esse script é o passo 1 da
>    cadeia canônica pós-Cleanroom.
>
> Corrigir o chart era **necessário e não suficiente**. O que a fatia entregou e que fica de pé: a
> policy versionada descreve o que o cliente pede; o teste que **deriva** os caminhos das constantes
> do cliente; o doc-comment que descrevia o caminho rejeitado pelo transit (em quatro lugares, não
> um); e a distinção entre negação permanente e falha transitória, extraída em função testável.
>
> **Um erro meu que o teste pegou:** meu primeiro patch caiu na policy errada
> (`loopzero-api-transit-encrypt`), porque há quatro entradas de nome parecido e uma delas é um *role*.
>
> O que falta para fechar está em **B-099**.
>
> Escolhida a versão do **runbook** — a mais estreita das três e a única com prova de execução. Ela
> carrega duas lições medidas ao vivo em 2026-06-10 que as outras não têm: `update` é exigido junto
> de `create` (o POST do cliente Go é HTTP PUT, que o OpenBao trata como update mesmo sem a chave
> existir — com `["create","read"]` o token reportava as duas e o PUT devolvia **403**), e a
> convenção é **hífen**, porque com barra o transit devolve 404 *"unsupported path"*.
>
> `encrypt`/`decrypt` foram **estreitados** de `+` para `loopzero-tenants-*`: `+` casa qualquer segmento
> único, e a única chave que o `api` nomeia vem de `tenantKeyName`. A policy sai desta fatia mais
> larga em um eixo e mais estreita em dois.
>
> **O teste deriva os caminhos das constantes do cliente**, não do runbook — prosa envelhece; o
> `tenantKeyPrefix` é o que executa. Terceira vez nesta série que o remédio para drift é comparar
> contra a fonte que executa, em vez de manter duas listas.
>
> **Um erro meu que o teste pegou:** meu primeiro patch caiu na policy **errada**
> (`loopzero-api-transit-encrypt`, não `loopzero-api`) — havia quatro declarações com nome parecido, e uma
> delas é um *role*. Sem o teste, eu teria commitado uma policy que não muda nada.
>
> `ErrTransitKeyCreateFailed` tinha **zero** leitores fora da declaração e do teste; agora
> `secrets.go` distingue negação (`"denied"`) de falha transitória (`"failed"`) — são fatos opostos
> para quem está de plantão, e colapsá-los é o que a Regra 8 chama de tratar todo erro igual.
>
> Achado no caminho: **`B-098`** — duas policies declaradas e nunca ligadas a role nenhum.

> **Fechado 2026-08-13, agora de verdade.** O item tinha sido reaberto porque a correção do chart não
> alcançava runtime — o `B-099` era essa lacuna, e fechou.
>
> As quatro balas: (1) o chart concede `transit/keys/loopzero-tenants-*` com `create`, `read` e `update`
> (`values.yaml:241-243`); (2) chart e runbook pararam de divergir — o runbook não escreve mais policy e
> nomeia `loopzero-api`; (3) `ErrTransitKeyCreateFailed` tem leitor desde a fatia 11
> (`api/internal/routes/secrets.go:120`, `transitFailureStatus`, com testes); (4) **a que faltava** —
> `TestOpenBaoInitAuth_ChartGrantsTheKeyPrefixTheCodeNames` lê `tenantKeyPrefix` do
> `pkg/vault/transit_client.go` e exige que o chart conceda esse prefixo nos três verbos. Mutante:
> mudar o prefixo no Go e não no chart reprova, nomeando os paths.

## B-073 — O chart guarda-chuva não tem consumidor conhecido, e mantê-lo custa   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 ao decidir o `B-048`. Os únicos consumidores de `charts/loopzero-platform` são de teste ou template: `taskfiles/helm.yml:32,:37,:42,:108`, `infra/tests/helm/loopzero-platform/defaults_test.go`, `infra/tests/helm/envparity/envparity_test.go:51`, `infra/tests/helm/README.md:125-128`. O **bootstrap do ArgoCD não o usa** — os únicos `path: charts/*` em `infra/argocd-bootstrap/apps/` são quatro Applications, uma por operator. Zero referências em qualquer repositório irmão do workspace.
why_now: a decisão do `B-048` foi tomada (é composição single-cluster, e isso passa a estar escrito no chart), mas ela levanta a pergunta que a evidência não fecha: **se o chart deveria existir**. Mantê-lo custa o gate `helm-overlap-parity`, os testes de envparity e a manutenção de 12 toggles. O dado que falta não é mensurável em disco — o README documenta instalação via `oci://registry.digitalocean.com/loopzero-system/helm/loopzero-platform`, e o conteúdo do registry não é visível daqui.
status: triaged
severity: baixa — nada quebra; é custo de manutenção de um artefato sem demanda medida.
dod:
  - está medido se o chart publicado em OCI é consumido fora do workspace
  - se não for: está decidido entre manter com o custo declarado, ou remover
  - `loopzero-monitoring` e `loopzero-dashboard` ganham plano declarado, ou fica escrito que não têm — hoje não há declaração em helmfile nem em doc

> Registrado 2026-08-12 ao escrever o ADR do `B-048`.

## B-074 — Deletar o Environment CR direto deixa a Application órfã, e o reaper não a alcança   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-12 ao decidir o `B-050`. `EnvironmentReconciler.reconcileDelete` (`operators/internal/environment/environment_controller.go:303-348`) deleta o Namespace (`:335`) e remove o finalizer (`:345-347`) — e **não toca na Application nem em `Tenant.Spec.Environments`**. Como a entrada permanece no spec, `expectedApplicationNames` (`operators/internal/tenant/tenant_gitops.go:146-160`) continua considerando a Application "esperada", e `pruneOrphanPreviewApps` (`:381-412`) nunca a remove. O caminho **coberto** pelo reaper é o outro: `RemoveEnvironment` (`api/internal/k8s/tenant_cr_manager.go:175-215`) tira a tupla do spec, e aí o prune alcança.
why_now: o `CLAUDE.md` § Known Systemic Patterns descreve o sintoma "Orphan ArgoCD Application … status Unknown forever" com contorno manual — e o texto está **desatualizado**: entrou em `c6fc9671` (2026-06-10), dois dias antes de o reaper existir (`ecbabf34`, 2026-06-12), e suas citações não resolvem mais (aponta `operators/internal/tenant/environment_controller.go:235`; o arquivo está em `operators/internal/environment/` e a função em `:303`). O padrão documentado descreve um caminho que foi consertado, e não descreve o que continua quebrado.
status: triaged
severity: média — a órfã não corrompe nada, mas gera exit 5 em `loopzero platform status` (drift) e o contorno é manual entre execuções de teste.
dod:
  - deletar o Environment CR direto passa a limpar a Application, ou a remover a tupla do `Tenant.Spec.Environments` para o reaper alcançar
  - o parágrafo do `CLAUDE.md` é corrigido: descreve o caminho que ainda quebra, com citações que resolvem
  - o comentário em `tenant_gitops.go:352-355`, que registra que `preview_controller.go:609` afirma uma propriedade que o `api` nunca ligou, é resolvido junto

> Registrado 2026-08-12 ao escrever o ADR do `B-050`.


## B-075 — O primitivo de crypto-shredding não tem dono, e o ADR quase o deletou   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 no review da fatia 2. `operators/internal/vault/client.go:26-33` declara `TenantTransitClient` com **dois** métodos, e o segundo é `DeleteTenantKey` — descrito no próprio doc-comment como *"the crypto-shredding primitive … When the per-tenant key is deleted, all ciphertexts encrypted by that key become permanently unrecoverable. EC-10 MUST FIX P0: cleanup job must enforce 2-person rule + 90-day window before calling this."* Medido: **zero chamadores de produção** (só dois mocks), e o lado `api` **não tem nenhum caminho de delete de chave transit** — zero hits para `transit/keys.*delete`, `deleteKey`, `ShredKey` em `pkg/vault/` e `api/`. E `docs/wiki/decisions/2026-06-02-openbao-adoption.md:67` (§ Q5) amarra o shredding a *"GDPR/LGPD right-to-erasure native"* e ao fluxo de soft-delete de 90 dias (migração `000005`).
why_now: a v1.0 do ADR `2026-08-12-tenant-transit-key-ownership` autorizava deletar a interface inteira, tendo medido apenas quem chama `EnsureTenantKey`. O review pegou. A v1.1 escopou a decisão ao **provisionamento** e deixou o shredding pendente — este item é esse pendente. E o argumento contrário que ninguém tinha construído: o ciclo de vida do tenant (criar → 90d → shred) é do operator e o `api` é request-scoped, então **é plausível que a seam exista porque o DELETE é do operator**, não porque esqueceram de ligar o CREATE.
### Medição de 2026-08-12 — a pergunta do item muda de natureza

Medido ao tentar desbloquear o `B-043`, que depende desta decisão:

| O que | Onde | Estado |
|---|---|---|
| `DeleteTenantKey` | `operators/internal/vault/client.go:33` | **só declaração de interface** — o arquivo tem zero `func New` e zero `struct` |
| O campo que a receberia | `operators/internal/tenant/tenant_controller.go:102` — `VaultClient vault.TenantTransitClient` | declarado |
| A atribuição em produção | `operators/cmd/tenant-operator/` | **não existe** — nada atribui `VaultClient` |
| Implementação em `pkg/vault` | — | **nenhuma**. `pkg/vault` não tem método de shredding |
| Chamada no handler api-side de exclusão | `api/internal/routes/tenants/tenants.go` (era `routes/tenants.go` na medição; a família virou subpacote na fatia slice-8) | **não menciona vault** |
| Runbook operacional | `docs/operations/` | **nenhum** arquivo sobre exclusão de tenant / apagamento |

**O único vestígio de crypto-shredding no repositório inteiro é um comentário** (`client.go:26`,
*"is the crypto-shredding primitive"*) sobre um método que nada implementa, nada atribui e ninguém
chama.

Isso reescreve a pergunta. O item foi escrito supondo que a seam pudesse existir **porque** o DELETE
é do operator. A medição mostra que a seam existe e **está inerte dos dois lados**: não é "quem é o
dono do DELETE", é "o DELETE não existe". É o padrão *"implementado mas não ligado"* que o
`CLAUDE.md` nomeia, aplicado ao primitivo que sustenta o direito ao apagamento.

Consequência para a decisão: ela deixa de ser uma escolha entre duas implementações e passa a ser
uma decisão sobre **construir** a que falta, e onde. O que não muda é o tier: continua T3, e continua
exigindo reunião — agora com peso maior, porque o que estava em jogo era supor que existisse.

status: triaged
severity: alta — não pelo estado atual (nada chama, então nada quebra), mas porque a decisão anterior quase apagou o primitivo de conformidade por ele estar na mesma interface que um método morto.
dod:
  - está decidido de quem é o `DeleteTenantKey`, olhando o ciclo de vida de deleção de tenant e não só quem chama o método
  - a regra de duas pessoas e a janela de 90 dias que o EC-10 declara P0 têm dono nomeado, ou está escrito que não são exigidas
  - se o shredding ficar operator-side e o provisionamento api-side, a divergência de nome de chave (`tenant-<id>` × `loopzero-tenants-<id>`) é resolvida — hoje o operator deletaria uma chave que o `api` nunca criou

> Registrado 2026-08-12 pelo review adversarial da fatia 2. O ADR foi corrigido de decisão total para parcial por causa deste achado.

> **Confirmado por um terceiro ângulo, 2026-08-13 (fatia do `B-099`).** A divergência de nome de chave
> não está só no código — está na **policy**. `infra/helm/openbao-init/values.yaml:165` e `:260`
> concedem `delete` em `transit/keys/tenant-+` a `tenant-controller-transit` e `loopzero-tenant-operator`,
> com doc-comment nomeando explicitamente o padrão de crypto-shredding.
>
> Ou seja: o lado operator **está autorizado** a apagar chaves com o nome `tenant-<id>` — que ninguém
> cria. O `api` cria `loopzero-tenants-<id>`. Ligar esse caminho hoje apagaria nada e reportaria sucesso.
>
> Achado por uma asserção que eu tinha escrito larga demais (proibindo `delete` em qualquer chave
> transit) e que a medição estreitou: o `delete` do operator é decisão deliberada e documentada, e
> proibi-lo seria um teste anulando uma decisão em vez de guardá-la. O que ficou guardado é o que
> importa: `delete` no prefixo que o `api` cria reprova, porque o `api` não tem caminho de shredding.

## B-076 — 21 divergências entre chart publicado e de bootstrap estão aprovadas sem que ninguém tenha medido por quê   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 durante a fatia slice-3. O `helm-drift` aprovava divergência **por par de charts** (`helmdrift/check.go`, mapa `approvedDrifts`): `loopzero-api:loopzero-api` e `loopzero-cloud:loopzero-cloud` apontavam para `ADR-0043`, e isso bastava para aprovar todo arquivo que aquele par viesse a divergir — inclusive arquivos que ninguém tinha visto. A fatia re-chaveou para `PAR:ARQUIVO`, o que exigiu enumerar as 21 divergências reais. Elas foram registradas com a justificativa honesta `herdado da aprovação em bloco por par; a razão por arquivo não foi medida` e o marcador `Inherited: true`.
why_now: o re-chaveamento tornou visível uma dívida que antes não tinha onde aparecer. Enquanto as 21 entradas disserem `Inherited`, o gate protege contra a **22ª** divergência mas não afirma entender as 21 primeiras — e essa distinção só existe porque foi escrita.
status: raw
severity: média — nenhuma das 21 é um defeito conhecido; o risco é que uma delas seja um segundo `vap-tenant-cr-shape.yaml` e ninguém saiba.
dod:
  - cada uma das 21 entradas tem uma razão medida, ou é removida do allowlist porque a divergência não deveria existir
  - nenhuma entrada resta com `Inherited: true`

> Registrado 2026-08-12 pela fatia slice-3 (T1.4). O campo `Inherited` existe exatamente para que esta dívida seja contável em vez de invisível.

## B-077 — A cerca de forma dos CRs Tenant está desligada exatamente no cluster onde os CRs vivem   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 na revisão da fatia slice-3. `infra/helm/admission-policies/values-mgmt.yaml:37` define `tenantCrShape: false`, e `infra/helmfile/core.helmfile.yaml.gotmpl:764-769` sobrepõe `values.yaml` com `values-mgmt.yaml` no release `admission-policies-mgmt` — o último vence. A VAP fica **habilitada apenas** via `cell.helmfile.yaml.gotmpl:180-186`, ou seja nas células de workload. Mas per `CLAUDE.md` e ADR-0018 v2, os CRs `Tenant` e `Environment` vivem em `loopzero-system` no cluster **mgmt**.
why_now: a cerca `failurePolicy: Fail` sobre a forma dos CRs Tenant roda nos clusters que não guardam CR Tenant nenhum, e está desligada no único que guarda. O plano da fatia slice-3 afirmou o contrário (`R3`: *"ela já roda em dev e em core pelo caminho L2"*) e usou isso para justificar o risco como aceitável — a premissa não se sustenta, e a revisão a derrubou.
status: shipped
severity: alta — é uma fronteira de segurança que parece ativa e não está onde importa.

> **Ressalva da auditoria fechada em 2026-08-14.** O teste **renderiza mesmo** — ela verificou
> (`exec.Command("helm","template",…)`, não `os.Stat`) e matou dois mutantes. Mas a pilha de values
> estava **escrita à mão** no teste enquanto a fonte é `infra/helmfile/core.helmfile.yaml.gotmpl`.
> Hoje coincidem; nada as mantinha coincidindo. Um terceiro overlay no helmfile e o teste seguiria
> verde renderizando uma composição que não é a implantada.
>
> É a mesma forma do achado H3 que originou este item — *"o teste mede render, mas não mede **onde** o
> valor é sobreposto"* — um nível acima.
>
> `TestTenantCrFence_TheValuesStackUnderTestIsTheDeployedOne` lê a pilha do helmfile e afirma que os
> literais **ainda são** ela. Deliberadamente **não** renderiza a partir da lista derivada: renderizar
> do derivado absorveria a divergência em vez de a denunciar. Verificado por mutante — terceiro overlay
> injetado, a asserção acusa nomeando os três arquivos.

dod:
  - está decidido e escrito por que a VAP está desligada no mgmt, ou ela é habilitada lá
  - a decisão é verificável por render, não por presença de arquivo

> Registrado 2026-08-12 pela revisão independente da fatia slice-3 (achado H3). Foi este achado que
> mostrou que o critério de aceitação por nome de arquivo era cego; o teste
> `TestAdmissionPolicies_PublishedRendersWhatBootstrapRenders` mede render, mas não mede *onde* o
> valor é sobreposto.

> **Fechado 2026-08-13** pela fatia `slice-16`, ADR
> `docs/wiki/decisions/2026-08-13-tenant-cr-fence-placement.md`, ata
> `MEET-ASYNC-AMENDMENT-2026-08-13-003`.
>
> **Medido por render, que é o que a segunda bala pede.** Antes: o values base (o que as células
> recebem) renderizava 8 VAPs **incluindo** a `loopzero-tenant-cr-shape`; o do mgmt renderizava **1**, e
> não era ela. E o CRD `tenants` vem do `loopzero-crds`, que aparece **6 vezes** no `core.helmfile` e
> **zero** no `cell.helmfile`. A cerca renderizava onde o recurso não existe e faltava onde existe.
>
> **Os dois motivos escritos no overlay eram falsos para ela.** `values-mgmt.yaml:24-26` a agrupava
> como *"runtime-only… guards a workload concern"* — mas o `matchConstraints` casa um CR de
> plataforma. E a justificativa geral (*"emitiria objetos sigstore num cluster sem
> policy-controller"*) não se aplica a uma VAP **nativa**, o que o próprio cabeçalho do arquivo
> reconhece. Ela entrou no balde por **associação de nome** com `tenantNamespace`.
>
> **Habilitar é seguro por medição:** os dois únicos escritores de CR Tenant (`EnsureTenant`,
> `tenant_cr_manager.go:68`; `patchTenantCR`, `cell_pinner.go:198`) rodam no processo `api`, cuja SA
> o CEL isenta, e `grep` por escrita de Tenant nos operators devolve **zero**. O CEL já cobre as três
> armadilhas do `CLAUDE.md` (só CREATE/UPDATE, `has()`-guarded, isenções não-vazias), e
> `environments: []` passa vacuamente porque o tipo declara `MinItems=0`.
>
> **Fica declarado, não resolvido: não há validação LIVE nesta sessão.** A prova disponível é o
> render. A obrigação LIVE está no ADR **com a armadilha nomeada** — o kubeconfig do e2e é
> `system:masters`, *grupo que esta política isenta*, então um teste assim passa por isenção e não
> por funcionamento; validar exige SA sem isenção **e com RBAC**, senão o `Forbidden` vem do RBAC.
>
> A Q1 (`vap-tenant-namespace` renderiza em lugar nenhum) virou **B-095**.

> **Correção da revisão, e é a mais importante desta fatia.** Eu afirmei — no ADR e na ata — que
> *"nenhum operator escreve CR Tenant"*, com base num `grep`. **Errado, em quatro sites**: as
> escritas são `r.Update(ctx, tenant)`, sem `&`, e uma está atrás de `EnsureFinalizer`, então
> procurar por `&loopzerov1.Tenant` não encontra nenhuma.
>
> E `tenant_deletion.go:215` é a escrita que **remove o finalizer e completa a deleção**. Habilitar
> a cerca sem isentar o operator deixaria qualquer Tenant cuja forma armazenada não satisfaça o CEL
> **permanentemente indeletável** — a classe de deadlock que o próprio cabeçalho da política afirma
> evitar. A pista estava no mesmo chart: `vap-protect-platform-deletes.yaml:84` já isentava os
> operators.
>
> Corrigido nos dois charts, e com um teste que **compara as isenções das duas políticas** em vez de
> reenumerar escritores — uma lista à mão erra de novo; a comparação pega a divergência. O mutante
> que remove a isenção reprova.

> **Verificado fechado 2026-08-13.** As duas balas: a VAP **está** habilitada onde os CRs vivem
> (`infra/helm/admission-policies/values-mgmt.yaml:35` → `tenantCrShape: true`) e desligada nas cells
> (base `values.yaml:103` → `false`, e o chart publicado concorda). E a decisão é verificável por
> **render**, não por presença de arquivo: `infra/tests/helm/tenant_cr_fence_placement_test.go` roda
> 5 asserções — renderiza no mgmt, não renderiza nas cells, os dois charts concordam no default, os
> renders não são vazios, e as identidades isentas batem com as da irmã. 5/5 PASS.
>
> Fechado pela fatia `slice-16`; o campo `status` não acompanhou até esta medição.

## B-078 — O gate de paridade de contexto não enxerga scripts, e não pode comparar quando um lado é parametrizado   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-12. O `helmfile-context-parity` varre `Taskfile.yml` + `taskfiles/*.yml` e reporta 8 comparadas contra 20 fora de alcance estático. As 12 invocações do `Taskfile.yml` caem em "parametrizadas" porque o `cell.helmfile` declara `requiredEnv`, então nenhuma delas é comparada. E os invocadores em script — `infra/scripts/local/vault-bootstrap-dev.sh:267-268`, `infra/scripts/production/operator-cutover-execute.sh:142,204`, `infra/scripts/openbao/enable-vault-backup.sh:205` — não são varridos.
why_now: o gate mede menos do que a leitura casual do seu nome sugere. Enquanto 20 de 28 invocações estiverem fora de alcance, a proteção é parcial — e é melhor que isso esteja escrito num item do que subentendido no `Summary()`.
status: raw
severity: média — o que ele compara, compara certo; o problema é a cobertura.
dod:
  - os invocadores em script são varridos, ou está escrito por que não são
  - a fração fora de alcance estático é reportada de forma que ninguém a confunda com cobertura

> Registrado 2026-08-12 pela revisão independente da fatia slice-3 (achado H4 residual + M3).
> Relacionado: `cell.helmfile.yaml.gotmpl` é reusado pelo plano de BUILD em
> `Taskfile.yml:374-375` (cert-manager + cluster-issuer, com selector restrito e motivo escrito).
> É reúso deliberado, não violação — mas um arquivo nomeado por um plano e usado por outro é
> exatamente o tipo de coisa que a auditoria de domínios apontou.

## B-079 — Remover os quatro tipos de persistência da fronteira do sink custa 57 call sites   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-12 na fatia slice-4. Quatro símbolos de `api/internal/db` atravessam a superfície pública de `services/deploycontracts` — `shared_contracts.go:207` (`WithLock(..., fn func(tx db.DBTX) error)`), `delivery.go:37` (`Activate(ctx, build *db.Build, ...)`), `delivery.go:55` (`TargetRelease *db.Release`) e `manifest_builder.go:23` (`Apps []db.BuildAppSpec`). **35 arquivos, em 10 pacotes, dependem do sink**, logo dependem transitivamente da persistência. Custo de remoção, contado: `WithLock` **34** call sites, `db.BuildAppSpec` **18**, `TargetRelease` **6**, `Activate(ctx` **5** — **63** no total (linhas fora de `_test.go` em `api/`, regra única; a primeira contagem citou 57 misturando três regras, e a revisão a desmentiu). Mais o alargamento da cerca `dbgeneratedboundary` para `api/internal/db`, que atingiria **71** arquivos (`services` 51, `cmd` 18, `routes` 2) sob a semântica do gate, que inclui `_test.go` de propósito — a DoD do `B-049` afirmava que isso era *"literalmente uma string"*, e a medição desmente.
why_now: a fatia slice-4 travou o vazamento com o gate `domain-sink-purity`, mas travar não é fechar. Enquanto os 4 estiverem lá, os 35 dependentes carregam persistência que não pediram — e `WithLock` é o primitivo do environment-lock que a Regra Inquebrável 6 põe em toda mutação, então mexer nele exige o cleanroom que `cleanroom-up-live-test-mandate.md` torna obrigatório para validação LIVE.
status: raw
scope_reduced: 2026-08-12 pela fatia slice-6 — a bala de `db.DBTX` na camada de interface saiu deste item porque foi ENTREGUE. Resta: (a) os 4 tipos de persistencia + 3 de aplicacao na superficie de deploycontracts, e (b) alargar a cerca para `services` (51) e `cmd` (18). `server` e `routes` ja estao cercados.
severity: média — o vazamento existe e está contido; o risco é ele parecer resolvido porque um gate ficou verde.
dod:
  - os 4 símbolos saem da superfície pública do sink, ou cada um que fica tem razão escrita
  - `FrozenDefault` em `domainsinkpurity` encolhe na mesma medida — o gate reporta `REMOVIDO` para guiar
  - a cerca alarga para `services` (51) e `cmd` (18) — `routes` foi entregue na fatia slice-6 e `server` na slice-4 —, ou o custo de 69 arquivos é reavaliado com números novos
  - a mudança em `WithLock` é validada contra um Cleanroom, não só contra a suíte

> Registrado 2026-08-12 pela fatia slice-4. O `domain-sink-purity` imprime as 4 congeladas em toda
> execução exatamente para que este item não seja esquecido — é o mesmo desenho do `B-076`.

## B-080 — Os 43 arquivos de `routes` ainda são um pacote plano, e mover 263 símbolos exportados é T3   [ ]

domain: engine-go
repo: loopzero
suggested_mode: evolve
source: discover-review
evidence: medido em 2026-08-12 na fatia slice-5. `api/internal/routes` tem **43 arquivos de produção** e **27.298** linhas contando testes; os maiores são `secrets.go` 846, `environments.go` 676, `builds.go` 628, `scale.go` 600, `projects.go` 504. `server/router.go` registra **19 prefixos de recurso** (`/api/v1/{admin,audit,auth,billing,builds,domains,environments,health,integrations,projects,secrets,source,teams,tenants,version,webhooks}`, `/billing`, `/internal`, `/v1/version`). O bloqueio de fato foi removido nesta fatia — as três funções de transporte saíram de `projects.go` para `routes/httpio`, 399 chamadas re-apontadas — e a cerca `routes-acyclic` já existe, registrando `httpio` como folha.
why_now: o `B-041` exige que a cerca preceda o movimento, e agora ela existe. O que resta é o movimento em si, e ele é T3: redistribui a superfície exportada — cujo tamanho **não reproduz** em 263: duas contagens independentes deram **355** (AST, incluindo métodos) e **301** (regex de declaração de topo, que subconta blocos `type (...)` agrupados). O número 263 vem do `evidence` do `B-041` e nenhuma regra o produz — está registrado aqui como não verificado em vez de repetido — e reescreve `server/router.go`, que é a **única** superfície onde o conjunto de rotas é lido — a Regra Inquebrável 5 torna a forma dessa superfície um contrato.
status: triaged
recipe_proven: 2026-08-12 pela fatia slice-8, que moveu a PRIMEIRA familia de ponta a ponta e mediu o resto.
  A receita, percorrida: (1) `git mv` dos arquivos da familia para `routes/<familia>/` levando os testes JUNTO — deixa-los para tras criaria a unica aresta que a cerca NAO vigia (pai -> filho); (2) trocar a clausula `package`; (3) o compilador aponta cada consumidor externo — e o RED e ele, nao um teste escrito a mao; (4) re-apontar e remover import orfao; (5) REGISTRAR a familia no allow-list de `routesacyclic`, que ate la reprova com "uma familia de handlers que ninguem decidiu".
  O passo (5) foi VERIFICADO como real: antes do registro o gate saiu 1 nomeando `version`. A cerca nao e decorativa.
  DENOMINADOR MEDIDO (CORRIGIDO 2026-08-12 pela revisao): `routes` declara **245** simbolos exportados de topo por `go doc -all` (355 contando metodos), dos quais 172 sao usados de FORA. O 704 da primeira versao era artefato de regex — ~72% dele eram funcoes `Test*`, que nao sao superficie importavel. E a MESMA falha que esta fatia diagnosticou para `tenants`, e eu corrigi o numero daquela familia sem recomputar este. O `why_now` deste item ja registrava 355 e 301 seis linhas acima, e o 704 foi acrescentado sem reconciliar, em 55 arquivos e 8 pacotes — 84 deles pela raiz de composicao (`api/cmd`). Por isso o fatiamento inteiro e T3 e uma familia por fatia: num commit unico, um erro em qualquer das 16 seria indistinguivel dos outros 15.
  A SEGUNDA FAMILIA (`tenants`) corrigiu a receita em tres pontos, todos revelados pelo COMPILADOR e nao por leitura:
  (a) enumerar simbolos por regex `^func [A-Z]` / `^type [A-Z]` SUBCONTA — perde blocos `type (...)` agrupados. `tenants` exportava 7, meu regex viu 4. Use `go doc -all ./internal/routes/<familia>` para a lista completa.
  (b) helpers privados de TRANSPORTE se escondem em arquivos de dominio. `writeErrorJSON` morava em `tenants.go` e era chamada de `billing.go`; foi para o `httpio` como quarto escritor de resposta. A medicao do B-041 contava so helpers usados por MAIS de 3 arquivos, e esta e usada por 2 — por isso escapou.
  (c) helpers de TESTE em `_test.go` nao sao importaveis entre pacotes. `withAuth` e usada por TRINTA arquivos de teste, e a familia perdeu acesso a ela ao sair. Nasceu `routes/routestest` — o irmao do `httpio` do lado dos testes, com os 4 helpers exportados; o pacote pai delega em vez de duplicar. A familia 1 (`version`) nao revelou isso porque nao tem auth.
  ORDEM SUGERIDA, por superficie externa medida (arquivos de fora que referenciam a familia): version 2 (FEITA), tenants 4 (FEITA), billing 2, auth 2. RECONTADO pela revisao: `tenants` tinha 4 consumidores externos e `auth` tem 2 — duas das quatro contagens estavam erradas, e como o criterio de ordenacao E a superficie medida, a ordem publicada nao era confiavel. `pagination` tem 0 mas nao e familia do router — e utilitario, e move como folha ao lado do `httpio`.
severity: alta — é o achado central da auditoria de arquitetura, e agora está desbloqueado.

> **Sonda de compilador re-executada em 2026-08-14, agora com o alvo certo — e o alvo era o defeito.**
>
> A primeira tentativa desta sessão compilou `./internal/routes/...` e devolveu **zero** acoplamento
> para as cinco famílias, inclusive uma de 879 linhas. Quatro zeros seguidos são a assinatura de uma
> sonda que não mediu nada: quem consome os handlers vive **fora** do pacote, então compilar só o
> pacote não podia quebrar. Com `go build ./...` do módulo, a medição aparece.
>
> | família | arquivos | referências quebradas | onde |
> |---|---|---|---|
> | `billing` | 1 | **1** — `BillingHandler` | `internal/server/server.go` |
> | `teams` | 1 | **1** — `TeamHandler` | idem |
> | `preview` | 1 | **1** — `PreviewHandler` | idem |
> | `auth` | 1 | **3** — `ActiveTeamStore`, `NewAuthHandler`, `UsernameLookup` | idem |
> | `secrets` | 3 | **6** — `NewSecretHandler`, `SecretStore`, `SecretEnvLocker`, `SecretK8sSync`, `SecretProjectChecker`, `SecretTransit` | idem |
>
> E o resultado que decide o próximo passo: **nenhuma das cinco referencia símbolo do pai.** O
> compilador não emitiu um único erro dentro dos pacotes movidos — verificado contra o teto de erros
> do Go (`too many errors` ausente, saída completa em 7 linhas). Extrair qualquer uma delas toca
> **um** consumidor.
>
> `secrets` é o melhor candidato por massa removida sobre risco: 3 arquivos, 879+ linhas, 6
> referências num só arquivo. `billing`, `teams` e `preview` custam uma referência cada.

> **`secrets` extraída em 2026-08-14 (fatia 28). O pai vai de 35 para 32 arquivos de produção, e o
> subpacote leva 879+ linhas em 3 arquivos mais 4 de teste.**
>
> **E a sonda tinha subestimado — a correção importa mais que o número.** Ela disse 6 referências, num
> consumidor. Real: `internal/server`, `cmd/` e `tests/unit`, achados iterando o build até fechar. Duas
> razões, ambas de truncamento: o Go **não compila** pacote que depende de um que falhou, então os de
> baixo ficam invisíveis enquanto o de cima está vermelho; e ele **para em 10 erros por pacote**, então
> mesmo o pacote visível mostra só os dez primeiros. Uma execução única mede um piso, não o total.
>
> A cerca decidiu duas coisas que eu teria decidido errado. Primeiro, ela recusa **pai → família**
> mesmo vindo de `_test.go`: eu havia qualificado o `cross_tenant_test.go` do pai para o subpacote,
> achando que teste podia. Não pode, e a razão é o próprio item — um pai que depende de uma família
> nunca esvazia. Os testes de isolamento de secrets foram para o subpacote. Segundo, os auxiliares de
> teste vieram do leaf `routestest`, que existe para isso.
>
> As fixtures (`xtTenantA`, `tenantBContext`, …) foram **duplicadas**, não exportadas do pai. DRY rege
> conhecimento, não linhas: são constantes de teste, e compartilhá-las recriaria a aresta proibida na
> direção que parece inofensiva.

> **Revisão adversarial: `READY_TO_MERGE_WITH_FOLLOWUPS`, cinco achados, todos consertados na mesma
> fatia.** O risco central — perda silenciosa de teste, já que dois arquivos foram montados recortando
> blocos do pai por linha — foi descartado com evidência que vale registrar: não a contagem, mas o
> **conjunto**. Os nomes dos testes executados por `go test -v` antes e depois são byte-idênticos
> (`diff` vazio sobre as listas ordenadas), e as somas fecham nos dois arquivos (41 = 35 + 6, 16 = 11 + 5).
> O registro do gate foi verificado por mutação nas duas direções: tirar `httpio` acusa 1 aresta, tirar
> `routestest` acusa 3. Nenhum símbolo precisou virar exportado, nenhuma rota mudou.
>
> Dois dos cinco eram **regressão desta fatia**, não dívida herdada, e a revisão provou isso extraindo
> os 7 arquivos tocados no commit-pai e mostrando `gofmt -l` vazio: (1) o import novo entrou fora de
> ordem em `api/cmd/main.go`, e nada é vermelho porque o `.golangci.yml` não habilita `gofmt`; (2) o
> pacote novo nasceu sem comentário de pacote, +3 `ST1000` — e o agravante é que `tenants` e `version`
> têm, `domains` e `repositories` não, e eu repeti a metade errada. Os outros três: doc de tipo colada
> no bloco de constantes pelo recorte por linha, duas constantes que morreram no pai quando os testes
> saíram, e um local `secrets :=` sombreando o próprio pacote contra a convenção `xHandler` das quatro
> famílias anteriores.
>
> Depois dos consertos: `golangci-lint` volta aos **11** achados da linha de base, cerca PASS, módulo
> verde.
>
> **E a revisão tropeçou três vezes no mesmo defeito que a fatia trata** — `| head` e `| tail` fazendo
> o `$?` reportar o exit do pipe e não do comando, uma vez dizendo `exit 0` para um lint que saía 1.
> Ela refez e disse. A armadilha não é de quem escreve o código; é de quem lê a saída.
>
> A sonda roda no lugar e restaura por `mv`, com `git status --porcelain api/` verificando o retorno —
> a cópia em scratch não serve, porque `go.work` aponta para módulos irmãos que a cópia não tem, e a
> ausência deles faz a sonda medir zero por outra razão (a mesma armadilha, dois níveis acima).

> **Fatia 29 — `preview` extraída (2026-08-14).** O pai vai de 32 para 31 arquivos de produção; o
> subpacote leva `preview.go` + seu teste. Cerca: **8 subpacotes**, 0 não registrados, 0 arestas irmãs.
> Lint do pacote novo: 0 achados — o comentário de pacote entrou desde o início, que foi a correção
> #2 da revisão anterior aplicada como prevenção em vez de conserto.
>
> Duas armadilhas novas, as duas sobre **como se lê o código**, não sobre acoplamento:
>
> A referência qualificada pode atravessar linha. `cmd/setup_server.go` tinha `sc.PreviewHandler =
> routes.` numa linha e `NewPreviewHandler(...)` na seguinte, encadeado. Um regex por linha não a vê,
> e o laço de compilador ficou repetindo o mesmo símbolo sem casar arquivo nenhum — cinco iterações
> gastas antes de eu ler o erro em vez da minha própria saída.
>
> E `preview` **colide com um pacote fora de routes**: já existe `internal/services/preview`. É fato
> de nome, não de acoplamento — a cerca mede import, e a colisão se resolve no importador. A convenção
> já na árvore sufixa o **serviço** (`domainsvc`, `placementsvc`, `releasesvc`), então o serviço virou
> `previewsvc` e o de rotas ficou `previewroutes` no único arquivo onde os dois se encontram.

> **Fatia 30 — `billing` extraída (2026-08-14).** Pai de 31 para **30** arquivos de produção; cerca com
> **9 subpacotes**, 0 não registrados, 0 arestas irmãs; lint do pacote novo em 0.
>
> **A extração desenterrou uma reimplementação de `strings.Contains`.** Ela vivia no arquivo de teste
> desta família e era alcançada por dois testes do pai (`admin_pin_cell_test.go`, `promote_test.go`) —
> nove linhas fazendo o que a stdlib faz numa chamada. Ninguém a escreveria de propósito; ela
> sobreviveu porque morava num pacote grande o bastante para que suas declarações fossem invisíveis.
> **Separar o pacote foi o que a tornou visível**, e é um argumento pela decomposição que o item não
> tinha: um pacote plano esconde não só acoplamento, mas duplicação de conhecimento com a biblioteca
> padrão. Substituída nos três sítios pelo degrau 2 da escada de parcimônia.
>
> E a armadilha da fatia 29 — referência qualificada atravessando linha — entrou na receita: o laço de
> compilador passou a casar `routes.` seguido de quebra e do símbolo, então ela não custa mais
> iteração nenhuma.

dod:
  - `ls -d api/internal/routes/*/` fica isomórfico às famílias de `server/router.go`
  - cada família nova entra no allow-list de `routes-acyclic` como declaração, não como descoberta
  - `server/router.go` continua sendo o único lugar onde a superfície inteira é lida
  - nenhuma família importa uma irmã (a cerca já reprova)
  - a mudança passa por reunião T3 antes do primeiro arquivo mover

> Registrado 2026-08-12 pela fatia slice-5, que entregou 3 das 5 balas do `B-041` e mediu esta.

> **Terceira família extraída — 2026-08-13, fatia `slice-19`.** O item **continua aberto**.
>
> Estado: **5** subpacotes (`httpio`, `routestest`, `version`, `tenants`, `repositories`), **40**
> arquivos de produção no pai.
>
> **A tabela que eu tinha publicado aqui media ocorrência de nome, não referência — o review a
> refutou.** Ela contava comentários, literais e, decisivamente, **métodos homônimos de outros
> tipos**: `domains` exporta `Get`/`List`/`Create`/`Delete`/`Verify`, então
> `r.URL.Query().Get("action")` e `func (h *BillingHandler) Get(...)` viravam "arestas" para ela. Os
> números eram 2 / 9 / 24 / 28 para `scale` / `rollback` / `status` / `domains`.
>
> **A medição correta é mover de verdade e perguntar ao compilador** — copiar a árvore, `git mv` a
> família para um subpacote, `go build` **dos dois lados**, ler os símbolos indefinidos:
>
> | Candidata | linhas · arq. | pai perde | família perde | veredito |
> |---|---|---|---|---|
> | **`domains`** | 886 · 5 | — | — | **próxima** — zero nos dois sentidos, e a maior |
> | `scale` | 601 · 1 | — | `ProjectResult` | alias; uma linha de import |
> | `rollback` | 236 · 2 | — | `ProjectResult` | alias; uma linha de import |
> | `status` | 728 · 2 | `StatusProjectStore` | `ProjectResult` | o único acoplamento real restante |
> | `repositories` | 469 · 1 | — | — | extraída nesta fatia |
>
> `ProjectResult` não é tipo compartilhado: é **alias** (`projects.go:37`) para
> `domain.ProjectResult`, que `preview.go` e `secrets.go` já importam qualificado.
>
> A extração de `repositories` continua correta — uma contagem super-inclusiva devolvendo ~0 é prova
> sólida de **ausência**. O que não sobreviveu foi a **rejeição** das outras: `domains` era a melhor
> candidata disponível e o artefato foi o que a rejeitou. A referência que sobrou era prosa:
> `integrations_github.go:61,63` menciona `RepositoryStore` para **explicar** por que declara a
> própria `InstallationLookup` — ele já aplicava ISP.
>
> **O probe quase mediu o vazio, e isso vale mais registrado do que a tabela.** A primeira execução
> devolveu zero para tudo nos dois sentidos, porque o `go.work` referencia o irmão
> `../loopzero-contracts`, ausente na cópia — nada compilou. É a aprovação vácua que esta sessão vinha
> consertando em gates, prestes a entrar como *correção* de uma tabela falsa. O probe só vale com um
> sanity-check de que a árvore **intacta** compila.
>
> **A receita da fatia 8 aplicou-se inteira, incluindo a falha que ela previu.** O teste quebrou com
> `undefined: withAuth` no instante em que o arquivo saiu — o mesmo helper, o mesmo erro que o
> comentário da cerca registra nominalmente. O leaf `routestest`, criado naquela fatia exatamente
> para isso, resolveu.
>
> **E a cerca recusou dois registros meus antes de aceitar o terceiro:** declarei conjunto vazio
> (supondo que a família tinha escritores próprios), depois só `httpio`, e ela reprovou os dois. É o
> registro sendo explícito em vez de derivado — a decisão que a fatia 9 tomou depois de a derivação
> se revelar ambígua.
>
> **A cerca ganhou uma quarta regra nesta fatia**, também vinda do review: `routestest` passou a ser
> um leaf **só-de-teste**. Declará-lo — o que toda família com teste precisa fazer — licenciava o
> código de **produção** da família a importá-lo, e `routestest.DefaultUserContext` devolve uma
> identidade `owner`. Provado por mutação: a import em `repositories.go` de produção deixava a árvore
> real GREEN em `0 sibling edge(s)`. `tenants` carregava a mesma declaração desde a fatia 8.

> **Fatia `slice-27` — a quarta família saiu: `domains`.** **6** subpacotes, **35** arquivos de
> produção no pai (eram 40).
>
> **Foi a primeira escolhida pelo compilador, e ela refuta diretamente a tabela que a fatia 19
> publicou.** Aquela tabela, por `grep -lw`, ranqueava `domains` em **último** com "28 arestas" — cada
> uma das quais era um método chamado `Get`/`List`/`Create`/`Delete` em **outro tipo**. Medida movendo
> a família numa cópia e lendo o que cada lado deixa de compilar: **o pai não perde nada, a família não
> perde nada de produção**. É a maior das candidatas (886 linhas, 5 arquivos), e era a melhor o tempo
> todo.
>
> O sanity-check do probe foi feito antes: a árvore intacta compila. Sem ele, "zero erros" e "zero
> medições" são indistinguíveis — foi o que quase aconteceu na fatia 19.
>
> **O único acoplamento real eram os helpers de teste**, e os quatro (`withAuth`,
> `defaultUserContext`, `memberUserContext`, `withChiParam`) já viviam no leaf `routestest` que a fatia
> 8 criou por prever exatamente isto.
>
> **E migrar consumidor por consumidor foi errado três vezes seguidas.** Meu primeiro `grep` de
> símbolos pegou funções de teste e perdeu tipos declarados em bloco `type (...)`; depois persegui um
> arquivo por vez enquanto o compilador apontava o próximo. O que resolveu foi uma varredura única
> sobre todos os consumidores, e ela pegou `internal/domain/validation.go`, que eu não tinha visto.
> **Deixar o compilador enumerar é mais barato que adivinhar, e é a mesma lição da tabela.**
>
> **O review não achou defeito na extração** — refez o probe do zero (com o sanity-check) e bateu; a
> cerca é mínima (sem `httpio` → 5 arestas, sem `routestest` → 2); os 33 testes migraram com nomes
> idênticos e o de rate-limit ficou **byte-idêntico** depois de desfazer o `routestest.`; o router não
> foi tocado. O que ele achou foi higiene, e duas coisas valem registro.
>
> **91 arquivos de puro `gofmt` estavam enterrando as 18 edições reais.** Meu `gofmt -w` alcançou o
> módulo e normalizou sujeira pré-existente do HEAD — não escondia nada (provado arquivo a arquivo),
> mas derrota a coesão de diff que o `cycle-implement § 4.7` exige. Revertidos; o diff é 18 arquivos,
> 7 da família e 11 consumidores.
>
> **E um arquivo alheio apareceu na árvore durante a revisão** — uma auditoria de RLS de sessão
> anterior, nunca commitada, com 3 `t.Skip` sem issue. Ela deixava `quality:all` vermelho, e um
> `git add -A` a teria levado junto, fazendo o push falhar por motivo que pareceria desta fatia.
> Isolada.
>
> **Fica registrado o que NÃO é desta fatia:** `api/internal/routes/projects.go` continua sem `gofmt`
> (estava assim no HEAD), e 5 símbolos exportados da família não têm consumidor externo — são aliases
> cujo comentário fala em "backward compatibility" com um nome que esta mudança já aposentou. Nenhum é
> código morto (todos usados dentro do pacote), mas a superfície do pacote merece decisão na próxima
> fatia que tocar `domains`.

## B-081 — As 277 mensagens de erro autoradas nos handlers não têm gate de vocabulário   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 na revisão da fatia slice-5. `httpio.WriteLoopZeroError` é pass-through puro para `errorshttp.WriteJSON` — ele não inventa vocabulário nem o filtra. Alimentado com `"ArgoCD sync failed for tenant_id tn-1; check Gitea and MinIO"`, os quatro termos banidos chegam ao corpo em `detail` **e** `message`, verbatim, e o writer está funcionando exatamente como projetado. A superfície de risco real são as **277** mensagens autoradas nos call sites, e entre os 122 gates **não existe nenhum** sobre o vocabulário delas.
why_now: a Regra Inquebrável 3 proíbe `ArgoCD`, `GitOps`, `CRD`, `tenant_id`, `Gitea`, `MinIO`, `Kaniko` e outros em saída ao usuário, e hoje isso é honrado por disciplina. A revisão confirmou que nenhuma mensagem em `routes` carrega termo banido **hoje** — então o risco é de regressão, não presente, e um gate agora é barato porque nasce verde.
status: raw
severity: média — nada quebrado hoje; o que falta é o que impede a próxima mensagem de quebrar.
dod:
  - existe gate que varre as mensagens de erro autoradas em `routes/` (e `services/`) contra a lista da Regra 3
  - ele nasce verde e reprova uma mensagem suja num fixture
  - a lista de termos vive num lugar só, não duplicada entre gate e regra

> Registrado 2026-08-12 pela revisão independente da fatia slice-5 (achado M1). O teste que existia
> em `httpio` afirmava cobrir *"EVERY error the API emits"* e cobria o que um pass-through pode
> cobrir; foi reescrito para dizer o que de fato mede, e este item guarda o que ele não mede.

## B-082 — `check-loopzero-api-vault-projected-token_test.sh` reprova de forma intermitente dentro do `quality:gates`   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: observado em 2026-08-12. Um run de `task quality:gates` reprovou com `FAIL: infra/helm/loopzero-api: env LOOPZERO_VAULT_ADDR MISSING` e `FAILURES: 1/15`. O mesmo script, rodado isoladamente antes **e** depois da mudança em stash, saiu **0** nas duas vezes; e o run seguinte de `quality:gates` completo saiu **0**. A variável `LOOPZERO_VAULT_ADDR` não está no ambiente nos dois casos, então a diferença não é ambiental de forma óbvia — o teste renderiza o chart e procura a env var no resultado (`infra/tests/secrets/check-loopzero-api-vault-projected-token_test.sh:154,180`).
why_now: `.claude/rules/testing.md` é categórico — *"testes devem ser determinísticos; se falha de forma intermitente, é um bug e deve ser corrigido ou removido"*. Um gate que reprova uma vez em N bloqueia um push por motivo que não se reproduz, e o custo real disso é ensinar o time a re-rodar até passar, que é como um gate morre.
status: raw
severity: média — não indica defeito de produto; indica um gate em que não se pode confiar de forma binária.
dod:
  - a causa da intermitência está medida (render em paralelo? cache do helm? dependência de ordem entre os 15 sub-testes?)
  - o teste passa em 10 execuções consecutivas do `quality:gates` completo, ou é corrigido/removido
  - se for dependência de ordem, ela é eliminada e não contornada com retry

> Registrado 2026-08-12 durante a fatia slice-5. Não foi causado por ela: provado com a mudança em
> stash — 0 antes, 0 depois, isoladamente.

## B-083 — `task lint` continua vermelho por 88 achados em `api/`, que o B-009 nunca cobriu   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12, ao fechar o `B-009`. Aquele item mediu *"11 achados, todos em `operators/`"* — e os 11 estão em zero agora. Mas `cd api && golangci-lint run ./...` devolve **88**: 44 `gosec`, 34 `staticcheck`, 7 `unused`, 1 `errorlint`, 1 `errcheck`. **Nenhum** deles está nos arquivos que a sessão de fatias tocou (`routes/secrets.go`, `routes/projects.go`, `db/query_executor.go`, `db/dbtx/`, `routes/httpio/`) — são pré-existentes. A medição original do `B-009` provavelmente parou no primeiro módulo que reprovou: `task lint` roda os módulos em comandos separados e o Task aborta no primeiro erro.
why_now: `task lint` é uma das linhas do `CLAUDE.md § Pre-completion checklist`, e continua não-marcável — agora por um corpo diferente e maior do que o item que a explicava. O `task lint:baseline` passa (88 < 200), então há duas tasks com veredictos opostos sobre a mesma coisa, e a que aparece na checklist é a estrita.
status: raw
severity: média — nada quebrado; o custo é uma linha de checklist que ninguém pode marcar, que é como uma checklist deixa de ser checklist.
dod:
  - os 88 estão corrigidos, ou a fração irredutível está allowlistada com sunset conforme `code-quality-allowlist.txt`
  - `task lint` e `task lint:baseline` deixam de dar veredictos opostos — ou uma some, ou a checklist nomeia qual vale
  - o baseline de 200 (`api=140 + operators=60`) é reajustado: `operators` está em 0

> Registrado 2026-08-12 ao fechar `B-009` e `B-071`. Os dois vermelhos que o `B-009` narrava como
> "a checklist que ninguém roda inteira" eram na verdade três: os 11 de `operators`, os 4 testes de
> migração, e estes 88 — que só ficaram visíveis quando os outros dois saíram da frente.

## B-084 — A cerca `routes-acyclic` não vigia o pacote pai, e o risco cresce a cada família   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido por experimento em 2026-08-12, na revisão da fatia slice-8. `routesacyclic.Check` (`cmd/loopzero-ops/internal/quality/routesacyclic/check.go:120-140`) itera apenas subdiretórios de `api/internal/routes`; arquivos do **pacote pai** nunca são parseados. Um `routes/zz_probe.go` importando `routes/tenants` sai `4 subpackage(s), 0 unregistered, 0 sibling edge(s)` — **PASS, exit 0**. O sentido inverso (filho → pai) **é** pego, e foi verificado: reporta `ARESTA IRMÃ version → <routes-parent>`.
why_now: hoje é benigno — a única aresta pai → filho é `routes → routestest`, para um leaf. Mas restam **14 famílias** para sair, e nada impede que `routes/billing.go` passe a importar `routes/tenants` na próxima fatia em vez de mandar o helper compartilhado para o leaf. O gate diria verde sobre exatamente o acoplamento que o fatiamento existe para remover. O commit `8d4d0ac1` reconhece o buraco em prosa; a cerca não o fecha.
status: planned
resolved_in: .claude/knowledge-base/plans/slice-9-parent-fence-plan.md v1.1 — os quatro sentidos da cerca estao fechados, cada um sondado na arvore real com file:line. O gate le 97 arquivos (89 do pai + 8 dos subpacotes), contra 8 antes.
  A regra e "folha sim, familia nao": o pai pode alcancar uma folha — medido, suas 29 arestas vao todas para `httpio` (28) e `routestest` (1), nenhuma para familia —, e alcancar uma familia reprova.
  DUAS CORRECOES QUE A REVISAO FORCOU: (1) o ADR D2 rejeitava declarar as folhas e mandava DERIVAR do conjunto vazio — errado, porque `version: {}` e familia e `httpio: {}` e folha, indistinguiveis; revertido, e a razao da rejeicao virou `ErrIncoherentRegistry`, que recusa um nome declarado das duas formas ANTES de varrer. (2) o piso prometido nao existia — `routes/` vazio saia PASS —, e a primeira correcao era fraca: `parsed > len(subpacotes)` deixava passar uma mutacao que derrubava a leitura de 97 para 49. Agora reconcilia contra contagem independente.
severity: alta — não pelo estado atual, mas porque é uma cerca que vai ser confiada por 14 movimentos e tem um lado aberto.
dod:
  - `routes-acyclic` parseia também os arquivos do pacote pai
  - um fixture com `routes/x.go` importando uma família reprova, com file:line
  - a aresta legítima `routes → routestest` continua passando, declarada
  - fechado **antes** da terceira família sair — depois disso deixa de ser teórico

> Registrado 2026-08-12 pela revisão independente da fatia slice-8 (achado M2) e ratificado em
> `MEET-ASYNC-AMENDMENT-2026-08-12-004 § Amendment 2`, que fixa a condição de preceder a família 3.

## B-085 — Dez arquivos escrevem `.status` fora do contrato, em 28 pontos   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12, durante a fatia `slice-10`. Com o contrato decidido pelo ADR `2026-08-12-status-writer-contract` (merge-patch a partir de snapshot), a contagem por controller é: `app/exposure_controller.go` **3**, `platform/clustercredential_controller.go` **5**, `platform/gitrepositorybinding_controller.go` **3**, `platform/platformbootstrap_controller.go` **5**, `preview/preview_controller.go` **6**, `tenant/tenant_controller.go` **3**, mais **1** em `app/application_controller.go` e **1** em `environment/environment_controller.go` (estes dois já usam merge-patch no caminho principal). Total **25** chamadas cruas a `Status().Update` nesses seis; 27 no repositório todo, contando 1 em `application` e 1 em `environment`, que já usam merge-patch no caminho principal. Os dois que seguem o contrato — `environment` (5 merge-patch) e `application` (1) — são o modelo, e `environment_controller_conflict_test.go` é o molde de teste determinístico por interceptor.
why_now: até 2026-08-12 havia **duas** regras mandando o oposto sobre como escrever status, então não existia alvo único de migração. O ADR desta data escolheu uma; estes 27 pontos são o que falta para o invariante ser um invariante em vez de uma descrição de dois controllers.
status: shipped
severity: alta — é o mesmo modo de falha do B-055 (perda de escrita sob concorrência), agora sem ambiguidade sobre qual é o padrão certo.
dod:
  - os 25 pontos persistem por merge-patch a partir de um snapshot tirado logo após o `Get`
  - nenhum retorna `ctrl.Result{}, nil` numa falha de escrita de status — o contrato manda erro e requeue
  - cada controller migrado ganha um teste de conflito determinístico no molde de `environment_controller_conflict_test.go`
  - um gate estático recusa `Status().Update` novo nos operators — sem ele a migração é reversível por descuido
  - onde houver mais de um escritor, o patch usa `MergeFromWithOptimisticLock`: o RFC 7386 trata array como escalar, então um merge-patch de `conditions` carrega o array inteiro e sem o lock apaga a escrita alheia (medido na `slice-10`)

> Registrado 2026-08-12 pela fatia `slice-10` (ADR D3). Deliberadamente **não** feito junto com a
> decisão de contrato: são seis fluxos de reconcile independentes, e juntar a escolha da regra com
> sua aplicação em 25 pontos torna impossível saber qual dos dois quebrou.

> **Um dos seis feito — 2026-08-13, fatia `slice-18`**, executando a D3 do ADR
> `2026-08-12-status-writer-contract` (ata `MEET-ASYNC-AMENDMENT-2026-08-13-005`). O item **continua
> aberto**.
>
> **A medição mudou o escopo: as 25 chamadas não são uniformes.** Classificadas por forma:
>
> | Controller | *refetch dance* | readiness inline | usa o helper | estado |
> |---|---:|---:|---|---|
> | `tenant/tenant_controller.go` | 2 | 1 | não | **migrado** |
> | `tenant/tenant_deletion.go` | 0 | 0 | — | **pendente** — 2 `Update` nus em `reconcileDelete`, chamado do mesmo `Reconcile` |
> | `app/exposure_controller.go` | 0 | 1 | não | pendente — mesma forma do `tenant`, menor |
> | `platform/clustercredential_controller.go` | 0 | 2 | sim | pendente — só merge-patch |
> | `platform/gitrepositorybinding_controller.go` | 0 | 0 | sim | pendente — só merge-patch |
> | `platform/platformbootstrap_controller.go` | 0 | 0 | sim | pendente — só merge-patch |
> | `preview/preview_controller.go` | 0 | 0 | sim | pendente — só merge-patch |
>
> Quatro dos seis já delegam ao `DeferReadinessSummary`; para eles resta só trocar `Update` por
> merge-patch. Dois o reimplementavam à mão, e o `tenant` era o único com as **duas** formas
> proibidas — por isso foi o primeiro, por medição e não por ordem.
>
> **Três correções, duas delas a erros meus que a revisão mediu antes do commit.**
>
> (a) Eu escrevi "4 danças" no plano; são **2** (o `grep` de `savedStatus` conta duas linhas por dança).
>
> (b) **A mais grave.** Eu capturei o snapshot **antes** do `EnsureFinalizer`. `client.MergeFrom`
> diferencia o objeto **inteiro**, e o `EnsureFinalizer` faz Update+Get, avançando o `resourceVersion`
> na primeira reconciliação de **todo** Tenant — então o patch embarcava uma trava otimista
> **obsoleta**, que a revisão mediu contra um apiserver real devolvendo **409**. Pior: o refetch que
> removi acontecia **imediatamente antes** da escrita, então sua janela de conflito era quase zero; a
> minha teria a duração do reconcile inteiro, com oito passos de rede no meio. Era regressão de
> confiabilidade vestida de correção de contrato. O `environment_controller.go:176` documenta o
> posicionamento certo **de propósito** — eu li a linha e coloquei o meu no lugar errado.
>
> (c) O escopo estava **superafirmado**: `tenant_deletion.go` tem 2 `Update` nus em `reconcileDelete`,
> chamado do mesmo `Reconcile`. O arquivo está migrado; o **controller** não. A tabela agora diz isso,
> e uma catraca no teste trava a contagem.
>
> **O teste que teria pego (b) não é caro:** `client.MergeFrom(base).Data(obj)` é função pura, e
> asserir que os bytes emitidos não contêm `resourceVersion` são ~40 linhas sem infraestrutura
> nenhuma. Quatro asserções estruturais passavam com o defeito presente. O par de testes agora inclui
> o inverso — um snapshot obsoleto **produz** o RV —, senão a primeira asserção não estaria medindo
> nada.
>
> **A asserção é estrutural, e isso é deliberado.** O traço definidor da dança é a **forma** — um
> `Get` cujo único propósito é obter uma versão que não conflita. Nenhuma asserção de runtime
> distingue "releu e sobrescreveu" de "escreveu uma vez" quando não há mais ninguém escrevendo; a
> forma é a evidência. Um teste de conflito comportamental para o `tenant` exigiria montar o
> reconciler com Harbor/Gitea/MinIO, e fica para quando o gate estático entrar.
>
> **O gate estático da quarta bala fica de fora de propósito.** Criado agora, nasceria com **cinco**
> isenções — e esta série mediu quatro vezes o que acontece com um gate cuja única falha é uma
> isenção conhecida: ele não é ligado, e então não vigia nada (`B-045`, `B-051`, `B-072`, `B-077`).
> Ele entra quando os seis estiverem migrados, sem isenção nenhuma.

> **Re-medido 2026-08-13.** A contagem do item envelheceu em duas direções ao mesmo tempo, e as duas
> valem registrar porque só uma é progresso.
>
> Hoje são **28** chamadas cruas a `Status().Update` em **10** arquivos de produção sob
> `operators/internal/`, não 25 em seis. O item tinha subcontado o alcance: `app/application_reaper.go`
> (1) e `tenant/tenant_deletion.go` (2) nunca apareceram na medição original, e os dois escrevem status.
>
> A fatia `slice-18` converteu `tenant_controller.go` de 3 para **1**, que é o progresso real. O resto
> está intacto:
>
> | arquivo | cruas |
> |---|---:|
> | `preview/preview_controller.go` | 6 |
> | `platform/platformbootstrap_controller.go` | 5 |
> | `platform/clustercredential_controller.go` | 5 |
> | `platform/gitrepositorybinding_controller.go` | 3 |
> | `app/exposure_controller.go` | 3 |
> | `tenant/tenant_deletion.go` | 2 |
> | `app/application_controller.go` · `app/application_reaper.go` · `environment/environment_controller.go` · `tenant/tenant_controller.go` | 1 cada |
>
> Próxima fatia por tamanho e por isolamento: `preview_controller.go` — 6 pontos num arquivo só, e o
> `preview` não compartilha escritor de status com nenhum outro controller.

> **Fatia `slice-21` — o `preview` saiu, 2026-08-13.** Restam **22** escritas cruas em **9** arquivos:
>
> | arquivo | cruas |
> |---|---:|
> | `platform/clustercredential_controller.go` | 5 |
> | `platform/platformbootstrap_controller.go` | 5 |
> | `app/exposure_controller.go` | 3 |
> | `platform/gitrepositorybinding_controller.go` | 3 |
> | `tenant/tenant_deletion.go` | 2 |
> | `app/application_controller.go` · `app/application_reaper.go` · `environment/environment_controller.go` · `tenant/tenant_controller.go` | 1 cada |
> | **total** | **22** |
>
> O `preview` foi de **6** para **0**. Duas coisas que a conversão dele ensinou e que valem para os
> próximos:
>
> **A base não podia ser tirada na cabeça de cada `reconcileX`.** O `Reconcile` **muta** `.Status` na
> inicialização de fase antes de despachar, então um snapshot tirado depois disso diferia a fase
> contra ela mesma e o patch omitiria silenciosamente a transição que ele existe para gravar. Ela vai
> **depois** do finalizer (senão carrega `resourceVersion` velho — o 409 da fatia 18) e **antes** da
> inicialização de fase. É uma janela de duas linhas.
>
> **São dois snapshots, não um.** O caminho de deleção desvia **antes** do bloco do finalizer, então
> não existe base para ele naquele ponto; e uma base única no topo estaria velha para o caminho
> normal. Escopos disjuntos tornam impossível pegar a do outro caminho.
>
> **E a lição do teste, que vale mais que a conversão.** Minha primeira asserção "decisiva" construía
> os objetos à mão e nunca chamava `Reconcile` — media `controller-runtime`, não a colocação da base.
> Com o defeito da fatia 18 instalado, a suíte inteira passava. A que vale **executa** o `Reconcile` e
> lê o corpo do patch por interceptor, e ela guarda **dois** contratos opostos de propósito: o corpo do
> controller usa `MergeFrom` e não pode carregar `resourceVersion`; o `DeferReadinessSummary` usa
> `MergeFromWithOptimisticLock` e **tem** de carregar, porque refaz o fetch e `conditions` é array —
> escalar no RFC 7386. Minha primeira versão reprovava a segunda como se fosse defeito.
>
> Próxima por tamanho: `clustercredential_controller.go` ou `platformbootstrap_controller.go`, 5 cada.

> **Fatia `slice-22` — o `platformbootstrap` saiu, 2026-08-13.** Restam **17** escritas cruas em **8**
> arquivos: `platform/clustercredential_controller.go` 5, `app/exposure_controller.go` 3,
> `platform/gitrepositorybinding_controller.go` 3, `tenant/tenant_deletion.go` 2, e 1 cada em
> `app/application_controller.go`, `app/application_reaper.go`, `environment/environment_controller.go`
> e `tenant/tenant_controller.go`.
>
> **Duas diferenças em relação ao `preview`, e as duas mudaram o desenho.**
>
> A base é **uma só**: o caminho de deleção deste controller retorna sem escrever status, então não
> precisa da sua — o `preview` precisava porque escreve status ao limpar. E os helpers de transição
> são chamados de **outros arquivos** do pacote (`_gitea.go`, `_argocd.go`, `_api.go`), então a base
> atravessa cinco assinaturas, não três.
>
> **A asserção passou a ter lista de isenções, e ela é o registro do que falta.** O pacote `platform`
> tem três controllers e o `B-085` converte um por fatia: varrer o arquivo é burlável por helper irmão
> (medido na fatia 21), e varrer o pacote reprovaria arquivos que a fatia não toca. A lista declara
> cada não-convertido com a contagem **e a data**, e o conjunto de chaves é congelado à parte — porque
> a contagem trava crescimento *dentro* de um isento, mas nada travava **acrescentar uma chave nova**;
> "a lista só encolhe" era convenção que eu afirmava em comentário e em lugar nenhum. Achado do review.
>
> **E a verificação de colocação deixou de comparar offsets de texto.** Eu conferia que `base :=`
> aparecia depois de `AddFinalizer(` no arquivo — e o review quebrou isso nas duas direções: extrair o
> finalizer para um helper definido **acima** do `Reconcile` faz a base textualmente posterior e
> semanticamente anterior (passa, com o defeito instalado); e se este controller ganhar uma base no
> caminho de deleção, como o `preview` tem, a primeira ocorrência cai antes e código correto reprova.
>
> Agora o teste **executa** o `Reconcile` com interceptor e afirma três coisas: o patch do corpo não
> carrega `resourceVersion`; ele carrega a transição de fase; e — a geral — **o que ficou persistido é
> o que o controller tinha em mãos**. Essa última é a única que enxerga um campo qualquer mutado antes
> do snapshot: o patch não pode revelá-lo, porque ele fica simplesmente **ausente**, e ausência é
> indistinguível de "não devia mudar".
>
> Uma armadilha do próprio teste, registrada porque quase o deixou vácuo: com `Generation` no valor
> zero da semente, a mutação de sonda escrevia 0, o `omitempty` o descartava, e a comparação não via
> nada. O fixture passou a nomear `Generation: 7`.

> **Fatia `slice-23` — o `clustercredential` saiu, 2026-08-13. Restam 12 escritas em 7 arquivos.** E
> ela achou a primeira coisa nesta série que **não** é mecânica.
>
> A conversão quebrou contra o apiserver do envtest, com uma mensagem que diz tudo:
> `status.connected: Required value`. O CRD declarava o campo como obrigatório, o merge-patch o **omite**
> (ele é igual na base e no objeto), e aplicar esse patch a um objeto cujo `status` nunca foi escrito
> produz um status sem o campo. O `Update` funcionava porque mandava o status inteiro.
>
> **Medido: dos oito CRDs, só este tem `status.required`.** Por isso as seis conversões anteriores
> passaram, e por isso vale saber que as sete restantes também são mecânicas nesse aspecto.
>
> A decisão (`MEET-ASYNC-AMENDMENT-2026-08-13-clustercredential-connected-optional`, T2) é que o campo
> vira `+optional`: para um `bool` puro, **ausente e `false` são o mesmo valor** — exigi-lo não comprava
> distinção nenhuma (isso pediria `*bool`). A tag JSON fica sem `omitempty` de propósito, para o campo
> continuar sempre serializado e a `printcolumn` seguir mostrando `false` em vez de vazio.
>
> **E o achado sobre os próprios testes desta série, que o review completou.** O cliente falso diverge
> do apiserver de **duas** formas, e a segunda é a que escondeu o defeito. (1) Ele não valida contra o
> esquema do CRD — aceita o que o servidor recusa, inclusive a semente do teste novo. (2) Ele guarda
> objetos **tipados**, não documentos: um campo de valor zero sem `omitempty` é sempre re-materializado
> no round-trip, então ele mostra `connected: false` onde o servidor não guarda chave nenhuma.
>
> A consequência vale para as sete fatias restantes: o detector de perda de escrita — o que compara o
> que o controller tinha em mãos com o que ficou persistido — é **estruturalmente cego** à classe
> ausente-versus-falso. Ele prova intenção de escrita, não forma do que ficou gravado. Quem enxerga essa
> classe é o envtest, lendo o objeto **não-estruturado**.
>
> **E eu tinha afirmado, em cinco lugares, algo falso:** que manter a tag sem `omitempty` preservava a
> coluna do `kubectl get`. A premissa era certa (o marshal do status inclui o campo) e a conclusão não
> decorria dela — **o merge-patch envia o diff, não o status serializado**. Medido pelo review contra
> apiserver real: a coluna ficava vazia em toda credencial ainda não conectada. O conserto é
> `+kubebuilder:default=false`, que faz o servidor materializar o campo, e está preso por asserção que
> lê o objeto não-estruturado — a leitura tipada devolve `false` nos dois casos e é cega para isto.

> **Fatia `slice-24` — o `gitrepositorybinding` saiu, e com ele o pacote `platform` inteiro.** Restam
> **9** escritas em **6** arquivos, todas fora dele: `app/exposure_controller.go` 3,
> `tenant/tenant_deletion.go` 2, e 1 cada em `app/application_controller.go`,
> `app/application_reaper.go`, `environment/environment_controller.go`, `tenant/tenant_controller.go`.
>
> A mais simples das quatro, e por uma razão que vale nomear: as três escritas estão **todas** no
> `Reconcile`, e os helpers apenas *setam* condições sem persistir. Nenhuma assinatura mudou. Onde as
> anteriores exigiram a base atravessando três e cinco funções, aqui ela nasce e morre no mesmo escopo.
>
> **A lista de isenções ficou VAZIA**, que é a primeira vez que ela prova o que afirma. Ela e sua irmã
> congelada continuam no código em vez de serem apagadas: são elas que fazem a próxima escrita crua
> neste pacote reprovar alto, em vez de chegar com uma isenção anexada. Mutante confirmou.
>
> **E a lição da fatia 23 foi aplicada antes de converter, não depois:** conferi que este CRD não tem
> `status.required` e que nenhum campo de status é valor-zero sem `omitempty` — as duas condições que
> tornam o merge-patch não-mecânico. Nenhuma se aplica aqui.

> **Fatia `slice-25` — o `exposure_controller` saiu.** Restam **5** escritas em **4** arquivos.
>
> E ela achou mais do que as três escritas: o arquivo carregava uma **cópia manual do sumário de
> prontidão**, e a cópia era a forma **anterior** ao conserto da fatia 10 — persistia com
> `Status().Update` (sem a trava otimista, então uma escrita de `conditions`, que é array e portanto
> escalar no RFC 7386, podia apagar escrita concorrente do array inteiro) e **descartava o erro**
> (`_ =`), no único lugar que não tem para onde retornar. Trocada pelo helper compartilhado. É por isso
> que conhecimento duplicado é caro: o conserto da fatia 10 nunca chegou aqui.
>
> A asserção nova — *ninguém rola o sumário à mão* — achou uma **segunda** ocorrência,
> `application_controller.go`. Mas ali não é cópia obsoleta: ele **funde** o sumário no próprio patch,
> e o comentário diz por quê — *"so the deferred Status().Update is no longer needed (single write per
> pass)"*. É desenho melhor, não pior: uma escrita por passagem, sem segundo escritor. Minha asserção
> perguntava *"chama o helper?"* em vez de *"reimplementa os DEFEITOS do helper?"*. Declarei a exceção
> com a razão em vez de enfraquecer a asserção até ela não pegar nada.
>
> Pré-checagem da fatia 23 feita antes de converter: nenhum dos dois CRDs do pacote tem
> `status.required`, e todo campo de status tem `omitempty`.
>
> **Correção de contagem, achada pelo review.** Eu vinha publicando "6 em 5" e o número real é **5 em
> 4**: a sexta era um **comentário** em `tenant/tenant_controller.go:179` citando
> `_ = r.Status().Update(...)` para chamá-lo de padrão proibido. O erro vem da fatia 24 e esta o
> propagou por aritmética. A ironia é exata: o `stripGoComments` que escrevi **neste mesmo teste**
> remove comentários antes de contar e teria acertado — eu não o apliquei à prosa. É a armadilha da
> fatia 22 numa terceira roupa.
>
> **E a exceção que declarei estava justificada errado.** Eu disse que o `application_controller.go`
> funde o sumário no próprio patch por desenho "melhor". O review mostrou que é forma **diferente e mais
> fraca**: ela é **pulada em retorno antecipado** — quatro caminhos de erro retornam antes de o sumário
> ser computado, e o helper é `defer` justamente porque o `ADR-0025 D3` exige que o sumário seja a
> última mutação de status *mesmo em retorno antecipado*; e a base dela é tirada **depois de um
> re-fetch**, então o diff **reverte ativamente** qualquer campo que outro escritor tenha mudado no
> intervalo. Não é a dança proibida pela letra (aquela nomeia `Update`, esta é `Patch`), mas o dano que
> o contrato nomeia sobrevive à troca de nome. Vira `B-105`.

> **Fatia `slice-26` — as três últimas saíram. De 28 para 1.**
>
> `tenant/tenant_deletion.go` (2), `environment/environment_controller.go` reconcileDelete (1) e
> `app/application_reaper.go` (1). As quatro tinham a **mesma forma**, e é por isso que sobraram: todas
> em caminho de deleção ou reaper, não no corpo do reconcile. O desvio de deleção acontece **antes** do
> bloco do finalizer, então cada um tira a própria base — o desenho que a fatia 21 validou no `preview`.
>
> **E aqui eu afirmei uma necessidade em vez de medi-la — em três lugares.** Escrevi que o `Get` depois
> da escrita do reaper continua necessário *"porque o merge-patch bumpa o `resourceVersion` e o objeto
> em memória fica velho"*. **Falso, e falso desde antes desta fatia.** `Status().Patch` e
> `Status().Update` **ambos** decodificam a resposta de volta no objeto do chamador
> (`typed_client.go:274` e `:305`, `.Into(body)`). Medido contra um cliente: `999` antes, **`1000` em
> memória e no servidor** depois. O objeto nunca fica velho.
>
> O `Get` é **conservador, não necessário** — uma chamada de API redundante por reap. Fica, porque
> removê-lo é mudança de comportamento numa função que **não tem chamador de produção nenhum** (grep
> devolve só testes), e o comentário passou a dizer isso em vez de inventar uma razão. O irmão
> `environment_controller` já faz patch de status e remove finalizer sem re-fetch entre os dois.
>
> É a forma da fatia 23 de novo: **afirmação de necessidade tratada como medição**, e desta vez ela já
> tinha entrado no backlog como achado.
>
> **A que resta está isenta por razão medida.** `application_controller.go:235` fica dentro do
> `reconcileExposedRoute`, chamado em `:135`, enquanto a única base do arquivo nasce em `:158` — depois
> da chamada. **Não existe base para essa escrita usar**, e inventar uma decidiria a ancoragem do
> arquivo, que é justamente o que o `B-105` reserva.
>
> **Duas lacunas fechadas, apontadas pelo review.** Das quatro escritas convertidas, a do ramo
> `len(deleteErrs) > 0` do `tenant_deletion.go` era executada por **teste nenhum** — cobertura 0 — e é
> justamente a que grava a condição de falha de limpeza, ou seja, a que um operador lê quando algo deu
> errado. E o ratchet conta a **ausência** de `Status().Update`, mas nada dizia sobre **onde a base
> está** nos caminhos de deleção. Agora há guarda que executa o `reconcileDelete` e lê o corpo do
> patch; ele aborta na primeira escrita, porque é ali que a ancoragem se decide e satisfazer as
> dependências seguintes o transformaria num teste de teardown. Mutante confirmado: base depois da
> mutação de fase reprova nomeando a transição perdida.
>
> **E o ratchet da fatia 18 pegou a própria fatia que o zerou.** Ele reprovou com *"down from 2 — this
> ratchet must be lowered in the same commit, or it stops measuring anything"*, que é a frase que
> escrevi nele. Baixado para 0 e mantido como piso: arquivo em zero é onde a próxima escrita crua
> passaria despercebida.
>
> **O item fica aberto**, esperando o `B-105`. Uma escrita de 28 é um resultado; chamá-lo de zero seria
> a deriva que esta sessão passou o tempo todo consertando.

> **Fechado 2026-08-14 — de 28 para ZERO.**
>
> **CORREÇÃO — este fechamento estava errado, e a auditoria adversarial de fechamento o pegou.** A
> contagem é verdadeira: nenhuma chamada de produção de `Status().Update` sobrevive. Mas a **bala 4 do
> DoD** pede outra coisa — *"um gate estático recusa `Status().Update` novo nos operators — sem ele a
> migração é reversível por descuido"* — e esse gate **não existia**. A auditoria provou por mutação:
> injetou uma escrita crua nova em `operators/internal/environment/environment_controller.go` e a suíte
> inteira de `operators/internal/...` saiu **exit 0**, 14 pacotes `ok`.
>
> A causa é de cobertura. As catracas são **por pacote** e existem em alguns; `environment` não tinha
> nenhuma. E dos 123 gates do `loopzero-ops`, **zero** casavam com o assunto.
>
> **O padrão vale mais que o defeito**, e a auditoria o encontrou em três itens: o fechamento foi
> contra *o resultado mais fácil de medir* — a contagem zerada — enquanto a bala que pedia a **guarda
> contra o retorno** ficou sem dono. É a forma que este próprio item nomeia noutra fatia: *"um gate
> cuja única falha é uma isenção conhecida não é ligado, e então não vigia nada"*. Houve deriva de
> alvo: uma nota anterior dizia que o gate *"entra quando os seis estiverem migrados"* — os seis
> migraram, o gate não entrou, e a nota de fechamento não mencionou.
>
> **Bala 4 fechada em 2026-08-14** com o gate `status-writer-contract`
> (`cmd/loopzero-ops/internal/quality/statuswritercontract/`), ligado em `quality:gates`. Ele varre **toda**
> a árvore `operators/` — 83 arquivos de produção — em vez de depender de uma catraca por pacote, e a
> inversão é o ponto: uma catraca protege o pacote que alguém lembrou de proteger, um gate que varre
> falha pela **ausência** de proteção.
>
> Reconhece as três formas que a revisão da fatia 27 provou serem burla de duas linhas: apelido local
> (`sw := r.Status()`), `SubResource("status")`, e as duas combinadas. **O mutante exato da auditoria
> agora reprova**, nomeando arquivo, linha, função e a forma pela qual o escritor foi alcançado.
> Decisão em `MEET-ASYNC-AMENDMENT-2026-08-14-status-writer-gate`. A última escrita saiu junto com o defeito que a
> bloqueava.
>
> Ela estava presa porque o `application_controller.go` tirava a base **depois de um re-fetch**, então
> não havia base para a escrita de `reconcileExposedRoute` usar. Medi o que aquele re-fetch protegia:
> as duas únicas escritas de metadata/spec anteriores — `EnsureFinalizer` e `reconcileHostname` — **já
> re-buscam o objeto por conta própria**. Quando o fluxo chega na composição do status, o objeto em
> memória já É o do servidor; o terceiro re-fetch não protegia nada e custava jogar fora o status
> computado para reconstruí-lo por atribuição.
>
> Com ele fora, a base é tirada no lugar certo, atravessa o `reconcileExposedRoute`, e a escrita que
> restava virou patch. `grep` de `Status().Update(` em `operators/internal/` sem comentários: **0**.
>
> **O que isso conserta é maior que a contagem.** O desenho anterior ancorava o diff no estado
> **fresco** enquanto carregava o composto **anterior** — ele *revertia* campo escalar que outro
> escritor tivesse mudado no intervalo (`url`, `phase`, `observedGeneration`, `endpointsReady`). Era o
> dano que o contrato nomeia, vestindo um `Patch`, e por isso a tabela mecânica o pontuava como
> conforme: ela conta a **chamada**, não a **âncora**.
>
> **Escalar, não tudo — a revisão corrigiu meu exagero.** `status.conditions` é array, e o RFC 7386
> trata array como escalar: qualquer passagem que mude uma condição continua substituindo o array
> inteiro. Isso não é regressão nem foi introduzido aqui, mas dizer "qualquer alteração" afirmava mais
> do que a mudança entrega.
>
> Decisão em `MEET-ASYNC-AMENDMENT-2026-08-14-application-status-anchor`. O `B-105` **continua aberto**
> pela outra bala: o sumário de prontidão inline é pulado em retorno antecipado, e o `ADR-0025 D3`
> exige que ele seja a última mutação de status mesmo aí. Uma coisa por fatia.

## B-086 — Um documento de decisão que registra a ausência de um ADR faz esse ADR resolver   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 pela revisão independente da fatia `slice-10`. O resolvedor de ADR do gate `docs-contract-refs` aceita como documentação qualquer `.md` sob `docs/` ou `.claude/` que **não** esteja em `cycleArtifactDirs` (`cmd/loopzero-ops/internal/quality/docsrefs/check.go:258-265`, `:286-317`) — e `docs/wiki/decisions/` não está na lista. O ADR `docs/wiki/decisions/2026-08-12-status-writer-contract.md` cita `ADR-0024` cinco vezes **para dizer que ele não existe**, e com isso `ADR-0024` passou a resolver. É textualmente o que o comentário do gate proíbe em `check.go:256` — *"Writing the document that records an absence must not manufacture a presence"* — e o que `TestCheck_CycleArtifactsAreNotADRDocumentation` (`check_test.go:257-275`) fecha para `.claude/knowledge-base/`, um diretório ao lado. Sem efeito hoje: nenhum comentário Go cita mais `ADR-0024`, e a isenção que os cobria foi retirada do allowlist na mesma fatia.
why_now: a fatia `slice-10` deletou as 8 citações que o `B-052` registrou, então o gate ficou sem nada para resolver — e a armadilha ficou latente em vez de visível. A próxima citação a um ADR fantasma que já tenha sido tema de um ADR passa despercebida.
status: raw
severity: média — latente, não ativa. Vira alta na primeira citação nova a um id de ADR que algum documento de decisão já mencione.
dod:
  - uma menção a `ADR-NNNN` dentro de um documento que **decide sobre** esse ADR deixa de contar como documentação dele
  - o critério que distingue "documenta" de "cita para decidir" está escrito, não inferido do diretório — `docs/wiki/decisions/` legítimo continua resolvendo os ADRs que ele de fato define
  - uma fixture reproduz o caso: um `.go` citando `ADR-XXXX` mais um ADR que só fala sobre `ADR-XXXX` reprova
  - o teste de não-regressão continua verde: um ADR real em `docs/wiki/decisions/` resolve normalmente

> Registrado 2026-08-12 pela fatia `slice-10` (`MEET-ASYNC-AMENDMENT-2026-08-12-006` amendment 2).
> **Substitui** a versão anterior deste item, que afirmava que o gate varria apenas markdown — falso,
> medido: `check.go:193-219` varre **comentários Go** sobre `api pkg operators cmd tools infra`, e as
> fixtures em `check_test.go:266` e `:282` já são arquivos `.go`. A afirmação errada era minha, feita
> sem medir, numa fatia cujo objeto é apagar afirmações normativas que não se sustentam.

## B-087 — `task quality:all` aborta no primeiro passo, contra um diretório que não existe desde o M9   [x]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-12 pela revisão da fatia `slice-11`. `taskfiles/quality.yml:58-62` define `quality:hermetic`, cujo primeiro comando é `cd pkg && go test ./quality/...`. O pacote `pkg/quality` **foi relocado no M9** para `cmd/loopzero-ops/internal/quality/`. Rodando: `pattern ./quality/...: lstat ./quality/: no such file or directory` → `task: Failed`. E `quality:all` é `deps: [hermetic, gates, contract]` (`quality.yml:368-370`), então **o alvo agregado inteiro aborta no primeiro passo**. Só `quality:gates` sozinho é verde — que é o que o `delivery-gate.sh` roda, e por isso ninguém percebeu.
why_now: o `CLAUDE.md` documenta `task quality:all` como "hermetic tests + full scan + contract gates". Quem seguir a documentação recebe uma falha imediata e pode concluir que o repositório está quebrado. E os testes hermeticos que o alvo prometia rodar **não rodam desde o M9**.
status: shipped
severity: alta — um alvo de qualidade documentado que aborta é pior que um ausente: ele parece cobertura.

> **CORREÇÃO 2026-08-14 — a auditoria adversarial reprovou a terceira bala, e tinha razão.**
>
> **A auditoria subiu o veredito de "fechado com ressalva" para FALSAMENTE FECHADO**, por uma razão
> mais forte que a minha: não é só que a guarda não pegaria um retorno futuro — ela **não estava
> pegando o que já estava lá**. O `taskfiles/security.yml:40` carregava a instância viva no HEAD, no
> momento em que o item foi marcado `shipped`.
>
> As duas medições convergiram de forma independente e na mesma janela: ela varreu os taskfiles
> versionados e achou 3 candidatos (2 artefato de regex, **1 real**); a guarda estendida achou
> exatamente esse 1 no primeiro run. O conserto já estava commitado quando o adendo chegou.
>
> A bala pede *"asserção contra alvo que entra em diretório inexistente"*, e a nota afirmava que
> `TestTaskfilesDoNotCdIntoMissingDirs` era essa asserção. A auditoria reintroduziu o defeito
> **verbatim** — `cd pkg && go test ./quality/... -count=1` — e a guarda saiu **exit 0, PASS**.
>
> A razão é precisa e vale registrar: o matcher fazia `stat` **só no argumento do `cd`**, e no defeito
> original `pkg` **existe** — é um módulo de verdade. Quem não existia era o padrão de pacote
> `./quality/...`, movido no M9. A guarda olhava para o lado certo da linha e para a metade errada.
>
> **Fechada** estendendo o matcher para validar também o padrão de `go test|build|vet`, resolvido
> relativo ao alvo do `cd`. Escopo estreito de propósito — `./...` é o módulo e é sempre válido,
> padrão com template não é nosso para resolver, e só a parte de diretório é verificada porque `...`
> é curinga **sob** um diretório real. Uma guarda que reprova task saudável é uma guarda que alguém
> desliga.
>
> **E ela achou um defeito vivo no primeiro run.** `taskfiles/security.yml:40` rodava
> `cd pkg && go test ./quality/deadlineratchet/...` — mesmo padrão, mesmo M9, e a task
> `service-auth-ed25519:test-rotation-ratchet` estava **falhando hoje**, medida antes e depois:
> `FAIL ./quality/deadlineratchet/... [setup failed]` → `ok ... 0.008s`. Ninguém tinha percebido
> porque nada afirmava sobre padrão de pacote. Repontada para `cmd/loopzero-ops/internal/quality/`.
>
> A mensagem ganhou tipo. A primeira versão dizia *"`cd pkg/quality` aponta para caminho inexistente"*
> quando o `cd` era `pkg` — mandava o leitor procurar uma linha que não existe no arquivo. Agora um
> `cd` morto e um padrão morto têm mensagens diferentes, porque o remédio é diferente.
>
> **Nota de reprodução:** a nota original afirma **121** passos em `quality:all`; hoje são **123**
> (exit 0). O número envelheceu com gates novos — e um número que não reproduz é indistinguível de um
> número inventado, que é por que este fica dito em vez de corrigido em silêncio.

dod:
  - `task quality:all` completa, com exit code real
  - `quality:hermetic` aponta para onde os testes vivem hoje, ou deixa de prometer o que não roda
  - existe asserção de que um alvo do Taskfile não referencia diretório inexistente

> Registrado 2026-08-12 pela revisão da fatia `slice-11`. Pré-existente, não introduzido pela fatia.

> **Fechado 2026-08-13** pela fatia `slice-14`. `quality:hermetic` passou a rodar
> `cd cmd/loopzero-ops && go test ./internal/quality/...` — restaurando o que o `desc` do alvo declara
> ("Run quality-gate hermetic Go tests"), que ele deixou de fazer no M9. Rejeitada a saída de apenas
> **remover** o comando quebrado: faria `quality:all` completar trocando um abort visível por um
> verde vazio.
>
> A terceira bala (*"asserção de que um alvo não referencia diretório inexistente"*) é coberta pelo
> gate `test-target-coverage` do `B-088`: um diretório que não existe não tem `go.mod`, e um módulo
> que existe sem alvo reprova.

> **Fechado — verificado 2026-08-13.** `task quality:all` completa com **exit 0** e executa **121**
> passos (`grep -c '^task: \[' `). As três balas: o alvo completa com código real; `quality:hermetic`
> passou a rodar `cd cmd/loopzero-ops && go test ./... -count=1` em vez de só `go build`; e
> `TestTaskfilesDoNotCdIntoMissingDirs` existe e roda — ela é a asserção de que um alvo não referencia
> diretório inexistente, que era a família do defeito. O conserto veio em fatia anterior; o item ficou
> `raw` até esta medição, que é o inverso do defeito que ele mesmo descreve.

## B-088 — Os testes de `cmd/loopzero-ops` e de `tools/` não são rodados por nenhuma task   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 pela revisão da fatia `slice-11`. `taskfiles/test.yml:107-111` (`test:unit`) roda `pkg`, `operators`, `api` e `infra/tests` — **não** `cmd/loopzero-ops`, **não** `tools/`. `quality:hermetic` faz `cd cmd/loopzero-ops && go build ./...` — **build, não test**. E `grep -rn "loopzero-dockerfile-lint" taskfiles/ Taskfile.yml .github/` devolve **zero**. Consequência medida na própria fatia: os 12 testes que ela escreveu (6 no linter, 6 no gate) **nunca executam** por nenhum caminho automatizado.
why_now: `cmd/loopzero-ops` é onde vivem os 123 quality gates — a substituição declarada do CI. Os testes que provam que um gate reprova o que deve reprovar não rodam em lugar nenhum, o que torna cada gate uma afirmação não verificada. É a mesma forma do `B-045` (gate que existe e não roda), um nível acima: o **teste** do gate que existe e não roda.
status: shipped
severity: alta — um teste que nunca executa não é teste, e aqui são os testes do mecanismo que substitui o CI.
dod:
  - `cmd/loopzero-ops` e `tools/loopzero-dockerfile-lint` entram num alvo que roda de fato
  - o `delivery-gate.sh` (ou o alvo que ele chama) cobre esses testes, senão eles seguem opcionais
  - existe asserção de que todo módulo do repositório está em algum alvo de teste

> Registrado 2026-08-12 pela revisão da fatia `slice-11`. Pré-existente. Depende do `B-087` — o alvo
> agregado que os incluiria é justamente o que aborta.

> **Fechado 2026-08-13** pela fatia `slice-14`. Medido: **9 módulos** no disco, `test:unit` nomeava
> **4 linhas**. Entraram `operators/api`, `cmd/loopzero-ops` e os dois de `tools/` (estes com
> `GOWORK=off`, por estarem fora do `go.work`). `infra/scripts` ficou **isento com razão medida**:
> zero `*_test.go`, e um alvo rodando `go test` sobre módulo sem teste reporta um verde que não
> significa nada.
>
> A terceira bala virou o gate `test-target-coverage`, e ele é o ponto da fatia: os dois itens eram
> a segunda e a terceira manifestação do mesmo descuido — **uma lista escrita à mão nunca comparada
> com o disco**. Consertar as listas sem a comparação deixaria o defeito armado para o quarto módulo.
>
> Provado por mutante: remover `cmd/loopzero-ops` dos dois taskfiles reprova nomeando-o, e trocar
> `go test` por `go build` reprova também — que é **o falso-positivo original**, o mesmo que fez o
> módulo parecer coberto.
>
> **Uma correção ao meu próprio mutante:** a primeira tentativa não falhou, e eu ia concluir que o
> gate era vazio. Não era — eu havia removido a cobertura de só um dos dois arquivos, e o outro
> ainda cobria. O mutante estava errado, não o gate.
>
> Nota sobre a bala do `delivery-gate`: ele chama `quality:gates`, não `quality:all`. Com o gate
> dentro do `quality:gates`, o hook passa a **verificar a cobertura**; executar os testes continua
> sendo do `test:unit`. A diferença está dita porque a bala pede o hook.

> **Medido 2026-08-13 — duas balas fechadas, uma não.** Fechadas: `cmd/loopzero-ops` e os dois binários de
> `tools/` entram em `task test:unit` (`taskfiles/test.yml:120-124`, com `GOWORK=off` para os de fora
> do workspace); e a asserção de que todo módulo está em algum alvo existe e passa — `test-target-coverage`
> devolve *"9 module(s): 8 named by a test target, 1 exempt with a dated reason"*.
>
> **Aberta: a bala 2.** O `delivery-gate.sh` roda `task quality:gates`, e `gates` tem `deps: [build]` e
> mais nada — `quality:hermetic`, onde `cd cmd/loopzero-ops && go test ./...` vive, **não** é chamado. Os
> testes que provam que um gate reprova o que deve reprovar seguem fora do único ponto obrigatório.
> Ninguém os pôs lá porque custam **408 s**, dos quais 392 s são quatro testes herméticos que esperam
> relógio real — ver `B-101`, que é o que destrava esta bala.

> **Fechado 2026-08-13 — a bala 2 caiu.** O `delivery-gate.sh` passou a invocar `task quality:all` em
> vez de `quality:gates`. O alvo não é novo: ele já se declarava *"hermetic tests + full-scan gates +
> contract"* e tem `deps: [hermetic, gates, contract]` — era o alvo certo não sendo chamado.
>
> Custo medido com a máquina ociosa: `gates` 80 s, `hermetic` 93 s, `all` ~175 s. Antes do `B-101`
> seriam ~440 s, e era esse número que mantinha a omissão de pé.
>
> O `tools/` também entrou — ele está no título do item e vivia só em `test:unit`, que o gate não
> chama. Custo somado: ~3 s.
>
> **Preso por asserção comportamental, não textual.** A primeira versão lia o hook como texto e a
> revisão a burlou de três formas, todas reproduzidas contra o arquivo real: `task quality:all --dry`
> (a regex pegava o alvo e ignorava a flag — o portão rodava **nada**), um `if [ -n "$LOOPZERO_FAST_PUSH" ]`
> (semântica de "qualquer um serve" era satisfeita pelo braço lento enquanto o rápido pulava tudo), e
> `command task` com uma isca em heredoc (o padrão ancorado em linha nunca via a invocação real).
> Agora o teste **executa** o hook com um substituto de `task` que grava o argv, e exige: exatamente
> uma invocação (duas são um branch, e é no branch que mora a válvula de escape), sem `--dry`, e com a
> expansão alcançando as três linhas de teste. As três formas reprovam.
>
> **O que ela NÃO pega, dito explicitamente:** o push que a remove. Hooks são lidos do disco, então um
> commit que reverte o hook é vigiado pelo hook **já revertido**, que roda `quality:gates` — e esse
> alvo não roda este teste. A asserção pega a divergência **depois**, não no ato. Achado da revisão.
>
> Decisão T2 em `MEET-ASYNC-AMENDMENT-2026-08-13-delivery-gate-hermetic`.

## B-089 — A mesma regra de env reservada tem uma TERCEIRA implementação, com um terceiro conjunto   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 pela revisão da fatia `slice-11`. Além de `api/internal/services/deploycontracts/build_plan.go:23` (5 prefixos) e `tools/loopzero-dockerfile-lint/main.go:84` (4), existe `api/internal/source/validator_env.go:46` com um **terceiro** conjunto — `DYLD_`, `KUBERNETES_`, `LOOPZERO_` — e seu próprio `isReservedEnvKey` (`:68`), sobre o env do `services.json` do usuário, ligado em `validator_services.go:156`. O resultado é que `DYLD_*` é bloqueado por uma cópia e permitido pelas outras duas, e `AWS_*` o inverso. E das três, a que **não** tem chamador é justamente a que o gate `env-prefix-parity` compara contra a API.
why_now: o gate passou a rodar em 2026-08-12 (`B-070`) e imprime `PASS — 2 declarations`. Duas de três. Um verde que cobre dois terços de uma regra de segurança é lido como cobertura da regra.
status: shipped
severity: alta — três respostas diferentes para "o tenant pode declarar esta variável?", e a decisão de qual vale nunca foi tomada.
dod:
  - está decidido qual conjunto é a regra, e por quê (os três diferem em `DYLD_`, `AWS_`, `DO_`, `GOOGLE_`)
  - o gate compara as três declarações, ou declara explicitamente por que uma fica fora
  - a saída do gate deixa de dizer um número que sugere cobertura total

> Registrado 2026-08-12 pela revisão da fatia `slice-11`, que corrigiu a afirmação `exactly two
> copies today (measured)` do próprio gate. Relacionado a `B-051` e `B-069`.

> **Fechado 2026-08-13** pela fatia `slice-15`, ADR
> `docs/wiki/decisions/2026-08-13-reserved-env-two-rules.md`, ata
> `MEET-ASYNC-AMENDMENT-2026-08-13-002`.
>
> **A premissa deste item estava errada, e quem a escreveu fui eu.** Não são três cópias de uma
> regra — são **duas regras** que compartilhavam nome de símbolo. `validator_env.go:20-25` enumera
> as ameaças dele em prosa: *dynamic linker hijack* (`LD_PRELOAD`, `LD_LIBRARY_PATH`), o equivalente
> macOS (`DYLD_*`), *shell/process discovery hijack* (`PATH`, `HOME`, `USER`, `SHELL`). Nada disso é
> sombreamento da injeção da plataforma, que é o que o `find_sec_004` trata. A interseção `LOOPZERO_` +
> `KUBERNETES_` existe porque as duas ameaças a exigem, por razões distintas.
>
> Eu registrei o item olhando três símbolos homônimos logo depois de medir os outros dois eixos da
> mesma regra — o nome produziu a conclusão errada em quem estava com o contexto fresco.
>
> As três balas: (1) decidido — a pergunta *"qual conjunto é a regra"* pressupõe uma regra só;
> unificar faria o validador parar de bloquear `LD_PRELOAD` **ou** o `build_plan` bloquear `DYLD_`
> num runtime Linux; (2) e (3) o gate declara por que a outra fica fora, nomeando a **ameaça**, e a
> saída deixou de dizer `2 of 3` — que trocava um exagero por outro, sugerindo que a terceira
> deveria entrar.
>
> Nenhum prefixo entrou ou saiu de nenhuma lista. A lacuna que fica aberta é o **B-093**.

## B-090 — A asserção de caixa do gate é evadível por um refactor comum   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 pela revisão da fatia `slice-11`, rodando seis variantes contra `Check` numa árvore temporária — **todas as seis** devolveram `err=nil`: `ToUpper` içado para ACIMA do range e usado dentro dele (a que um refactor normal produz); laço por índice em vez de `range`; `range` sobre uma variável que aliasa o slice; `slices.ContainsFunc`; `import str "strings"`; `bytes.ToUpper`. `casematch.go` só inspeciona `*ast.RangeStmt` cujo `X` é o identificador do símbolo.
why_now: o eixo da caixa acabou de ser decidido (`B-070`) e a asserção que o protege tem uma porta larga. A variante içada restaura exatamente o comportamento que o ADR proíbe, e o gate segue verde.
status: raw
severity: média — o passo vazio (site sem loop) já foi fechado, e o teste comportamental do linter pega todas as seis; mas esse teste não roda (`B-088`), e a cópia vigiada é código morto (`B-070`).
dod:
  - a variante içada é detectada, ou está escrito por que não é
  - a asserção acompanha a expressão comparada, não a forma do laço
  - as seis variantes viram fixtures

> Registrado 2026-08-12 pela revisão da fatia `slice-11`. As seis variantes estão enumeradas no
> doc-comment de `casematch.go` — foram documentadas em vez de implicadas inexistentes.

## B-091 — `infra/quality/dry-run-baseline.txt` é a segunda baseline sem leitor   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-12 na fatia `slice-13`, ao remover a baseline órfã que o `B-045` nomeia. Dos quatro arquivos em `infra/quality/`, `coverage-baseline.json` e `host-ports.txt` têm 3 leitores em código cada; `dry-run-baseline.txt` tem **zero** — nenhuma referência em `*.go`, `*.yml` ou `*.sh`. É a mesma classe do `environment-lock-baseline.txt` removido nesta fatia.
why_now: o `B-045` nomeou uma baseline órfã e ela era duas. Um arquivo em `infra/quality/` é lido como insumo de gate; sem leitor, ele parece contrato e não é.
status: raw
severity: baixa — não quebra nada; é ruído que se disfarça de garantia. Mesmo argumento do `B-045`.
dod:
  - `dry-run-baseline.txt` é removido ou ganha consumidor
  - existe asserção de que todo arquivo em `infra/quality/` tem pelo menos um leitor em código

> Registrado 2026-08-12 pela fatia `slice-13`. Fora da DoD do `B-045`, que nomeia só a baseline de
> environment-lock — registrado em vez de removido junto, para não ampliar escopo em silêncio.

## B-092 — 16 arquivos de teste em `infra/tests` não rodam, e dois subdiretórios estão vermelhos   [ ]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-13 pela revisão da fatia `slice-14`. `taskfiles/test.yml` roda `cd infra/tests && go test ./unit/...`, o que deixa três subárvores fora: `helm/` (27 arquivos de teste, **verde** — ligado nesta fatia), `scripts/` (11, **vermelho**) e `config/` (5, **vermelho**). Os dois vermelhos acusam coisas reais: `TestLoopZeroCloudImageOverlayUsesGHCRCanonical` (`loopzero_cloud_ghcr_canonical_test.go:62`) diz que `infra/helmfile/values/dev-public/loopzero-cloud.yaml.gotmpl` não referencia `ghcr.io/getloopscale/loopzero-cloud` (issue #104), e `TestB31ImageTagsPinned` (`regression_bugs_early_test.go:245`) diz que a tag em `infra/helm/loopzero-api/values.yaml` viola o ADR-0020 D1.
why_now: são 16 testes que ninguém executa e que estão reprovando — a forma exata do `B-088` um nível abaixo do módulo. O gate `test-target-coverage` criado na mesma fatia **não os vê**: a granularidade dele é o módulo, e `infra/tests` conta como coberto por causa do `./unit/...`.
status: raw
severity: alta — dois testes de regressão estão vermelhos há tempo indeterminado, e o que eles acusam (imagem apontando para registro errado, tag fora do contrato) é a classe que o `B-016` já mostrou custar cluster parado.
dod:
  - `scripts/` e `config/` entram num alvo que roda
  - os dois testes vermelhos passam, ou o que eles acusam vira decisão registrada
  - está decidido se o `test-target-coverage` passa a granularidade de pacote, ou se a granularidade de módulo fica declarada como o limite dele

> Registrado 2026-08-13 pela revisão da fatia `slice-14`. `helm/` foi ligado na mesma fatia por estar
> verde; os dois vermelhos ficam de fora porque adicioná-los deixaria `task test:unit` vermelho sem
> consertar o que acusam — e um alvo vermelho vira alvo desligado, que é o defeito de origem.

## B-093 — O validador do `services.json` não bloqueia os prefixos que a plataforma injeta   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 na fatia `slice-15`. `api/internal/source/validator_env.go` bloqueia sequestro de processo (`LD_PRELOAD`, `DYLD_*`, `PATH`, `SHELL`) mais `LOOPZERO_` e `KUBERNETES_`. Ele **não** bloqueia `AWS_`, `DO_` nem `GOOGLE_` — os prefixos que `api/internal/services/deploycontracts/build_plan.go` reserva porque a plataforma os injeta. Um tenant que declare `AWS_ACCESS_KEY_ID` no `services.json` passa pelo validador. Verificado por teste de não-regressão (`TestReservedEnv_ThreatsBlockedAreUnchanged`), que trava o estado atual de propósito para que fechar isto seja uma decisão e não um efeito colateral.
why_now: o ADR `2026-08-13-reserved-env-two-rules` separou as duas regras e, ao fazê-lo, deixou visível que a fronteira de sombreamento tem um lado desguarnecido. Enquanto as duas eram lidas como cópias, a lacuna parecia divergência de conjunto.
status: raw
severity: média — **rebaixada na revisão da mesma fatia.** A metade alcançável está medida: o env do `services.json` chega ao contêiner do workload (`validator_services.go:58` → `services_input_mapping.go:158` → `renderer_deployment.go:77-84`). A outra metade **não se sustenta**: para haver sombreamento, a plataforma teria de injetar `AWS_`/`DO_`/`GOOGLE_` **naquele mesmo contêiner**, e `renderer_deployment.go:51-53` injeta apenas `PORT` mais um `envFrom` do Secret do usuário. O `B-051` já media que não existe injeção de `GOOGLE_*` em env de workload. Vira alta se alguma injeção desse tipo aparecer.
dod:
  - está medido se o env do `services.json` alcança o mesmo processo que recebe a injeção da plataforma
  - se alcançar, os três prefixos entram — com a contagem de quantos payloads de tenant seriam rejeitados hoje
  - se não alcançar, isso está escrito, e o teste de não-regressão passa a citar a razão em vez do estado

> Registrado 2026-08-13 pela fatia `slice-15` (ADR D2). Fechá-lo **rejeita payload que hoje é
> aceito**, então o raio de explosão aponta para fora — mesma forma do eixo do conjunto do `B-051`.
>
> **Correção da revisão:** eu escrevi que *"o dado que decide não é visível deste repositório"*.
> Metade dele é, e a revisão a mediu em três greps — o env do `services.json` alcança o contêiner. O
> que falta é a outra metade, e ela aponta contra o item: não há injeção de `AWS_`/`DO_`/`GOOGLE_`
> naquele contêiner, então não há o que sombrear hoje. Severidade rebaixada.

## B-094 — A metade API do `find_sec_004` guarda um campo que ninguém popula   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 pela revisão da fatia `slice-15`. `api/internal/services/deploycontracts/build_plan.go:120` chama `isReservedEnvKey` dentro de `for k, v := range unit.Env` (`:114`). `unit.Env` vem de `app_unit.go:130` ← `raw.Env` ← `AppSpec.Env` (`shared_contracts.go:32`). **Nenhum site de produção popula esse campo**, e o tipo de fronteira HTTP `routes.AppInput` (`builds.go:53-67`) sequer o declara — verificado também em `adapters/build_adapter.go:39-52`, `api/cmd/autodeploy_adapters.go:131-137`, `loopzero_yaml_apps.go`, `workflow_builder.go` e `languages.go`. O laço nunca itera; a regra não dispara.
why_now: três artefatos afirmavam, no presente, que essa regra **impede** sombreamento — o doc-comment de `validator_env.go`, o ADR `2026-08-13-reserved-env-two-rules` e a saída do gate `env-prefix-parity`. As três foram corrigidas na mesma fatia, mas o código continua: uma regra de segurança inalcançável que parece cobertura, e um gate verde comparando duas cópias dela.
status: raw
severity: média — não há vulnerabilidade aberta (o caminho que ela guardaria também não existe); o custo é a aparência de proteção, que é a classe do `B-055` e do `B-045`.
dod:
  - está decidido se `AppSpec.Env` deve ser populado (e então a regra passa a valer) ou removido (e então a regra sai com ele)
  - se for removido, o gate `env-prefix-parity` e o linter acompanham a decisão
  - a saída do gate deixa de sugerir cobertura de uma regra que não roda

> Registrado 2026-08-13 pela revisão da fatia `slice-15`. É a terceira instância nesta série do padrão
> *declarado + testado + nunca ligado* — as outras duas são `B-055` (helper sem chamador) e `B-045`
> (gates que não rodam).

## B-095 — A segunda cerca sobre CRs `loopzero.io` renderiza em lugar nenhum   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 na fatia `slice-16`. `infra/helm/admission-policies/templates/vap-tenant-namespace.yaml:26-29` casa `loopzero.io` `tenants` **e** `environments` — os mesmos recursos que a `vap-tenant-cr-shape`, portanto também exclusivos do cluster mgmt. Mas `tenantNamespace` é `false` no `values.yaml` base **e** no `values-mgmt.yaml`, então ela não renderiza em cluster nenhum. O comentário do base diz *"Default OFF: toggle on via overlay AFTER T3.3 migration ran successfully"* — e o overlay que ligaria é justamente o do mgmt, onde ela está `false`.
why_now: a fatia `slice-16` mediu a colocação da cerca irmã e, ao fazê-lo, tornou visível que esta não está em lugar nenhum. Enquanto a `tenantCrShape` estava desligada no mgmt, as duas pareciam ter o mesmo estado; agora só uma está resolvida.
status: raw
severity: média — não há regressão nova (ela nunca renderizou), e o comentário indica uma migração como pré-condição. Vira alta se a T3.2/T3.3 do ADR-0018 v2 já tiver rodado, porque então a cerca está pronta e desligada.
dod:
  - está medido se a migração T3.3 que ela aguarda já rodou
  - se rodou, ela é habilitada no overlay do mgmt — e o teste de render passa a cobri-la
  - se não rodou, isso está escrito no `values-mgmt.yaml`, e não só no base

> Registrado 2026-08-13 pela fatia `slice-16` (Q1 do plano). O teste
> `infra/tests/helm/tenant_cr_fence_placement_test.go` já está no molde para cobri-la — basta a
> decisão.

## B-096 — O chart publicado do `loopzero-api` renderiza uma SA que as políticas de admissão não isentam   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 pela revisão da fatia `slice-16`. `charts/loopzero-api/templates/deployment.yaml:35` deriva a SA de `loopzero-common.fullname` (`charts/loopzero-common/templates/_helpers.tpl:170-176`) com `serviceAccount.name: ""` (`charts/loopzero-api/values.yaml:189-191`). `helm template t charts/loopzero-api` rende `t-loopzero-api`; como subchart de `charts/loopzero-platform` sob o release `loopzero-platform`, a regra de colapso (`_helpers.tpl:29-32`) não dispara e a SA vira `loopzero-platform-loopzero-api`. As VAPs isentam `system:serviceaccount:loopzero-system:loopzero-api` — **nenhum desses dois nomes**. O chart de bootstrap (`infra/helm/loopzero-api/templates/deployment.yaml:48`) usa o nome fixo e está correto.
why_now: a fatia `slice-16` ligou a `loopzero-tenant-cr-shape` no mgmt, cuja validação de imutabilidade de `cellID` isenta o `loopzero-api`. Se algum dia o deploy passar pelo chart publicado, `cell_pinner.go:163` é negado — e `:170` registra um Warn **sem reverter o banco**, que é exatamente o split-brain DB/CR que o comentário da isenção existe para evitar.
status: raw
severity: média — **latente**: nenhum caminho de deploy usa o chart publicado para o `loopzero-api` hoje. Vira alta no dia em que usar.
dod:
  - a SA renderizada pelo chart publicado é a mesma que as VAPs isentam, ou as VAPs cobrem as duas formas
  - existe asserção que compara a SA renderizada com a lista de isenção, em vez de as duas serem mantidas à mão
  - está decidido se `cell_pinner.go:170` deve reverter o banco quando o patch do CR é negado

> Registrado 2026-08-13 pela revisão da fatia `slice-16`. Mesma família do `B-046`: os dois charts
> divergem numa propriedade que só aparece no render.

## B-097 — O CLI documenta um `kubectl patch` que a nova cerca nega   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 pela revisão da fatia `slice-16`. `api/internal/services/placement/cell_pinner.go:172` emite, como remédio manual, `kubectl -n loopzero-system patch tenant <id> --type=merge -p '{"spec":{"cellID":...}}'`. A validação de imutabilidade da `loopzero-tenant-cr-shape` (`vap-tenant-cr-shape.yaml:58-61`) nega exatamente isso, exceto para `system:masters` ou a SA do `loopzero-api`.
why_now: a cerca foi habilitada no mgmt em 2026-08-13. O comportamento é provavelmente o desejado — a mensagem da própria política diz *"Use 'loopzero admin tenants pin' to move a team"* —, mas o código emite um remédio que a política bloqueia, e quem o seguir com um kubeconfig sem privilégio recebe um `Denied` sem entender por quê.
status: raw
severity: baixa — quem opera o mgmt normalmente tem kubeconfig admin (`system:masters`), que é isento; o custo é uma mensagem que engana.
dod:
  - a mensagem de `cell_pinner.go:172` diz que o patch direto é negado pela política, ou deixa de oferecê-lo
  - está decidido se o remédio manual continua existindo

> Registrado 2026-08-13 pela revisão da fatia `slice-16`.

## B-098 — Duas policies do OpenBao são declaradas e nunca ligadas a role nenhum   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 na fatia `slice-17`. `infra/helm/openbao-init/values.yaml` declara quatro entradas com nome parecido: `loopzero-api-read` (:126), `loopzero-api-transit-encrypt` (:131), `loopzero-api` (:188 — a policy) e `loopzero-api` (:328 — um **role** no mount `k8s-core`). Só a terceira é referenciada: o role em `:334-335` lista `policies: [loopzero-api]`. **nenhum role neste arquivo as liga** — treze roles declaram `policies:`, e o único que nomeia uma policy `loopzero-api-*` é o de k8s-core, que liga `loopzero-api`. (A redação anterior dizia que um `grep` repo-wide devolvia zero; **falso** — `docs/operations/vault-cluster-local-runbook.md:202` liga `loopzero-api-read` a um role, e há outras menções em runbooks e auditorias. A afirmação certa é sobre este arquivo, e é ela que o item precisa.) Duas policies existem, são aplicadas pelo `helmfile sync`, e nenhuma identidade as carrega.
why_now: ao corrigir o `B-072` eu apliquei o patch na policy errada — `loopzero-api-transit-encrypt`, porque o padrão casou primeiro. Só o teste novo pegou. Quatro nomes parecidos, dois deles inertes, e um que é um role: quem for editar a policy do `api` tem quatro candidatos e três estão errados.
status: raw
severity: média — não há risco de segurança (uma policy sem role não concede nada a ninguém); o custo é o mesmo do `B-055` e do `B-045`, dívida que parece cobertura, e neste caso já custou um patch no lugar errado.
dod:
  - está decidido se as duas são removidas ou ligadas a alguma identidade
  - se removidas, o `loopzero-api-read` e o `loopzero-api-transit-encrypt` saem do `values.yaml`
  - existe asserção de que toda policy declarada é referenciada por ao menos um role

> Registrado 2026-08-13 pela fatia `slice-17`. Quarta instância nesta série do padrão *declarado e
> nunca ligado* — as outras: `B-055` (helper sem chamador), `B-045` (gates que não rodam), `B-094`
> (regra que percorre campo não populado).

## B-099 — A policy corrigida não alcança a identidade que cria a chave, e o bootstrap a sobrescreve   [x]

domain: engine-go
repo: loopzero
suggested_mode: bug
source: discover-review
evidence: medido em 2026-08-13 pela revisão da fatia `slice-17`. **(a)** `TransitClient` autentica exclusivamente por AppRole — `pkg/vault/transit_client.go:584` é o único login do arquivo (`POST /v1/auth/approle/login`), e `api/cmd/setup_transit.go:47-52` é o único ponto de construção, passando `RoleID`/`SecretID`. O chart `infra/helm/openbao-init` **não declara AppRole**: a policy `loopzero-api` está ligada só ao role de mount `k8s-core` (`values.yaml:329-336`), cujo cliente `pkg/vault.NewClient` tem **zero** chamadores de produção. **(b)** `infra/scripts/local/vault-bootstrap-dev.sh` aplica o chart no passo 5 (`helmfile sync --selector name=openbao-init`) e, nos passos 6-7 (`:429-445`), faz `bao policy write loopzero-api` com `transit/keys/*` incluindo `delete` e `list`, e liga o AppRole a `token_policies=loopzero-api`. Mesmo nome, escrita incondicional, último escritor vence — e o script é o passo 1 de `seed-post-cleanroom-all.sh:151`. **(c)** O runbook (`openbao-transit-cutover-runbook.md:144`) liga o AppRole a `loopzero-api-approle`, um **terceiro nome** que o chart não declara.
why_now: a fatia `slice-17` corrigiu a policy do chart e mediu que isso **não tem efeito em runtime** por (a) e (b). Sem este item, o `B-072` pareceria fechado — a policy versionada está certa, e nada a carrega.
status: shipped
severity: alta — é o mesmo defeito do `B-072` (criação de chave de tenant negada), com a diferença de que agora a correção existe e não alcança. Uma correção que não alcança é pior que a ausência dela, porque parece cobertura.
dod:
  - está decidido onde o AppRole do `loopzero-api` é declarado — no chart, ou fora dele com o chart ciente
  - o `vault-bootstrap-dev.sh` para de sobrescrever a policy que o chart aplica, ou o chart para de declará-la
  - os três nomes (`loopzero-api`, `loopzero-api-approle`, e a policy do script) são reconciliados num só
  - `transit/keys/loopzero-tenants-*` é estreitado para não alcançar `/config` e `/rotate` — escrever `min_decryption_version` em `/config` inutiliza permanentemente ciphertext existente do tenant — **com verificação LIVE da semântica de `+` em segmento parcial**
  - existe asserção de que a policy que a identidade do cliente carrega é a que tem a concessão

> Registrado 2026-08-13 pela revisão da fatia `slice-17`. Quinta instância nesta série do padrão
> *declarado e nunca ligado*, e a primeira em que **eu** a produzi: a policy corrigida existe, está
> versionada, e nenhuma identidade a carrega.

> **Fechado 2026-08-13.** Decisão em `MEET-ASYNC-AMENDMENT-2026-08-13-vault-policy-ownership`: **a
> policy é do chart; o AppRole é do script.** O que a identidade PODE fazer é declarativo e versionado;
> o ciclo de vida da credencial só roda contra cofre vivo.
>
> `vault-bootstrap-dev.sh` parou de escrever a policy e manteve o binding do AppRole. O runbook parou
> de escrevê-la e o terceiro nome (`loopzero-api-approle`) virou `loopzero-api`. A baseline de 2026-06-10 que
> também o cita **fica intocada** — é trilha de auditoria, e reescrevê-la para parecer atual destruiria
> a evidência do que era verdade então.
>
> **É estreitamento, não risco, e isso foi medido antes de remover.** O `TransitClient` monta
> exatamente três caminhos — `encrypt`, `keys`, `decrypt`, todos sob `loopzero-tenants-<id>` — mais o login.
> Nenhum LIST, nenhum DELETE, nenhum KV. Sai da policy efetiva: `delete` em `transit/keys/*` (o verbo de
> crypto-shredding, concedido a quem pelo `B-075` não tem caminho de shredding nenhum), `list`, o `*`
> que alcançava chave que não é de tenant, e duas concessões KV mortas dos dois lados.
>
> Preso por `TestOpenBaoInitAuth_NoScriptWritesAPolicyTheChartOwns`, que reprova se qualquer shell sob
> `infra/` voltar a escrever policy. Mutante verificado.

## B-100 — O gate de contrato trabalha por nome de tag nos dois lados, e chama campo homônimo de consumidor   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13, quando o gate bloqueou o push da fatia 19. Dois limites, ambos por trabalhar com **nome** em vez de referência. (1) `cmd/loopzero-ops/internal/quality/contractconsumers/check.go:212` — `tagExistsAt` pergunta se a tag existe em **qualquer** struct do conjunto rastreado. `api/internal/routes/repositories/repositories.go:105,117` declara `json:"repo"` duas vezes, então remover de uma é invisível. Coberto pelo teste `TestCollectDroppedJSONTags_ADuplicateTagMasksTheDrop`, que documenta o limite e diz para atualizá-lo — não apagá-lo — quando fechar. (2) `check.go:242` — `grepConsumersForTag` casa `\.<tag>\b`, o que pega **campo de struct homônimo** e **literal de string**: na ocorrência real, dos 9 "consumidores" de `repo`, 8 eram `p.repo.RepoURL(...)` (`p.repo` é `*gitclient.GiteaRepoManager` — `api/internal/gitops/gitea_pusher.go:45`) e 1 era `attribute.String("loopzero.github.repo", ...)`. Zero liam a tag.
why_now: é o **mesmo defeito** que o review da fatia 19 achou na minha tabela de acoplamento — contar ocorrência de nome como referência — e nesta sessão ele apareceu de forma independente em dois lugares. Aqui ele custa: o gate bloqueou um push que não mudava contrato nenhum, e um gate que erra assim é um gate que as pessoas aprendem a contornar.
status: triaged
severity: média — não deixa contrato quebrado passar (o caminho de detecção continua fechado para a última declaração sair); gera ruído, que é como um gate perde autoridade
dod:
  - a existência da tag é verificada na struct de onde ela saiu, não em qualquer uma do conjunto — e `TestCollectDroppedJSONTags_ADuplicateTagMasksTheDrop` é reescrito para exigir o contrário do que exige hoje
  - um campo Go homônimo (`p.repo`) e um literal de string (`"loopzero.github.repo"`) deixam de ser reportados como consumidores, provado com estes dois casos exatos
  - a detecção de consumidor em TypeScript continua funcionando — ali `.repo` é de fato o acesso à propriedade, e estreitar demais mataria o lado que motivou o gate

> Registrado 2026-08-13, pela fatia 19: o gate reprovou o push porque `gofmt -w` reduziu o espaçamento antes de um comentário de fim de linha, e uma linha *modificada* contendo a tag era lida como tag *removida*. Essa parte foi corrigida na mesma fatia; o que fica aqui é a granularidade de nome, dos dois lados.


## B-101 — Quatro testes herméticos de seed queimam 390s de relógio, e é isso que mantém a suíte fora do push   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13. `cd cmd/loopzero-ops && go test ./... -short -count=1` leva **408 s**, dos quais **392 s** são de `internal/seed` — e o `-short` não muda nada, porque nenhum teste do pacote o consulta (`grep -c 'testing.Short()' cmd/loopzero-ops/internal/seed/*.go` → 0). Quatro testes respondem por 390 s: `TestLokiCrossCluster_URLDerivaDoGatewayNaoDoContainer` (120 s), `TestLokiCrossClusterSeed_Run_PatchesPromtailAndSetsLokiURL` (120 s), `TestPatchKratosSeed_Run_PatchesAndRolloutsBoth` (90 s) e `TestNetworkingCorednsSeed_HappyPath` (60 s). Nenhum toca cluster — usam `fake.NewClientBuilder()`. Eles pagam o prazo inteiro de `waitForRolloutSettled` (`cmd/loopzero-ops/internal/seed/vault_cross_cluster_addr.go:223`), cujo valor está **cravado na chamada**: `120*time.Second` em `broker.go:109`, `loki_cross_cluster.go:125` e `vault_role_id_sync.go:81`, `60*time.Second` em `networking_cross_cluster_coredns.go:84`. Com cliente falso o Deployment nunca assenta, então o laço espera até o fim.
why_now: é o obstáculo concreto da bala 2 do DoD do `B-088` — o `delivery-gate.sh` roda `task quality:gates`, que **não** chama `quality:hermetic`, que é onde `cd cmd/loopzero-ops && go test ./...` vive. Medido: `gates` tem `deps: [build]` e nada mais. Ou seja, os testes que provam que um gate reprova o que deve reprovar não estão no caminho de push — e ninguém os colocou lá porque custariam ~7 min por push. Some o custo e a bala fica trivial.
status: triaged
severity: média — não há defeito de produção; o custo é que a rede de proteção dos gates fica fora do único ponto que é obrigatório
dod:
  - o prazo de espera de rollout é injetável pela raiz de composição, como `Policy` já é em `Deps` (`seed.go:101`), com o zero preservando o valor de hoje
  - os quatro testes passam a injetar um prazo curto, e `cd cmd/loopzero-ops && go test ./... -short` cai para a ordem de segundos
  - o valor cravado deixa de aparecer no ponto de chamada de cada seed

> Registrado 2026-08-13, ao medir o `B-088` para fechá-lo. `.claude/rules/testing.md § 6` nomeia isto: *"Time/randomness in unit tests — inject a clock/RNG so the test is deterministic"*.


## B-102 — `task --dry` imprime bloco multi-linha sem avaliar o controle de fluxo, e é nisso que uma asserção de segurança se apoia   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 pela revisão do `B-088`. `task --dry` expande deps transitivas com fidelidade — `grep -nE 'preconditions:|internal: true|run: once|status:|sources:|generates:|platforms:' taskfiles/quality.yml Taskfile.yml` devolve **nada**, então nenhum passo é pulado por condição e o `--dry` nunca sub-reporta. Mas ele **super-reporta**: um `cmds:` em bloco (`- |`) é impresso verbatim, sem avaliar o `if`/`exit` dentro dele. Prova viva já na árvore — `taskfiles/quality.yml:298-301` aborta com `exit 1` quando falta o `helmfile` ou quando `LOOPZERO_TEST_SKIP_HELMFILE` está setado, e mesmo assim o `task --dry quality:all` imprime `cd infra/tests && go test ./charts/ -count=1` incondicionalmente.
why_now: a asserção que fecha a bala 2 do `B-088` (`TestDeliveryGate_RunsOneRealTargetThatCoversThisModule`) confere a **cobertura** lendo a saída do `--dry`. Um alvo futuro com a forma `- |` / `[ -n "$SKIP" ] && exit 0` / `cd cmd/loopzero-ops && go test ./...` satisfaz a asserção e não roda nada — a mesma classe do bypass `--dry` que a revisão construiu, só que alcançada **sem ninguém querer burlar**. A metade comportamental do teste (o substituto de `task` que grava o argv) já cobre o que o hook executa; o que falta é a metade que confere o que o alvo executa.
status: triaged
severity: média — hoje nenhum alvo do caminho de push tem essa forma, e a metade comportamental fecha o caso deliberado; o buraco é para o acidente futuro
dod:
  - a cobertura é conferida por execução observada, não por leitura do `--dry` — por exemplo rodando o alvo com um substituto de `go` que grava os argv, como o teste já faz com o `task`
  - existe caso que prova o buraco: um alvo de teste com `- |` e saída antecipada satisfaz a asserção de hoje e reprova na nova
  - a regra vale para qualquer asserção futura que leia `--dry` como prova de execução

> Registrado 2026-08-13 pela revisão do `B-088`. É a mesma lição da fatia 19 e da 20, numa terceira
> roupa: ler o texto de um comando não é observar o comando. As duas anteriores foram `grep -lw` como
> medida de acoplamento e regex como guarda de literal; esta é `--dry` como prova de execução.

## B-103 — 43 itens estão `raw` carregando evidência medida, e o loop os trata como não medidos   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-13 por `check_backlog_structure.py`, que já tinha o achado `raw_with_evidence` e nunca fora rodado contra o registro inteiro: **43** de 102 itens declaram `status: raw` com o campo `evidence` preenchido. São eles: B-008, B-013, B-014, B-015, B-016, B-017, B-018, B-019, B-021, B-022, B-024, B-025, B-027, B-028, B-029, B-030, B-031, B-032, B-033, B-034, B-035, B-036, B-037, B-038, B-039, B-076, B-078, B-079, B-081, B-082, B-083, B-085, B-086, B-090, B-091, B-092, B-093, B-094, B-095, B-096, B-097, B-098, B-099. O verificador descreve o defeito na própria mensagem — *"measurement happened and the status was never advanced"*.
why_now: `cycle-maintenance § SELECT` ordena `triaged` antes de `raw`, porque medido vence não-medido. Com 43 itens medidos declarando-se não-medidos, essa ordenação escolhe errado de forma sistemática — e a regra de desempate seguinte é idade, então os mais antigos entre eles ficam eternamente atrás. É a mesma família do `closure_note_without_shipped` fechado nesta mesma fatia, um passo antes no ciclo de vida: lá a prosa dizia fechado e o campo dizia aberto; aqui o campo `evidence` diz medido e o campo `status` diz que não.
status: triaged
severity: média — nada quebra; o custo é o loop de manutenção priorizar mal, de forma invisível
dod:
  - cada um dos 43 é verificado individualmente e recebe o status que sua evidência sustenta — `triaged` se a medição vale, `raw` com `evidence: none-yet` se o campo foi preenchido por engano
  - fica escrito por que a intake por `--sweep` nasce com evidência e mesmo assim é registrada `raw`, ou o `/backlog-item` passa a registrá-la `triaged`
  - `check_backlog_structure.py` sai sem nenhum `raw_with_evidence` contra o registro real

> Registrado 2026-08-13 pela fatia que fechou o `closure_note_without_shipped`. **Deliberadamente não
> corrigido em lote:** virar 43 status sem abrir cada item é exatamente o atalho que produziu a deriva.
> Os seis que corrigi nesta fatia foram verificados um a um por execução — inclusive um (`B-070`) em
> que minha primeira sonda deu o resultado errado e a nota estava certa.

## B-104 — O webhook do GitHub se desliga sozinho em silêncio, e `optional: true` é o que permite   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-live-test
evidence: medido no droplet em 2026-08-14, ao investigar o `B-021`. O `loopzero-api` sobe normalmente com os **dois** secrets do webhook ausentes — `loopzero-github-webhook` e `loopzero-api-github` —, porque as duas referências são `optional: true`: a variável (`GITHUB_WEBHOOK_SECRET` via `secretKeyRef … optional: true`) e o `envFrom` do `loopzero-api-github`. Sem o segredo, `api/cmd/setup_github.go:152-158` deixa o `WebhookHandler` nil e o roteador não registra a rota: `POST /api/v1/webhooks/github` devolve **404** enquanto `POST /api/v1/health` devolve **405**. Nenhum log de erro, nenhuma condição, nenhuma métrica — o único vestígio é uma linha `Info` no arranque.
why_now: é uma superfície de segurança que se desliga sozinha, e a consequência é que o `git push` de um cliente não dispara build nenhum. O `B-021` trata da credencial ausente, que só o patrocinador provê; **este trata da invisibilidade**, que é defeito nosso e independente dela. Enquanto o segredo existiu, isto era invisível; a reconstrução total o apagou e o modo de falha ficou exposto — mas quem o expôs foi uma sondagem manual, não o sistema.
status: triaged
severity: alta — não pelo estado atual (o ambiente é de desenvolvimento), mas porque o mesmo desenho em produção significa auto-deploy morto sem ninguém saber
dod:
  - a ausência do segredo do webhook deixa de ser silenciosa: ou o arranque falha alto, ou existe sinal observável (condição, métrica ou readiness) que diga que a superfície está desligada
  - está decidido e escrito por que cada `optional: true` do `loopzero-api` é opcional — hoje são dez `envFrom` e nenhuma decisão registrada distingue os que podem faltar dos que não podem
  - existe verificação de que a rota do webhook está registrada quando o segredo existe, e que a ausência dele é detectável sem ler log

> Registrado 2026-08-14 pela medição do `B-021` no droplet. A credencial é do patrocinador; a
> invisibilidade é nossa.

## B-105 — O `application_controller` ancora a base depois de um re-fetch, e pula o sumário em retorno antecipado   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-14 pelo review da fatia `slice-25`. Dois defeitos independentes no mesmo trecho de `operators/internal/app/application_controller.go`. **(1)** `:151-163` compõe o status, faz `r.Get` de re-fetch em `:154`, e só então tira `client.MergeFrom(app.DeepCopy())` em `:158` — a base é o estado **fresco** do servidor enquanto a carga é o composto **anterior** ao re-fetch, então o diff **reverte** qualquer campo de status que outro escritor tenha mudado entre `:89` e `:154`. Não é a dança que o contrato proíbe pela letra (aquela nomeia `Status().Update`, esta é `Patch`), mas é o mesmo dano com outro nome. A forma prescrita pelo ADR — base logo após o `Get` inicial — não tem essa propriedade. **(2)** o sumário de prontidão é computado inline em `:162`, e **quatro** caminhos de erro retornam antes disso (`:121`, `:125`, `:136`, `:155`, mais `reconcileDelete` em `:117`). O `ADR-0025 D3`, citado em `reconcilehelpers/readiness.go:8-11`, exige que o sumário seja a última mutação de status **mesmo em retorno antecipado** — que é por que o helper é um `defer`.
why_now: eu declarei essa forma como "desenho melhor" ao isentá-la da asserção da fatia 25, e estava errado nos dois pontos — o review mediu. A tabela mecânica do `B-085` a pontuou como conforme porque conta a **chamada** (`Patch` e não `Update`), não a **âncora**. Enquanto isso, em todo caminho de erro a condição `Ready` fica velha, que é exatamente quando alguém a lê.
severity: média — o raio prático é pequeno porque `LoopZeroApplication.status` deveria ter um escritor só; o custo é que o invariante vale por contagem de chamada e não por forma, e uma condição de prontidão velha é lida como verdadeira
dod:
  - a base é tirada logo após o `Get` inicial, sem re-fetch entre a composição e o patch — ou está escrito por que o re-fetch é necessário aqui e o que ele protege
  - o sumário de prontidão passa a ser a última mutação de status também nos cinco caminhos de retorno antecipado
  - a asserção do `B-085` deixa de pontuar por chamada e passa a pontuar por âncora, ou fica escrito que ela não distingue as duas coisas

> Registrado 2026-08-14 pelo review da fatia 25, corrigindo uma isenção que eu tinha justificado errado.

> **Fechado em 2026-08-14. Três balas, e uma delas respondida ao contrário do que pedia.**
>
> **Bala 1 — fechada em `673c820c`.** A base passou a ser tirada logo após `reconcileHostname`, sem
> re-fetch entre a composição e o patch; a dança `computedStatus` saiu. Decisão em
> `MEET-ASYNC-AMENDMENT-2026-08-14-application-status-anchor`, que também registra a arqueologia:
> o comentário justificava o re-fetch por UID, e o `git log -S` mostrou que a razão original era
> `resourceVersion` sob `Update` — vestigial desde `a07f0531`.
>
> **Bala 2 — fechada, e o item exagerava.** Ela fala em **cinco** caminhos; medido, são **quatro
> não-defeitos e um defeito**. `:117` (`reconcileDelete`) não muta `.Status`, e o helper canônico pula
> objeto em deleção; `:121` e `:125` retornam antes de `base` existir, sem mutação de status na
> passagem — escrever um sumário ali exigiria inventar uma base, que é a armadilha registrada na
> decisão da âncora; `:155` **deixou de existir** quando a bala 1 removeu o re-fetch. O defeito é
> `:136` (hoje `:152`): `reconcileExposedRoute` marca `IngressReady=False` e **persiste** antes de
> devolver o erro, então o servidor fica com um `IngressReady=False` fresco ao lado de um `Ready` da
> geração anterior.
>
> **A primeira redação desta nota trocou dois desses caminhos em silêncio** — largou o `:117` que o
> item nomeia e pôs no lugar `:91`/`:93`, que ele não nomeia — mantendo a contagem "cinco" intacta.
> Manter o número enquanto se troca o conjunto é a forma mais discreta de não medir o que se afirmou
> medir. A revisão adversarial da fatia pegou, e é por isso que ela existe.
>
> O conserto encapsula os **dois** pontos de persistência num `patchStatus`, que dobra o sumário
> dentro — e o `observedGeneration` foi para dentro dele junto, porque no ramo de falha ele ficava na
> geração anterior enquanto o `Ready` era carimbado na atual: uma escrita de status discordando de si
> mesma. Decisão em
> `MEET-ASYNC-AMENDMENT-2026-08-14-application-status-persist-encapsulation`, que registra por que
> `DeferReadinessSummary` foi rejeitado — ele é um **segundo escritor por desenho**, e este controller
> teve um removido no T5.1 por flapping e 409.
>
> **Bala 3 — respondida pelos dois lados.** A asserção do `B-085` continua contando **chamada**
> (`Patch` vs `Update`) e **não distingue âncora** — isso fica dito, porque é verdade e nenhuma
> asserção barata a torna anchor-aware para o pacote inteiro. Mas para este arquivo o encapsulamento
> permitiu uma asserção **estrutural**, por AST:
> `TestApplicationController_StatusIsPersistedFromExactlyOnePlace` conta os sítios de persistência e
> exige que haja **um**, dentro de `patchStatus`. Não vê a âncora; vê quantos lugares estão em posição
> de violá-la, e mantém esse número em um.
>
> **E o guard estrutural precisou ser reescrito, porque a primeira versão era burlável.** Ela parseava
> **um** arquivo e exigia que o receptor fosse literalmente `x.Status()`. A revisão matou-a com três
> mutantes de duas linhas, todos compilando e deixando o pacote verde: `sw := r.Status()` seguido de
> `sw.Patch(...)`, `r.SubResource("status").Patch(...)`, e a mesma escrita num arquivo irmão. Eu havia
> afirmado, no ADR e na mensagem de commit, que "não existe caminho que persista status sem resumir" —
> falso, e falso de um jeito que ninguém notaria até o defeito voltar.
>
> A forma nova varre todos os arquivos de produção do pacote, resolve apelido local, trata
> `SubResource("status")` como sinônimo, e afirma sobre a **função que envolve** cada persistência:
> ela precisa dobrar o sumário em linha ou estar coberta por um `defer DeferReadinessSummary`. Os três
> mutantes reprovam, e a segunda forma segue permitida — é a que os outros sete controllers usam, e
> contar sítios a teria proibido.
>
> Isso expôs um sítio que ninguém guardava: `application_reaper.go:166` persiste `Phase=Cleaned` sem
> resumir. Não é bug vivo — `MarkAsCleaned` não tem chamador de produção — mas era a prova de que a
> classe seguia aberta. Fechado junto, para o invariante não precisar de uma isenção que ninguém
> revisitaria.
>
> Todos os guards são comportamentais ou estruturais, nunca textuais, e cada asserção foi verificada
> por mutante — incluindo os três que a revisão usou para matar a versão anterior.
status: shipped

## B-106 — 215 dos 582 caminhos do inventário de testes não existem, e o gate que o lê nunca verifica   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-14 pelo review da fatia `slice-27`. `infra/quality/test-inventory.json` declara **582** caminhos de arquivo de teste; **215** deles (**37%**) não existem no disco. A causa não é única: repositórios extraídos (`cloud/…` foi para `loopzero-cloud` no M4), testes movidos por fatias de decomposição, e arquivos simplesmente apagados (`operators/internal/conditions/retry_test.go` saiu com o helper proibido na fatia 10). O inventário é **config viva**, lida por `cmd/loopzero-ops/quality_gates_testgov.go:158` — não é trilha de auditoria —, e o gate passa porque **nunca valida existência de caminho**.
why_now: o review achou isto porque a fatia 19 moveu `repositories_test.go` e não atualizou o inventário; a 27 repetiu com `domains_test.go`. Os dois foram corrigidos nessa fatia, e a medição do resto revelou que eles eram 2 de 217. Um inventário em que 37% dos caminhos são fantasmas não descreve mais a suíte — ele descreve o que a suíte era em algum momento, e nenhum gate percebe a diferença.
status: triaged
severity: média — nada quebra hoje; o custo é um gate de governança de testes que mede uma lista que não corresponde à realidade, e que por isso não pode acusar cobertura perdida
dod:
  - o gate que lê o inventário passa a reprovar caminho inexistente, ou o inventário deixa de listar caminho e passa a ser derivado da árvore
  - os 215 são triados por causa — extraído para repo irmão, movido, apagado — e cada grupo tem destino decidido
  - existe verificação de que mover um arquivo de teste sem atualizar o inventário reprova, senão a deriva recomeça na próxima fatia

> Registrado 2026-08-14 pelo review da fatia 27. Os 2 caminhos de `routes/` foram corrigidos naquela
> fatia; os outros 215 são anteriores a ela e não são dela.

## B-107 — O checkbox do título e o campo `status:` são o mesmo fato, e discordavam em 23 itens   [x]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-14 ao fechar o `B-105`. Cada item carrega o estado em **dois** lugares — o `[ ]`/`[x]` do título e o campo `status:` — e nada os mantinha de acordo. Cruzando os dois sobre 106 itens: `[x]` com `status: raw` em **10**, `[ ]` com `status: shipped` em **13**. Vinte e três discordâncias, e a direção perigosa é a primeira: o box afirma entregue para trabalho que o registro diz que ninguém mediu. O box é o que se lê ao passar os olhos; o campo é o que `cycle-maintenance § SELECT` lê para escolher o próximo item.
why_now: apareceu ao marcar o `B-105` como `shipped` e notar que treze irmãos já `shipped` continuavam com `[ ]`. Não é cosmético: as duas representações são lidas por públicos diferentes — humano e laço — e quem olha a errada é enganado sem sinal nenhum.
status: shipped
severity: média — nenhuma trava, mas um registro cujas duas metades discordam em 22% dos itens deixa de ser fonte de verdade, que é a única coisa que ele é
dod:
  - o checador falha quando as duas representações discordam, nas duas direções, com severidade proporcional
  - as 23 divergências existentes são resolvidas pela evidência de cada item, não por regra única
  - `killed` fica de fora enquanto não houver convenção decidida, e isso está escrito

> Registrado e fechado 2026-08-14, na mesma fatia que o `B-105`.

> **Fechado 2026-08-14.**
>
> **Bala 1.** `check_backlog_structure.py` ganhou `checkbox_status_mismatch`, determinístico, com
> severidade que segue a direção do erro: `[x]` sobre status aberto é **major** (afirma entregue o que
> não foi), `[ ]` sobre `shipped` é **minor** (subestima). Quatro testes o fixam, incluindo o caso que
> deve **calar** — as duas direções concordando.
>
> **Bala 2.** As 23 foram resolvidas **por evidência de cada item**, não por regra única, porque a
> regra única estaria errada nos dois sentidos. Dos 10 com `[x]` sobre `raw`, **cinco** carregavam nota
> de fechamento — nesses o campo é que estava velho, e virou `shipped`; os outros **cinco** não
> carregavam, e ali o box é que mentia. Os 13 `shipped` com `[ ]` ganharam o `[x]`.
>
> **Bala 3.** `killed` não é julgado. É fechado sem ser entregue, e o registro não tem convenção
> decidida para qual box isso é; inventar uma aqui fabricaria achado a partir de pergunta não
> respondida. Um teste garante que a regra permanece calada nos dois boxes para `killed`.
>
> **E o achado colateral vale mais que o item.** Ao cruzar as duas representações apareceu que a regra
> `closure_note_without_shipped` — que eu mesmo escrevi nesta sessão — só conhecia `Fechado`/`Closed` e
> **não conhecia `RESOLVIDO`**, que é o marcador que este registro de fato usa. Ela perdia **cinco de
> seis** casos reais e relatava limpo porque olhava para o lado. Ampliada, achou os seis — e dois deles
> são **falsos positivos**: no `B-016` o `RESOLVIDO` fecha um argumento que o autor estava corrigindo,
> e no `B-012` fecha um contorno aplicado ao host enquanto o conserto durável seguia aberto. Uma
> expressão regular não sabe qual substantivo uma frase de fechamento fecha. A regra foi reclassificada
> de `deterministic/major` para `heuristic/minor`, com a taxa medida no comentário e um teste que fixa
> a classificação — declarar palpite com confiança de medição é exatamente o defeito que este checador
> existe para pegar, e ele o estava cometendo em si mesmo.

## B-108 — Cinco seeds gravam Secret num namespace que podem não encontrar   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: medido em 2026-08-14 ao consertar o `B-021`. Sete arquivos de `cmd/loopzero-ops/internal/seed/` gravam Secret via `CreateOrUpdateSecret`, e antes desta medição **um** garantia o namespace antes (`broker.go`). O `github_app_dev.go` foi consertado porque a falha dele está medida — numa sala limpa ele rodou antes de `loopzero-system` existir, falhou no pré-requisito, e o efeito só apareceu como `404` no log de entrega do GitHub. Restam **cinco** com a mesma forma: `argocd_gitea_creds.go`, `argocd_token.go`, `gitea_token.go`, `stripe_api_keys.go`, `stripe_webhook_secret.go`.
why_now: a falha do `B-021` não foi de código, foi de **ordem** — o seed pressupôs um namespace que a cadeia ainda não tinha criado. A pressuposição é a mesma nos cinco; o que não se sabe é se a posição de cada um na cadeia já a garante. Consertar os cinco sem medir seria adivinhar, e adivinhar é o que produziu o item que os revelou.
status: raw
severity: média — nenhum está medido falhando hoje, mas o modo de falha é silencioso por construção: o seed falha, a cadeia segue, e o efeito aparece num sistema de terceiros
dod:
  - para cada um dos cinco, está medido se o namespace é garantido pela posição na cadeia ou não
  - os que não são garantidos passam a garanti-lo, e entram no contrato de `namespace_precondition_test.go`
  - os que são garantidos ficam com a razão escrita, para que a próxima pessoa não re-pergunte

> Registrado 2026-08-14, ao consertar o `B-021` — a auditoria adversarial de fechamento achou a instância, a medição achou a classe.

## B-109 — Guarda nova é medida contra mutante sintético e nunca contra a população que já existe   [ ]

domain: engine-go
repo: loopzero
suggested_mode: review
source: discover-review
evidence: |
  medido em 2026-08-14. A auditoria adversarial de fechamento nomeou esta falha **sobre si mesma**: ela provou que `TestTaskfilesDoNotCdIntoMissingDirs` era cega a uma forma injetando a forma, e nunca perguntou *quantas instâncias dessa forma já existem*. Havia **uma, viva, no HEAD** (`taskfiles/security.yml:40`), e a task estava falhando desde o M9.

  A mesma lacuna apareceu do meu lado, no mesmo dia: `TestApp_EveryStatusPersistFoldsTheReadinessSummary` afirma o invariante do sumário de prontidão e roda **num pacote**. A população são cinco. Varrida, deu **21 persistências: 19 cobertas por `defer` no pacote, 2 em linha, 0 descobertas** — o invariante valia, mas ninguém sabia, e nada impedia que uma linha removida descobrisse quatro de uma vez.

  Das seis guardas criadas nesta sessão, **cinco** tiveram varredura de população (checkbox 23 achados, marcador de fechamento 6, seeds 7→1, padrão de pacote 1 real, `Status().Update` 83 arquivos) e **uma** não. A que não teve é a que só existia como teste de pacote.
why_now: mutante sintético prova que a guarda **enxerga** a forma; varredura prova quantas vezes a forma **já aconteceu**. As duas perguntas são diferentes, e a segunda é a que encontra defeito hoje em vez de prevenir defeito amanhã. Esta sessão gastou três correções em cima de itens que mediram o mecanismo e não a população.
status: raw
severity: média — nenhuma guarda está errada; o que falta é o passo que transforma cada uma de prevenção em achado
dod:
  - a regra "toda guarda nova roda contra a população real antes de ser declarada pronta" está escrita onde as guardas são escritas
  - para cada guarda de âmbito de pacote existente nos operators, está medido se o invariante vale nos outros pacotes ou se ela só cobre o seu
  - o resultado da varredura fica registrado junto da guarda, mesmo quando é zero — zero medido e zero suposto são fatos diferentes

> Registrado 2026-08-14. A lição é da auditoria, que a aplicou a si mesma antes de eu aplicá-la a mim.

