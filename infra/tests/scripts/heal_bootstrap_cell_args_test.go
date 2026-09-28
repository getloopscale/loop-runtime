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
	m := regexp.MustCompile(`for cell in ([0-9 ]+); do[\s\S]*?\$BOOTSTRAP" --cell "\$cell"`).FindStringSubmatch(string(heal))
	if m == nil {
		t.Fatal("the cell repair loop calling bootstrap-cell-cc.sh --cell \"$cell\" was not found")
	}
	cells := strings.Fields(m[1])
	if strings.Join(cells, ",") != "0,1" {
		t.Fatalf("the heal repairs cells %v, want 0 and 1", cells)
	}

	bootstrap := filepath.Join("..", "..", "..", "infra", "helmfile", "scripts", "bootstrap-cell-cc.sh")
	for _, cell := range cells {
		out, _ := exec.Command("bash", bootstrap, "--cell", cell, "--env", "standalone-multi", "--dry-run").CombinedOutput()
		if strings.Contains(string(out), "--cell must be one of") {
			t.Errorf("bootstrap-cell-cc.sh refuses --cell %s:\n%s", cell, out)
		}
	}
}
