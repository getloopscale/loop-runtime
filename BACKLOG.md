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
  - `git grep -n "loopzero-mgmt\|mgmt-cluster\|loopzero-workload\|workload-cluster\|loopzero-runtime\|runtime-cluster"` retorna zero fora de `CHANGELOG.md`, ADRs e documentos históricos — que **preservam** os nomes antigos, porque descrevem o passado
  - `loopzero-build` intocado: 657 hits/253 arquivos e `build-cluster` 122/69 antes e depois
  - nenhum falso positivo renomeado — pacote `runtime` do Go, `runtimeClassName`, paths do OpenBao e vocabulário genérico do Kubernetes inalterados, provado pela suíte compilando e os testes passando
  - todos os gates de qualidade passam (`task quality:gates`)
  - `infra/tests/snapshots/helmfile-*.yaml` e `docs/architecture/helm-topology.md` **regenerados** pelo gate, com diff contendo só renomeação
  - **o ambiente sobe do zero com os nomes novos**, pelo caminho que `.claude/rules/cleanroom-up-live-test-mandate.md` exige — rename de cluster que nunca subiu não está verificado
  - ADR escrito (com as alternativas rejeitadas: `control`, `platform`, `ops`, reciclar `mgmt`, `bao`) e `CHANGELOG.md` atualizado sob `[Unreleased] § Changed`

**O mapa.** `mgmt`/`loopzero-mgmt` → **`core`** (é o produto, não gestão). `workload` + `runtime` como
cluster → **`cell`**, com `loopzero-workload` → `cell-1` e `loopzero-runtime-2` → `cell-2`, alinhando a infra ao
vocabulário que o código Go já usa em 137 arquivos (tabela `cells`, `cellID`, `maxPerCell`,
`K8sCellStore`). `vault`/`loopzero-vault`/`vault-system` → **`secrets`** (o cluster roda OpenBao, não Vault:
`infra/helmfile/vault.helmfile.yaml.gotmpl` usa o chart `openbao-helm`, appVersion `v2.5.4`, binário
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
para `vault-system` → `secrets-system`: é mudança de **recurso vivo**, não de texto — o namespace
precisa ser recriado, as cargas migradas, e o `ClusterSecretStore` `openbao-transit` aponta para
`openbao-transit.vault-system.svc:8200`; o rename de texto sozinho deixa referências apontando para um
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
