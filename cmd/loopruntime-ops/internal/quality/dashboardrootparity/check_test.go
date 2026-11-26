// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package dashboardrootparity_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/dashboardrootparity"
)

// tree writes a minimal Caddyfile and runbook.
func tree(t *testing.T, caddy, runbook string) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("infra/local/Caddyfile", caddy)
	mk("docs/runbooks.md", runbook)
	return root
}

// The runbook must name the SAME path the edge serves.
//
// B-039, measured 2026-08-18: deploying a fix to the dashboard required finding out where the
// code lives on the droplet — and the obvious path, `~/loopruntime-cloud`, is a clone of the
// **umbrella**, not of the product. The two clones that looked right were ad-hoc, with a
// restricted refspec. The path the edge actually serves, `~/loopruntime-cloud/loopruntime-cloud/dashboard/dist`,
// works and was in no runbook at all.
//
// Documentation nobody ties down rots: whoever changes the Caddyfile's `root *` would leave the
// runbook pointing at a path that is no longer served, and the next person would pay for the
// same discovery with a document that confirms the error.
// The fixtures use `/home/dev/...` and not anyone's real home. The `publish-hygiene`
// gate refuses a workstation path in a versioned file — published history is
// retroactively public — and it refused these three lines. `dev` is on the list of
// SYNTHETIC homes the detector itself declares acceptable
// (`publishhygiene/check.go:82`), so this is the intended mechanism and not evasion.
// This gate's logic does not read the user name: `check.go` contains the literal
// nowhere.
func TestRunbookNamesTheServedRoot(t *testing.T) {
	root := tree(t,
		"app-dev.getloopscale.com {\n  root * /home/dev/loopruntime-cloud/loopruntime-cloud/dashboard/dist\n}\n",
		"# runbook\n\nThe dashboard is built in `~/loopruntime-cloud/loopruntime-cloud`.\n")

	if _, err := violationOf(dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root})); err != nil {
		t.Fatalf("runbook and Caddyfile agree; got %v", err)
	}
}

// Divergence is the defect: the runbook sends the person where the edge does not serve.
func TestDivergenceIsRefused(t *testing.T) {
	root := tree(t,
		"app-dev.getloopscale.com {\n  root * /home/dev/loopruntime-cloud/loopruntime-cloud/dashboard/dist\n}\n",
		"# runbook\n\nThe dashboard is built in `~/.buildmeta-cloud/loopruntime-cloud`.\n")

	out, err := violationOf(dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root}))
	if !errors.Is(err, dashboardrootparity.ErrDivergent) {
		t.Fatalf("expected ErrDivergent, got %v", err)
	}
	if out.Served == "" || out.Documented == "" {
		t.Errorf("the error must name both sides: %+v", out)
	}
}

// A runbook that mentions no path at all is the state BEFORE the item, and must not pass in
// silence — it is what made the discovery expensive.
func TestSilentRunbookIsRefused(t *testing.T) {
	root := tree(t,
		"app-dev.getloopscale.com {\n  root * /home/dev/loopruntime-cloud/loopruntime-cloud/dashboard/dist\n}\n",
		"# runbook\n\nNothing about where the dashboard is built.\n")

	if _, err := violationOf(dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root})); !errors.Is(err, dashboardrootparity.ErrUndocumented) {
		t.Fatalf("expected ErrUndocumented, got %v", err)
	}
}

// A Caddyfile with no `root *` means the edge serves no files — there is no parity to
// verify, and reporting "in agreement" would be green over nothing.
func TestNoServedRootIsRefusedNotPassed(t *testing.T) {
	root := tree(t, "app-dev.getloopscale.com {\n  reverse_proxy localhost:1\n}\n", "# runbook\n")

	if _, err := violationOf(dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: root})); !errors.Is(err, dashboardrootparity.ErrNothingServed) {
		t.Fatalf("expected ErrNothingServed, got %v", err)
	}
}

// The real population: this repository's tree must agree with itself.
func TestRealTreeAgrees(t *testing.T) {
	out, err := violationOf(dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: filepath.Join("..", "..", "..", "..", "..")}))
	if err != nil {
		t.Fatalf("real tree diverges: %v (served=%q documented=%q)", err, out.Served, out.Documented)
	}
	t.Logf("real population: served=%q documented=%q", out.Served, out.Documented)
}
