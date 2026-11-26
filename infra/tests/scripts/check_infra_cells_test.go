// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// check-infra.sh C5 checks one ClusterCredential per cell. After the
// mgmt/workload/runtime-2 -> core/cell-0/cell-1 rename its loop read
// "cell-0 cell-0 cell-1", so cell-0 was checked twice and reported as two
// results with the same id.
func TestCheckInfra_ChecksEachCellOnce(t *testing.T) {
	stubs := t.TempDir()
	for name, body := range map[string]string{
		"kubectl": "#!/bin/sh\ncase \"$*\" in *clustercredential*status.connected*) printf true;; esac\nexit 0\n",
		"docker":  "#!/bin/sh\nexit 0\n",
		"curl":    "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "local", "check-infra.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "--json")
	cmd.Env = append(os.Environ(), "PATH="+stubs+string(os.PathListSeparator)+"/usr/bin:/bin")
	// The stubs answer nothing for most checks, so the run reports failures
	// and exits 1; only the C5 records matter here.
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("check-infra.sh --json: %v", err)
	}
	var report struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("parse report: %v\n%s", err, out)
	}

	seen := map[string]int{}
	for _, r := range report.Results {
		if len(r.ID) > 3 && r.ID[:3] == "C5." {
			seen[r.ID]++
		}
	}

	if len(seen) != 2 || seen["C5.0"] != 1 || seen["C5.1"] != 1 {
		t.Fatalf("C5 results = %v, want C5.0 and C5.1 once each", seen)
	}
}
