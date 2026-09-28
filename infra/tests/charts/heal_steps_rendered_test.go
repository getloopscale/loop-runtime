// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// What k3d-cross-cluster-heal.sh H8 and H9 patched into the running install,
// proven to be rendered by the install itself (principle_violations#243).
package charts

import (
	"strings"
	"testing"
)

const localCloneRunner = "k3d-loopzero-registry:5000/loopzero-clone-runner:v0.1.0"

func cloneRunnerImage(t *testing.T, env string) string {
	t.Helper()
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", env, "loopzero-build-cwts", "charts/loopzero-build-cwts")
	cwt := renderedObject(t, rendered, "ClusterWorkflowTemplate", "loopzero-clone")
	if cwt == nil {
		t.Fatalf("no loopzero-clone ClusterWorkflowTemplate in the %s render", env)
	}
	for _, line := range strings.Split(renderYAML(t, cwt), "\n") {
		if strings.Contains(line, "loopzero-clone-runner") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
		}
	}
	t.Fatalf("loopzero-clone names no clone-runner image in the %s render", env)
	return ""
}

// H9 ran `helm upgrade` of loopzero-build-cwts with the local runner after every
// restart; the environments whose cells pull from the local registry render it.
func TestLocalRegistryEnvironmentsCloneWithTheLocalRunner(t *testing.T) {
	for _, env := range []string{"dev-public", "standalone-multi"} {
		t.Run(env, func(t *testing.T) {
			if got := cloneRunnerImage(t, env); got != localCloneRunner {
				t.Errorf("clone runner = %q, want %q", got, localCloneRunner)
			}
		})
	}
}

func TestRegistryBackedEnvironmentsKeepThePublishedRunner(t *testing.T) {
	if got := cloneRunnerImage(t, "prod"); strings.HasPrefix(got, "k3d-loopzero-registry") {
		t.Errorf("prod clone runner = %q, a local-registry image", got)
	}
}

// H8 set LOOPZERO_COSIGN_IMAGE/LOOPZERO_SYFT_IMAGE on loopzero-api to :latest; the only
// k3d environment whose loopzero-api Helmfile installs renders them itself.
func TestDevPublicLoopZeroAPIRendersTheLocalAttestationImages(t *testing.T) {
	data := configMapData(t, renderDevPublicLoopZeroAPI(t), "loopzero-api")
	for _, key := range []string{"LOOPZERO_COSIGN_IMAGE", "LOOPZERO_SYFT_IMAGE"} {
		if v, _ := data[key].(string); !strings.HasPrefix(v, "k3d-loopzero-registry:5000/") {
			t.Errorf("loopzero-api %s = %q, want an image from the local registry", key, v)
		}
	}
}
