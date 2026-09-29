// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shared build-publish workflow is called from other repositories (loopzero-cloud,
// loopzero-memory, loopzero-rag, loopzero-lens). A Fulcio certificate issued inside a reusable workflow
// names the REUSABLE in its SAN and the CALLER in its source-repository claim, so the
// verify step must pin each half to the right thing (#460). Pinning the caller's repository
// in the SAN, as it did, rejects every signature this workflow produces for another
// repository; pinning only the reusable path would accept a signature any repository of
// the org produced by calling it — the confused deputy the step exists to block.
func verifyStep(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), ".github", "workflows", "build-publish.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	start := strings.Index(body, "- name: Verify the signature (identity-pinned)")
	if start < 0 {
		t.Fatal("build-publish.yml has no identity-pinned verify step")
	}
	rest := body[start:]
	if end := strings.Index(rest[1:], "\n      - name:"); end >= 0 {
		rest = rest[:end+1]
	}
	return rest
}

func TestBuildPublishVerifyPinsTheReusableInTheCertificateIdentity(t *testing.T) {
	step := verifyStep(t)
	if !strings.Contains(step, `--certificate-identity-regexp "^https://github\.com/${OWNER}/loopzero/\.github/workflows/build-publish\.yml@"`) {
		t.Errorf("the verify step does not pin the reusable's own path in the SAN:\n%s", step)
	}
	if strings.Contains(step, "${IDENTITY_REPO}/\\.github") {
		t.Error("the SAN is still pinned to the calling repository, which never matches a reusable's certificate")
	}
}

func TestBuildPublishVerifyPinsTheCallerRepository(t *testing.T) {
	step := verifyStep(t)
	if !strings.Contains(step, `--certificate-github-workflow-repository "${CALLER_REPO}"`) ||
		!strings.Contains(step, "CALLER_REPO: ${{ github.repository }}") {
		t.Errorf("the verify step does not pin the calling repository's claim:\n%s", step)
	}
}
