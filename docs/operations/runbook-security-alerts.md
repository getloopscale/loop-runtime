# Runbook — alertas de segurança da entrega

Cobre os três alertas que o `B-067` marca como **sem exceção possível**: eles falam de
assinatura de release, de atestado de proveniência e de propagação de secret. Um alerta desses
sem runbook acorda alguém às 3h sem dizer o que fazer.

Cada seção é ancorada no **ponto de emissão da métrica**, não em descrição genérica. Se o código
mudar de lugar, a âncora quebra e o gate `dangling-data-refs` acusa — o que é preferível a um
runbook que descreve um sistema que já não existe.

---

## LoopZeroUnsignedReleasePushed

**Dispara quando:** `sum(rate(loopzero_release_signed_total{signed="false"}[5m])) > 0` por 2 min.
**Severidade:** warning. **Significado:** uma release foi promovida ao registry **sem assinatura**.

**Onde a métrica nasce:** `api/internal/services/release/container_delivery.go` — o rótulo
`signed` é calculado imediatamente antes do `Inc()`, e recebe `"false"` quando a entrega
concluiu sem assinatura cosign.

**Por que importa:** a política de admissão de imagem (`vap-require-image-signature`) recusa
imagem não assinada no cluster de runtime. Uma release não assinada, portanto, **não vai subir** —
o alerta antecede a falha de deploy, e não o contrário.

**Diagnóstico, em ordem:**

1. Identifique a release: a métrica carrega `tenant` e `environment` como rótulos.
2. Verifique se o passo de assinatura rodou no workflow do build (`sign-image-app`). O modo de
   falha registrado no `CLAUDE.md` é o `clone-repo` esgotar a janela de retry sob carga
   concorrente: o workflow reporta `Succeeded` no `phase` enquanto o passo de assinatura ficou
   `Omitted`. **A fase do workflow não é a verdade dos passos** — leia `status.nodes.*`.
3. Confirme o modo do cosign: `kubectl get configmap loopzero-cosign-config -n loopzero-system`. Em modo
   `keyed`, uma chave ausente ou rotacionada faz o passo falhar sem derrubar o workflow.

**Ação:**

- **Não** promova a release manualmente contornando a assinatura. A ausência de assinatura é a
  informação; suprimi-la remove a única evidência de que algo saiu do caminho.
- Reenvie o build. Se reincidir, verifique a contenção de I/O no cluster de build antes de mexer
  em cosign — o modo de falha mais comum é de carga, não de configuração.

**Não é exceção porque:** um release não assinado que chegasse a rodar seria exatamente o que a
cadeia de assinatura existe para impedir.

---

## LoopZeroAttestationSaveFailing

**Dispara quando:** `sum(rate(loopzero_release_attestation_save_fail_total[5m])) > 0` por 2 min.
**Severidade:** warning. **Significado:** a persistência do atestado por aplicação está falhando.

**Onde a métrica nasce:** `api/internal/services/release/container_delivery.go`, no ramo de erro
do salvamento dentro da transação. O log ao lado dela diz `attestation persist failed
(non-blocking) — release proceeds without per-app`.

**A palavra que decide a urgência é `non-blocking`.** A release **prossegue**. Ou seja: não há
incidente de disponibilidade em curso, e sim perda silenciosa de proveniência. O que se perde é a
capacidade de responder depois "de qual build veio esta imagem".

**Diagnóstico:**

1. O `Inc()` carrega `environment` como rótulo — comece por ele.
2. É falha de transação, então olhe o Postgres antes do código: saturação de pool, migração
   pendente, ou constraint violada por dado novo.
3. Correlacione com `LoopZeroUnsignedReleasePushed`. Se os dois disparam juntos, a causa provável é
   o passo de assinatura, não o banco.

**Ação:** as releases afetadas ficam sem atestado por aplicação e **isso não se recupera
sozinho** — o caminho é reprocessar os builds afetados depois de corrigida a causa.

**Não é exceção porque:** silencioso por desenho, e a perda é de evidência de auditoria.

---

## LoopZeroSecretESOSyncFailing

