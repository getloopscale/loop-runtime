package charts

import "strings"

// containsLiteralInCodeLine returns true if needle appears in body on any
// non-comment line (skips lines starting with `#` after leading whitespace).
// Extracted from the deleted m7_preflight_test.go so that
// loop_runtime_bin_workspace_discovery_test.go still compiles after the M10 DoD#5
// quarantine DELETE (commit that removes archive/canonical-tests-v1/).
func containsLiteralInCodeLine(body, needle string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}
