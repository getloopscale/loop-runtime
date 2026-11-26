// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import "os"

// secretL19DBURL returns the PG connection string for real-PG-gated root-db
// tests. Relocated here (from secret_wrapper_test.go, which moved to db/store/
// in M9 5c) so the remaining root consumers (releases_active_unique_test,
// loopruntime_worker_role_test) keep the shared helper. Mirrors the store-package
// self-contained copy.
func secretL19DBURL() string {
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://postgres:postgres@localhost:5432/loopruntime_test?sslmode=disable"
}
