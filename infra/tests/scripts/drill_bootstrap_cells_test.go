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

// drill-helmfile-bootstrap.sh provisions a DigitalOcean drill environment and
// cannot run here, so these tests pin the three contracts it shares with code
// that can: the cell ids bootstrap-cell-cc.sh accepts, one status patch per
// cell, and the api's leader-election lock key.

func readDrillBootstrap(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "drill", "drill-helmfile-bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The drill passed --cell default/1/2 after bootstrap-cell-cc.sh had moved to
// 0/1, so the first call was refused and, under pipefail, the drill exited.
func TestDrillBootstrap_CreatesEachCellWithAnIDBootstrapAccepts(t *testing.T) {
	m := regexp.MustCompile(`for cell_id in ([0-9 ]+); do\s+bash "\$CELL_SCRIPT" --cell "\$cell_id"`).FindStringSubmatch(readDrillBootstrap(t))
	if m == nil {
		t.Fatal("the cell bootstrap loop was not found")
	}
	ids := strings.Fields(m[1])
	if strings.Join(ids, ",") != "0,1" {
		t.Fatalf("cell ids = %v, want 0 and 1", ids)
	}
	bootstrap := filepath.Join("..", "..", "..", "infra", "helmfile", "scripts", "bootstrap-cell-cc.sh")
	for _, id := range ids {
		out, _ := exec.Command("bash", bootstrap, "--cell", id, "--env", "drill", "--dry-run").CombinedOutput()
		if strings.Contains(string(out), "--cell must be one of") {
			t.Errorf("bootstrap-cell-cc.sh refuses --cell %s:\n%s", id, out)
		}
	}
}

func TestDrillBootstrap_PatchesEachCellOnce(t *testing.T) {
	m := regexp.MustCompile(`for cell in ([a-z0-9 -]+); do\s+kubectl patch clustercredential`).FindStringSubmatch(readDrillBootstrap(t))
	if m == nil {
		t.Fatal("the ClusterCredential patch loop was not found")
	}
	if got := strings.Join(strings.Fields(m[1]), ","); got != "cell-0,cell-1" {
		t.Fatalf("patched cells = %s, want cell-0,cell-1", got)
	}
}

// The drill must not terminate backends by advisory lock. It used to kill the
// holder of the API's leader-election lock (Bug #69), and before that every
// granted advisory lock, which also killed sessions holding per-environment
// deploy locks mid-transaction. The API now elects through the fenced lease in
// leader_generations, which a new pod takes over once the old heartbeat is
// stale, so no session holds a leader lock and any such kill can only reach a
// deploy lock.
func TestDrillBootstrap_TerminatesNoAdvisoryLockHolder(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "cmd", "leader_election.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "leader.BackgroundLoopsLeaseName") {
		t.Fatal("the API no longer elects through the fenced lease; re-examine whether the drill needs a lock heal")
	}
	drill := readDrillBootstrap(t)
	if regexp.MustCompile(`pg_terminate_backend[^;]*pg_locks`).MatchString(drill) {
		t.Fatal("the drill terminates a backend by lock; the only locks left to hit are deploy locks")
	}
}
