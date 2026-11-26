// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// E2E test suite for the LoopRuntime platform.
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
// Requires: LOOPRUNTIME_E2E=1, running k3d clusters, seeded E2E data.
package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

func TestDeployHappyPath(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reporterConfig := GinkgoConfiguration()

	// JUnit XML report if JUNIT_REPORT_PATH is set.
	if junitPath := os.Getenv("JUNIT_REPORT_PATH"); junitPath != "" {
		reporterConfig.JUnitReport = junitPath
	}

	RunSpecs(t, "LoopRuntime E2E Suite", suiteConfig, reporterConfig)
}

// loudSkip prints an unmissable banner to stderr before skipping the whole
// suite. A bare BeforeSuite Skip reports SUCCESS with 0 specs — the documented
// "looks GREEN but ran nothing" false-green trap (CLAUDE.md e2e onboarding).
// The banner makes it obvious in CI logs that nothing actually ran, so a green
// run cannot be mistaken for a passing suite.
func loudSkip(reason string) {
	banner := "\n" +
		"════════════════════════════════════════════════════════════════════\n" +
		"  ⚠  LOOPRUNTIME E2E SUITE SKIPPED — 0 specs ran. THIS IS NOT A PASS.\n" +
		"     " + reason + "\n" +
		"     (set LOOPRUNTIME_E2E=1 with a reachable cluster to actually run the suite)\n" +
		"════════════════════════════════════════════════════════════════════\n"
	fmt.Fprint(os.Stderr, banner)
	GinkgoWriter.Print(banner)
	Skip(reason)
}

var _ = SynchronizedBeforeSuite(func() []byte {
	// First process: verify infrastructure health and seed data.
	//
	// LOOPRUNTIME_E2E unset -> intentional hermetic skip (no cluster expected).
	if os.Getenv("LOOPRUNTIME_E2E") == "" {
		loudSkip("LOOPRUNTIME_E2E unset")
	}

	// Past this point LOOPRUNTIME_E2E IS set -> the operator asked for the E2E suite
	// to run. Infra problems here are FAILURES, not silent skips. The old
	// behavior (Skip on unreachable API / down cluster even with LOOPRUNTIME_E2E=1)
	// was the false-green trap: a broken cluster reported SUCCESS running 0
	// specs. Fail loud instead so LOOPRUNTIME_E2E=1 + broken infra is unmistakably RED.
	Expect(verifyAPIHealth()).To(Succeed(),
		"LOOPRUNTIME_E2E=1 but API not reachable — infra must be up for the E2E suite (not a skip)")
	Expect(clusterRunning()).To(BeTrue(),
		"LOOPRUNTIME_E2E=1 but clusters not running (kubectl cluster-info failed) — infra must be up (not a skip)")

	// Seed E2E data (idempotent).
	err := seed.Run(seed.Config{})
	Expect(err).NotTo(HaveOccurred(), "E2E seed failed")

	// T2 amendment 2026-05-13 (finding-2026-05-13-e2e-token-refresh-divergence):
	// populate per-suite XDG_CONFIG_HOME with a freshly-issued JWT via
	// `loopscale login --token`. After this, all CLI subprocesses spawned by
	// runLoopRuntime* read the token from configStore (matching the production
	// path) instead of LOOPRUNTIME_TOKEN env injection. The on401 → ensureFreshToken
	// pipeline now has access to the same configStore the CLI would use in
	// production (Rule 10 — local equals production).
	//
	// XDG_CONFIG_HOME isolation prevents the test from clobbering the
	// operator's real ~/.loopruntime/config.json. The directory is recreated each
	// run for hermeticity. Set BEFORE the login subprocess starts so the
	// CLI writes into this isolated location.
	xdgDir, err := os.MkdirTemp("", "loopruntime-e2e-config-")
	Expect(err).NotTo(HaveOccurred(), "create XDG_CONFIG_HOME tempdir")
	Expect(os.Setenv("XDG_CONFIG_HOME", xdgDir)).To(Succeed())
	GinkgoWriter.Printf("XDG_CONFIG_HOME=%s\n", xdgDir)

	token := resolveToken()
	Expect(token).NotTo(BeEmpty(), "resolveToken returned empty — no dogfood JWT available")
	// Plan 2026-05-19-drill-skills-rigor-restoration: --expires-in 3600 (1h)
	// was too short — drill full suite runs 4h+ and token expired mid-suite,
	// causing all post-1h specs to fail AUTH_REQUIRED. Use 86400 (24h) matching
	// JWT actual TTL from Hydra client_credentials grant.
	loginOut, loginErrOut, loginCode := execLoopRuntime(
		loopruntimeInvocation{env: []string{"XDG_CONFIG_HOME=" + xdgDir}},
		"login", "--token", token, "--expires-in", "86400", "--json")
	Expect(loginCode).To(Equal(0),
		"loopscale login --token failed (exit %d): %s%s", loginCode, loginOut, loginErrOut)
	// Sanity: configStore file exists at expected path.
	cfgPath := filepath.Join(xdgDir, "loopruntime", "config.json")
	if _, statErr := os.Stat(cfgPath); statErr != nil {
		// Fallback path (non-XDG mode would write to baseDir/.loopruntime).
		cfgPath = filepath.Join(xdgDir, ".loopruntime", "config.json")
	}
	Expect(cfgPath).To(BeAnExistingFile(),
		fmt.Sprintf("loopscale login completed but configStore file missing under %s", xdgDir))
	GinkgoWriter.Printf("loopscale login wrote config to %s\n", cfgPath)

	return nil
}, func(_ []byte) {
	// All processes: nothing additional needed.
})