**Dispara quando:** `sum(rate(loopzero_secret_externalsecret_total{result="fail"}[5m])) > 0.05` por
5 min. **Severidade:** warning. **Significado:** a criação do `ExternalSecret` está falhando em
mais de 5% das tentativas.

**Onde a métrica nasce:** `api/internal/routes/secrets/secrets.go`. Atenção ao rótulo: além de
`fail` existe `skip`, emitido com o log `ExternalSecret sync skipped — k8sSync nil at boot`.

**`skip` não é `fail`, e a diferença é operacional:** `skip` significa que o cliente Kubernetes
não foi montado no boot — o secret foi gravado no cofre e **nunca vai propagar**, sem que
nenhuma tentativa falhe. O alerta acima, que filtra `result="fail"`, não o pega — quem o pega é
**`LoopZeroSecretESOSyncSkipped`**, um alerta irmão sobre `result="skip"` com limiar `> 0` (qualquer
ocorrência) e janela de 2 min. Ao investigar, consulte os dois rótulos e os dois alertas.

> Esta seção afirmava que o caso `skip` não era coberto por alerta nenhum. Estava errado: o
> alerta irmão existe, e o gate `alert-runbooks` foi quem o mostrou, ao listá-lo como sem
> runbook na mesma execução que verificou este documento.

**Diagnóstico:**

1. `result="skip"` em volume: o `loopzero-api` subiu sem kubeconfig de cell. O `CLAUDE.md` registra o
   sintoma — `secret K8s manager not available — ESO sync disabled` nos logs — e a causa:
   `CELL_0_KUBECONFIG` não montado.
2. `result="fail"`: verifique o controller do ESO e o `ClusterSecretStore` no cell.
3. Confirme o cofre: um `ExternalSecret` referenciando caminho inexistente falha na
   reconciliação, não na criação.

**Ação:** o valor do secret **está** no cofre; o que falta é a propagação. Não repita a gravação —
corrija a propagação e deixe o ESO reconciliar. Regravar sob suspeita de perda é como um segundo
valor entra em circulação.

**Não é exceção porque:** o efeito é a aplicação rodar com secret velho ou sem ele, e o modo
`skip` já demonstrou ser silencioso.

---

## LoopZeroSecretESOSyncSkipped

**Dispara quando:** `sum(rate(loopzero_secret_externalsecret_total{result="skip"}[5m])) > 0` por
2 min — **qualquer** ocorrência. **Severidade:** warning.
**Significado:** `loopzero secret set` virou no-op silencioso.

**Onde a métrica nasce:** `api/internal/routes/secrets/secrets.go`, no ramo `else` que emite
`skip` junto com o log `ExternalSecret sync skipped — k8sSync nil at boot`.

**Por que o limiar é `> 0` e não uma taxa:** não existe volume aceitável disto. Uma única
ocorrência significa que um secret foi aceito, gravado no cofre, e **não vai chegar à
aplicação** — sem erro para o usuário, sem falha para contar. O nome do alerta usa a palavra
certa: *silently no-ops*.

**Diagnóstico:** é condição de boot, não de requisição. O `loopzero-api` subiu sem o kubeconfig de
cell montado; o `CLAUDE.md` registra o sintoma nos logs (`secret K8s manager not available — ESO
sync disabled`) e a causa (`CELL_0_KUBECONFIG` ausente). Confirme antes de olhar o ESO: se o
cliente é nil, o ESO não é o problema.

**Ação:** monte o kubeconfig e reinicie. Os secrets gravados durante a janela **continuam no
cofre** e precisam ser re-propagados — não regrave o valor, force a reconciliação.

**Não é exceção porque:** é o modo de falha mais perigoso da família, justamente por não falhar.

---

## Convenção de `runbook_url`

Aponte para caminho que **existe no repositório**. Medido em 2026-08-19: das URLs de runbook na
árvore, a maioria não levava a nada — inclusive a que citava `infra/tests/k6/README.md`, cujo
arquivo real está em `infra/tests/performance/k6/README.md`.

Um `runbook_url` que dá 404 é pior que a ausência dele: quem está de plantão gasta o primeiro
minuto descobrindo que o link não serve.
