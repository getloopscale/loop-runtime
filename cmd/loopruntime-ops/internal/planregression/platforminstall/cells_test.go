// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platforminstall

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// No task pins the Helmfile environment.
//
// `LOOPRUNTIME_HELMFILE_ENV=dev-public task cloud:setup` used to sync Ory with a FIXED
// `-e standalone-multi`, ignoring the variable. standalone-multi declares no
// `publicAuthUrl`, so the Hydra chart fell back to the cluster FQDN and the OIDC
// discovery document advertised
// `http://ory-hydra-public.loopruntime-auth.svc.cluster.local:4444` in nine fields —
// the `issuer` among them. The task did not fail; it synced the wrong
// environment successfully.
func TestNoTaskPinsTheHelmfileEnvironment(t *testing.T) {
	b, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read taskfiles/infra.yml: %v", err)
	}
	var offenders []string
	for i, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "-e standalone-multi") {
			offenders = append(offenders, fmt.Sprintf("infra.yml:%d", i+1))
		}
	}
	if len(offenders) > 0 {
		t.Errorf("%d task(s) pin `-e standalone-multi` and ignore LOOPRUNTIME_HELMFILE_ENV: %s\n"+
			"Use `${LOOPRUNTIME_HELMFILE_ENV:-standalone-multi}` like the others: a pinned environment "+
			"installs the WRONG environment successfully.", len(offenders), strings.Join(offenders, ", "))
	}
}

// taskBlock returns the lines of one top-level task (two-space indented key)
// up to the next top-level task.
func taskBlock(lines []string, task string, maxLines int) (start int, block []string) {
	start = -1
	for i, l := range lines {
		if strings.TrimSpace(l) == task+":" {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, nil
	}
	for i := start; i < len(lines) && (maxLines <= 0 || i < start+maxLines); i++ {
		if i > start && strings.HasPrefix(lines[i], "  ") && !strings.HasPrefix(lines[i], "   ") &&
			strings.HasSuffix(strings.TrimSpace(lines[i]), ":") {
			break
		}
		block = append(block, lines[i])
	}
	return start, block
}

// infra:install:cell installs ONE cell's platform, so its kube context is a
// parameter, never cell-0.
//
// Measured 2026-08-10: cell-0 ran 8 platform components and cell-1 ran 4,
// because infra:install:cell synced the full helmfile with a FIXED
// `--kube-context {{.CELL_0_CONTEXT}}`. A tenant placed on cell-1 got less
// platform than one on cell-0, and `clusters:recreate -- cell-1` installed on
// the wrong cluster without an error. The guard is textual on purpose: it
// rejects the fixed context, which is the cause, not the divergence it causes.
func TestInstallCellDoesNotPinOneCell(t *testing.T) {
	b, err := os.ReadFile(taskfilePath)
	if err != nil {
		t.Fatalf("read taskfiles/infra.yml: %v", err)
	}
	lines := strings.Split(string(b), "\n")
	start, block := taskBlock(lines, "infra:install:cell", 0)
	if start < 0 {
		t.Fatal("task infra:install:cell not found — the guard lost its target and must be revisited")
	}
	var offenders []string
	for i, l := range block {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue // the explanation of the fix names the old variable
		}
		if strings.Contains(l, "CELL_0_CONTEXT") {
			offenders = append(offenders, fmt.Sprintf("infra.yml:%d", start+i+1))
		}
	}
	if len(offenders) > 0 {
		t.Errorf("infra:install:cell pins cell-0 on %d line(s): %s\n"+
			"The task installs ONE cell's platform, so the context must be a parameter.",
			len(offenders), strings.Join(offenders, ", "))
	}
}

// Every cell appears in every list that enumerates the cells.
//
// The cell lists are still written by hand in three tasks, and a third cell
// added to infra/k3d/ could enter some and not others — the same failure as a
// pinned context (a cell with less platform, without an error). The source of
// truth is infra/k3d/cell-*-cluster.yaml: each file is a cell that exists.
func TestEveryCellAppearsInEveryListThatEnumeratesThem(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "infra", "k3d", "cell-*-cluster.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no infra/k3d/cell-*-cluster.yaml found — the guard lost its source of truth: %v", err)
	}
	var cells []string
	for _, f := range files {
		cells = append(cells, strings.TrimSuffix(filepath.Base(f), "-cluster.yaml"))
	}
	sort.Strings(cells)

	lists := []struct {
		file, task, why string
	}{
		{"taskfiles/k3d.yml", "clusters:delete", "a cell missing here survives `clean:all` and the next cleanroom starts dirty"},
		{"taskfiles/infra.yml", "infra:install", "a cell missing here gets no platform"},
		{"Taskfile.yml", "k3d:bootstrap-cells", "a cell missing here gets no access credential"},
	}
	for _, l := range lists {
		t.Run(l.task, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(repoRoot, l.file))
			if err != nil {
				t.Fatalf("read %s: %v", l.file, err)
			}
			start, block := taskBlock(strings.Split(string(b), "\n"), l.task, 40)
			if start < 0 {
				t.Fatalf("task %q not found in %s — the guard lost its target", l.task, l.file)
			}
			text := strings.Join(block, "\n")
			for _, c := range cells {
				// `cell-0`, `loopruntime-cell-0` and `CELL_0_CONTEXT` are the same cell.
				envVar := "CELL_" + strings.TrimPrefix(c, "cell-") + "_"
				if !strings.Contains(text, c) && !strings.Contains(text, envVar) {
					t.Errorf("cell %q exists (infra/k3d/%s-cluster.yaml) and does not appear in `%s`: %s",
						c, c, l.task, l.why)
				}
			}
		})
	}
}
