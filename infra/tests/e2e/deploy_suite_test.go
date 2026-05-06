// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// E2E test suite for the LoopZero platform.
//
// This suite covers:
//   - Deploy happy path (existing tests)
//   - CLI command contracts (existing tests)
//   - Journey tests: full end-to-end flows migrated from bash scripts
//   - Edge cases: concurrency, failure modes, state machine transitions
//
// Run with Labels:
//
//	ginkgo --label-filter="smoke"    # Quick subset (<5 min)
//	ginkgo --label-filter="journey"  # All journeys
//	ginkgo --label-filter="edge-case" # Edge cases
//	ginkgo                           # Everything
//
// Requires: LOOPZERO_E2E=1, running k3d clusters, seeded E2E data.
package e2e

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/loopzero/loopzero/infra/tests/seed"
)

func TestDeployHappyPath(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reporterConfig := GinkgoConfiguration()

	// JUnit XML report if JUNIT_REPORT_PATH is set.
	if junitPath := os.Getenv("JUNIT_REPORT_PATH"); junitPath != "" {
		reporterConfig.JUnitReport = junitPath
	}

	RunSpecs(t, "LoopZero E2E Suite", suiteConfig, reporterConfig)
}

var _ = SynchronizedBeforeSuite(func() []byte {
	// First process: verify infrastructure health and seed data.
	if os.Getenv("LOOPZERO_E2E") == "" {
		Skip("Set LOOPZERO_E2E=1 to run E2E tests")
	}

	// Verify API is reachable.
	if err := verifyAPIHealth(); err != nil {
		Skip("Infrastructure not ready: " + err.Error())
	}

	// Verify clusters are running.
	if !clusterRunning() {
		Skip("kubectl cluster-info failed — clusters not running")
	}

	// Seed E2E data (idempotent).
	err := seed.Run(seed.Config{})
	Expect(err).NotTo(HaveOccurred(), "E2E seed failed")

	return nil
}, func(_ []byte) {
	// All processes: nothing additional needed.
})
