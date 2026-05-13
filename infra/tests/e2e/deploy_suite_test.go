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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// T2 amendment 2026-05-13 (finding-2026-05-13-e2e-token-refresh-divergence):
	// populate per-suite XDG_CONFIG_HOME with a freshly-issued JWT via
	// `loopzero login --token`. After this, all CLI subprocesses spawned by
	// runLoopZero* read the token from configStore (matching the production
	// path) instead of LOOPZERO_TOKEN env injection. The on401 → ensureFreshToken
	// pipeline now has access to the same configStore the CLI would use in
	// production (Rule 10 — local equals production).
	//
	// XDG_CONFIG_HOME isolation prevents the test from clobbering the
	// operator's real ~/.loopzero/config.json. The directory is recreated each
	// run for hermeticity. Set BEFORE the login subprocess starts so the
	// CLI writes into this isolated location.
	xdgDir, err := os.MkdirTemp("", "loopzero-e2e-config-")
	Expect(err).NotTo(HaveOccurred(), "create XDG_CONFIG_HOME tempdir")
	Expect(os.Setenv("XDG_CONFIG_HOME", xdgDir)).To(Succeed())
	GinkgoWriter.Printf("XDG_CONFIG_HOME=%s\n", xdgDir)

	token := resolveToken()
	Expect(token).NotTo(BeEmpty(), "resolveToken returned empty — no dogfood JWT available")
	loginCmd := exec.Command("loopzero", "login",
		"--token", token,
		"--expires-in", "3600",
		"--json",
	)
	loginCmd.Env = append(os.Environ(),
		"LOOPZERO_API_URL="+apiURL(),
		"XDG_CONFIG_HOME="+xdgDir,
	)
	loginOut, loginErr := loginCmd.CombinedOutput()
	Expect(loginErr).NotTo(HaveOccurred(),
		"loopzero login --token failed: %s", string(loginOut))
	// Sanity: configStore file exists at expected path.
	cfgPath := filepath.Join(xdgDir, "loopzero", "config.json")
	if _, statErr := os.Stat(cfgPath); statErr != nil {
		// Fallback path (non-XDG mode would write to baseDir/.loopzero).
		cfgPath = filepath.Join(xdgDir, ".loopzero", "config.json")
	}
	Expect(cfgPath).To(BeAnExistingFile(),
		fmt.Sprintf("loopzero login completed but configStore file missing under %s", xdgDir))
	GinkgoWriter.Printf("loopzero login wrote config to %s\n", cfgPath)

	return nil
}, func(_ []byte) {
	// All processes: nothing additional needed.
})
