// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package dbtx

// PlacementLockID is the pg_advisory_xact_lock key every writer of
// tenants.cell_id takes before deciding where a tenant lives: the placement
// engine when it assigns a cell, and the admin pin when it moves one. Both read
// a cell's tenant count and then write, so they must serialize on one key or a
// capacity check can be passed by two writers at once.
//
// It lives here, in the SQL-access port, so both the placement package and the
// store can name it without either importing the other.
const PlacementLockID int64 = 0x706C636D746C636B
