// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// generate-secrets.sh provisions the core cluster's credentials. It used to log
// "noop" and exit 0 for any other cluster name, so when the core cluster was
// renamed from `mgmt` (B-001) the two Taskfile call sites that still passed `mgmt`
// installed the platform with no database Secret, and only a cold install noticed
// (#397). A name the script does not handle is a caller's mistake, not a no-op.

func TestAClusterNameTheScriptDoesNotHandleIsRefused(t *testing.T) {
	run := runScript(t, "../../helmfile/scripts/generate-secrets.sh", dbRolesProfile,
		[]string{secretRecordEnv + "=" + t.TempDir() + "/secrets.tsv"}, "standalone", "mgmt")
	expectExit(t, run, 1, "mgmt")
}

func TestEveryTaskfileCallNamesTheCoreCluster(t *testing.T) {
	call := regexp.MustCompile(`generate-secrets\.sh\s+(\S+)\s+(\S+)`)
	var checked int
	for _, glob := range []string{"../../../Taskfile.yml", "../../../taskfiles/*.yml"} {
		files, err := filepath.Glob(glob)
		if err != nil {
			t.Fatalf("glob %s: %v", glob, err)
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			for i, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
					continue
				}
				m := call.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				checked++
				if m[2] != "core" {
					t.Errorf("%s:%d calls generate-secrets.sh with cluster %q; the script handles only core", filepath.Base(f), i+1, m[2])
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no generate-secrets.sh call in the Taskfiles; the check measured nothing")
	}
}
