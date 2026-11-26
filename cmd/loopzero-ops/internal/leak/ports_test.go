// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func writePorts(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "host-ports.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return p
}

// The polarity: the previous firewall opened by default, and 17 ports were
// measured reachable from the internet on no list. Here absence FAILS.
func TestUndeclaredPortFails(t *testing.T) {
	decls, err := ParsePorts(writePorts(t, "3002 | edge | loopzero-cloud through the edge\n"))
	if err != nil {
		t.Fatalf("ParsePorts: %v", err)
	}
	undeclared, _ := AuditPorts([]int{3002, 30082}, decls)
	if len(undeclared) != 1 || undeclared[0].Port != 30082 {
		t.Fatalf("want 30082 reported as undeclared, got %+v", undeclared)
	}
	if !strings.Contains(undeclared[0].Why, "closed by default") {
		t.Errorf("the reason should explain the polarity, got %q", undeclared[0].Why)
	}
}

// The class decides, not the reader's intent: only `external` stays open.
func TestOnlyExternalStaysOpen(t *testing.T) {
	decls, err := ParsePorts(writePorts(t, `8787  | external | therag_api read by a CI gate from outside
14433 | edge     | Kratos through the edge over HTTPS
30082 | admin    | ArgoCD
5432  | host     | Postgres
18083 | pending  | consumer not measured
`))
	if err != nil {
		t.Fatalf("ParsePorts: %v", err)
	}
	toClose := PortsToClose([]int{8787, 14433, 30082, 5432, 18083}, decls)
	want := map[int]bool{14433: true, 30082: true, 5432: true}
	if len(toClose) != len(want) {
		t.Fatalf("want %d ports to close, got %d: %+v", len(want), len(toClose), toClose)
	}
	for _, d := range toClose {
		if !want[d.Port] {
			t.Errorf("port %d (%s) should not be closed", d.Port, d.Class)
		}
	}
}

// The 2026-07-30 lesson: closing a port whose consumer nobody measured took
// down the CI gates that detect deploys. A pending port is never closed blind.
func TestPendingPortIsNotClosedBlind(t *testing.T) {
	decls, err := ParsePorts(writePorts(t, "18083 | pending | consumer not measured\n"))
	if err != nil {
		t.Fatalf("ParsePorts: %v", err)
	}
	if toClose := PortsToClose([]int{18083}, decls); len(toClose) != 0 {
		t.Errorf("a pending port cannot be closed before its consumer is measured, got %+v", toClose)
	}
}

// In a security registry, the line the parser skips is exactly the port nobody
// reviewed: a malformed line is an error.
func TestMalformedLineIsAnErrorNotSilence(t *testing.T) {
	cases := map[string]string{
		"no class":      "3002\n",
		"invalid class": "3002 | whatever | something\n",
		"no reason":     "3002 | edge | \n",
		"invalid port":  "abc | edge | something\n",
		"duplicated":    "3002 | edge | one\n3002 | host | other\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePorts(writePorts(t, content)); err == nil {
				t.Error("want an error; a security registry cannot accept a line nobody can review")
			}
		})
	}
}

// The file that actually governs the host parses, and every `external` row
// names its consumer.
func TestRepositoryPortRegistryIsValid(t *testing.T) {
	decls, err := ParsePorts(filepath.Join(qtestutil.MustRepoRoot(), "infra", "quality", "host-ports.txt"))
	if err != nil {
		t.Fatalf("the real registry does not parse: %v", err)
	}
	if len(decls) < 20 {
		t.Errorf("the registry has %d ports; 39 were measured listening on 2026-08-10, so a much "+
			"smaller registry suggests it stopped following the host", len(decls))
	}
	for _, d := range decls {
		if d.Class == ClassExternal && len(d.Reason) < 20 {
			t.Errorf("port %d is `external` with too short a reason (%q) — the exception must name "+
				"the consumer and how it was measured", d.Port, d.Reason)
		}
	}
}
