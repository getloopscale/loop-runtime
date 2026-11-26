// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
	if got := dig(cluster, "metadata", "namespace"); got != nil && got != "loopzero-data-plane" {
		t.Errorf("Cluster namespace = %v, want loopzero-data-plane", got)
	}
	image, _ := dig(cluster, "spec", "imageName").(string)
	if !regexp.MustCompile(`^ghcr\.io/cloudnative-pg/postgresql:16\.\d+[a-z-]*@sha256:[0-9a-f]{64}$`).MatchString(image) {
		t.Errorf("Cluster image %q is not a PostgreSQL 16 release pinned by digest", image)
	}
	if got := dig(cluster, "spec", "instances"); got != 1 {
		t.Errorf("Cluster instances = %v, want 1", got)
	}
}

// releaseRow is one release as `helmfile list` reports it for an environment.
type releaseRow struct {
	Name      string `json:"name"`
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

func TestStaging_CloudNativePGOperatorVersionIsPinned(t *testing.T) {
	row := releaseInEnvironment(t, "staging", "cloudnative-pg")
	if !row.Installed {
		t.Error("staging does not install the cloudnative-pg release")
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(row.Version) {
		t.Errorf("the cloudnative-pg chart version is %q, want an exact version", row.Version)
	}
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
