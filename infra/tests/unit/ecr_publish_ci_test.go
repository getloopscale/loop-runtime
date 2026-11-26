// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// The platform images are published to Amazon ECR, the registry production pulls from.
//
// The repositories are created by loopruntime-infra-live (`registry/ecr` unit) with IMMUTABLE
// tags, so a push that reuses a tag is refused. That is the property that makes an image
// reference traceable to one commit, and it is also why this file exists: the GHCR channel
// moves `:develop` and `:latest` on every build, and copying those rules onto ECR would
// fail on the second push of the day.
//
// Like split_operators_ci_test.go, there is deliberately no skip here. A workflow that
// stops publishing to ECR must fail these tests, not make them vacuous.

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const buildPublishWorkflow = ".github/workflows/build-publish.yml"

// engineImageDockerfiles is every ECR repository this repository builds. The other two
// repositories in the registry (loopruntime-cloud, loopruntime-dashboard) are built by loopruntime-cloud.
var engineImageDockerfiles = map[string]string{
	"loopruntime-api":               "api/Dockerfile",
	"loopruntime-tenant-operator":   "operators/Dockerfile.tenant-operator",
	"loopruntime-app-operator":      "operators/Dockerfile.app-operator",
	"loopruntime-preview-operator":  "operators/Dockerfile.preview-operator",
	"loopruntime-platform-operator": "operators/Dockerfile.platform-operator",
	"loopruntime-minio":             "infra/images/minio/Dockerfile",
	"loopruntime-clone-runner":      "infra/images/clone-runner/Dockerfile",
	"loopruntime-syft-scanner":      "infra/images/syft-scanner/Dockerfile",
	"loopruntime-cosign-signer":     "infra/images/cosign-signer/Dockerfile",
}

type workflowStep struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	With map[string]string `yaml:"with"`
}

type workflowFile struct {
	On struct {
		WorkflowCall struct {
			Inputs map[string]any `yaml:"inputs"`
		} `yaml:"workflow_call"`
	} `yaml:"on"`
	Jobs map[string]struct {
		Permissions map[string]string `yaml:"permissions"`
		With        map[string]any    `yaml:"with"`
		Steps       []workflowStep    `yaml:"steps"`
	} `yaml:"jobs"`
}

func loadWorkflow(t *testing.T, rel string) workflowFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), rel))
	if err != nil {
		t.Fatalf("%s: cannot read the workflow this test guards: %v", rel, err)
	}
	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("%s is not valid YAML: %v", rel, err)
	}
	return wf
}

// Every image the registry holds for this repository is in the release matrix, built
// from a Dockerfile that is on disk. An image missing here is an ECR repository nothing
// ever fills, and the environment pulling it fails at deploy time.
func TestReleaseMatrixBuildsEveryEngineImage(t *testing.T) {
	built := map[string]string{}
	for _, img := range planMatrix(t, loadReleaseArtifactsPlan(t)) {
		built[img.Image] = img.Dockerfile
	}
	for image, dockerfile := range engineImageDockerfiles {
		got, ok := built[image]
		if !ok {
			t.Errorf("%s: %q is not in the release matrix — its ECR repository would stay empty", releaseArtifactsWorkflow, image)
			continue
		}
		if got != dockerfile {
			t.Errorf("%s: %q builds from %q, expected %q", releaseArtifactsWorkflow, image, got, dockerfile)
		}
		if _, err := os.Stat(filepath.Join(repoRoot(), got)); err != nil {
			t.Errorf("%s: %q points at %q, which is not in this tree: %v", releaseArtifactsWorkflow, image, got, err)
		}
	}
}

// The images job hands the reusable workflow the ECR registry and the publish role, both
// from repository variables rather than text, and grants the OIDC token the role needs.
func TestImagesJobPublishesToECRThroughOIDC(t *testing.T) {
	job, ok := loadWorkflow(t, releaseArtifactsWorkflow).Jobs["images"]
	if !ok {
		t.Fatalf("%s: no `images` job", releaseArtifactsWorkflow)
	}
	for input, want := range map[string]string{
		"ecr_registry": "${{ vars.ECR_REGISTRY }}",
		"aws_role_arn": "${{ vars.AWS_IMAGE_PUBLISH_ROLE_ARN }}",
	} {
		if got, _ := job.With[input].(string); got != want {
			t.Errorf("%s: images.with.%s = %q, want %q", releaseArtifactsWorkflow, input, got, want)
		}
	}
	if job.Permissions["id-token"] != "write" {
		t.Errorf("%s: the images job must grant id-token: write — the ECR push role is assumed through OIDC", releaseArtifactsWorkflow)
	}
}

func buildPublishStep(t *testing.T, id string) workflowStep {
	t.Helper()
	for _, s := range loadWorkflow(t, buildPublishWorkflow).Jobs["build-publish"].Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("%s: no step with id %q", buildPublishWorkflow, id)
	return workflowStep{}
}

