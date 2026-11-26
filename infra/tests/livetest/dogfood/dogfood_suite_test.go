// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.2 of plan canonical-tests-rewrite-v2 — Ginkgo suite bootstrap for the
// dogfood LIVE journeys. Without this RunSpecs entry point the 25 journey
// `Describe` blocks are never executed (`go test` finds 0 specs → false-green,
// the "Suite skipped in BeforeSuite" trap in CLAUDE.md § Common Mistakes).
//
// Runs ONLY under -tags=live against a live dev-public cluster:
//   go test -tags=live ./infra/tests/livetest/dogfood/...

package dogfood

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDogfoodLive(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dogfood LIVE Suite (canonical-tests-rewrite-v2 T3.2)")
}
