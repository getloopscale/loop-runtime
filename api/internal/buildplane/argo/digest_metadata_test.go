// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every app's build writes metadata-<app>.json to the SHARED workspace, so a
// step that reads "the first metadata file" reads another app's digest in a
// multi-app build. Each step must read its own app's file.

func TestDigestFromMetadataReadsOnlyTheNamedAppsFile(t *testing.T) {
	// Arrange: two apps built into one workspace; "api" sorts first.
	ws := t.TempDir()
	write := func(app, digest string) {
		body := `{"containerimage.config.digest":"sha256:cfg","containerimage.digest":"` + digest + `"}`
		if err := os.WriteFile(filepath.Join(ws, "metadata-"+app+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("api", "sha256:aaaa")
	write("web", "sha256:bbbb")

	// Act
	out, err := exec.Command("sh", "-c", digestFromMetadata(filepath.Join(ws, "metadata-web.json"))).Output()

	// Assert
	if err != nil {
		t.Fatalf("digest extraction failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "sha256:bbbb" {
		t.Errorf("digest = %q, want web's sha256:bbbb", got)
	}
}

func TestBuildImageStepRecordsItsOwnAppsDigest(t *testing.T) {
	script, err := buildkitEntrypointScript("/run/buildkit/buildkitd.sock", false, "registry.example.com", metadataFileFor("web"))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(script, "metadata-*") {
		t.Errorf("build-image reads the first metadata file on the shared workspace, not its own:\n%s", script)
	}
	if !strings.Contains(script, "/workspace/metadata-web.json") {
		t.Errorf("build-image does not read metadata-web.json:\n%s", script)
	}
}
