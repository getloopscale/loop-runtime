// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cleanroom

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// Tilt deploys into the CURRENT kube context, and nothing in the chain set it: the
// droplet's was left on k3d-loopzero-secrets, so Tilt applied the platform into the
// secrets cluster ("namespaces \"argocd\" not found") and its local resources ran
// against a kubeconfig holding only that context ("context \"k3d-loopzero-core\" does
// not exist"). `allow_k8s_contexts` never stopped it: every k3d-* context is
// already treated as local.

func TestTheCleanroomStartsTiltOnTheCoreContext(t *testing.T) {
	args := tiltUpArgs()
	i := slices.Index(args, "--context")
	if i < 0 || i+1 >= len(args) || args[i+1] != TiltContext {
		t.Fatalf("tilt is started with %v; it must pin --context %s instead of inheriting whatever context is current", args, TiltContext)
	}
}

func TestTheTiltfileRefusesAnyOtherContext(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "Tiltfile"))
	if err != nil {
		t.Fatal(err)
	}
	guard := regexp.MustCompile(`if k8s_context\(\) != "` + regexp.QuoteMeta(TiltContext) + `":\s*\n\s*fail\(`)
	if !guard.Match(raw) {
		t.Errorf("the Tiltfile does not fail when the context is not %s; it would deploy the platform into whichever cluster is current", TiltContext)
	}
}
