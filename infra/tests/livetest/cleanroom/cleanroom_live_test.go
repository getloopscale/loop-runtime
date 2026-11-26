// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build live

// T3.1 of plan canonical-tests-rewrite-v2 v1.0 per ADR-2026-06-17-T3 D1
// (Ginkgo BDD + internal/livetest helpers).
//
// LIVE Ginkgo orchestrator that replaces the v1 bash script
// archive/canonical-tests-v1/scripts/local/cleanroom-canonical-chain-runner.sh
//
// Build with: go test -tags=live ./infra/tests/livetest/cleanroom/...
//
// Wall-clock budget: ~30-45min for full Phase 0..5 sequence per mandate.

package cleanroom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cleanroom canonical chain v2", Label("live", "cleanroom"), func() {
	var (
		ctx      context.Context
		cancel   context.CancelFunc
		opts     Options
		repoRoot string
	)

	BeforeEach(func() {
		// Use repo root via env or cwd ascent
		repoRoot = os.Getenv("LOOP_RUNTIME_REPO_ROOT")
		if repoRoot == "" {
			wd, err := os.Getwd()
			Expect(err).ToNot(HaveOccurred())
			// ascend until we find go.work (engine root marker)
			for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
				if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
					repoRoot = dir
					break
				}
			}
		}
		Expect(repoRoot).ToNot(BeEmpty(), "could not locate repo root (set LOOP_RUNTIME_REPO_ROOT)")
		opts = DefaultOptions(repoRoot)

		// 90min: setup alone is budgeted at 30-45min, then 4min of Tilt boot and up to
		// 10min of doctor. A 45min context killed the chain before its own doctor gate.
		ctx, cancel = context.WithTimeout(context.Background(), 90*time.Minute)
	})

	AfterEach(func() {
		cancel()
	})

	Context("when invoked end-to-end (Phase 0..5)", func() {
		It("destroys k3d, recreates infra, gates doctor, and passes baseline diff", Label("full"), func() {
			skipUnlessDestroyConsented()
			outcome, err := Run(ctx, opts)
			Expect(err).ToNot(HaveOccurred())
			Expect(outcome).ToNot(BeNil())
			Expect(outcome.OK).To(BeTrue(),
				"expected chain to succeed end-to-end; phases=%+v", outcome.Phases)
			Expect(len(outcome.Phases)).To(BeNumerically(">=", 5),
				"expected at least 5 fases executed; got %d", len(outcome.Phases))
		})
	})

	Context("when invoked with --skip-cleanroom (debug iteration)", func() {
		It("only runs Phase 4 doctor gate", Label("smoke"), func() {
			// Destroys nothing: it gates the doctor against the clusters already up.
			opts.SkipCleanroom = true
			opts.SkipBaseline = true
			outcome, err := Run(ctx, opts)
			Expect(err).ToNot(HaveOccurred())
			Expect(outcome.OK).To(BeTrue())
			Expect(len(outcome.Phases)).To(Equal(1), "only doctor phase should run")
			Expect(outcome.Phases[0].Phase).To(ContainSubstring("doctor"))
		})
	})

	Context("when invoked with --skip-baseline (back-compat)", func() {
		It("runs Phases 1-4 without Phase 0 + Phase 5", Label("partial"), func() {
			skipUnlessDestroyConsented()
			opts.SkipBaseline = true
			outcome, err := Run(ctx, opts)
			Expect(err).ToNot(HaveOccurred())
			Expect(outcome.OK).To(BeTrue())
			// Phases: 1 clean:all, 2 setup, 3 tilt, 4 doctor = 4
			Expect(len(outcome.Phases)).To(Equal(4))
		})
	})
})

// skipUnlessDestroyConsented lets a spec that deletes the k3d clusters run only
// when the caller said so. `task cleanroom:v2` grants it; a bare `go test -tags=live`
// does not, and says what to set instead of skipping in silence.
func skipUnlessDestroyConsented() {
	if !DestroyConsented() {
		Skip(fmt.Sprintf("destroys and recreates the k3d clusters — set %s=1 (task cleanroom:v2 does) to run it", DestroyConsentEnv))
	}
}
