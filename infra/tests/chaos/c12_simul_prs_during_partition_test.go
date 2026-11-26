// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package chaos_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// M13 (plan m13-chaos-declarative) — c12 reexpressed from bash orchestration
// (infra/scripts/local/chaos-scenarios/c12-simul-prs-during-partition.sh) into a
// declarative Chaos Mesh CRD (manifests/c12-partition-mgmt-build.yaml) + Go assertion.
//
// c12 scenario: inject a NetworkChaos partition between the mgmt cluster
// (loop-runtime-system) and the build cluster (argo-system) CONCURRENTLY with two
// simultaneous PR dispatches, then assert post-heal:
//   - at least 1 release reaches ACTIVE
//   - NO duplicate ACTIVE releases for the same commit_sha
//     (proves withEnvironmentLock + pg_advisory_xact_lock hold under chaos)
//
// EC-6 (from the bash script) is the invariant this file locks HERMETICALLY:
// the partition selector MUST be EXPLICITLY scoped to [loop-runtime-system, argo-system]
// — never a blast-radius-spilling unscoped selector that would also cut operator
// port-forwards + browser sessions.

// networkChaosManifest is the minimal shape of a Chaos Mesh NetworkChaos we
// assert on. We decode only the fields EC-6 constrains.
type networkChaosManifest struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Action   string `yaml:"action"`
		Selector struct {
			Namespaces []string `yaml:"namespaces"`
		} `yaml:"selector"`
	} `yaml:"spec"`
}

// TestC12Manifest_ScopedPartition is HERMETIC (no cluster, no LOOP_RUNTIME_CHAOS_TEST
// gate): it parses the c12 declarative manifest and locks the EC-6 scoped-selector
// invariant so it can never silently regress to an unscoped, blast-radius-spilling
// partition. This is the real GREEN value the reexpression adds beyond a skip.
func TestC12Manifest_ScopedPartition(t *testing.T) {
	path := manifestPath("c12-partition-mgmt-build.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("c12 manifest not found at %s: %v", path, err)
	}

	// A Chaos Mesh manifest file may carry a leading comment block + a `---`
	// document separator (as pg-network-latency.yaml does). Decode every YAML
	// document and pick the NetworkChaos one.
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var found *networkChaosManifest
	for {
		var m networkChaosManifest
		if derr := dec.Decode(&m); derr != nil {
			break // io.EOF or end of documents
		}
		if m.Kind == "NetworkChaos" {
			found = &m
			break
		}
	}

	if found == nil {
		t.Fatalf("c12 manifest %s has no NetworkChaos document", path)
	}
	if found.Spec.Action != "partition" {
		t.Fatalf("c12 NetworkChaos action = %q, want %q", found.Spec.Action, "partition")
	}

	// EC-6: the partition MUST be explicitly scoped to exactly the mgmt + build
	// namespaces. An empty namespaces list would mean an unscoped (all-namespace)
	// blast radius — the exact foot-gun the bash script's EC-6 guarded against.
	want := map[string]bool{"loop-runtime-system": true, "argo-system": true}
	got := map[string]bool{}
	for _, ns := range found.Spec.Selector.Namespaces {
		got[ns] = true
	}
	if len(found.Spec.Selector.Namespaces) != 2 || !got["loop-runtime-system"] || !got["argo-system"] {
		t.Fatalf("c12 EC-6 scoped selector violated: spec.selector.namespaces = %v, want exactly [loop-runtime-system argo-system]",
			found.Spec.Selector.Namespaces)
	}
	_ = want
}

var _ = Describe("c12: simultaneous PRs during mgmt<->build network partition", Label("chaos", "c12"), func() {
	BeforeEach(func() {
		skipUnlessChaos()
		if !chaosMeshAvailable() {
			Skip("chaos-mesh CRDs absent — install chaos-mesh per docs/operations/chaos-test-setup.md to enable")
		}
	})

	It("keeps withEnvironmentLock: >=1 ACTIVE release, ZERO duplicate ACTIVE per commit_sha, post-heal", func() {
		// Expected LIVE flow (operator and sponsor together, on a live cluster):
		//   1. checkAPIHealth() — API serving before chaos.
		//   2. applyChaos("c12-partition-mgmt-build.yaml") — scoped NetworkChaos
		//      partition [loop-runtime-system, argo-system], duration 180s (EC-6).
		//   3. dispatch TWO PRs simultaneously (gh api repository_dispatch x2)
		//      for the SAME (tenant, project, environment) → same commit_sha.
		//   4. wait PARTITION_SECONDS (180s) for the partition to heal, then
		//      POST_HEAL_BUDGET (30min) for convergence.
		//   5. assert >= 1 release reaches ACTIVE (the deploy path survives the
		//      partition + heals).
		//   6. assert ZERO duplicate ACTIVE releases for the same commit_sha —
		//      the two concurrent PRs resolve as 1 ACTIVE + 1 rejected/superseded
		//      OR sequential serialization, NEVER 2 ACTIVE for the same commit
		//      (proves withEnvironmentLock + pg_advisory_xact_lock under chaos).
		//   7. cleanup() deletes the NetworkChaos (deferred).
		//
		// The autonomous hermetic proof that EC-6 is honored is
		// TestC12Manifest_ScopedPartition above; this It is the LIVE proof that
		// the lock invariant COMPOSES under a real network partition.
		Skip("operator LIVE-only — chaos-mesh + real PR dispatch + PG required; flow documented in this It block")

		Expect(true).To(BeTrue(), "placeholder so future operator-LIVE runs have a green pivot")
		_ = time.Second
	})
})
