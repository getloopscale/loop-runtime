// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every ExternalSecret a rotation can reach has a restart entry in rotate-secret.sh,
// and every entry names one.
//
// rotate-secret.sh resolves WHICH ExternalSecrets read a path from the chart
// inventory itself, so that list has one source and cannot drift. What the chart
// does not say is which Deployments consume each target Secret — that is the one
// table the script keeps, RESTARTS_BY_EXTERNALSECRET, keyed by targetSecret.
//
// The two directions fail differently, and both fail:
//   - an inventory ExternalSecret missing from the table: the script refuses the
//     rotation (it will not skip a restart), so an operator under incident pressure
//     is blocked on a secret the chart already ships;
//   - a table entry naming no inventory ExternalSecret: no rotation reaches it, and
//     a restart list that outlived its secret describes a platform that is gone.

const externalSecretsInventory = "../../helm/loop-runtime-external-secrets/values.yaml"

func TestRotationRestartTableMatchesTheExternalSecretInventory(t *testing.T) {
	inventory := inventoryTargets(t)
	table := restartTable(t)
	if len(table) == 0 {
		t.Fatalf("RESTARTS_BY_EXTERNALSECRET parsed to zero entries in %s — the heredoc moved or was renamed", rotateScript)
	}

	for _, es := range inventory {
		if !slices.Contains(table, es) {
			t.Errorf("ExternalSecret %q is in the inventory with no restart entry — rotate-secret.sh refuses it. "+
				"Add '%s|<comma-separated-deployments>' to RESTARTS_BY_EXTERNALSECRET.", es, es)
		}
	}
	for _, es := range table {
		if !slices.Contains(inventory, es) {
			t.Errorf("restart entry %q names no ExternalSecret in the inventory — no rotation reaches it. "+
				"Remove it from RESTARTS_BY_EXTERNALSECRET.", es)
		}
	}
}

// inventoryTargets returns the distinct targetSecret names of the chart inventory.
func inventoryTargets(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(externalSecretsInventory)
	if err != nil {
		t.Fatalf("read the inventory: %v", err)
	}
	var inv struct {
		ExternalSecrets []struct {
			TargetSecret string `yaml:"targetSecret"`
		} `yaml:"externalSecrets"`
	}
	if err := yaml.Unmarshal(b, &inv); err != nil {
		t.Fatalf("decode %s: %v", externalSecretsInventory, err)
	}
	var names []string
	for _, es := range inv.ExternalSecrets {
		names = append(names, es.TargetSecret)
	}
	if len(names) == 0 {
		t.Fatalf("%s declares no ExternalSecret, so there is nothing to compare", externalSecretsInventory)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// restartTable returns the distinct keys of the RESTARTS_BY_EXTERNALSECRET heredoc.
func restartTable(t *testing.T) []string {
	t.Helper()
	var keys []string
	inTable := false
	for _, line := range strings.Split(readSource(t, rotateScript), "\n") {
		switch {
		case strings.HasPrefix(line, "RESTARTS_BY_EXTERNALSECRET=$(cat <<"):
			inTable = true
		case inTable && strings.HasPrefix(line, "EOF"):
			inTable = false
		case inTable && strings.Contains(line, "|"):
			key, _, _ := strings.Cut(line, "|")
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}
