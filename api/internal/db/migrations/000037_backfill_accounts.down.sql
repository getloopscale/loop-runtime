-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- 000037 is the account-POPULATION migration; its down DEPOPULATES the accounts
-- table (the inverse). After 000039.down/000038.down have re-NULLed the tenant
-- and key links, this removes the account rows.
--
-- NOTE (dev-only rollback semantics): this cannot distinguish backfilled rows
-- from accounts later created by the app (EnsureAccount runs inside EnsureTenant,
-- so every account owner also owns a tenant). A PARTIAL rollback to 000036 while
-- the app has been running on >=000039 therefore clears app-created accounts too.
-- The supported rollback is FULL (down to 000033), where 000034.down drops the
-- table anyway. Do NOT use a partial rollback to 000036 against a live dataset.
DELETE FROM accounts;
