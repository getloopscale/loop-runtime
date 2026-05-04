# Shared Packages Architecture

## Escopo

`pkg/` contém as bibliotecas compartilhadas do LoopZero.

Elas existem para centralizar contratos que precisam permanecer iguais em múltiplos componentes. Em uma PaaS como o LoopZero, isso importa muito porque o mesmo conceito aparece em API, operators, cloud services e tooling:

- um nome precisa ser gerado sempre igual;
- um estado precisa transicionar sempre igual;
- um erro precisa ser interpretado sempre igual;
- um sinal de health precisa ser agregado sempre igual.

## Mapa Por Pacote

| Pacote | Responsabilidade principal | Domínio | Status |
|---|---|---|---|
| `pkg/statemachine` | invariantes de lifecycle e transições válidas | state contracts | ✅ active |
| `pkg/errors` | erros tipados e serialização base | error contracts | ✅ active |
| `pkg/naming` | nomes canônicos e compatibilidade RFC 1123 | naming contracts | ✅ active |
| `pkg/id` | geração de IDs estruturados e estáveis | identity/resource IDs | ✅ active |
| `pkg/logging` | enrich de logger com contexto transversal | observability/logging | ✅ active (global state — ver roadmap S9-5) |
<!-- pkg/health DELETADO em 2026-05-04 per ADR-0028 (0 importadores). Substituído conceitualmente por pkg/conditions per ADR-0032. -->
| `pkg/circuitbreaker` | contenção de falha externa compartilhada | resilience | ✅ active (v1-style — ver roadmap S9-1) |
| `pkg/enforcement` | plan limits e enforcement compartilhado | product policy | ✅ active (acopla `*sql.DB` — ver roadmap S9-4) |
| `pkg/jwt` | validação e emissão de JWT/session tokens | auth/token contracts | ✅ active (gaps RFC 8725bis — ver roadmap QW-2) |

## Real Usage Matrix

Tabela gerada via grep de `import "github.com/loopzero/loopzero/pkg/X"` em 2026-05-04. Atualizar via script `task pkg:usage-matrix` (a criar — ver gate PKG-G14).

| Pacote | Imports | api/internal | operators/internal | cloud | Outros |
|---|---:|---:|---:|---:|---:|
| `pkg/errors` | 70 | maioria | sim | sim | tests |
| `pkg/statemachine` | 54 | sim | sim | — | tests |
| `pkg/naming` | 13 | sim | sim | — | — |
| `pkg/logging` | 10 | sim | sim | sim | — |
| `pkg/jwt` | 6 | sim | — | sim | — |
| `pkg/enforcement` | 6 | sim | — | — | tests |
| `pkg/id` | 5 | sim | — | — | tests |
| `pkg/circuitbreaker` | 5 | sim | sim | — | — |
<!-- `pkg/health` DELETADO 2026-05-04 per ADR-0028 (was 0 imports) -->

**Top consumidores** (top-level packages que importam `pkg/`):

| Top-level | Total imports |
|---|---:|
| `api/internal` | 89 |
| `operators/internal` | 19 |
| `api/tests` | 16 |
| `api/cmd` | 5 |
| `operators/cmd` | 4 |
| `cloud/internal` | 2 |
| `cloud/tests` | 1 |
| `cloud/cmd` | 1 |

**Observações:**

- A shared layer é predominantemente **api↔operators**. A descrição original "API, operators, cloud services" gera expectativa errada — `cloud/` tem apenas 4 imports vs 89 da `api/`.
- `pkg/health` foi DELETADO em 2026-05-04 per ADR-0028 (era dead code com 0 importadores). Phase aggregation conceitualmente correta migra para `pkg/conditions` per ADR-0032 quando T2.2 do plan executar.
- `pkg/errors` (70 imports) é o pacote crítico cross-cutting; mudanças aqui exigem PKG-G13 a sério.

## Domínios Funcionais

### `state-and-lifecycle`

Responsável por:

- descrever state machines rígidas;
- impedir transições inválidas;
- tornar lifecycle consistente entre API e operators.

Arquivos âncora:

- `pkg/statemachine/machine.go`
- `pkg/statemachine/*.go`

### `identity-and-naming`

Responsável por:

- IDs estáveis e com prefixos contratuais;
- nomes RFC 1123;
- composição canônica de namespaces, repos, hostnames e recursos.

Arquivos âncora:

- `pkg/id/id.go`
- `pkg/naming/naming.go`

### `error-and-health-contracts`

Responsável por:

- erros tipados e serializáveis;
- contratos de health/readiness agregados;
- consistência de interpretação por consumidores distintos.

Arquivos âncora:

- `pkg/errors/errors.go`
- `pkg/health/*.go`

### `observability-and-resilience`

Responsável por:

- enrich de logging com trace context sem acoplamento direto à implementação de tracing;
- circuit breaker genérico para integrações externas.

Arquivos âncora:

- `pkg/logging/context.go`
- `pkg/circuitbreaker/circuitbreaker.go`

### `policy-and-auth`

Responsável por:

- enforcement de plano e limites;
- validação e emissão de tokens/sessões compartilhadas.

Arquivos âncora:

- `pkg/enforcement/*.go`
- `pkg/jwt/validator.go`

## Fronteiras Arquiteturais

### O que pertence a `pkg/`

- invariantes sistêmicos reutilizados por mais de um módulo;
- pequenas abstrações transversais;
- tipos e helpers cuja divergência entre componentes seria bug sistêmico.

### O que não pertence a `pkg/`

- lógica de caso de uso da API;
- lógica de CLI;
- lógica específica de controller;
- adapters de provider ou framework;
- helpers genéricos sem consumidor transversal claro.

## Regra De Ouro

Um pacote entra em `pkg/` quando a divergência entre consumidores seria um bug de plataforma. Caso contrário, ele provavelmente pertence ao módulo consumidor.
