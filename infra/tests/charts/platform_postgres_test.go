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
image: registry.example/postgresql:19.0
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
		t.Errorf("bootstrap.initdb.dataChecksums = %v, want true: PostgreSQL before 18 leaves it off", got)
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

const platformPostgresBackupValues = `
backup:
  bucket: loopzero-test-postgres-backups
  roleArn: arn:aws:iam::000000000000:role/loopzero-test-core-platform-postgres
`

func TestPlatformPostgres_WithoutABucketRendersNoBackup(t *testing.T) {
	out := renderPlatformPostgres(t, "")
	for _, kind := range []string{"ObjectStore", "ScheduledBackup"} {
		if len(renderedObjectsOfKind(t, out, kind)) != 0 {
			t.Errorf("a %s rendered with no backup.bucket", kind)
		}
	}
	if dig(platformCluster(t, ""), "spec", "plugins") != nil {
		t.Error("the Cluster names a plugin with no backup.bucket")
	}
}

// The base backups and the WAL archive go to the bucket through the Barman Cloud plugin,
// which replaces the operator's own barmanObjectStore (removed in CloudNativePG 1.32).
func TestPlatformPostgres_ArchivesWALAndBacksUpToTheBucket(t *testing.T) {
	out := renderPlatformPostgres(t, platformPostgresBackupValues)

	store := renderedObject(t, out, "ObjectStore", "platform-postgres")
	if store == nil {
		t.Fatal("the chart renders no ObjectStore")
	}
	if got := dig(store, "spec", "configuration", "destinationPath"); got != "s3://loopzero-test-postgres-backups/" {
		t.Errorf("destinationPath = %v", got)
	}
	if got := dig(store, "spec", "retentionPolicy"); got != "30d" {
		t.Errorf("retentionPolicy = %v, want the default 30d", got)
	}

	cluster := renderedObject(t, out, "Cluster", "platform-postgres")
	plugins, _ := dig(cluster, "spec", "plugins").([]any)
	if len(plugins) != 1 {
		t.Fatalf("the Cluster names %d plugins, want 1", len(plugins))
	}
	plugin, _ := plugins[0].(map[string]any)
	if plugin["name"] != "barman-cloud.cloudnative-pg.io" || plugin["isWALArchiver"] != true {
		t.Errorf("plugin = %v, want the Barman Cloud plugin as the WAL archiver", plugin)
	}
	if got := dig(plugin, "parameters", "barmanObjectName"); got != "platform-postgres" {
		t.Errorf("the plugin reads the ObjectStore %v, want platform-postgres", got)
	}

	backup := renderedObject(t, out, "ScheduledBackup", "platform-postgres")
	if backup == nil {
		t.Fatal("the chart renders no ScheduledBackup")
	}
	if got := dig(backup, "spec", "method"); got != "plugin" {
		t.Errorf("ScheduledBackup method = %v, want plugin", got)
	}
	if got := dig(backup, "spec", "pluginConfiguration", "name"); got != "barman-cloud.cloudnative-pg.io" {
		t.Errorf("ScheduledBackup plugin = %v", got)
	}
	// Six fields, seconds first: five would be read as a different schedule.
	if schedule, _ := dig(backup, "spec", "schedule").(string); len(strings.Fields(schedule)) != 6 {
		t.Errorf("schedule %q does not have the six fields the operator reads", schedule)
	}
	if got := dig(backup, "spec", "immediate"); got != true {
		t.Errorf("immediate = %v: a new cluster would have no base backup until the first schedule", got)
	}
}

// No key is stored anywhere: the instances assume the role their service account names.
func TestPlatformPostgres_ReachesTheBucketAsItsRoleWithoutAKey(t *testing.T) {
	out := renderPlatformPostgres(t, platformPostgresBackupValues)

	cluster := renderedObject(t, out, "Cluster", "platform-postgres")
	annotations, _ := dig(cluster, "spec", "serviceAccountTemplate", "metadata", "annotations").(map[string]any)
	if got := annotations["eks.amazonaws.com/role-arn"]; got != "arn:aws:iam::000000000000:role/loopzero-test-core-platform-postgres" {
		t.Errorf("the instances' service account assumes %v", got)
	}
	store := renderedObject(t, out, "ObjectStore", "platform-postgres")
	credentials, _ := dig(store, "spec", "configuration", "s3Credentials").(map[string]any)
	if len(credentials) != 1 || credentials["inheritFromIAMRole"] != true {
		t.Errorf("s3Credentials = %v, want only inheritFromIAMRole", credentials)
	}
}

func TestPlatformPostgres_ABucketWithoutARoleIsRefused(t *testing.T) {
	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte(platformPostgresBase+"backup:\n  bucket: some-bucket\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := renderChartE(t, uniqueChart(t, "platform-postgres"), "platform-postgres", nil, f)
	if err == nil || !strings.Contains(err.Error(), "backup.roleArn") {
		t.Errorf("render with a bucket and no role: err = %v, want one naming backup.roleArn", err)
	}
}

// Recovery builds the cluster from a server's backups in the bucket, up to a moment when
// one is given. The recovered cluster archives under another name: writing WAL over the
// archive it was recovered from would destroy the only way back.
func TestPlatformPostgres_RecoversFromTheBucket(t *testing.T) {
	cluster := platformCluster(t, platformPostgresBackupValues+`  serverName: platform-postgres-2
recovery:
  serverName: platform-postgres
  targetTime: "2026-11-26T12:00:00Z"
`)
	if dig(cluster, "spec", "bootstrap", "initdb") != nil {
		t.Error("a recovered cluster still bootstraps with initdb")
	}
	if got := dig(cluster, "spec", "bootstrap", "recovery", "source"); got != "recovery" {
		t.Errorf("bootstrap.recovery.source = %v", got)
	}
	if got := dig(cluster, "spec", "bootstrap", "recovery", "recoveryTarget", "targetTime"); got != "2026-11-26T12:00:00Z" {
		t.Errorf("recoveryTarget.targetTime = %v", got)
	}
	external, _ := dig(cluster, "spec", "externalClusters").([]any)
	if len(external) != 1 {
		t.Fatalf("externalClusters has %d entries, want 1", len(external))
	}
	source, _ := external[0].(map[string]any)
	if got := dig(source, "plugin", "parameters", "serverName"); got != "platform-postgres" {
		t.Errorf("the cluster recovers from server %v, want platform-postgres", got)
	}
	plugins, _ := dig(cluster, "spec", "plugins").([]any)
	own, _ := plugins[0].(map[string]any)
	if got := dig(own, "parameters", "serverName"); got != "platform-postgres-2" {
		t.Errorf("the recovered cluster archives as %v, want platform-postgres-2", got)
	}
}

func TestPlatformPostgres_RecoveryOverItsOwnArchiveIsRefused(t *testing.T) {
	f := filepath.Join(t.TempDir(), "values.yaml")
	values := platformPostgresBase + platformPostgresBackupValues + "recovery:\n  serverName: platform-postgres\n"
	if err := os.WriteFile(f, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := renderChartE(t, uniqueChart(t, "platform-postgres"), "platform-postgres", nil, f)
	if err == nil || !strings.Contains(err.Error(), "backup.serverName") {
		t.Errorf("recovery from the server the cluster archives as: err = %v, want one naming backup.serverName", err)
	}
}
