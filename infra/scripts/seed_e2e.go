// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package main is a thin wrapper around the seed package for standalone use.
//
// It delegates all seeding logic to infra/scripts/seed and adds an optional
// API health check. This avoids duplicating SQL, Tenant CR, and namespace
// polling logic that already lives in the seed package.
//
// Usage:
//
//	DATABASE_URL=postgres://... API_URL=http://localhost:3001 go run infra/scripts/seed_e2e.go
//
// Exit codes:
//
//	0 — seed complete, namespaces exist
//	1 — timeout or fatal error
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:3001"
	}

	cfg := seed.Config{
		DatabaseURL: dbURL,
		// KubeContext and ReconcileTimeout use seed package defaults
	}

	fmt.Println("[seed] Starting E2E seed...")

	if err := seed.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[seed] FATAL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[seed] Database seeded, Tenant CR applied, namespaces reconciled")

	// Optional health check — not fatal since API might not be running yet
	if err := checkAPIHealth(apiURL); err != nil {
		fmt.Fprintf(os.Stderr, "[seed] WARNING: API health check failed: %v\n", err)
	} else {
		fmt.Println("[seed] API health check passed")
	}

	fmt.Println("[seed] E2E seed complete")
}

// checkAPIHealth verifies the API server is accessible.
func checkAPIHealth(apiURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiURL + "/api/v1/health")
	if err != nil {
		return fmt.Errorf("health request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}
