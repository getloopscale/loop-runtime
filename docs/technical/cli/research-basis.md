# CLI Research Basis

## Objetivo

Este documento registra a base técnica usada para definir os gates SOTA da LoopZero CLI.

Ele combina:

- padrões oficiais externos;
- comportamento recomendado pela stack usada pelo módulo;
- evidências do código atual em `cli/`.

## Fontes Externas Principais

### 1. Commander.js

Fonte:

- https://github.com/tj/commander.js

Pontos relevantes para os gates:

- Commander é estrito com opções não reconhecidas e implementa help automático.
- A biblioteca oferece `configureOutput()` e `exitOverride()` para controlar streams e códigos de saída.
- Isso fundamenta `CLI-G01`, `CLI-G02` e `CLI-G04`.

Por que importa no LoopZero:

- O LoopZero já usa Commander como parser raiz.
- O comportamento de parse não deve depender de handlers ad hoc por comando.

### 2. Node.js TTY / process / readline

Fontes:

- https://nodejs.org/api/tty.html
- https://nodejs.org/docs/latest/api/process.html
- https://nodejs.org/api/readline.html

Pontos relevantes para os gates:

- `process.stdout.isTTY` e `process.stdin.isTTY` são o mecanismo preferido para detectar contexto interativo.
- Prompts e escrita interativa devem respeitar o tipo real do stream.
- Isso fundamenta `CLI-G04` e `CLI-G05`.

Por que importa no LoopZero:

- A CLI roda tanto em terminal humano quanto em CI e pipes.
- Prompt bloqueando em stdin fechado é regressão crítica de UX e automação.

### 3. XDG Base Directory Specification

Fonte:

- https://specifications.freedesktop.org/basedir/latest/

Pontos relevantes para os gates:

- Configuração do usuário deve respeitar `XDG_CONFIG_HOME` quando presente.
- Isso fundamenta `CLI-G06`.

Por que importa no LoopZero:

- Ferramenta moderna de CLI precisa se comportar bem em Linux desktop, containers, runners e ambientes gerenciados.

### 4. W3C Trace Context

Fonte:

- https://www.w3.org/TR/trace-context/

Pontos relevantes para os gates:

- Define headers padronizados para propagação de contexto distribuído.
- Isso fundamenta `CLI-G10`.

Por que importa no LoopZero:

- O LoopZero é um sistema multi-plano; sem propagação padronizada, o trace se quebra entre CLI e API.

### 5. OpenTelemetry Propagators

Fonte:

- https://opentelemetry.io/docs/specs/otel/context/api-propagators/

Pontos relevantes para os gates:

- W3C TraceContext é o padrão de propagação esperado no ecossistema OTel.
- Isso reforça `CLI-G10`.

Por que importa no LoopZero:

- A CLI já usa OTel; o gate precisa seguir o padrão aceito pelo ecossistema, não um header proprietário.

### 6. RFC 9457 Problem Details for HTTP APIs

Fonte:

- https://www.rfc-editor.org/rfc/rfc9457

Pontos relevantes para os gates:

- Define envelope estruturado para erros HTTP.
- Isso fundamenta parcialmente `CLI-G09`, mesmo que a API atual do LoopZero ainda use envelope próprio.

Por que importa no LoopZero:

- O princípio importante para a CLI é consumir erros remotos de forma estruturada e estável.
- Mesmo sem migrar imediatamente para `application/problem+json`, este RFC é a melhor referência para evolução futura.

### 7. Semantic Versioning

Fonte:

- https://semver.org/

Pontos relevantes para os gates:

- Um artefato com API pública precisa tratar breaking changes explicitamente.
- Isso fundamenta `CLI-G15`.

Por que importa no LoopZero:

- Flags, argumentos e `--json` são API pública da CLI.

## Evidências Internas Do LoopZero

### Parsing, output e lifecycle

Arquivos:

- `cli/src/index.ts`
- `cli/src/output/formatter.ts`
- `cli/src/output/prompt-confirm.ts`

O que sustentam:

- saída separada por stream;
- parse centralizado;
- erro JSON em parse failures;
- help/version como success path;
- guardas para CI e non-TTY.

### Configuração e credenciais

Arquivos:

- `cli/src/config/store.ts`
- `cli/tests/unit/config/store-xdg.test.ts`

O que sustentam:

- suporte a XDG;
- permissões restritas;
- override por env var;
- persistência atômica.

### Transporte, retry e auth refresh

Arquivos:

- `cli/src/api/client.ts`
- `cli/tests/unit/api/client.test.ts`
- `cli/tests/unit/api-client-refresh.test.ts`

O que sustentam:

- retry com escopo controlado;
- timeout;
- refresh de token;
- centralização de error mapping.

### Tracing e correlação

Arquivos:

- `cli/src/tracing/propagation.ts`
- `cli/src/tracing/index.ts`
- `cli/tests/unit/tracing/propagation.test.ts`

O que sustentam:

- propagação W3C/OTel;
- spans do comando;
- continuidade observável CLI → API.

### Contrato de processo real

Arquivos:

- `cli/tests/integration/exit-code-contract.test.ts`

O que sustentam:

- validação de exit status real do processo;
- consistência entre `--json` e `process.status`.

## Conclusão

Os gates definidos em [sota-gates.md](./sota-gates.md) não são aspiracionais no vazio. Eles foram escolhidos porque:

1. São compatíveis com os padrões oficiais da stack usada.
2. São coerentes com a natureza da LoopZero CLI como interface humana e programável.
3. Podem ser auditados diretamente no código e nos testes existentes do módulo.
