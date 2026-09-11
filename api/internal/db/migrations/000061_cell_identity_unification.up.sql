-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- Unifica cell e cluster em 1:1. ADR docs/decisions.md,
-- BACKLOG.md B-005.
--
-- O catalogo tinha TRES cells para DUAS maquinas. Tres fontes independentes mediram que
-- `cell-default` e `cell-1` sao o mesmo cluster sob dois nomes:
--   * infra/helmfile/environments/dev.yaml       — "runtime cluster maps to cell-1"
--   * infra/helmfile/environments/drill.yaml     — "cell-default + cell-1 -> runtimeCluster"
--   * 000006_cells.up.sql:120                    — cell-1 e "Canonical first cell. Maps to runtime-1"
-- e a auditoria LIVE de 2026-05-01 encontrou tres cells `active` para DOIS ClusterCredential.
-- A terceira identidade nunca teve maquina.
--
-- Por isso esta migracao FUNDE identidades, e nao apenas renomeia. A ordem das duas fases e
-- OBRIGATORIA: `cell-2` so pode virar `cell-1` DEPOIS que o `cell-1` antigo deixou de existir.
-- Fazer as duas ao mesmo tempo colapsaria identidades distintas sem deixar rastro.
--
-- Nao ha chave estrangeira para `cells(cell_id)` em nenhuma das seis colunas (`git grep
-- "REFERENCES cells"` nao devolve nada), entao a fusao e um conjunto de UPDATEs e nao exige
-- janela de indisponibilidade. Isso tambem significa que nada no banco impede um `cell_id`
-- orfao — o motivo pelo qual a fase 4 troca a sentinela por uma falha explicita.

-- ---------------------------------------------------------------------------------------------
-- Fase 1 — funde `cell-default` + `cell-1` na cell que a maquina realmente e: `cell-0`.
-- ---------------------------------------------------------------------------------------------
UPDATE tenants                  SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE builds                   SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE releases                 SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE leader_generations       SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE preview_environments     SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE release_app_attestations SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');

-- As linhas de dados ja sairam das duas identidades acima, entao apagar as duplicatas e
-- seguro. `cell-1` so pode cair AQUI enquanto ainda significa a identidade antiga — depois
-- da fase 2 ela passa a ser a renumeracao de `cell-2`, e apaga-la seria destruir a segunda
-- cell. E por isso que a guarda abaixo existe: este migrador REAPLICA todos os arquivos a
-- cada execucao (infra/scripts/production/run-migrations.sh), entao a 000006 volta a
-- inserir `cell-default`/`cell-1`/`cell-2` e esta migracao roda de novo por cima. Medido no
-- droplet app-dev em 2026-08-10: sem a guarda, a segunda passagem tentava renomear
-- `cell-default` para um `cell-0` que ja existia. Toda esta fase e escrita para ser
-- re-executavel sem mudar o resultado.
DELETE FROM cells WHERE cell_id = 'cell-1'
   AND EXISTS (SELECT 1 FROM cells c WHERE c.cell_id = 'cell-2');

UPDATE cells
   SET cell_id      = 'cell-0',
       display_name = 'Cell 0',
       notes        = 'Primeira cell. 1:1 com o cluster loopzero-cell-0. Absorveu cell-default '
                   || '(que era sentinela de "ainda nao resolvido", nao uma cell) e cell-1 '
                   || '(que nomeava esta mesma maquina). ADR 2026-08-09.'
 WHERE cell_id = 'cell-default'
   AND NOT EXISTS (SELECT 1 FROM cells c WHERE c.cell_id = 'cell-0');

-- Se `cell-0` ja existia (re-execucao), a linha antiga apenas desaparece.
DELETE FROM cells WHERE cell_id = 'cell-default';

-- ---------------------------------------------------------------------------------------------
-- Fase 2 — so agora `cell-1` esta livre. Renumera `cell-2`, preservando o `status` dela.
-- ---------------------------------------------------------------------------------------------
UPDATE tenants                  SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE builds                   SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE releases                 SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE leader_generations       SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE preview_environments     SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE release_app_attestations SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';

UPDATE cells
   SET cell_id      = 'cell-1',
       display_name = 'Cell 1',
       notes        = 'Segunda cell. 1:1 com o cluster loopzero-cell-1. Era cell-2; renumerada '
                   || 'quando a fusao liberou o numero. O status nao muda aqui — quem admite '
                   || 'trafego continua sendo decidido pelo canary. ADR 2026-08-09.'
 WHERE cell_id = 'cell-2'
   AND NOT EXISTS (SELECT 1 FROM cells c WHERE c.cell_id = 'cell-1');

-- Idem: numa re-execucao `cell-1` ja e a renumerada, e a `cell-2` reinserida pela 000006
-- so precisa sumir.
DELETE FROM cells WHERE cell_id = 'cell-2';

-- ---------------------------------------------------------------------------------------------
-- Fase 3 — os defaults deixam de gravar uma sentinela.
--
-- `tenants` e `leader_generations` NAO tem o gatilho (tenants e a fonte de onde ele le), entao
-- precisam de um default real: `cell-0`, que agora e uma cell de verdade e nao um marcador.
-- As quatro tabelas COM gatilho perdem o default de proposito — omitir a coluna passa a produzir
-- NULL, que e o unico sinal de "ainda nao resolvido" a partir daqui.
-- ---------------------------------------------------------------------------------------------
ALTER TABLE tenants                  ALTER COLUMN cell_id SET DEFAULT 'cell-0';
ALTER TABLE leader_generations       ALTER COLUMN cell_id SET DEFAULT 'cell-0';

ALTER TABLE builds                   ALTER COLUMN cell_id DROP DEFAULT;
ALTER TABLE releases                 ALTER COLUMN cell_id DROP DEFAULT;
ALTER TABLE preview_environments     ALTER COLUMN cell_id DROP DEFAULT;
ALTER TABLE release_app_attestations ALTER COLUMN cell_id DROP DEFAULT;

-- ---------------------------------------------------------------------------------------------
-- Fase 4 — o gatilho para de usar valor magico, e passa a falhar alto.
--
-- Antes, `cell-default` significava DUAS coisas ao mesmo tempo: "heranca pendente" e "ultimo
-- recurso quando o tenant nao tem cell". A fusao torna isso insustentavel — `cell-0` agora e uma
-- cell real com tenants, e mante-la como sentinela faria o gatilho SOBRESCREVER toda linha
-- legitimamente colocada em cell-0.
--
-- `NULL` passa a ser o unico sinal de "nao resolvido". O gatilho e BEFORE INSERT e roda antes da
-- checagem de NOT NULL, entao um INSERT que omita a coluna e resolvido antes de a restricao
-- olhar para a linha — comportamento documentado do Postgres, e provado por
-- api/tests/integration/cell_identity_unification_test.go, que existe porque afirmar sem provar e o que
-- este trabalho evita.
--
-- E quando o tenant tambem nao tem cell, a linha deixa de ser gravada. Um build cuja cell nao
-- resolve e um build que nao deveria existir; grava-lo com uma sentinela empurra o defeito para
-- o deploy, onde ele aparece como roteamento errado em vez de erro de dados
-- (.claude/rules/error-handling.md § 2 — falhar rapido, alto e claro).
CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN
            RAISE EXCEPTION
                'cell_id nao resolvido para tenant % na tabela %: o tenant nao existe ou nao tem cell',
                NEW.tenant_id, TG_TABLE_NAME;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
