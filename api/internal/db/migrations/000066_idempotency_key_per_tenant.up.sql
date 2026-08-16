-- A chave de idempotência era única GLOBALMENTE, e quem a escolhe é o cliente.
--
-- `idempotency_keys_pkey` cobria `(key)` e nada mais. O valor vem do header
-- `Idempotency-Key` da requisição — texto arbitrário que o cliente inventa. Duas
-- consequências, e a segunda é a que importa:
--
--   1. Dois clientes que escolham a mesma string disputam a mesma chave primária.
--      Não é hipótese remota: `deploy-1`, a data do dia, o nome do branch. O
--      segundo recebe violação de unicidade DEPOIS que o trabalho já rodou —
--      o registro é gravado após a resposta —, então o efeito colateral
--      aconteceu e a requisição termina em erro.
--   2. O espaço de nomes é ocupável de propósito. Um cliente pode gravar chaves
--      até cobrir o que outro provavelmente usaria, e negar-lhe o serviço sem
--      tocar em nada dele.
--
-- O que NÃO acontecia: vazamento de resposta entre inquilinos. A leitura
-- (`GetIdempotencyRecord`) sempre casou `key = $1 AND tenant_id = $2`, então
-- ninguém jamais recebeu a resposta gravada por outro. O defeito é de
-- disponibilidade e de acoplamento, não de confidencialidade — e dizer qual dos
-- dois é importa mais do que o susto.
--
-- A unicidade sempre foi por inquilino; a coluna simplesmente não estava na
-- chave. Alargá-la nunca cria colisão: toda linha única sob `(key)` continua
-- única sob `(tenant_id, key)`.
--
-- Forward-only. Nenhuma FK aponta para esta tabela (verificado em pg_constraint),
-- então trocar a chave primária não tem efeito em cascata.
--
-- @see api/internal/db/generated/idempotency.sql.go — as consultas, já escopadas
-- @see api/internal/db/migrations/000062_rls_fail_closed.up.sql — a policy da tabela

BEGIN;

ALTER TABLE idempotency_keys
  DROP CONSTRAINT IF EXISTS idempotency_keys_pkey;

ALTER TABLE idempotency_keys
  ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (tenant_id, key);

COMMIT;
