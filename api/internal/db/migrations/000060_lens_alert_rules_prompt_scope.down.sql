-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- Reverte 000060. A ordem importa: as regras escopadas a prompt precisam SAIR
-- antes de o CHECK estreitar, senão o próprio ALTER falha validando linhas que
-- o novo CHECK rejeita.
--
-- O histórico em lens_fired_alerts é apagado junto, e isso é deliberado: um
-- incidente de uma métrica que deixou de existir não tem como ser lido depois.
-- Mesma escolha que 000059 fez ao reescrever 'guard_breach' → 'monitor_breach'.

SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

DELETE FROM lens_fired_alerts WHERE metric = 'prompt_resolutions';
DELETE FROM lens_alert_rules  WHERE metric = 'prompt_resolutions';

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_prompt_pairing_check;
DROP INDEX IF EXISTS idx_lens_alert_rules_prompt;

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));

ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));

ALTER TABLE lens_alert_rules DROP COLUMN IF EXISTS prompt_name;
