-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Migration 000047 — widen usage_records.quantity INTEGER -> BIGINT
-- (plan metering-compute-loop T2.2).
--
-- A compute counter (cpu_core_seconds summed over a calendar month across a
-- tenant's pods) can exceed the 2^31 INTEGER ceiling (~2.1e9): e.g. 1 core
-- reserved for a full 31-day month is ~2.68e6 core-seconds, and a tenant with
-- hundreds of cores crosses 2^31 within a period. BIGINT (2^63) removes the
-- overflow. Widening is non-lossy and holds no lock beyond a brief catalog
-- update in modern Postgres (the type is binary-coercible for stored values).

ALTER TABLE usage_records ALTER COLUMN quantity TYPE BIGINT;
