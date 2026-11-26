// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// bootstrap-cell-cc-common.sh runs under `set -u`, so reading a variable nothing
// assigned aborts the script. Its closing log line read ${SECRET_NAME}, left over
// from before the ClusterCredential constructor took over naming the Secret: the
// Secret and the CR were applied, then the log line exited 1, and the heal marked
// both cells disconnected (H12) and failed a cold setup.
//
// Every variable the library reads must be assigned in it (a `local`, a plain
// assignment, a `read`, a `for` variable) or read with a default (${X:-…} / ${X:?…}).
func TestCellBootstrapLibraryReadsNoVariableItNeverAssigns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", "lib", "bootstrap-cell-cc-common.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// Comment lines are not executed; only code can trip `set -u`.
	var code []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			code = append(code, line)
		}
	}
	src := strings.Join(code, "\n")

	assigned := map[string]bool{}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)\b(?:local|export|declare|readonly)\s+(?:-\w+\s+)?([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`(?m)(?:^|[\s;(])([A-Za-z_][A-Za-z0-9_]*)=`),
		regexp.MustCompile(`\bread\s+(?:-\w+\s+)*([A-Za-z_][A-Za-z0-9_]*)`),
		regexp.MustCompile(`\bfor\s+([A-Za-z_][A-Za-z0-9_]*)\s+in\b`),
		regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):?[-?=]`),
	} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			assigned[m[1]] = true
		}
	}

	shellSet := map[string]bool{"HOME": true, "PATH": true, "BASH_SOURCE": true, "RANDOM": true, "PWD": true}
	missing := map[string]bool{}
	for _, m := range regexp.MustCompile(`\$\{?([A-Z_][A-Z0-9_]*)\}?`).FindAllStringSubmatch(src, -1) {
		if !assigned[m[1]] && !shellSet[m[1]] {
			missing[m[1]] = true
		}
	}
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for n := range missing {
			names = append(names, n)
		}
		sort.Strings(names)
		t.Errorf("bootstrap-cell-cc-common.sh reads %v under `set -u` without assigning them or giving a default", names)
	}
}
