// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/exposure"
)

// Hosts returns, in declaration order and without repeats, every host the
// exposure inventory declares.
//
// Measured 2026-08-18: app.getloopscale.com, cloud.getloopscale.com and auth.getloopscale.com
// — the names WITHOUT the `-dev` suffix — had their own Caddyfile blocks and
// inventory rows, and none of them had ever been probed: Targets filters by one
// host and the probe defaulted to app-dev/auth-dev. Measured live, they were
// production hostnames serving development instances (401 on
// cloud.getloopscale.com/v1/billing, 200 on auth.getloopscale.com/health/ready).
//
// Listing from the inventory rather than naming those three keeps the fourth
// future host from going invisible for the same reason.
func Hosts(inventory string) ([]string, error) {
	decls, err := loadInventory(inventory)
	if err != nil {
		return nil, err
	}
	var hosts []string
	seen := map[string]bool{}
	for _, d := range decls {
		if !seen[d.Site] {
			seen[d.Site] = true
			hosts = append(hosts, d.Site)
		}
	}
	return hosts, nil
}

// loadInventory reads the exposure inventory through the parser the
// proxy-exposure gate uses, and refuses a malformed row: a row the probe cannot
// read is a declared route it would silently not probe.
func loadInventory(path string) ([]exposure.Declaration, error) {
	decls, malformed, err := exposure.Load(path)
	if err != nil {
		return nil, err
	}
	if len(malformed) > 0 {
		return nil, fmt.Errorf("exposure inventory %s has malformed rows: %s", path, strings.Join(malformed, "; "))
	}
	return decls, nil
}
