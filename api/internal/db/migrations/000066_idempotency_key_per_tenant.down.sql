-- Volta a chave primária para `(key)` — global, como era.
--
-- Rollback real, não no-op: nada depende da chave nova, e deixar meia decisão
-- para trás é pior do que devolver o estado que a migração de subida encontrou.
--
-- ATENÇÃO ao rodar: se dois inquilinos já gravaram a MESMA chave sob a chave
-- ampliada, este `ADD CONSTRAINT` falha — e falhar é o comportamento correto.
-- Estreitar uma chave única pode colidir; o erro nomeia exatamente as linhas
-- que a versão antiga não conseguia representar.
BEGIN;

ALTER TABLE idempotency_keys
  DROP CONSTRAINT IF EXISTS idempotency_keys_pkey;

ALTER TABLE idempotency_keys
  ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);

COMMIT;
