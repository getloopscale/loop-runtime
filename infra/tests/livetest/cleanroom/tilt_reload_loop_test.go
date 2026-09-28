// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cleanroom

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// `k8s_yaml(".tilt-rendered/loopzero-api-runtime.yaml")` makes that file a dependency of
// the Tiltfile, and the Tiltfile rewrites it with `>` on every load. Each load
// therefore changed the file, which reloaded the Tiltfile, which rewrote the file:
// on the droplet "(Tiltfile)" stayed in_progress for good and loopzero-api never left
// pending. The render is deterministic, so replacing the file only when its content
// differs ends the loop without losing a real change.
func TestTheRuntimeRenderReplacesTheWatchedFileOnlyWhenItChanges(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "Tiltfile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^_API_RUNTIME_RENDER = (.+)$`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("the Tiltfile no longer defines _API_RUNTIME_RENDER")
	}
	render := string(m[1])
	if regexp.MustCompile(`>\s*\.tilt-rendered/loopzero-api-runtime\.yaml"?\s*$`).MatchString(render) {
		t.Errorf("the render writes straight into the file k8s_yaml watches, so every Tiltfile load reloads the Tiltfile:\n%s", render)
	}
	if !regexp.MustCompile(`cmp -s`).MatchString(render) {
		t.Errorf("the render does not compare before replacing the watched file:\n%s", render)
	}
}
