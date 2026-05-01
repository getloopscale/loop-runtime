# Shared Packages SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo`
- `Critério de aprovação`
- `Evidência esperada`
- `Notas LoopZero`

No contexto de uma PaaS, bibliotecas compartilhadas precisam ser mais conservadoras que o restante do sistema, porque elas irradiam comportamento para múltiplos módulos.

Regra transversal de validação:

- testes unitários de contrato são o mínimo;
- quando um pacote governa invariantes sistêmicos, a evidência final também depende de consumidores reais exercitando esse contrato em integração ou parity flows.

## Gate PKG-G01: True Cross-Cutting Scope

`Objetivo`

Garantir que `pkg/` só abrigue problemas realmente transversais.

`Critério de aprovação`

- o pacote atende pelo menos dois consumidores ou domínios relevantes;
- a centralização reduz risco real de divergência;
- a movimentação para `pkg/` não é apenas conveniência de import.

`Evidência esperada`

- uso real em múltiplos módulos;
- responsabilidade clara e pequena.

`Notas LoopZero`

- `naming`, `statemachine`, `errors` e `health` são exemplos fortes desse gate.

## Gate PKG-G02: Minimal Public Surface

`Objetivo`

Evitar APIs compartilhadas grandes, vagas ou difíceis de evoluir.

`Critério de aprovação`

- tipos exportados são os mínimos necessários;
- helpers exportados refletem domínio concreto, não genericismo excessivo;
- nomes de pacote e API são idiomáticos em Go.

`Evidência esperada`

- poucos exports bem definidos;
- ausência de “util”, “common”, “helpers” genéricos.

`Notas LoopZero`

- `pkg/` é shared contract layer, não caixa de ferramentas amorfa.

## Gate PKG-G03: No Consumer Leakage

`Objetivo`

Impedir que detalhes de API, operators, CLI ou infra vazem para a shared layer.

`Critério de aprovação`

- o pacote não depende de frameworks ou runtimes específicos sem necessidade transversal;
- não há import de módulos superiores por conveniência;
- contratos continuam úteis fora de um consumidor isolado.

`Evidência esperada`

- dependências pequenas;
- comentários e package docs alinhados a responsabilidade transversal.

`Notas LoopZero`

- `pkg/logging` é um bom exemplo: recebe extractor por registro e evita dependência direta de OTel.

## Gate PKG-G04: Invariants Over Convenience

`Objetivo`

Centralizar em `pkg/` apenas aquilo que precisa ser uniforme em todo o sistema.

`Critério de aprovação`

- o pacote define ou protege um invariante de plataforma;
- divergência entre implementações locais seria bug sistêmico;
- consumidores usam o pacote para obedecer o contrato, não só economizar linhas.

`Evidência esperada`

- comments e testes que explicitam o invariante;
- uso recorrente em boundaries do sistema.

`Notas LoopZero`

- `statemachine`, `naming` e `id` existem principalmente por esse motivo.

## Gate PKG-G05: Contract Tests First

`Objetivo`

Assegurar que shared behavior seja testado como contrato público.

`Critério de aprovação`

- cada pacote exportado tem testes de contrato;
- edge cases do domínio compartilhado são cobertos;
- mudanças breaking são detectáveis por testes do próprio pacote.

`Evidência esperada`

- `*_test.go` ao lado de todos os pacotes relevantes;
- testes orientados a invariantes, não só linhas executadas.

`Notas LoopZero`

- o módulo atual já mostra boa disciplina de testes unitários em praticamente todos os pacotes.

## Gate PKG-G06: Deterministic Naming And IDs

`Objetivo`

Preservar consistência entre recursos e contratos de sistema.

`Critério de aprovação`

- geração de nomes é determinística;
- validação de nomes reflete o contrato real (RFC 1123 quando aplicável);
- IDs mantêm prefixos e semântica estáveis;
- a API não depende de composição manual espalhada pelo código.

`Evidência esperada`

- `pkg/naming`;
- `pkg/id`;
- testes cobrindo formato e restrições.

`Notas LoopZero`

- para uma PaaS multi-plane, naming inconsistente gera bugs difíceis de rastrear entre API, operators e infra.

## Gate PKG-G07: Strict State Contracts

`Objetivo`

Tratar lifecycle como invariante de plataforma, não como string solta.

`Critério de aprovação`

- state machines são explícitas e pequenas;
- transição inválida é erro de programação ou erro explícito para fonte externa;
- máquinas expõem somente o que consumidores realmente precisam.

