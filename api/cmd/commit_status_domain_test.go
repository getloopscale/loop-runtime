// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

// The commit status a push deploy leaves on GitHub links the deployed app. It was
// composed on BASE_DOMAIN, the internal placeholder, so on staging it pointed at
// <app>.staging.getloopscale.com, which nothing serves, while the app answers on
// LOOPZERO_PUBLIC_DOMAIN (<app>.apps.staging.getloopscale.com).
func TestCommitStatusLinksTheAppOnThePublicDomain(t *testing.T) {
	cfg := config.Config{BaseDomain: "staging.example.test", PublicDomain: "apps.staging.example.test"}

	if got := commitStatusDomain(cfg); got != "apps.staging.example.test" {
		t.Errorf("commitStatusDomain = %q, want the public domain", got)
	}
}

func TestCommitStatusFallsBackToTheBaseDomainWithoutAPublicOne(t *testing.T) {
	cfg := config.Config{BaseDomain: "local.example.test"}

	if got := commitStatusDomain(cfg); got != "local.example.test" {
		t.Errorf("commitStatusDomain = %q, want the base domain", got)
	}
}
