// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

// dev-public pulls every platform image from Amazon ECR, the registry production pulls
// from (#435). It used to pull `:develop` from GHCR with `pullPolicy: Always`, which
// neither mirrors production nor says which commit is running; before that, from a
// DigitalOcean registry that no longer exists.
//
// These tests render what Helmfile would install, rather than reading the overlay
// text, so a value that never reaches a Pod cannot satisfy them.

import (
	"regexp"
	"strings"
	"testing"
)

const ecrRegistry = "776217504929.dkr.ecr.us-east-1.amazonaws.com/"

// commitTag is the only shape an ECR tag has in this environment: the full commit SHA
// the image was built from. The repositories are immutable, so the tag names one build.
var commitTag = regexp.MustCompile(`^[0-9a-f]{40}$`)

// assertECRImage checks one image reference: from ECR, the expected repository, a
// commit tag.
func assertECRImage(t *testing.T, where, ref, repository string) {
	t.Helper()
	if !strings.HasPrefix(ref, ecrRegistry+repository+":") {
		t.Errorf("%s: image %q is not %s%s:<commit>", where, ref, ecrRegistry, repository)
		return
	}
	if tag := strings.TrimPrefix(ref, ecrRegistry+repository+":"); !commitTag.MatchString(tag) {
		t.Errorf("%s: image %q is tagged %q, not a 40-character commit SHA", where, ref, tag)
	}
}

// The platform Deployments on the core cluster: each pulls its image from ECR by
// commit, with no pull secret — the node's kubelet credential provider authenticates,
// as on EKS — and without re-pulling an immutable tag on every start.
func TestDevPublicPlatformDeploymentsPullFromECR(t *testing.T) {
	releases := map[string]string{
		"loopzero-api":               "charts/loopzero-api",
		"loopzero-tenant-operator":   "charts/loopzero-tenant-operator",
		"loopzero-app-operator":      "charts/loopzero-app-operator",
		"loopzero-preview-operator":  "charts/loopzero-preview-operator",
		"loopzero-platform-operator": "charts/loopzero-platform-operator",
		"loopzero-cloud":             "charts/loopzero-cloud",
	}
	// The data plane (#446): one chart, one release per service enabled on dev-public.
	for svc := range enabledDataPlane(t) {
		releases[svc] = "infra/helm/data-plane-service"
	}
	for release, chart := range releases {
		t.Run(release, func(t *testing.T) {
			rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", release, chart, helmfileCoreEnv...)
			deployments := renderedObjectsOfKind(t, rendered, "Deployment")
			if len(deployments) == 0 {
				t.Fatalf("%s rendered no Deployment", release)
			}
			for _, d := range deployments {
				name, _ := dig(d, "metadata", "name").(string)
				spec, _ := dig(d, "spec", "template", "spec").(map[string]any)
				if secrets, ok := spec["imagePullSecrets"]; ok && secrets != nil {
					t.Errorf("%s: Deployment %s sets imagePullSecrets %v — ECR pulls authenticate through the node", release, name, secrets)
				}
				containers, _ := spec["containers"].([]any)
				for _, c := range containers {
					cm, _ := c.(map[string]any)
					image, _ := cm["image"].(string)
					assertECRImage(t, release+"/"+name, image, release)
					if policy, _ := cm["imagePullPolicy"].(string); policy != "IfNotPresent" {
						t.Errorf("%s: Deployment %s pulls with %q; an immutable commit tag needs IfNotPresent", release, name, policy)
					}
				}
			}
		})
	}
}

// loopzero-api launches the build steps that sign and scan images; those images come from
// the same registry, pinned the same way.
func TestDevPublicBuildStepImagesComeFromECR(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopzero-api", "charts/loopzero-api", helmfileCoreEnv...)
	cm := renderedObject(t, rendered, "ConfigMap", "loopzero-api")
	if cm == nil {
		t.Fatal("loopzero-api rendered no ConfigMap loopzero-api")
	}
	for key, repository := range map[string]string{
		"LOOPZERO_COSIGN_IMAGE": "loopzero-cosign-signer",
		"LOOPZERO_SYFT_IMAGE":   "loopzero-syft-scanner",
	} {
		ref, _ := dig(cm, "data", key).(string)
		assertECRImage(t, "loopzero-api ConfigMap "+key, ref, repository)
	}
}

// MinIO and its client are built from source (#394) and published by the same pipeline.
func TestDevPublicMinioImagesComeFromECR(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "minio", helmfileCoreEnv...)
	for key, repository := range map[string]string{"image": "loopzero-minio", "mcImage": "loopzero-clone-runner"} {
		repo, _ := dig(values, key, "repository").(string)
		tag, _ := dig(values, key, "tag").(string)
		assertECRImage(t, "minio."+key, repo+":"+tag, repository)
	}
}

// The build cluster's clone step runs the clone-runner image.
func TestDevPublicCloneRunnerComesFromECR(t *testing.T) {
	values := helmfileValues(t, "build.helmfile.yaml.gotmpl", "dev-public", "loopzero-build-cwts", helmfileCoreEnv...)
	ref, _ := dig(values, "images", "cloneRunner").(string)
	assertECRImage(t, "loopzero-build-cwts.images.cloneRunner", ref, "loopzero-clone-runner")
}
