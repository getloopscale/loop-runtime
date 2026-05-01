# CLI SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo`: por que o gate existe.
- `Critério de aprovação`: condição objetiva para considerar o gate atendido.
- `Evidência esperada`: onde esse gate deve aparecer em código e testes.
- `Notas LoopZero`: como isso se aplica especificamente ao LoopZero.

Os gates são cumulativos. Uma CLI excelente em UX mas fraca em contrato não é SOTA.

Regra transversal de validação:

- testes de integração e fork-based são necessários;
- para comandos críticos de produto, isso ainda não basta sem validação production-parity ou dogfood real contra a plataforma.

## Gate CLI-G01: Strict Command Grammar

`Objetivo`

Garantir parsing previsível, sem aceitar typos silenciosos nem argumentos ambíguos.

`Critério de aprovação`

- Comando desconhecido falha com erro explícito.
- Flag desconhecida falha com erro explícito.
- Argumento obrigatório ausente falha com erro explícito.
- `--help` e `--version` continuam caminhos de sucesso.

`Evidência esperada`

- Parse estrito no bootstrap da CLI.
- Testes de processo real para unknown command, unknown option e missing arg.

`Notas LoopZero`

- `cli/src/index.ts` já usa Commander com `configureOutput` e `exitOverride`.
- `cli/tests/integration/exit-code-contract.test.ts` cobre parte material desse gate.

## Gate CLI-G02: Stable Exit-Code Contract

`Objetivo`

Permitir automação segura em CI, scripts e wrappers.

`Critério de aprovação`

- Cada falha previsível mapeia para exit code estável.
- `--json` e status do processo concordam no mesmo `exit_code`.
- Help e version sempre retornam `0`.
- Novos comandos não introduzem novos exit codes sem revisão de contrato.

`Evidência esperada`

- Tabela explícita de mapping.
- Teste fork-based contra o binário real.

`Notas LoopZero`

- O contrato oficial do repo é `0/1/2/3/4/5`.
- O teste de contrato em `cli/tests/integration/exit-code-contract.test.ts` deve continuar sendo gate de merge.

## Gate CLI-G03: JSON Contract Stability

`Objetivo`

Fazer da CLI uma interface programável estável, não só uma interface humana.

`Critério de aprovação`

- Todo comando suportado com `--json` retorna envelope válido e parseável.
- Campos existentes não são renomeados ou removidos sem versionamento deliberado.
- Saída JSON não mistura texto humano antes ou depois do payload final, exceto em casos explicitamente modelados e testados.
- Erros estruturados têm shape estável.

`Evidência esperada`

- Testes de snapshot/shape por comando.
- Uma função central ou padrão compartilhado para serialização.
- Referência de schema no documento de produto ou testes.

`Notas LoopZero`

- Este gate é especialmente crítico porque `docs/cli-reference.md` trata `--json` como contrato estável.
- Onde houver streaming ou múltiplas linhas, o formato precisa estar explicitamente documentado.

## Gate CLI-G04: Stdout/Stderr Discipline

`Objetivo`

Separar dados para máquina de mensagens para humano e evitar quebrar pipes.

`Critério de aprovação`

- JSON de sucesso sai em `stdout`.
- Mensagens humanas e diagnósticos não corrompem o stream JSON.
- Erros humanos não estruturados saem em `stderr`.
- O comportamento em pipe permanece parseável.

`Evidência esperada`

- Wiring explícito de `writeOut` e `writeErr`.
- Testes que capturam `stdout` e `stderr` separadamente.

`Notas LoopZero`

- O bootstrap atual já diferencia `writeTo` e `writeError`.
- Novos comandos não devem fazer `console.log` ou `console.error` direto fora do padrão estabelecido.

## Gate CLI-G05: TTY And CI Correctness

`Objetivo`

Fazer a CLI funcionar bem em terminal interativo, pipe, cron, CI e subshell.

`Critério de aprovação`

- A CLI detecta TTY pelo `isTTY` real do stream.
- Prompts interativos não bloqueiam quando stdin não é TTY.
- Em `CI=true`, a saída evita animações/spinners e caminhos que exigem interação.
- EOF em prompt é tratado como cancelamento seguro.

`Evidência esperada`

