// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C22.
// Source: docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md
//
// The four split operators must each be built by CI, from their own Dockerfile.
//
// B-182 — what this file used to be, and why it stopped asserting anything.
//
// It read `.github/workflows/build-images.yml` and skipped when the file was
// absent. That file was deleted in 5b32b76f0 (2026-05-23, "remove GitHub Actions
// CI/CD — not used"), so from that commit on the skip was the only branch ever
// taken: three tests below the guard never ran again while the package printed
// `ok`. The skip's own message then aged into a second defect. It announced
// "GitHub Actions removed 2026-05-23" as a standing fact, and that stopped being
// true in 32bb066b3 (2026-06-18, the AM6 artifact supply-chain pipeline).
// Measured 2026-09-06, `.github/workflows/` holds FIVE workflows — build-publish,
// ci, delivery-gate-armed, release-artifacts, workflow-hygiene — so a reader who
// believed the message would have gone looking for a CI this repository has had
// again for two and a half months.
//
// The invariant did not die with the file; only its shape did. There are no longer
// four `build-<x>-operator:` jobs. release-artifacts.yml declares the same four
// operators TWICE, and its `images` job fans them out over the reusable
// build-publish.yml:
//
//   - the `plan.filter` step (dorny/paths-filter) keys change-detection per image.
//     That is what decides whether a push to develop builds a given operator at all.
//   - the `plan.set` step's ALL matrix pairs each image with its Dockerfile. That is
//     what a release tag builds, and what the reusable workflow receives.
//
// So the tests come back pointed at the live file, and they assert MORE than the
// deleted ones did: the two declarations must AGREE. An operator listed in ALL but
// missing from the filters is never built on a develop push, and nothing errors —
// it is simply absent from the matrix, which is the quiet failure the original
// "one job per operator" pin existed to prevent.
//
// There is deliberately NO skip in this file. If release-artifacts.yml is renamed
// or restructured these tests must FAIL and be re-pointed. A guard that cannot find
// its subject and says nothing is exactly the defect B-169, B-180 and B-182 keep
// removing, and re-introducing it here to be tidy would restart the cycle.

package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseArtifactsWorkflow is the live home of the four-split-operator invariant.
const releaseArtifactsWorkflow = ".github/workflows/release-artifacts.yml"

// splitOperatorDockerfiles is the contract: four operators, four Dockerfiles, one
// image each. Consolidating back to a single build — the refactor the original test
// was written to catch — shows up here as a missing key.
var splitOperatorDockerfiles = map[string]string{
	"loop-runtime-tenant-operator":   "operators/Dockerfile.tenant-operator",
	"loop-runtime-app-operator":      "operators/Dockerfile.app-operator",
	"loop-runtime-preview-operator":  "operators/Dockerfile.preview-operator",
	"loop-runtime-platform-operator": "operators/Dockerfile.platform-operator",
}

// nonImageChangeFilters are paths-filter keys that legitimately have no matrix
// entry. `charts` selects the chart-publishing half of the pipeline, not an image;
// the `plan.set` step reads it with `jq 'index("charts")'` rather than through ALL.
// Anything else appearing there means a filter that builds nothing, so the set is
// closed on purpose and a new member has to be declared rather than assumed benign.
var nonImageChangeFilters = map[string]bool{"charts": true}

// releaseArtifactsPlan is the `plan` job, parsed as YAML rather than grepped. The
// old tests used strings.Contains over the raw file, which cannot tell a live
// declaration from a commented-out one; parsing removes that whole class and
// additionally proves the workflow is valid YAML.
type releaseArtifactsPlan struct {
	Jobs struct {
		Plan struct {
			Steps []struct {
				ID   string `yaml:"id"`
				Run  string `yaml:"run"`
				With struct {
					Filters string `yaml:"filters"`
				} `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"plan"`
	} `yaml:"jobs"`
}

func loadReleaseArtifactsPlan(t *testing.T) releaseArtifactsPlan {
	t.Helper()
	path := filepath.Join(repoRoot(), releaseArtifactsWorkflow)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("this guard could not find its subject at %s: %v\n"+
			"Read that as: the workflow was renamed or restructured, and THIS TEST must be "+
			"re-pointed at wherever the four split operators are built now. Do not add a "+
			"skip — a previous version of this file did exactly that and stopped asserting "+
			"anything for three months (B-182).", releaseArtifactsWorkflow, err)
	}
	var plan releaseArtifactsPlan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		t.Fatalf("%s is not valid YAML: %v", releaseArtifactsWorkflow, err)
	}
	if len(plan.Jobs.Plan.Steps) == 0 {
		t.Fatalf("%s: job `plan` has no steps — the workflow structure this test reads is gone", releaseArtifactsWorkflow)
	}
	return plan
}

