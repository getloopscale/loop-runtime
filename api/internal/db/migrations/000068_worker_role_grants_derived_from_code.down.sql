-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Rollback of 000068. Revokes exactly what 000068 granted, which returns
-- loopruntime_worker to the privileges 000023 and 000046 left: the bootstrap path and
-- the deploy outbox are refused again on the cross-tenant pool, and admin
-- eviction fails at its metering-cursor step and rolls back.

REVOKE DELETE ON metering_cursor FROM loopruntime_worker;
REVOKE USAGE ON SEQUENCE deploy_events_id_seq FROM loopruntime_worker;
REVOKE INSERT ON deploy_events FROM loopruntime_worker;
REVOKE SELECT, INSERT ON accounts FROM loopruntime_worker;
