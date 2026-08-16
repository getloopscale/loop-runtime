-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- Reverte 000061 ATE ONDE E HONESTO REVERTER.
--
-- A fase 1 da subida foi uma FUSAO: as linhas que estavam em `cell-default` e as que estavam em
-- `cell-1` viraram todas `cell-0`, e a informacao de qual era qual nao existe mais em lugar nenhum.
-- Um down que "desfizesse" isso teria de inventar a divisao, e um down que inventa dados e pior do
-- que um down que nao existe: ele devolve um banco que parece o antigo e nao e.
--
-- Entao este down faz o que da para fazer sem mentir:
--   * devolve a numeracao (`cell-1` volta a ser `cell-2`, `cell-0` volta a ser `cell-default`);
--   * devolve os defaults e o gatilho a forma anterior, com a sentinela;
--   * NAO recria a linha `cell-1` do catalogo, e NAO tenta separar as linhas fundidas.
-- O resultado e o esquema antigo com os dados fundidos. Quem precisar do estado exato anterior
-- restaura backup — que e a operacao correta para desfazer uma fusao.

UPDATE tenants                  SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE builds                   SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE releases                 SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE leader_generations       SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE preview_environments     SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE release_app_attestations SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE cells SET cell_id = 'cell-2', display_name = 'Cell 2 (runtime-2)' WHERE cell_id = 'cell-1';

UPDATE tenants                  SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE builds                   SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE releases                 SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE leader_generations       SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE preview_environments     SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE release_app_attestations SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE cells SET cell_id = 'cell-default', display_name = 'Default Cell' WHERE cell_id = 'cell-0';

ALTER TABLE tenants                  ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE leader_generations       ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE builds                   ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE releases                 ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE preview_environments     ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE release_app_attestations ALTER COLUMN cell_id SET DEFAULT 'cell-default';

CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id = 'cell-default' OR NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN NEW.cell_id := 'cell-default'; END IF;
    END IF;
    RETURN NEW;
END;
$$;
