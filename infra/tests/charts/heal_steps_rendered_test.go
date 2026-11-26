// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// What k3d-cross-cluster-heal.sh H8 and H9 patched into the running install,
// proven to be rendered by the install itself (principle_violations#243).
package charts

import (
	"strings"
	"testing"
)

const localCloneRunner = "k3d-loop-runtime-registry:5000/loop-runtime-clone-runner:v0.1.0"

func cloneRunnerImage(t *testing.T, env string) string {
	t.Helper()
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", env, "loop-runtime-build-cwts", "charts/loop-runtime-build-cwts")
	cwt := renderedObject(t, rendered, "ClusterWorkflowTemplate", "loop-runtime-clone")
	if cwt == nil {
		t.Fatalf("no loop-runtime-clone ClusterWorkflowTemplate in the %s render", env)
	}
	for _, line := range strings.Split(renderYAML(t, cwt), "\n") {
		if strings.Contains(line, "loop-runtime-clone-runner") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
		}
	}
	t.Fatalf("loop-runtime-clone names no clone-runner image in the %s render", env)
	return ""
}

// H9 ran `helm upgrade` of loop-runtime-build-cwts with the local runner after every
// restart; an environment whose cells pull from the local registry, and that
// declares no platform registry, renders it. dev-public declares one (#435) and
// renders the ECR runner instead — TestDevPublicCloneRunnerComesFromECR.
func TestLocalRegistryEnvironmentsCloneWithTheLocalRunner(t *testing.T) {
	for _, env := range []string{"standalone-multi"} {
		t.Run(env, func(t *testing.T) {
			if got := cloneRunnerImage(t, env); got != localCloneRunner {
				t.Errorf("clone runner = %q, want %q", got, localCloneRunner)
			}
		})
	}
}

func TestRegistryBackedEnvironmentsKeepThePublishedRunner(t *testing.T) {
	if got := cloneRunnerImage(t, "prod"); strings.HasPrefix(got, "k3d-loop-runtime-registry") {
		t.Errorf("prod clone runner = %q, a local-registry image", got)
	}
}

// H8 set LOOP_RUNTIME_COSIGN_IMAGE/LOOP_RUNTIME_SYFT_IMAGE on loop-runtime-api to :latest. The dev-public
// install renders them itself, pinned by commit from ECR (#435):
// TestDevPublicBuildStepImagesComeFromECR.
