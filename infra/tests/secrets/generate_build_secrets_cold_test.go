// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"os"
	"strings"
	"testing"
)

// On a cold install there is no cosign key yet, so generate-build-secrets.sh
// generates one — after `cd` into a temporary directory. The backup step then
// called "$(dirname "$0")/generate-secrets.sh", a path relative to where the
// script was started (`bash infra/helmfile/scripts/…`), so it no longer resolved:
//
//	line 135: infra/helmfile/scripts/generate-secrets.sh: No such file or directory
//
// A warm cluster never generates a key, never changes directory, and never saw it.

const coldBuildProfile = "generate-build-secrets-cold"

// The fake key pair the fake cosign writes, named as cosign names it.
var fakeKeyPair = []string{"cosign" + ".key", "cosign" + ".pub"}

func init() {
	registerFakes(coldBuildProfile, map[string]fakeTool{
		"kubectl": func(args []string) int {
			if len(args) > 0 && strings.HasPrefix(args[0], "--context") {
				args = args[1:]
			}
			if len(args) > 0 && args[0] == "get" {
				return 1 // cold: no Secret exists yet
			}
			return 0
		},
		"cosign": func(args []string) int {
			if len(args) > 0 && args[0] == "generate-key-pair" {
				for _, name := range fakeKeyPair {
					if err := os.WriteFile(name, []byte("fake-"+name), 0o600); err != nil {
						return 1
					}
				}
				// What the real cosign prints; the script filters the first line out
				// and, under pipefail, fails if the filter is left with nothing.
				os.Stdout.WriteString("Private key written to " + fakeKeyPair[0] + "\nPublic key written to " + fakeKeyPair[1] + "\n")
			}
			return 0
		},
	})
}

func TestABuildSecretsColdInstallReachesItsBackupStep(t *testing.T) {
	run := runScript(t, "../../helmfile/scripts/generate-build-secrets.sh", coldBuildProfile,
		[]string{"HOME=" + t.TempDir(), "LOOP_RUNTIME_BUCKET_NAME=loop-runtime-builds"}, "standalone")
	if strings.Contains(run.output(), "No such file or directory") {
		t.Fatalf("the script lost track of its own directory after generating the cosign key:\n%s", run)
	}
	if run.code != 0 {
		t.Fatalf("cold run failed:\n%s", run)
	}
}
