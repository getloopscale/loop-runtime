// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repoinvariants

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// Push-path coverage used to be checked by READING `task --dry`, and `--dry`
// over-reports: a block `cmds:` (`- |`) is printed verbatim without evaluating
// the `if`/`exit` inside it. quality.yml already shows it — it exits 1 when
// helmfile is missing, and `--dry` prints the next command anyway.
//
// A target shaped `- |` / `[ -n "$SKIP" ] && exit 0` / `cd cmd/loopzero-ops && go
// test ./...` would satisfy a reading of `--dry` and run nothing (B-102). So
// coverage is checked by OBSERVED EXECUTION: the target really runs, with a
// stand-in `go` on PATH that records its directory and argv. If control flow
// skips the call, it is absent from the record — which is exactly the
// difference between what the target PRINTS and what it RUNS.
func TestHermeticLegRunsThisModulesWholeSuite(t *testing.T) {
	if _, err := exec.LookPath("task"); err != nil {
		// A failure, not a skip: a skip would erase the guard on exactly the
		// machine where someone is improvising. The path that runs this test
		// goes through `task`, so on the machine that matters it exists.
		t.Fatal("go-task is not on PATH, and it is a declared prerequisite of this repository")
	}
	root := qtestutil.MustRepoRoot()

	bin := t.TempDir()
	record := filepath.Join(bin, "record")
	standIn := "#!/bin/sh\nprintf '%s\\t%s\\n' \"$(pwd)\" \"$*\" >> " + record + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(standIn), 0o755); err != nil {
		t.Fatalf("write the go stand-in: %v", err)
	}

	cmd := exec.Command("task", "quality:hermetic")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run quality:hermetic with the stand-in: %v\n%s", err, out)
	}

	b, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the target never invoked `go` — nothing was observed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("empty record: a check over no execution passes vacuously")
	}

	// The WHOLE suite of this module, run from its directory. `./...` and not
	// `./internal/quality/...`: the repository invariants in this package and
	// the delivery-path guards live outside internal/quality, and a scan that
	// excludes them stops covering exactly them.
	found := false
	for _, l := range lines {
		parts := strings.SplitN(l, "\t", 2)
		if len(parts) == 2 && strings.HasSuffix(parts[0], filepath.Join("cmd", "loopzero-ops")) &&
			strings.HasPrefix(parts[1], "test ./...") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("the hermetic leg did not EXECUTE `go test ./...` from cmd/loopzero-ops.\n\n"+
			"Observed:\n%s\n\nIf the target hides the call behind a `- |` with an `exit` in the middle, "+
			"it still shows in `task --dry` and does not run — the defect this test exists to catch (B-102).",
			strings.Join(lines, "\n"))
	}
}