- Guardas explícitas para `process.stdin.isTTY` e `process.stdout.isTTY`.
- Testes para non-TTY, EOF e `CI=true`.

`Notas LoopZero`

- `cli/src/output/prompt-confirm.ts` já recusa prompt em non-TTY.
- `cli/src/output/formatter.ts` já desabilita cor em CI.

## Gate CLI-G06: Local Config And Secret Hygiene

`Objetivo`

Proteger credenciais e fazer a configuração local seguir convenções seguras do sistema operacional.

`Critério de aprovação`

- Config é salva em diretório por usuário, não compartilhado globalmente.
- Arquivo de credenciais é gravado com permissões restritas.
- Escrita é atômica para evitar corrupção parcial.
- `XDG_CONFIG_HOME` é respeitado quando disponível.
- Variável de ambiente de token pode sobrescrever config persistida para uso em CI.

`Evidência esperada`

- Implementação explícita de permissão e rename atômico.
- Testes cobrindo fallback `HOME` e modo XDG.

`Notas LoopZero`

- `cli/src/config/store.ts` já implementa `0o700`, `0o600`, write temp + rename e suporte a XDG.

## Gate CLI-G07: Local-First Validation

`Objetivo`

Falhar rápido quando o problema já é detectável no workspace local.

`Critério de aprovação`

- Erros de config local, app inexistente e environment inválido falham antes de upload.
- Warnings locais são emitidos sem bloquear quando apropriado.
- A CLI não faz round-trip remoto para descobrir erro trivial de input.

`Evidência esperada`

- Validação explícita em adapters/commands antes da chamada remota.
- Testes unitários para config inválida, arquivo ausente e seleção de app inexistente.

`Notas LoopZero`

- O `deploy` já materializa bem esse padrão em `validatePrerequisites()`.
- Esse gate deve ser estendido aos demais comandos que dependem de contexto local.

## Gate CLI-G08: Transport Resilience Without Domain Drift

`Objetivo`

Tolerar falhas transitórias de rede sem duplicar lógica de negócio do backend.

`Critério de aprovação`

- Retries só acontecem para classes de erro transitórias claramente definidas.
- Backoff é bounded e com jitter.
- 401 pode tentar refresh uma única vez quando o modelo de auth suportar isso.
- A CLI não faz retry cego em erros semânticos de usuário ou conflito de negócio.

`Evidência esperada`

- Cliente HTTP central com policy de retry.
- Testes de retry, timeout, refresh e non-retryable errors.

`Notas LoopZero`

- `cli/src/api/client.ts` já implementa retry para `429/502/503/504`, timeout e refresh de token.
- Esse gate protege especialmente `deploy`, `status`, `logs` e `platform`.

## Gate CLI-G09: Actionable Error Normalization

`Objetivo`

Traduzir erros remotos ou locais em mensagens que levam o usuário ao próximo passo.

`Critério de aprovação`

- Erros autenticados vs autorização vs validação vs indisponibilidade são distinguíveis.
- Mensagens sugerem próxima ação quando cabível.
- Em `--json`, o erro mantém código estável e texto parseável.
- O mapeamento erro HTTP → erro da CLI é centralizado.

`Evidência esperada`

- Uma fonte única de truth para mapping de erro.
- Testes cobrindo 401, 403, 409, 422, 5xx e offline.

`Notas LoopZero`

- `ApiError.fromHttpStatus()` já cumpre parte importante desse gate.
- Evolução futura deve aproximar API e CLI de problem details mais estruturados, sem quebrar o contrato atual.

## Gate CLI-G10: Trace And Correlation Propagation

`Objetivo`

Preservar observabilidade de ponta a ponta entre a CLI e o control plane.

`Critério de aprovação`

- Cada request relevante carrega identificador de correlação.
- Quando tracing está ativo, a CLI injeta contexto de trace no boundary HTTP.
- O fluxo do comando principal é instrumentado por span raiz e spans de fases críticas.

`Evidência esperada`

- Headers explícitos de correlation e trace propagation.
- Testes de propagation.
- Inicialização e shutdown limpos da instrumentação.

`Notas LoopZero`

- `cli/src/api/client.ts` injeta `Correlation-ID`.
- `cli/src/tracing/propagation.ts` injeta W3C Trace Context via OpenTelemetry.
- `cli/src/index.ts` e `cli/src/commands/deploy.ts` já têm spans relevantes.

