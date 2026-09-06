# Runbook — alertas de auditoria do cofre (OpenBao)

Cobre os quatro alertas de `infra/helm/monitoring/manifests/prometheusrule-vault-audit.yaml`.
Os quatro são `critical`, e os quatro ficaram **sem runbook** entre 2026-08-19 e a escrita deste
documento: eles apontavam para `docs.loopzero.example/runbooks/vault-*`, um site que responde 404
(medido no `B-161`). Remover os links mortos foi certo — apontar para o vazio é pior que não
apontar — mas deixou quatro alertas críticos mudos, que é o que o `B-067` nomeia.

**O cofre roda num cluster separado** (`infra/k3d/secrets-cluster.yaml`), e isso muda o primeiro
passo de todo diagnóstico aqui: o contexto do `kubectl` não é o mesmo dos outros alertas.

---

## VaultSealedOrDown

**Dispara quando:** `vault_core_unsealed{job="openbao"} == 0` **ou** a série está ausente, por 1 min.

**A disjunção é a parte importante.** `== 0` significa *selado*: o processo vive e recusa toda
operação. `absent(...)` significa *não estamos medindo* — o scrape caiu, o pod sumiu, ou o
`ServiceMonitor` deixou de casar. Os dois viram o mesmo alerta de propósito, porque a alternativa
seria um cofre inalcançável que não acorda ninguém. Separe os dois antes de agir: eles têm
consertos opostos.

**Efeito enquanto durar:** nada que precise de segredo prossegue. O `loopzero-api` não decifra
credencial de cell, o ESO não sincroniza, e o operator de tenant não emite chave de transit.

**Diagnóstico:**

1. Descubra qual dos dois casos é: se `vault_core_unsealed` responde `0`, está selado; se não
   responde nada, é observabilidade.
2. **Selado:** um restart de pod sela o cofre por desenho — Shamir exige unseal manual. Confirme
   se houve rollout antes de procurar causa exótica.
3. **Ausente:** verifique o pod no cluster de secrets e o alvo no Prometheus. Um `job` que deixou
   de casar produz `absent()` sem que o cofre tenha qualquer problema.

**Ação:** o unseal é operação com material de chave e **não é automatizável aqui** — siga o
procedimento de custódia. Nunca contorne registrando as unseal keys em lugar nenhum que este
repositório alcance.

---

## VaultAuthFailureRateHigh

**Dispara quando:** falhas de autenticação acima de 10/min num caminho `auth/*`, por 5 min.
O rótulo `path` diz **qual** ponto de montagem.

**Duas leituras, e elas se distinguem pelo `path`:**

- Um único `path` — normalmente um binário da plataforma cujo token expirou ou cuja SA foi
  recriada. O `CLAUDE.md` registra essa armadilha: um token de SA em cache continua sendo enviado
  depois que o UID da SA mudou num rollout, e falha antes do `exp` do próprio claim.
- Vários `path` ao mesmo tempo — suspeite do cofre, não dos clientes: política alterada, mount
  removido, ou relógio fora de sincronia invalidando JWT.

**Diagnóstico:**

1. Agrupe por `path`. Um caminho ou muitos é a bifurcação inteira.
2. Para um caminho: identifique a identidade e verifique se a SA foi recriada. Renovar o token é
   o conserto; reinstalar o binário não é.
3. Para muitos: compare com o horário da última mudança em `openbao-init` — provisionamento de
   role e de policy passa por lá.

**Ação:** não eleve permissão para silenciar o alerta. Uma falha de auth em volume é informação
sobre quem está tentando o quê, e afrouxar a política apaga a informação junto com o sintoma.

---

## VaultPolicyDenialRateHigh

**Dispara quando:** negações por permissão acima de 5/min, por 5 min, agrupadas por
`request_path`.

**Não é o mesmo que o anterior.** Ali a identidade não conseguiu **entrar**; aqui ela entrou e
pediu algo que a policy dela não concede. O limiar é mais baixo (5 contra 10) porque negação em
volume costuma indicar caminho errado no código, não força bruta.

**Diagnóstico:**

1. O `request_path` diz o que foi pedido. Compare com as policies provisionadas em
   `infra/helm/openbao-init/values.yaml`.
2. Divergência de **prefixo** é a causa mais comum e a mais silenciosa: uma policy que concede
   sobre `tenant-*` enquanto o código escreve `loopzero-tenants-*` nega tudo sem que nada pareça
   errado dos dois lados. Esse caso exato está registrado nos comentários daquele arquivo.
3. Confirme qual policy a identidade carrega — desde o `B-098` existe uma por role, e o gate
   `openbao-init-auth-mounts` recusa policy que nenhum role carregue.

**Ação:** corrija o caminho no código ou a policy, **o que estiver errado** — e o critério é o
menor privilégio, não o que faz o alerta parar. Alargar uma policy para casar com um caminho
equivocado concede mais do que alguém pediu.

---

## VaultAuditLogStalled

**Dispara quando:** há tráfego (`rate(vault_audit_log_request_count) > 0`) **e** a última entrada
de auditoria tem mais de 10 min, por 5 min.

**É o mais grave dos quatro, e o menos visível.** O cofre continua servindo — ninguém reclama —
enquanto deixa de registrar o que serviu. O alerta existe porque a conjunção é a única evidência:
tráfego sem registro. Silêncio no log de auditoria com tráfego zero seria normal, e é por isso
que a expressão exige os dois.

**Consequência:** toda operação nessa janela fica sem trilha. Não se recupera depois — o registro
que não foi escrito não existe.

**Diagnóstico:**

1. Verifique o destino do audit device. Um device em arquivo com disco cheio, ou um socket cujo
   consumidor caiu, faz o cofre parar de registrar.
2. O OpenBao **bloqueia** operações quando todos os audit devices falham. Se ele ainda serve, há
   device sobrevivendo — ou o device que falhou não era o único.
3. Compare com a rotação de log. Uma rotação que não reabre o arquivo produz exatamente este
   sintoma.

**Ação:** trate como incidente de auditoria, não de disponibilidade. Restaure o device, e registre
a janela sem trilha — o intervalo em que não houve registro é, ele próprio, o fato a documentar.

---

## Convenção de `runbook_url`

Aponte para caminho que existe no repositório. Foi por não fazer isso que estes quatro alertas
ficaram mudos: apontavam para um site público que responde 404, e o link morto sobreviveu meses
justamente porque ninguém clica num runbook até às 3h da manhã.
