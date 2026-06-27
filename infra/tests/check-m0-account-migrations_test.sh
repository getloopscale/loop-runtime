#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# M0 account-level-tenancy migrations (000034-000039) integration test:
# clones the real pg-cloud schema into a throwaway DB, seeds a multi-tenant
# fixture, applies the M0 up-migrations, asserts the backfill linkage, then
# applies the down-migrations and asserts full reversibility.
#
# Hermetic-ish: runs entirely inside a reachable pg-cloud container. Skips
# (exit 0) when no such container is present (e.g. unit-only CI), so it never
# yields a false RED — mirror of the Go e2e E2E_PG_CLOUD_DSN skip contract.
#
# Override the container with M0_TEST_PG_CONTAINER. DB user/name = loopzero.
set -uo pipefail

C="${M0_TEST_PG_CONTAINER:-loopzero-workspace-pg-cloud-1}"
MIG="$(cd "$(dirname "$0")/../../api/internal/db/migrations" && pwd)"
PSQL="docker exec -i $C psql -U loopzero -d m0test -v ON_ERROR_STOP=1 -q"

if ! command -v docker >/dev/null 2>&1 || ! docker inspect "$C" >/dev/null 2>&1; then
  echo "SKIP: pg-cloud container '$C' not running — M0 migration itest skipped"
  exit 0
fi
fail(){ echo "FAIL: $*"; docker exec "$C" dropdb -U loopzero --if-exists m0test >/dev/null 2>&1; exit 1; }

docker exec "$C" dropdb -U loopzero --if-exists m0test >/dev/null 2>&1
docker exec "$C" createdb -U loopzero m0test || fail "createdb"
docker exec "$C" sh -c 'pg_dump -U loopzero --schema-only loopzero | psql -U loopzero -d m0test -q -v ON_ERROR_STOP=1' >/dev/null 2>&1 || fail "schema clone"

$PSQL <<'SQL' || fail "seed"
INSERT INTO users (id,email,display_name,username) VALUES
  ('usrA','a@x.io','User A','usera'), ('usrB','b@x.io','User B','userb');
INSERT INTO tenants (id,display_name,slug,owner_id,cell_id) VALUES
  ('tn-a1','TA1','ta1','usrA','cell-1'),
  ('tn-a2','TA2','ta2','usrA','cell-1'),
  ('tn-b1','TB1','tb1','usrB','cell-1');
INSERT INTO loopzero_api_keys (id,subject,tenant_id,key_name,key_hash,key_prefix,scopes) VALUES
  ('apk-a1','usrA','tn-a1','kA','hashA','loopzero_live_AA','["memory:read"]'),
  ('apk-b1','usrB','tn-b1','kB','hashB','loopzero_live_BB','["memory:read"]');
SQL

[ "$(docker exec "$C" psql -U loopzero -d m0test -tAc "SELECT to_regclass('public.accounts') IS NOT NULL")" = "f" ] \
  || fail "RED precondition: accounts already present"

for n in 000034_create_accounts 000035_tenants_account_id 000036_loopzero_api_keys_account_id \
         000037_backfill_accounts 000038_link_tenants_account 000039_link_keys_account; do
  $PSQL < "$MIG/${n}.up.sql" || fail "apply $n up"
done

$PSQL <<'SQL' || fail "backfill assertions"
DO $$
DECLARE v text; n int;
BEGIN
  IF to_regclass('public.accounts') IS NULL THEN RAISE EXCEPTION 'accounts missing'; END IF;
  SELECT is_nullable INTO v FROM information_schema.columns WHERE table_name='tenants' AND column_name='account_id';
  IF v <> 'YES' THEN RAISE EXCEPTION 'tenants.account_id not nullable (%)', v; END IF;
  SELECT is_nullable INTO v FROM information_schema.columns WHERE table_name='loopzero_api_keys' AND column_name='account_id';
  IF v <> 'YES' THEN RAISE EXCEPTION 'loopzero_api_keys.account_id not nullable (%)', v; END IF;
  SELECT count(*) INTO n FROM accounts; IF n <> 2 THEN RAISE EXCEPTION 'expected 2 accounts, got %', n; END IF;
  SELECT count(*) INTO n FROM (SELECT owner_id FROM accounts GROUP BY owner_id HAVING count(*)>1) x;
  IF n <> 0 THEN RAISE EXCEPTION 'duplicate account per owner'; END IF;
  SELECT count(*) INTO n FROM tenants WHERE account_id IS NULL; IF n<>0 THEN RAISE EXCEPTION '% tenants unlinked', n; END IF;
  SELECT count(*) INTO n FROM tenants t JOIN accounts a ON a.id=t.account_id WHERE a.owner_id<>t.owner_id;
  IF n<>0 THEN RAISE EXCEPTION '% tenants linked to wrong owner', n; END IF;
  SELECT count(*) INTO n FROM loopzero_api_keys WHERE account_id IS NULL; IF n<>0 THEN RAISE EXCEPTION '% keys unlinked', n; END IF;
  SELECT count(*) INTO n FROM loopzero_api_keys k JOIN tenants t ON t.id=k.tenant_id WHERE k.account_id<>t.account_id;
  IF n<>0 THEN RAISE EXCEPTION '% keys linked to wrong account', n; END IF;
END $$;
SQL

# idempotency: re-running the backfill must not create duplicates
$PSQL < "$MIG/000037_backfill_accounts.up.sql" || fail "re-backfill"
[ "$(docker exec "$C" psql -U loopzero -d m0test -tAc "SELECT count(*) FROM accounts")" = "2" ] || fail "backfill not idempotent"

for n in 000039_link_keys_account 000038_link_tenants_account 000037_backfill_accounts \
         000036_loopzero_api_keys_account_id 000035_tenants_account_id 000034_create_accounts; do
  $PSQL < "$MIG/${n}.down.sql" || fail "apply $n down"
done

$PSQL <<'SQL' || fail "reverse assertions"
DO $$
DECLARE n int;
BEGIN
  IF to_regclass('public.accounts') IS NOT NULL THEN RAISE EXCEPTION 'accounts still present after down'; END IF;
  SELECT count(*) INTO n FROM information_schema.columns WHERE table_name='tenants' AND column_name='account_id';
  IF n<>0 THEN RAISE EXCEPTION 'tenants.account_id still present'; END IF;
  SELECT count(*) INTO n FROM information_schema.columns WHERE table_name='loopzero_api_keys' AND column_name='account_id';
  IF n<>0 THEN RAISE EXCEPTION 'loopzero_api_keys.account_id still present'; END IF;
END $$;
SQL

docker exec "$C" dropdb -U loopzero --if-exists m0test >/dev/null 2>&1
echo "PASS: M0 account migrations backfill + round-trip reversibility verified"
