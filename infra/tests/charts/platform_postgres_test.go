// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The platform's PostgreSQL (infra/helm/platform-postgres) is one CloudNativePG Cluster.
// These pin what a production database has to have whatever the environment sets: no
// plaintext connection, checksummed pages, memory and CPU it cannot be evicted for, and
// a network that admits only its clients. What an environment chooses (how many
// instances, how they replicate, a WAL volume) is pinned as a consequence of its values.

const platformPostgresBase = `
image: registry.example/postgresql:16.14
databases:
  - { name: looptrace, owner: looptrace, passwordSecret: looptrace-postgres-role }
`

func renderPlatformPostgres(t *testing.T, values string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte(platformPostgresBase+values), 0o600); err != nil {
		t.Fatal(err)
	}
	return renderChart(t, uniqueChart(t, "platform-postgres"), "platform-postgres", nil, f)
}

func platformCluster(t *testing.T, values string) map[string]any {
	t.Helper()
	cluster := renderedObject(t, renderPlatformPostgres(t, values), "Cluster", "platform-postgres")
	if cluster == nil {
		t.Fatal("the chart renders no Cluster")
	}
	return cluster
}

func TestPlatformPostgres_RefusesConnectionsWithoutTLS(t *testing.T) {
	hba, _ := dig(platformCluster(t, ""), "spec", "postgresql", "pg_hba").([]any)

	// The operator appends `host all all all scram-sha-256` after these, which admits
	// plaintext. The first matching rule wins, so the pair below has to come first.
	want := []string{"hostssl all all all scram-sha-256", "hostnossl all all all reject"}
	if len(hba) != len(want) {
		t.Fatalf("pg_hba = %v, want %v", hba, want)
	}
	for i, rule := range want {
		if hba[i] != rule {
			t.Errorf("pg_hba[%d] = %v, want %q", i, hba[i], rule)
		}
	}
}

func TestPlatformPostgres_ChecksumsItsPages(t *testing.T) {
	if got := dig(platformCluster(t, ""), "spec", "bootstrap", "initdb", "dataChecksums"); got != true {
		t.Errorf("bootstrap.initdb.dataChecksums = %v, want true: PostgreSQL 16 leaves it off", got)
	}
}

func TestPlatformPostgres_IsNotEvictableForItsResources(t *testing.T) {
	resources, _ := dig(platformCluster(t, ""), "spec", "resources").(map[string]any)
	requests, _ := resources["requests"].(map[string]any)
	limits, _ := resources["limits"].(map[string]any)
	for _, resource := range []string{"cpu", "memory"} {
		if requests[resource] == nil || requests[resource] != limits[resource] {
			t.Errorf("%s: requests %v, limits %v; equal values of both give the pod the Guaranteed class",
				resource, requests[resource], limits[resource])
		}
	}
}

func TestPlatformPostgres_SingleInstanceDoesNotBlockNodeDrains(t *testing.T) {
	cluster := platformCluster(t, "instances: 1\n")
	if got := dig(cluster, "spec", "enablePDB"); got != false {
		t.Errorf("enablePDB = %v with one instance: its budget allows no disruption, so no node could be drained", got)
	}
	if dig(cluster, "spec", "postgresql", "synchronous") != nil {
		t.Error("one instance has no standby to replicate to synchronously")
	}
}

func TestPlatformPostgres_ThreeInstancesSurviveAZoneAndLoseNoCommit(t *testing.T) {
	cluster := platformCluster(t, "instances: 3\n")

	if got := dig(cluster, "spec", "enablePDB"); got != true {
		t.Errorf("enablePDB = %v with three instances, want true", got)
	}
	if got := dig(cluster, "spec", "affinity", "topologyKey"); got != "topology.kubernetes.io/zone" {
		t.Errorf("instances spread over %v, want zones", got)
	}
	sync, _ := dig(cluster, "spec", "postgresql", "synchronous").(map[string]any)
	if sync["method"] != "any" || sync["number"] != 1 {
		t.Errorf("synchronous = %v, want a quorum of one standby", sync)
	}
	if got := dig(cluster, "spec", "primaryUpdateMethod"); got != "switchover" {
		t.Errorf("primaryUpdateMethod = %v, want switchover: a standby is promoted before the primary restarts", got)
	}
}

func TestPlatformPostgres_KeepsWALOnItsOwnVolumeWhenSized(t *testing.T) {
	if dig(platformCluster(t, ""), "spec", "walStorage") != nil {
		t.Error("walStorage rendered with no size set")
	}
	cluster := platformCluster(t, "walStorage:\n  size: 2Gi\n")
	if got := dig(cluster, "spec", "walStorage", "size"); got != "2Gi" {
		t.Errorf("walStorage.size = %v, want 2Gi", got)
	}
}

func TestPlatformPostgres_AdmitsOnlyItsClientsTheOperatorAndTheScraper(t *testing.T) {
	policy := renderedObject(t, renderPlatformPostgres(t, ""), "NetworkPolicy", "platform-postgres")
	if policy == nil {
		t.Fatal("the chart renders no NetworkPolicy for the cluster's pods")
	}
	if got := dig(policy, "spec", "podSelector", "matchLabels", "cnpg.io/cluster"); got != "platform-postgres" {
		t.Errorf("the policy selects %v, want the cluster's pods", got)
	}
	raw := renderPlatformPostgres(t, "")
	for _, want := range []string{
		"kubernetes.io/metadata.name: cnpg-system",       // the operator: status and SQL
		"app.kubernetes.io/part-of: loopzero-data-plane", // the services that keep data here
		"kubernetes.io/metadata.name: monitoring",        // the metrics scraper
		"port: 8000", "port: 5432", "port: 9187",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("the NetworkPolicy lacks %q", want)
		}
	}
	types, _ := dig(policy, "spec", "policyTypes").([]any)
	if len(types) != 1 || types[0] != "Ingress" {
		t.Errorf("policyTypes = %v, want only Ingress: the pods reach the API server and the backup store", types)
	}
}

// Zone spread is a preference, so two instances can still land in one zone. The quorum
// then has to pick its standby from another zone, or one outage takes the primary and
// the only copy of its last commits.
func TestPlatformPostgres_SynchronousStandbyIsInAnotherZone(t *testing.T) {
	keys, _ := dig(platformCluster(t, "instances: 3\n"), "spec", "postgresql", "synchronous", "nodeFailureDomainKeys").([]any)
	if len(keys) != 1 || keys[0] != "topology.kubernetes.io/zone" {
		t.Errorf("synchronous.nodeFailureDomainKeys = %v, want the zone label", keys)
	}
}

func TestPlatformPostgres_ResizesWithoutRestartingTheInstance(t *testing.T) {
	if got := dig(platformCluster(t, ""), "spec", "resourcesUpdateStrategy"); got != "inPlace" {
		t.Errorf("resourcesUpdateStrategy = %v, want inPlace: a change of CPU or memory would restart the only instance", got)
	}
}
