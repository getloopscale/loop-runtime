// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The CloudNativePG operator is installed from the manifest its release publishes,
// vendored in infra/helm/cloudnative-pg. A release candidate has no Helm chart upstream,
// and the manifest is the installation the project documents for one.
//
// cloudNativePGDigest is the sha256 of the asset
// https://github.com/cloudnative-pg/cloudnative-pg/releases/download/v1.31.0-rc1/cnpg-1.31.0-rc1.yaml
// as published. The vendored file is that asset without its first document, the
// Namespace: Helm creates the release's namespace and refuses to adopt one it did not
// create. Putting the document back has to give the published bytes, so an edit to the
// vendored file, or a bump that forgets the digest, fails here.
const (
	cloudNativePGVersion = "1.31.0-rc1"
	cloudNativePGDigest  = "83638d85e4e43746bde8a74fc7fce5fd97fc2d6abb3c3b2954ce11fea97769ab"

	cloudNativePGNamespaceDocument = `apiVersion: v1
kind: Namespace
metadata:
  labels:
    app.kubernetes.io/name: cloudnative-pg
  name: cnpg-system
---
`
)

func cloudNativePGChartDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "infra", "helm", "cloudnative-pg")
}

func TestCloudNativePGOperatorIsTheOfficialRelease(t *testing.T) {
	dir := cloudNativePGChartDir(t)

	manifest, err := os.ReadFile(filepath.Join(dir, "templates", "cnpg-"+cloudNativePGVersion+".yaml"))
	if err != nil {
		t.Fatalf("read the vendored CloudNativePG manifest: %v", err)
	}
	sum := sha256.Sum256(append([]byte(cloudNativePGNamespaceDocument), manifest...))
	if got := hex.EncodeToString(sum[:]); got != cloudNativePGDigest {
		t.Errorf("Namespace document + vendored manifest sha256 = %s, want %s (the %s release asset)",
			got, cloudNativePGDigest, cloudNativePGVersion)
	}

	var chart struct {
		Version    string `yaml:"version"`
		AppVersion string `yaml:"appVersion"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	if err := yaml.Unmarshal(raw, &chart); err != nil {
		t.Fatalf("parse Chart.yaml: %v", err)
	}
	if chart.AppVersion != cloudNativePGVersion || chart.Version != cloudNativePGVersion {
		t.Errorf("chart version %q, appVersion %q; both name the vendored release %q",
			chart.Version, chart.AppVersion, cloudNativePGVersion)
	}
}

func TestCloudNativePGOperatorRunsTheReleaseImageInItsNamespace(t *testing.T) {
	out := renderChart(t, cloudNativePGChartDir(t), "cloudnative-pg", nil)

	deployment := renderedObject(t, out, "Deployment", "cnpg-controller-manager")
	if deployment == nil {
		t.Fatal("the chart renders no operator Deployment")
	}
	if got := dig(deployment, "metadata", "namespace"); got != "cnpg-system" {
		t.Errorf("the operator runs in %v, want cnpg-system: platform-postgres admits the operator by that namespace", got)
	}
	if want := "image: ghcr.io/cloudnative-pg/cloudnative-pg:" + cloudNativePGVersion; !strings.Contains(out, want) {
		t.Errorf("the operator does not run %q", want)
	}
	if renderedObject(t, out, "Namespace", "cnpg-system") != nil {
		t.Error("the chart renders the Namespace: Helm creates it for the release and cannot adopt it")
	}
	for _, crd := range []string{"clusters.postgresql.cnpg.io", "databases.postgresql.cnpg.io", "scheduledbackups.postgresql.cnpg.io"} {
		if renderedObject(t, out, "CustomResourceDefinition", crd) == nil {
			t.Errorf("the chart lacks the CRD %s", crd)
		}
	}
}

// The release keeps its name and namespace, so an environment that ran the upstream chart
// upgrades in place. A release under another name would have to take the CRDs from the
// old one, and uninstalling that deletes them with every Cluster.
func TestStaging_InstallsTheVendoredCloudNativePGOperator(t *testing.T) {
	row := releaseInEnvironment(t, "staging", "cloudnative-pg")
	if !row.Installed {
		t.Error("staging does not install the cloudnative-pg release")
	}
	if row.Namespace != "cnpg-system" || !strings.HasSuffix(row.Chart, "helm/cloudnative-pg") {
		t.Errorf("release cloudnative-pg: namespace %q, chart %q; want cnpg-system and the vendored chart", row.Namespace, row.Chart)
	}
}