## Gate CLI-G11: Source And Workspace Determinism

`Objetivo`

Garantir que a CLI saiba exatamente o que está enviando para deploy.

`Critério de aprovação`

- O identificador de source é determinístico para o conteúdo enviado.
- A CLI avisa sobre mudanças não commitadas quando isso altera a expectativa do operador.
- O bundle é validado antes de upload.
- O comando deixa claro se o deploy está usando commit git ou diretório atual.

`Evidência esperada`

- Adapters dedicados para git/source bundle.
- Testes de dirty repo, missing lockfile, bundle size e source id.

`Notas LoopZero`

- O fluxo de `deploy` já mostra warning de dirty repo e ausência de lockfile.
- Esse gate é crucial para manter confiança no comportamento do deploy.

## Gate CLI-G12: Test Matrix Completeness

`Objetivo`

Evitar regressões de UX e contrato que unit tests isolados não detectam.

`Critério de aprovação`

- Há testes unitários para regras locais.
- Há testes de integração por comando.
- Há pelo menos um conjunto de testes fork-based no binário real para plumbing de processo, streams e exit code.
- Mudanças em output, auth, config store e API client exigem cobertura específica.

`Evidência esperada`

- `cli/tests/unit/*`
- `cli/tests/integration/*`
- Casos explícitos para parse errors, auth, deploy, config e JSON contract.

`Notas LoopZero`

- O módulo já tem uma base de testes ampla; o gate agora é preservar cobertura nos caminhos de contrato.

## Gate CLI-G13: No Internal Platform Leakage

`Objetivo`

Preservar linguagem de produto e impedir que detalhes internos virem contrato acidental.

`Critério de aprovação`

- Saída de CLI para usuário não expõe termos internos proibidos.
- Erros remotos internalistas são sanitizados antes de chegar à interface.
- IDs e conceitos internos só aparecem quando fazem parte de contrato intencional e documentado.

`Evidência esperada`

- Sanitização de mensagens em output/commands.
- Testes de regressão lexical sobre palavras proibidas.

`Notas LoopZero`

- Este gate é mandatório por regra do repo.
- A CLI deve falar em deploy, environment, app, service e release; não em mecanismos internos.

## Gate CLI-G14: Modular Command Ownership

`Objetivo`

Manter o crescimento da CLI sem colapsar em um arquivo central com acoplamento excessivo.

`Critério de aprovação`

- Registro de comandos fica separado da execução.
- Regras de domínio ficam nos comandos/adapters certos.
- O client HTTP e output utilities não absorvem lógica de caso de uso indevida.
- Novos grupos de comando entram por composição, não por branching global.

`Evidência esperada`

- Pastas `register/`, `commands/`, `api/`, `output/`, `adapters/` com papéis claros.
- Ausência de lógica de produto concentrada em `index.ts`.

`Notas LoopZero`

- A organização atual do módulo é boa base; o gate serve para evitar regressão estrutural.

## Gate CLI-G15: Release Discipline For Public Contracts

`Objetivo`

Tratar a CLI como API pública para humanos e máquinas.

`Critério de aprovação`

- Mudanças em output estruturado são tratadas como mudança de contrato.
- Mudanças breaking em flags, argumentos ou JSON exigem documentação e versionamento intencional.
- A versão da CLI comunica alteração compatível vs incompatível.

`Evidência esperada`

- Referência de comandos atualizada.
- PRs que alteram contratos apontam impacto explícito.

`Notas LoopZero`

- Este gate conecta diretamente a CLI ao `docs/cli-reference.md` e à disciplina de versionamento do binário.

## Ordem De Prioridade

Se houver conflito de esforço, a prioridade de gates é:

1. `CLI-G02`, `CLI-G03`, `CLI-G05`, `CLI-G09`
2. `CLI-G01`, `CLI-G06`, `CLI-G08`, `CLI-G10`
3. `CLI-G07`, `CLI-G11`, `CLI-G12`, `CLI-G13`, `CLI-G14`, `CLI-G15`

Justificativa:

- Primeiro vem confiabilidade para automação e usuário.
- Depois resiliência operacional e observabilidade.
- Por fim, disciplina estrutural e evolução sustentável.