// ECR tags are immutable, so the ECR tag set may hold only tags that name one build:
// the full commit SHA, and the release version. `:develop`, `:latest` and `X.Y` move by
// design and would be refused on the second push.
func TestBuildPublishTagsECROnlyWithImmutableTags(t *testing.T) {
	tags := buildPublishStep(t, "meta-ecr").With["tags"]
	if !strings.Contains(tags, "type=sha,format=long,prefix=") {
		t.Errorf("%s: meta-ecr must tag the full commit SHA with no prefix; tags are:\n%s", buildPublishWorkflow, tags)
	}
	if !strings.Contains(tags, "type=semver,pattern={{version}}") {
		t.Errorf("%s: meta-ecr must tag the release version; tags are:\n%s", buildPublishWorkflow, tags)
	}
	for _, moving := range []string{"type=raw", "{{major}}.{{minor}}", "type=ref"} {
		if strings.Contains(tags, moving) {
			t.Errorf("%s: meta-ecr carries %q, a tag that moves — ECR refuses it on the second push", buildPublishWorkflow, moving)
		}
	}
}

// The ECR half is opt-in: a caller that passes no registry (the other repositories of
// the org call this workflow too) must not try to assume a role or log in.
func TestBuildPublishECRStepsAreOptIn(t *testing.T) {
	inputs := loadWorkflow(t, buildPublishWorkflow).On.WorkflowCall.Inputs
	for _, in := range []string{"ecr_registry", "aws_role_arn"} {
		if _, ok := inputs[in]; !ok {
			t.Errorf("%s: missing workflow_call input %q", buildPublishWorkflow, in)
		}
	}
	for _, id := range []string{"aws-credentials", "ecr-login", "meta-ecr"} {
		if cond := buildPublishStep(t, id).If; !strings.Contains(cond, "inputs.ecr_registry != ''") {
			t.Errorf("%s: step %q runs unconditionally (if: %q) — a caller without ECR would fail", buildPublishWorkflow, id, cond)
		}
	}
}

// A reusable workflow may never ask for more than its caller grants: GitHub refuses the
// whole run at startup (`startup_failure`, no job log, no message in the API). This
// happened on 2026-09-28 — `attestations: write` entered build-publish.yml on 2026-09-16,
// the images job never granted it, and the first develop push after it published nothing.
func TestImagesJobGrantsEveryPermissionTheReusableDeclares(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), buildPublishWorkflow))
	if err != nil {
		t.Fatalf("%s: %v", buildPublishWorkflow, err)
	}
	var reusable struct {
		Permissions map[string]string `yaml:"permissions"`
	}
	if err := yaml.Unmarshal(data, &reusable); err != nil {
		t.Fatalf("%s is not valid YAML: %v", buildPublishWorkflow, err)
	}
	if len(reusable.Permissions) == 0 {
		t.Fatalf("%s declares no top-level permissions — this test reads them", buildPublishWorkflow)
	}
	granted := loadWorkflow(t, releaseArtifactsWorkflow).Jobs["images"].Permissions
	for scope, level := range reusable.Permissions {
		if granted[scope] != level {
			t.Errorf("%s: build-publish.yml asks for %s: %s, the images job grants %q — GitHub refuses the run at startup",
				releaseArtifactsWorkflow, scope, level, granted[scope])
		}
	}
}

// GitHub-native attestations are unavailable to a private repository on the
// organisation's plan ("Feature not available for the getloopscale organization"), and
// every repository calling this workflow is private. Unconditional, the step failed the
// whole publish after the image was already pushed — and before the cosign keyless
// signature that follows it could run (loopruntime-cloud run 36512769358). The cosign
// signature is the guarantee; the native attestation is where a public repository
// also publishes it.
func TestNativeAttestationRunsOnlyOnPublicRepositories(t *testing.T) {
	var step *workflowStep
	for _, s := range loadWorkflow(t, buildPublishWorkflow).Jobs["build-publish"].Steps {
		if s.Uses != "" && strings.HasPrefix(s.Uses, "actions/attest-build-provenance@") {
			s := s
			step = &s
		}
	}
	if step == nil {
		t.Fatalf("%s: no actions/attest-build-provenance step", buildPublishWorkflow)
	}
	if !strings.Contains(step.If, "!github.event.repository.private") {
		t.Errorf("%s: the attestation step runs on private repositories (if: %q) — the org plan refuses it and the publish fails", buildPublishWorkflow, step.If)
	}
}

// The engine's images go to production, so they pass the same vulnerability gate the
// control plane's do (CRITICAL+HIGH with a fix available, in build-publish.yml). Without
// it the images built on Go 1.26.1 reached ECR carrying 53 HIGH findings (loopruntime-api) and
// nothing measured them.
func TestImagesJobGatesOnKnownVulnerabilities(t *testing.T) {
	job := loadWorkflow(t, releaseArtifactsWorkflow).Jobs["images"]
	if got, _ := job.With["trivy"].(bool); !got {
		t.Errorf("%s: images.with.trivy = %v — the engine images publish without the vulnerability gate", releaseArtifactsWorkflow, job.With["trivy"])
	}
}
