# Shared Packages Architecture

## Escopo

`pkg/` contém as bibliotecas compartilhadas do LoopZero.

Elas existem para centralizar contratos que precisam permanecer iguais em múltiplos componentes. Em uma PaaS como o LoopZero, isso importa muito porque o mesmo conceito aparece em API, operators, cloud services e tooling:

- um nome precisa ser gerado sempre igual;
- um estado precisa transicionar sempre igual;
- um erro precisa ser interpretado sempre igual;
- um sinal de health precisa ser agregado sempre igual.

## Mapa Por Pacote

| Pacote | Responsabilidade principal | Domínio |
|---|---|---|
| `pkg/statemachine` | invariantes de lifecycle e transições válidas | state contracts |
| `pkg/errors` | erros tipados e serialização base | error contracts |
| `pkg/naming` | nomes canônicos e compatibilidade RFC 1123 | naming contracts |
| `pkg/id` | geração de IDs estruturados e estáveis | identity/resource IDs |
| `pkg/logging` | enrich de logger com contexto transversal | observability/logging |
| `pkg/health` | agregação de saúde e fases | health contracts |
| `pkg/circuitbreaker` | contenção de falha externa compartilhada | resilience |
| `pkg/enforcement` | plan limits e enforcement compartilhado | product policy |
| `pkg/jwt` | validação e emissão de JWT/session tokens | auth/token contracts |

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
