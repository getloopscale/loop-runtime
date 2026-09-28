-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- Returns the trigger message and the cell notes to the text migration 000061 wrote.
--
-- A real rollback, not a no-op: the state the up migration found is 000061's, Portuguese
-- included. Notes are restored only where they still hold the English text the up migration
-- wrote, so an operator's later edit survives the rollback as it survived the up.
CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN
            RAISE EXCEPTION
                'cell_id nao resolvido para tenant % na tabela %: o tenant nao existe ou nao tem cell', -- english-only: rollback restores the verbatim Portuguese text of migration 000061
                NEW.tenant_id, TG_TABLE_NAME;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

UPDATE cells
   SET notes = 'Primeira cell. 1:1 com o cluster loopzero-cell-0. Absorveu cell-default ' -- english-only: rollback restores the verbatim Portuguese text of migration 000061
            || '(que era sentinela de "ainda nao resolvido", nao uma cell) e cell-1 ' -- english-only: rollback restores the verbatim Portuguese text of migration 000061
            || '(que nomeava esta mesma maquina). ADR 2026-08-09.' -- english-only: rollback restores the verbatim Portuguese text of migration 000061
 WHERE cell_id = 'cell-0'
   AND notes = 'First cell. 1:1 with the loopzero-cell-0 cluster. Absorbed cell-default '
            || '(which was the sentinel for "not resolved yet", not a cell) and cell-1 '
            || '(which named this same machine). ADR 2026-08-09.';

UPDATE cells
   SET notes = 'Segunda cell. 1:1 com o cluster loopzero-cell-1. Era cell-2; renumerada ' -- english-only: rollback restores the verbatim Portuguese text of migration 000061
            || 'quando a fusao liberou o numero. O status nao muda aqui — quem admite ' -- english-only: rollback restores the verbatim Portuguese text of migration 000061
            || 'trafego continua sendo decidido pelo canary. ADR 2026-08-09.' -- english-only: rollback restores the verbatim Portuguese text of migration 000061
 WHERE cell_id = 'cell-1'
   AND notes = 'Second cell. 1:1 with the loopzero-cell-1 cluster. Was cell-2; renumbered '
            || 'when the merge freed the number. The status does not change here — whether it '
            || 'admits traffic is still decided by the canary. ADR 2026-08-09.';
