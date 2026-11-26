// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cleanroom

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The Tilt dev loop applies loopruntime-api's ConfigMap and Secret from a plain
// `helm template` of charts/loopruntime-api. The chart follows Helm's convention and
// declares no namespace — a release supplies it — so Tilt applied both objects to
// `default`, and the loopruntime-api Deployment in loopruntime-system failed with
// `configmap "loopruntime-api" not found`. Measured on the droplet after the bootstrap copy
// of the chart (which hard-coded the namespace) was merged into the published one.
func TestTheDevLoopAppliesTheAPIRuntimeObjectsInLoopRuntimeSystem(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "Tiltfile"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if regexp.MustCompile(`(?m)^k8s_yaml\("\.tilt-rendered/loopruntime-api-runtime\.yaml"\)`).MatchString(src) {
		t.Error("the Tiltfile applies the raw render, whose objects carry no namespace and land in `default`")
	}
	if !regexp.MustCompile(`decode_yaml_stream\(read_file\("\.tilt-rendered/loopruntime-api-runtime\.yaml"\)\)`).MatchString(src) ||
		!regexp.MustCompile(`\["namespace"\]\s*=\s*"loopruntime-system"`).MatchString(src) {
		t.Error("the Tiltfile does not place the rendered loopruntime-api runtime objects in loopruntime-system")
	}
}
