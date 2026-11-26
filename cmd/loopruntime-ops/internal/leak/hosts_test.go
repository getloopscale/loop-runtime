// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package leak_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/leak"
)

var realInventory = filepath.Join("..", "..", "..", "..", "infra", "quality", "proxy-exposure.txt")

// The inventory declares what the edge exposes; the probe must reach EVERY
// host it declares, in declaration order, without repeats.
func TestHosts_ListsEveryDeclaredHost(t *testing.T) {
	inv := writeInventory(t, `# comment ignored
app-dev.getloopscale.com /v1/x | reviewed | note
cloud.getloopscale.com - | reviewed | other
app-dev.getloopscale.com /v1/y | reviewed | repeated
auth.getloopscale.com - | reviewed | third
`)
	got, err := leak.Hosts(inv)
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	want := []string{"app-dev.getloopscale.com", "cloud.getloopscale.com", "auth.getloopscale.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("hosts = %v, want %v (declaration order)", got, want)
	}
}

// A missing inventory is an error, not an empty list: probing zero hosts is
// indistinguishable from having nothing to probe.
func TestHosts_MissingInventoryIsAnError(t *testing.T) {
	if _, err := leak.Hosts(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing inventory should be an error")
	}
}

// The real population: this repository's inventory must be read in full, and
// the unsuffixed production names removed on 2026-08-19 (they served
// development instances under production names) must not come back.
func TestHosts_RealInventoryListsEveryDeclaredHost(t *testing.T) {
	hosts, err := leak.Hosts(realInventory)
	if err != nil {
		t.Fatalf("real inventory: %v", err)
	}
	// Floor measured 2026-08-19: 4 hosts. Fewer means the extraction shrank and
	// the green is worth less without anything saying so.
	if len(hosts) < 4 {
		t.Errorf("only %d host(s) reached (%v); the inventory declared 4 on 2026-08-19", len(hosts), hosts)
	}
	for _, h := range hosts {
		for _, forbidden := range []string{"app.getloopscale.com", "cloud.getloopscale.com", "auth.getloopscale.com"} {
			if h == forbidden {
				t.Errorf("%s is back in the inventory; it was removed for serving a development "+
					"instance under a production name", h)
			}
		}
	}
}

func writeInventory(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "proxy-exposure.txt")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A host declared as a whole site (`-`) must not leave the probe in SILENCE:
// it is the widest exposure there is, and exempting it once made live hosts
// produce zero targets and zero complaints.
func TestTargets_WholeSiteIsReportedNotSilentlyExempt(t *testing.T) {
	inv := writeInventory(t, "cloud.getloopscale.com - | reviewed | authenticated BFF\n")
	targets, uncovered, err := leak.Targets(inv, "cloud.getloopscale.com")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(targets) == 0 && len(uncovered) == 0 {
		t.Fatal("a declared host produced zero targets AND zero complaints — silence, indistinguishable from coverage")
	}
}

// Over the real population: no declared host may vanish from the probe
// without a trace.
func TestTargets_NoDeclaredHostVanishesSilently(t *testing.T) {
	hosts, err := leak.Hosts(realInventory)
	if err != nil {
		t.Fatalf("real inventory: %v", err)
	}
	var silent []string
	for _, h := range hosts {
		if strings.HasPrefix(h, "*.") {
			continue // a wildcard has no concrete hostname; the subcommand reports it
		}
		targets, uncovered, err := leak.Targets(realInventory, h)
		if err != nil {
			t.Fatalf("%s: %v", h, err)
		}
		if len(targets) == 0 && len(uncovered) == 0 {
			silent = append(silent, h)
		}
	}
	if len(silent) > 0 {
		t.Errorf("declared hosts that vanish from the probe without a trace: %v", silent)
	}
}

// A coverage hole must say WHOSE it is: several identical "-" lines tell the
// reader nothing about which host is uncovered.
func TestTargets_CoverageHoleNamesItsHost(t *testing.T) {
	inv := writeInventory(t, "cloud.getloopscale.com - | reviewed | authenticated BFF\n")
	_, uncovered, err := leak.Targets(inv, "cloud.getloopscale.com")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(uncovered) == 0 || !strings.Contains(uncovered[0], "cloud.getloopscale.com") {
		t.Errorf("the hole does not name its host: %v", uncovered)
	}
}

// An exemption declared with a glob covers what the glob declares.
func TestTargets_GlobExemptionCoversWhatItDeclares(t *testing.T) {
	inv := writeInventory(t, `h.example.dev /self-service/registration | reviewed | note
h.example.dev /self-service/registration/methods/password | reviewed | note
`)
	_, uncovered, err := leak.Targets(inv, "h.example.dev")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(uncovered) != 0 {
		t.Errorf("paths covered by a glob exemption are not holes: %v", uncovered)
	}
}

// An `internal` route has no credential to deny; demanding a 401 from it is a
// category error, so it is neither a target nor a hole.
func TestTargets_InternalClassIsNotACoverageHole(t *testing.T) {
	inv := writeInventory(t, "h.example.dev /assets/* | internal | static bundle, no user data\n")
	targets, uncovered, err := leak.Targets(inv, "h.example.dev")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(uncovered) != 0 || len(targets) != 0 {
		t.Errorf("an internal class is neither a hole nor a target: targets=%v holes=%v", targets, uncovered)
	}
}

// A family the identity probe already covers cannot be counted as a hole: the
// report used to show `/userinfo` as "ok 401" and, lines below, as uncovered.
func TestUncoveredAfterIdentity_RemovesTheContradiction(t *testing.T) {
	holes := []string{
		"auth-dev.getloopscale.com /userinfo",
		"auth-dev.getloopscale.com /.well-known/*",
		"auth-dev.getloopscale.com /oauth2/*",
	}
	rest := leak.UncoveredAfterIdentity(holes, leak.IdentityTargets("auth-dev.getloopscale.com"))
	if len(rest) != 1 || !strings.Contains(rest[0], "/oauth2/") {
		t.Errorf("only /oauth2/* is not probed by identity and must remain a hole; got %v", rest)
	}
}

// A row the probe cannot read is a declaration it would silently not probe.
// The inventory format is shared with the proxy-exposure gate, which rejects
// the same row; the probe must not treat it as absent.
func TestHostsAndTargets_RefuseAMalformedInventory(t *testing.T) {
	inv := writeInventory(t, "app-dev.getloopscale.com /v1/* | reviewed | ok\napp-dev.getloopscale.com /v2/* | nonsense\n")
	if _, err := leak.Hosts(inv); err == nil {
		t.Error("Hosts: want an error for a malformed row")
	}
	if _, _, err := leak.Targets(inv, "app-dev.getloopscale.com"); err == nil {
		t.Error("Targets: want an error for a malformed row")
	}
}
