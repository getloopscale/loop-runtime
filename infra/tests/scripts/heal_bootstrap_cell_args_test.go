// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// k3d-cross-cluster-heal.sh repairs a missing ClusterCredential by calling
// bootstrap-cell-cc.sh --cell <id>. After the cell rename that script accepts
// only 0 and 1, while the heal still passed 1 for cell-0 (healing the wrong
// cell) and 2 for cell-1 (refused outright), so neither heal could ever work.
//
// Each --cell value the heal passes is handed to bootstrap-cell-cc.sh itself,
// in dry-run mode, and must not be refused as an argument.
func TestHeal_PassesBootstrapACellIDItAccepts(t *testing.T) {
	heal, err := os.ReadFile(filepath.Join("..", "..", "scripts", "local", "k3d-cross-cluster-heal.sh"))
	if err != nil {
		t.Fatal(err)
	}
	calls := regexp.MustCompile(`bootstrap-cell-cc\.sh|\$BOOTSTRAP\w*" --cell (\d+)`).FindAllStringSubmatch(string(heal), -1)
	var cells []string
	for _, c := range calls {
		if c[1] != "" {
			cells = append(cells, c[1])
		}
	}
	if len(cells) != 2 {
		t.Fatalf("found --cell values %v, want the two heals (H12 for cell-0, H14 for cell-1)", cells)
	}
	if cells[0] != "0" || cells[1] != "1" {
		t.Errorf("H12 heals cell-0 and H14 cell-1, got --cell %v", cells)
	}

	bootstrap := filepath.Join("..", "..", "..", "infra", "helmfile", "scripts", "bootstrap-cell-cc.sh")
	for _, cell := range cells {
		out, _ := exec.Command("bash", bootstrap, "--cell", cell, "--env", "standalone-multi", "--dry-run").CombinedOutput()
		if strings.Contains(string(out), "--cell must be one of") {
			t.Errorf("bootstrap-cell-cc.sh refuses --cell %s:\n%s", cell, out)
		}
	}
}
