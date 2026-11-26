// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestClassifyRenderFailureSeparatesNetworkFromContent(t *testing.T) {
	cases := []struct {
		name        string
		output      string
		wantNetwork bool
	}{
		{"dns failure", "Error: failed to download: no such host", true},
		{"connection refused", "dial tcp 1.2.3.4:443: connection refused", true},
		{"handshake", "Get \"https://charts.example\": TLS handshake timeout", true},
		// The case met when --skip-deps was applied in the wrong place: it is
		// configuration, not connectivity. Calling it network would send the next
		// person looking for a connection defect over a file error.
		{"undeclared repo is NOT network", "Error: repo openbao not found", false},
		{"template error", "Error: template: loopruntime-cloud/templates/svc.yaml:12: nil pointer", false},
		{"schema", "values don't meet the specifications of the schema(s)", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			network, explanation := ClassifyRenderFailure(c.output)
			if network != c.wantNetwork {
				t.Errorf("classified network=%v, want %v — output: %q", network, c.wantNetwork, c.output)
			}
			if explanation == "" {
				t.Error("the explanation is empty; the message is this function's product")
			}
			if c.wantNetwork && !strings.Contains(explanation, "NETWORK") {
				t.Errorf("the network explanation does not say it is network: %q", explanation)
			}
		})
	}
}

// The real population (`rules/testing.md` § 4.2): how many of this gate's tests
// EXECUTE `helmfile`, and how many of them no longer contact the network.
//
// Measured 2026-08-19: of the 8 mentions of "helmfile" in the tests, only TWO are
// executions (`loopruntime_cloud_edge_nodeport_test.go` and
// `vault_host_published_reach_test.go`); the rest are file paths or literals
// inside assertions about scripts. Of the two, `loopruntime-cloud` renders a LOCAL chart
// and uses `--skip-deps` (0s, zero repos); `openbao` renders a REMOTE chart and
// cannot.
//
// Since 2026-09-25 the network-free execution lives in ONE helper,
// `helmfile_render_test.go`, which forces `--skip-deps` on every call and offers
// `template` for local charts and `write-values` for remote ones; `loopruntime-cloud`
// and every later render go through it. The count stays at two files.
//
// This test exists so the next execution added is a conscious decision: it fails
// if a third appears without `--skip-deps` and without a declared way out.
func TestHelmfileExecutionsInTheGateAreTheMeasuredOnes(t *testing.T) {
	execs := listHelmfileExecutions(t)
	if len(execs) == 0 {
		t.Fatal("no helmfile execution found — a sweep that reaches nothing reports green over nothing")
	}
	if len(execs) > 2 {
		t.Errorf("%d helmfile executions appeared in the gate's tests; there were 2 on 2026-08-19 (%v). "+
			"Each new execution turns network unavailability into a blocked delivery — use "+
			"`--skip-deps` if the chart is local, or declare the way out as the openbao test "+
			"does (B-156).", len(execs), execs)
	}
}

// listHelmfileExecutions walks the gate's tests and returns the files that
// EXECUTE `helmfile` — telling execution from mention. The distinction matters:
// of the 8 occurrences of the name, 6 are a file path or a literal inside an
// assertion about a script.
func listHelmfileExecutions(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	base := filepath.Join(root, "infra", "tests")
	var found []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		// The sweep matches itself: this file carries both literals it looks for,
		// inside its own detection logic. That is the detector, not an execution.
		// Excluding by name beats sharpening the pattern — any smarter pattern would
		// be wrong somewhere else, and a real helmfile execution ADDED to this file
		// would be odd enough to be seen in review.
		if strings.HasSuffix(p, "render_failure_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(filepath.Clean(p))
		if rerr != nil {
			return nil
		}
		// Execution = `exec.Command(` followed, in the same expression, by the
		// literal "helmfile".
		for _, chunk := range strings.Split(string(b), "exec.Command(")[1:] {
			end := strings.Index(chunk, ")")
			if end < 0 {
				end = len(chunk)
			}
			if strings.Contains(chunk[:end], `"helmfile"`) {
				rel, _ := filepath.Rel(root, p)
				found = append(found, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", base, err)
	}
	sort.Strings(found)
	return found
}
