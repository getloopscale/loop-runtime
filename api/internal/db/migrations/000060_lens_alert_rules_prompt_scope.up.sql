-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- ============================================================================
-- 000060_lens_alert_rules_prompt_scope
--
-- loopzero-promptly M21 DoD-5 — "um monitor avisa quando a produção muda".
--
-- A cadeia monitor → incidente → webhook JÁ EXISTE (000054 / 000056 + o
-- avaliador em loopzero-cloud/internal/lens_alert_evaluator). O que falta é
-- DIMENSIONAL, não estrutural: as métricas de hoje são do workspace inteiro, e
-- a métrica do M15 é por PROMPT. Ver ADR 0004 do umbrella e getloopscale/loopzero-cloud#450.
--
-- Aditiva por construção: coluna nova anulável + CHECK ampliado. Nenhuma regra
-- existente muda de comportamento, e uma regra escopada a prompt é
-- irrepresentável sem o nome do prompt (ver o CHECK de pareamento abaixo).
--
-- Mesma disciplina de lock que 000058/000059: o risco é a AQUISIÇÃO do lock
-- atrás de uma sessão idle-in-transaction, não a duração da varredura. Um
-- timeout aqui é o resultado ESPERADO sob contenção — tente de novo, nunca
-- aumente o teto.
-- ============================================================================

SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

-- 1. A dimensão que falta.
ALTER TABLE lens_alert_rules ADD COLUMN IF NOT EXISTS prompt_name TEXT;

-- 2. Os DOIS CHECKs de metric andam JUNTOS. Não é zelo — é a lição que 000058
--    documenta e que 000059 seguiu: lens_fired_alerts é a metade perigosa. O
--    CHECK dela rejeita o INSERT de histórico que roda DENTRO da transação de
--    disparo, então o rollback (correto) desfaz o flip de status e a regra
--    tenta para sempre, sem nunca entregar — enquanto a tela do cliente lê 'OK'.
--    A partir desta migração isso deixa de depender de disciplina do autor:
--    lens_alert_metric_lockstep_test.go trava o pareamento.
ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach', 'prompt_resolutions'));

ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach', 'prompt_resolutions'));

-- 3. O pareamento, pelo mesmo motivo de 000058: sem ele DOIS estados são
--    representáveis e AMBOS falham em silêncio.
--
--    (a) metric='prompt_resolutions' com prompt_name NULL — o avaliador não tem
--        o que consultar no loopzero-lens e pula a regra para sempre; o cliente vê
--        uma regra armada que nunca dispara.
--    (b) prompt_name preenchido numa regra de 'cost' — simplesmente ignorado, e
--        o próximo leitor não sabe dizer se o escopo valia ou não.
--
--    Uma linha de DDL torna os dois irrepresentáveis na única camada capaz de
--    impor isso de verdade.
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_prompt_pairing_check
  CHECK ((metric = 'prompt_resolutions') = (prompt_name IS NOT NULL));

-- 4. Índice parcial: só regras escopadas a prompt são consultadas por prompt.
CREATE INDEX IF NOT EXISTS idx_lens_alert_rules_prompt
  ON lens_alert_rules (prompt_name) WHERE prompt_name IS NOT NULL;
