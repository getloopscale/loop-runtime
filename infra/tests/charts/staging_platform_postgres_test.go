// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"encoding/json"
	"regexp"
	"testing"
)

// Staging runs the platform's own PostgreSQL for data-plane services on CloudNativePG:
// one Cluster on the core cluster. RDS stays the control plane's database. The other
// environments do not turn it on, so they render nothing of it.

func TestStaging_PlatformPostgresRendersOneClusterOnAPinnedImage(t *testing.T) {
	out := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "platform-postgres",
		"infra/helm/platform-postgres", helmfileCoreEnv...)

	clusters := renderedObjectsOfKind(t, out, "Cluster")
	if len(clusters) != 1 {
		t.Fatalf("staging renders %d CloudNativePG Clusters, want 1", len(clusters))
	}
	cluster := clusters[0]
	if got := dig(cluster, "metadata", "namespace"); got != nil && got != "loopruntime-data-plane" {
		t.Errorf("Cluster namespace = %v, want loopruntime-data-plane", got)
	}
	image, _ := dig(cluster, "spec", "imageName").(string)
	if !regexp.MustCompile(`^ghcr\.io/cloudnative-pg/postgresql:19\.\d+[a-z-]*@sha256:[0-9a-f]{64}$`).MatchString(image) {
		t.Errorf("Cluster image %q is not a PostgreSQL 19 release pinned by digest", image)
	}
	if got := dig(cluster, "spec", "instances"); got != 1 {
		t.Errorf("Cluster instances = %v, want 1", got)
	}
}

// releaseRow is one release as `helmfile list` reports it for an environment.
type releaseRow struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Chart     string `json:"chart"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

func releaseInEnvironment(t *testing.T, env, release string) releaseRow {
	t.Helper()
	out := helmfileRun(t, helmfileCoreEnv,
		"-f", "infra/helmfile/core.helmfile.yaml.gotmpl", "-e", env, "list", "--output=json")
	var rows []releaseRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decode helmfile list output: %v\n%s", err, truncate(out, 500))
	}
	for _, row := range rows {
		if row.Name == release {
			return row
		}
	}
	t.Fatalf("helmfile list -e %s has no release %q", env, release)
	return releaseRow{}
}

func TestOtherEnvironments_DoNotInstallPlatformPostgres(t *testing.T) {
	for _, env := range []string{"dev", "dev-public", "prod", "standalone"} {
		for _, release := range []string{"cloudnative-pg", "platform-postgres"} {
			if releaseInEnvironment(t, env, release).Installed {
				t.Errorf("%s installs %s; only staging turns platformPostgres on", env, release)
			}
		}
	}
}

// looptrace is the one data-plane service staging runs. Its release and its database are
// turned on together: the release writes the Secret the database role's password is read
// from, and the service connects as that role.
func TestStaging_RunsLooptraceOnThePlatformPostgres(t *testing.T) {
	if !releaseInEnvironment(t, "staging", "looptrace").Installed {
		t.Fatal("staging does not install the looptrace release")
	}

	service := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "looptrace",
		"infra/helm/data-plane-service", stagingOutputs...)
	deployment := renderedObject(t, service, "Deployment", "looptrace")
	if deployment == nil {
		t.Fatal("the looptrace release renders no Deployment")
	}
	if len(renderedObjectsOfKind(t, service, "StatefulSet")) != 0 {
		t.Error("the looptrace release runs a PostgreSQL of its own; staging keeps its data on platform-postgres")
	}
	if renderedObject(t, service, "ExternalSecret", "looptrace-postgres-role") == nil {
		t.Error("the looptrace release does not write the Secret its database role reads")
	}

	database := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "platform-postgres",
		"infra/helm/platform-postgres", stagingOutputs...)
	db := renderedObject(t, database, "Database", "platform-postgres-looptrace")
	if db == nil {
		t.Fatal("platform-postgres renders no Database for looptrace")
	}
	if got := dig(db, "spec", "owner"); got != "looptrace" {
		t.Errorf("the looptrace database is owned by %v, want looptrace", got)
	}
}

// Staging backs the platform's PostgreSQL up to the bucket and as the role infra-live
// declares for it (storage/s3 postgres-backups, identity/core-platform-postgres). The
// names are built from the environment's own, as every bucket and role is.
func TestStaging_PlatformPostgresBacksUpToItsBucketAsItsRole(t *testing.T) {
	out := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "platform-postgres",
		"infra/helm/platform-postgres", helmfileCoreEnv...)

	store := renderedObject(t, out, "ObjectStore", "platform-postgres")
	if store == nil {
		t.Fatal("staging renders no ObjectStore for platform-postgres")
	}
	if got := dig(store, "spec", "configuration", "destinationPath"); got != "s3://loopruntime-staging-postgres-backups-000000000000/" {
		t.Errorf("destinationPath = %v", got)
	}
	cluster := renderedObject(t, out, "Cluster", "platform-postgres")
	role := dig(cluster, "spec", "serviceAccountTemplate", "metadata", "annotations", "eks.amazonaws.com/role-arn")
	if role != "arn:aws:iam::000000000000:role/loopruntime-staging-core-platform-postgres" {
		t.Errorf("the instances assume %v", role)
	}
	if renderedObject(t, out, "ScheduledBackup", "platform-postgres") == nil {
		t.Error("staging renders no ScheduledBackup for platform-postgres")
	}
}

// The plugin serves the operator over mutual TLS with certificates cert-manager issues,
// and has to be in the operator's namespace.
func TestStaging_InstallsTheBarmanCloudPluginBesideTheOperator(t *testing.T) {
	row := releaseInEnvironment(t, "staging", "plugin-barman-cloud")
	if !row.Installed {
		t.Error("staging does not install the plugin-barman-cloud release")
	}
	if row.Namespace != "cnpg-system" {
		t.Errorf("plugin-barman-cloud is in %q, want cnpg-system, the operator's namespace", row.Namespace)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(row.Version) {
		t.Errorf("the plugin-barman-cloud chart version is %q, want an exact version", row.Version)
	}
	for _, env := range []string{"dev", "dev-public", "prod", "standalone"} {
		if releaseInEnvironment(t, env, "plugin-barman-cloud").Installed {
			t.Errorf("%s installs plugin-barman-cloud; only staging turns platformPostgres on", env)
		}
	}
}
