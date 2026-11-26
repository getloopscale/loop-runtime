-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Rewrites in English the text migration 000061 left in the database (#380).
--
-- 000061 defined the cell trigger's error message and the notes of the two unified cells in
-- Portuguese. It is applied history and is not edited: a changed migration file does not reach
-- a database that already ran it, so the only way to change what those databases hold is a new
-- migration. This one replaces:
--
--   * the RAISE message of populate_cell_id_from_tenant(). The logic is 000061's, unchanged —
--     the trigger still fails loudly when neither the row nor its tenant resolves a cell;
--   * the notes of cell-0 and cell-1, ONLY where they still hold the exact text 000061 wrote.
--     A note an operator has edited since is theirs, and is left alone.
CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN
            RAISE EXCEPTION
                'cell_id not resolved for tenant % in table %: the tenant does not exist or has no cell',
                NEW.tenant_id, TG_TABLE_NAME;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

UPDATE cells
   SET notes = 'First cell. 1:1 with the loopruntime-cell-0 cluster. Absorbed cell-default '
            || '(which was the sentinel for "not resolved yet", not a cell) and cell-1 '
            || '(which named this same machine). ADR 2026-08-09.'
 WHERE cell_id = 'cell-0'
   AND notes = 'Primeira cell. 1:1 com o cluster loopruntime-cell-0. Absorveu cell-default ' -- english-only: matches the verbatim Portuguese text migration 000061 wrote
            || '(que era sentinela de "ainda nao resolvido", nao uma cell) e cell-1 ' -- english-only: matches the verbatim Portuguese text migration 000061 wrote
            || '(que nomeava esta mesma maquina). ADR 2026-08-09.'; -- english-only: matches the verbatim Portuguese text migration 000061 wrote

UPDATE cells
   SET notes = 'Second cell. 1:1 with the loopruntime-cell-1 cluster. Was cell-2; renumbered '
            || 'when the merge freed the number. The status does not change here — whether it '
            || 'admits traffic is still decided by the canary. ADR 2026-08-09.'
 WHERE cell_id = 'cell-1'
   AND notes = 'Segunda cell. 1:1 com o cluster loopruntime-cell-1. Era cell-2; renumerada ' -- english-only: matches the verbatim Portuguese text migration 000061 wrote
            || 'quando a fusao liberou o numero. O status nao muda aqui — quem admite ' -- english-only: matches the verbatim Portuguese text migration 000061 wrote
            || 'trafego continua sendo decidido pelo canary. ADR 2026-08-09.'; -- english-only: matches the verbatim Portuguese text migration 000061 wrote
