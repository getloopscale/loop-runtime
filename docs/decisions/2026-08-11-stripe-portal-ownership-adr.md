# ADR — Quem emite a sessão do portal do Stripe

**Data:** 2026-08-11
**Status:** PROPOSED — material para decisão; a parte 1 já foi implementada por ser correção de segurança independente
**Tier:** T2 — contrato de API, autorização e credencial de provedor de pagamento
**Item:** `BACKLOG.md` B-029, com B-031 e B-032 nascidos da medição

## O que a medição encontrou, e o que ela refutou

A pergunta inicial era *"o faturamento tem dois donos?"*. Três medições sucessivas reduziram o
escopo e mudaram a direção do conserto duas vezes. Registrar isso importa mais que o resultado: cada
refutação veio de ler o código em vez de confiar na leitura anterior.

**Refutação 1 — não são dois donos.** O engine não conhece preço, plano nem catálogo. Ele recebe um
`stripe_customer_id` e pede uma sessão. O `loopzero-cloud` cria cliente, assinatura, checkout e reporta
uso. A sobreposição é de **um verbo**.

**Refutação 2 — a direção óbvia estava errada.** Propus que o engine repassasse ao `loopzero-cloud`. O
fluxo do sistema é `cloud → engine` (`loopzero-cloud/cmd/main.go:1602`) e não existe inverso. Fazer o
engine chamar o painel inverteria a dependência — o defeito do `B-022`, agravado.

**Refutação 3 — as implementações não são equivalentes.** O cloud **cria** o cliente Stripe quando
falta (`EnsureStripeCustomer`); o engine **exige** que exista (`400 NO_STRIPE_CUSTOMER`). O cloud não
é uma cópia: é um passo antes. Fazer ele repassar quebraria o provisionamento do cliente.

**O achado que decidiu.** O engine **não tem a chave do Stripe no app-dev**: o Secret
`loopzero-api-stripe` é `optional: true` e não existe. O código cai num caminho sem credencial que monta
`https://billing.stripe.com/p/login/` + `customerID` (`billing.go:245`) — uma URL construída, não uma
sessão emitida. A "credencial em dois processos" que motivou o item é **potencial, não atual**.

## Decisão

**O `loopzero-cloud` é o único emissor de sessão do portal. O engine deixa de emitir.**

Três razões, em ordem de peso:

1. **É o mais seguro.** Elimina o caminho que hoje devolve URL construída (`B-032`) e garante que a
   chave do Stripe nunca precise existir no engine — que é o processo que atende o CLI e o caminho
   crítico de deploy. Menos superfície, e a que sobra fica no componente que já é dono do assunto.
2. **Respeita a direção do sistema.** O cloud já chama o engine; o inverso nunca existiu. A decisão
   não cria dependência nova em direção nenhuma.
3. **Preserva a pré-condição que o cloud garante.** Ele cria o cliente quando falta; o engine não. A
   capacidade fica onde a pré-condição é tratada.

### Como o CLI continua funcionando

O CLI fala com o engine, e a rota `POST /api/v1/billing/portal` **permanece** — removê-la
reintroduziria o 404 que ela consertou (`routes/billing.go:7`). O que muda é o que ela faz quando não
pode emitir: em vez de construir uma URL, ela **falha com erro tipado** dizendo que o faturamento é
gerido pelo painel, com o endereço para onde ir.

Isso é pior em conveniência e melhor em honestidade: uma URL construída que *parece* funcionar é o
tipo de resposta que esconde o problema até alguém investigar por que o portal não abriu.

## Alternativas rejeitadas

**Manter as duas implementações com guarda de divergência.** Foi a recomendação que eu ia dar antes
do `B-032`. Rejeitada porque a duplicação não é o dano — o dano é o caminho sem credencial, e mantê-lo
com um guarda seria vigiar um defeito em vez de removê-lo.

**O CLI passa a bater no cloud.** Exigiria rota nova na borda: medido em 2026-08-11 que o CLI **não
alcança** o cloud hoje — `POST app-dev/v1/storage/share` devolve 404, e o 401 pelo `/engine/` vem do
`requireJWT` do proxy, antes de qualquer rota. O que parecia precedente não era.

**O engine chama o cloud no passo final.** Inverte a direção da dependência. É o pior desfecho
segundo a medição do discover, e o `B-022` já registra o custo dessa direção.

## Consequências

- `loopzero billing portal` deixa de devolver URL quando o faturamento não está configurado no engine, e
  passa a orientar o usuário ao painel. **Mudança de comportamento visível**, e vai ao CHANGELOG.
- O `B-032` fecha junto: o caminho que constrói URL deixa de existir.
- O `B-029` fecha com escopo reduzido e honesto: não era "dois donos", era um verbo duplicado sobre
  pré-condições diferentes, com um caminho inseguro em um dos lados.
- A chave do Stripe fica em **um** processo, por desenho e não por acidente de configuração.

## O que já foi feito

A parte 1 — `RequireRole("owner")` na rota do engine (`B-031`) — foi implementada antes deste ADR,
por ser correção de segurança independente da decisão de desenho: a mesma capacidade exigia `owner`
num lado e nada no outro, e isso é defeito com ou sem unificação.