func planStepByID(t *testing.T, plan releaseArtifactsPlan, id string) (run, filters string) {
	t.Helper()
	for _, s := range plan.Jobs.Plan.Steps {
		if s.ID == id {
			return s.Run, s.With.Filters
		}
	}
	t.Fatalf("%s: job `plan` has no step with id %q — re-point this test at the step that owns the image list now", releaseArtifactsWorkflow, id)
	return "", ""
}

// allMatrixPattern extracts the single-quoted JSON array the `set` step assigns to
// ALL. Anchored to start-of-line plus the assignment, so a mention of ALL inside a
// shell comment cannot be picked up instead.
var allMatrixPattern = regexp.MustCompile(`(?m)^\s*ALL='(\[.*\])'\s*$`)

type plannedImage struct {
	Image      string `json:"image"`
	Dockerfile string `json:"dockerfile"`
}

func planMatrix(t *testing.T, plan releaseArtifactsPlan) []plannedImage {
	t.Helper()
	run, _ := planStepByID(t, plan, "set")
	m := allMatrixPattern.FindStringSubmatch(run)
	if m == nil {
		t.Fatalf("%s: could not find the `ALL='[...]'` image matrix in the `set` step. "+
			"The build list moved; re-point this test at it.", releaseArtifactsWorkflow)
	}
	var images []plannedImage
	if err := json.Unmarshal([]byte(m[1]), &images); err != nil {
		t.Fatalf("%s: the ALL matrix is not valid JSON (%v) — the workflow would fail at `jq` too", releaseArtifactsWorkflow, err)
	}
	if len(images) == 0 {
		t.Fatalf("%s: the ALL matrix is empty — nothing is built", releaseArtifactsWorkflow)
	}
	return images
}

// The four operators are each in the release matrix, each with its own Dockerfile,
// and each of those Dockerfiles is really on disk. That last clause is what keeps
// the matrix honest: a Dockerfile path can be edited in the workflow without any
// build running locally, and CI would only discover the typo at tag time.
func TestSplitOperators_ReleaseWorkflowBuildsAllFour(t *testing.T) {
	built := map[string]string{}
	for _, img := range planMatrix(t, loadReleaseArtifactsPlan(t)) {
		built[img.Image] = img.Dockerfile
	}

	for image, dockerfile := range splitOperatorDockerfiles {
		got, ok := built[image]
		if !ok {
			t.Errorf("%s: split operator %q is not in the release matrix — it would never be built", releaseArtifactsWorkflow, image)
			continue
		}
		if got != dockerfile {
			t.Errorf("%s: %q builds from %q, expected %q", releaseArtifactsWorkflow, image, got, dockerfile)
			continue
		}
		if _, err := os.Stat(filepath.Join(repoRoot(), got)); err != nil {
			t.Errorf("%s: %q points at %q, which is not in this tree: %v", releaseArtifactsWorkflow, image, got, err)
		}
	}
}

// Change detection and the release matrix must describe the same set of images.
// This is the assertion the deleted "resolve job output flags" test was reaching
// for — one build switch per operator — in the shape the pipeline actually has.
// Divergence is silent in both directions: an image in ALL with no filter key is
// never selected on a develop push, and a filter key with no image builds nothing.
func TestSplitOperators_ChangeFilterMatchesReleaseMatrix(t *testing.T) {
	plan := loadReleaseArtifactsPlan(t)
	_, filtersYAML := planStepByID(t, plan, "filter")
	if filtersYAML == "" {
		t.Fatalf("%s: the `filter` step declares no `filters` — change detection is gone", releaseArtifactsWorkflow)
	}
	var filters map[string][]string
	if err := yaml.Unmarshal([]byte(filtersYAML), &filters); err != nil {
		t.Fatalf("%s: the `filters` block is not valid YAML: %v", releaseArtifactsWorkflow, err)
	}

	inMatrix := map[string]bool{}
	for _, img := range planMatrix(t, plan) {
		inMatrix[img.Image] = true
		if _, ok := filters[img.Image]; !ok {
			t.Errorf("%s: %q is in the release matrix but has no change filter — a push to develop would never build it", releaseArtifactsWorkflow, img.Image)
		}
	}
	for key := range filters {
		if nonImageChangeFilters[key] || inMatrix[key] {
			continue
		}
		t.Errorf("%s: change filter %q selects nothing — it is neither an image in the release matrix nor a declared non-image filter (%v). "+
			"Add it to ALL, or add it to nonImageChangeFilters with the reason.", releaseArtifactsWorkflow, key, nonImageChangeFilters)
	}
}
