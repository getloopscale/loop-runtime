// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

// giteaInternalURLDefault captures the kubebuilder default rendered for the
// Gitea block's internalURL: the first `internalURL:` property after the
// `gitea:` property, and the `default:` line that immediately follows it. The
// ArgoCD block has an internalURL too; anchoring on `gitea:` keeps it out.
var giteaInternalURLDefault = regexp.MustCompile(`(?ms)^\s+gitea:\n.*?^\s+internalURL:\n\s+default:\s+(\S+)\s*$`)

// TestCRDGiteaInternalURLDefaultIsTheDefaultInternalURL pins the one place the
// default Gitea address cannot import the Go constant — the kubebuilder default
// marker rendered into the PlatformBootstrap CRD schema — to
// gitearepo.DefaultInternalURL, so a PlatformBootstrap created without an
// address and a component running on its defaults reach the same Gitea.
func TestCRDGiteaInternalURLDefaultIsTheDefaultInternalURL(t *testing.T) {
	const crdPath = "../../config/crd/bases/loopzero.io_platformbootstraps.yaml"
	content, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD YAML: %v", err)
	}

	match := giteaInternalURLDefault.FindSubmatch(content)

	if match == nil {
		t.Fatalf("%s declares no default for spec.gitea.internalURL", crdPath)
	}
	if got := string(match[1]); got != gitearepo.DefaultInternalURL {
		t.Fatalf("%s defaults spec.gitea.internalURL to %q, want gitearepo.DefaultInternalURL %q",
			crdPath, got, gitearepo.DefaultInternalURL)
	}
}
