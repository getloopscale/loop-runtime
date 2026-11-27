// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import "os"

// fixtureDBURL is where the store's database tests seed and run. Their fixtures write
// rows of arbitrary tenants with no tenant scope, which under fail-closed RLS only the
// cross-tenant worker role admits — the owner lost its maintenance policies in 000064
// (#387). DATABASE_URL remains the fallback for a database without the role split.
func fixtureDBURL() string {
	if u := os.Getenv("LOOP_RUNTIME_WORKER_DATABASE_URL"); u != "" {
		return u
	}
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@localhost:5432/loop_runtime_test?sslmode=disable"
}
