// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cleanroom

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The Tilt dev loop applies loopzero-api's ConfigMap and Secret from a plain
// `helm template` of charts/loopzero-api. The chart follows Helm's convention and
// declares no namespace — a release supplies it — so Tilt applied both objects to
// `default`, and the loopzero-api Deployment in loopzero-system failed with
// `configmap "loopzero-api" not found`. Measured on the droplet after the bootstrap copy
// of the chart (which hard-coded the namespace) was merged into the published one.
func TestTheDevLoopAppliesTheAPIRuntimeObjectsInLoopZeroSystem(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "Tiltfile"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if regexp.MustCompile(`(?m)^k8s_yaml\("\.tilt-rendered/loopzero-api-runtime\.yaml"\)`).MatchString(src) {
		t.Error("the Tiltfile applies the raw render, whose objects carry no namespace and land in `default`")
	}
	if !regexp.MustCompile(`decode_yaml_stream\(read_file\("\.tilt-rendered/loopzero-api-runtime\.yaml"\)\)`).MatchString(src) ||
		!regexp.MustCompile(`\["namespace"\]\s*=\s*"loopzero-system"`).MatchString(src) {
		t.Error("the Tiltfile does not place the rendered loopzero-api runtime objects in loopzero-system")
	}
}