`Evidência esperada`

- `Transition` e `TryTransition`;
- testes de transições válidas, inválidas e estados terminais.

`Notas LoopZero`

- esse gate é central porque API e operators compartilham lifecycle semantics.

## Gate PKG-G08: Error Typing Without Overfitting

`Objetivo`

Fornecer erro estruturado consistente sem amarrar todo o sistema a um único transporte.

`Critério de aprovação`

- erros carregam código e contexto suficiente;
- `errors.Is` e `errors.As` funcionam de maneira previsível;
- serialização existe quando necessária, mas não domina o design do tipo.

`Evidência esperada`

- `LoopZeroError`;
- `Unwrap`, `Is`, serialização JSON básica;
- testes de matching e wrapping.

`Notas LoopZero`

- esse pacote precisa servir API e outros consumidores sem virar DSL de HTTP.

## Gate PKG-G09: Observability Decoupling

`Objetivo`

Compartilhar observabilidade sem fixar cada processo à mesma stack concreta.

`Critério de aprovação`

- pacotes de logging/tracing usam abstrações pequenas;
- contexto observável é propagado sem dependência forte quando possível;
- enrichment é opcional e seguro.

`Evidência esperada`

- registration de extractor;
- logger context enrichment;
- ausência de import pesado desnecessário.

`Notas LoopZero`

- esse gate permite que API e operators usem o mesmo pacote sem acoplamento excessivo.

## Gate PKG-G10: Resilience Primitives Are Generic But Concrete

`Objetivo`

Permitir reuso de mecanismos de resiliência sem cair em abstrações vagas.

`Critério de aprovação`

- circuit breaker, aggregator e similares têm semântica clara;
- não tentam resolver todos os padrões de resiliência ao mesmo tempo;
- comportamento é previsível e testável.

`Evidência esperada`

- config pequena;
- máquina interna explícita;
- testes de transição de estado e thresholds.

`Notas LoopZero`

- `pkg/circuitbreaker` é um exemplo de bom escopo: pequeno, específico e útil.

## Gate PKG-G11: Security-Sensitive Packages Are Conservative

`Objetivo`

Aplicar maior rigor a pacotes que tocam autenticação, tokens e enforcement.

`Critério de aprovação`

- validação de JWT é explícita quanto a issuer, audience e algoritmo;
- rotation/TTL semantics são claras;
- enforcement falha fechado quando o contrato está ausente e isso for mais seguro.

`Evidência esperada`

- `pkg/jwt`;
- `pkg/enforcement`;
- testes cobrindo rotação, fallback e denial paths.

`Notas LoopZero`

- em PaaS, policy bug compartilhado vira falha sistêmica de produto.

## Gate PKG-G12: No “Shared Bucket” Entropy

`Objetivo`

Impedir que `pkg/` cresça por agregação oportunista.

`Critério de aprovação`

- novos pacotes entram com justificativa clara;
- helpers sem coesão são rejeitados ou mantidos no consumidor;
- a pasta continua navegável por domínio, não por coincidência.

`Evidência esperada`

- número pequeno de pacotes com nomes concretos;
- documentação de responsabilidade por pacote.

`Notas LoopZero`

- este gate protege o futuro da pasta mais do que o presente.

## Gate PKG-G13: Backward Compatibility Of Shared Contracts

`Objetivo`

Evitar que mudanças em `pkg/` quebrem múltiplos módulos silenciosamente.

`Critério de aprovação`

- mudanças em assinatura exportada ou semântica exigem revisão deliberada;
- contracts sensíveis têm testes nos próprios consumidores ou no pacote;
- o pacote evolui por adição preferencial, não por churn arbitrário.

`Evidência esperada`

- testes;
- uso concentrado em interfaces estáveis;
- package docs atualizadas quando o contrato muda.

`Notas LoopZero`

- impacto de mudança em `pkg/` é multiplicado; a barra de compatibilidade precisa ser mais alta.

## Ordem De Prioridade

1. `PKG-G01`, `PKG-G03`, `PKG-G04`, `PKG-G05`
2. `PKG-G06`, `PKG-G07`, `PKG-G08`, `PKG-G11`
3. `PKG-G02`, `PKG-G09`, `PKG-G10`, `PKG-G12`, `PKG-G13`

Justificativa:

- primeiro, escopo e invariantes corretos;
- depois, contratos concretos mais sensíveis;
- por fim, disciplina estrutural e evolução.
