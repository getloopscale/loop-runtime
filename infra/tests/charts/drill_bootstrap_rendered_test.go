// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// The values drill-helmfile-bootstrap.sh used to patch into live objects after
// `helmfile sync`, proven to be rendered by the sync instead
// (principle_violations#234).
//
// The drill ran `kubectl set env` twice on loopzero-api (Bugs #52, #65/#73/#79), set
// env on all four operators and patched their NetworkPolicies (Bugs #57, #59),
// and rewrote the loopzero-clone ClusterWorkflowTemplate with sed (Bug #64). Each
// test below names the patch it replaces and asserts the value the patch wrote is
// now in the render, so the drill is the same install as the charts declare.
package charts

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/pkg/gitearepo"
	"gopkg.in/yaml.v3"
)

// The drill overlay reads what drill-helmfile-bootstrap.sh exports before its
// Phase 3 sync: the DOKS context names and the DO Spaces key pair.
var helmfileDrillEnv = append(append([]string{}, helmfileCoreEnv...),
	"CORE_KUBE_CONTEXT=do-drill-core",
	"BUILD_KUBE_CONTEXT=do-drill-build",
	"CELL_0_KUBE_CONTEXT=do-drill-cell-0",
	"LOOPZERO_SPACES_KEY=spaces-key-placeholder",
	"LOOPZERO_SPACES_SECRET=spaces-secret-placeholder",
)

// Gitea is installed by core.helmfile into loopzero-system in every environment,
// which is the address gitearepo.DefaultInternalURL names. Asserting the
// helmfile's rendered value against the constant pins the helmfile's
// $giteaNamespace to it as well (issue #354).
const giteaInternalURL = gitearepo.DefaultInternalURL

// The drill's DO Spaces source bucket (infra/pulumi-drill `buckets.builds`).
const drillSourceBucket = "loopzero-drill-builds-sfo3"

func renderDrillLoopZeroAPI(t *testing.T) string {
	t.Helper()
	return helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "drill", "loopzero-api", "charts/loopzero-api", helmfileDrillEnv...)
}

// Bugs #52 and #65/#73/#79: `kubectl set env deployment/loopzero-api` after the sync.
func TestDrillLoopZeroAPIConfigIsRenderedNotPatched(t *testing.T) {
	data := configMapData(t, renderDrillLoopZeroAPI(t), "loopzero-api")

	want := map[string]string{
		"GITEA_URL":      giteaInternalURL,
		"GITEA_ORG":      "loopzero",
		"ARGOCD_URL":     "http://argocd-server.argocd.svc.cluster.local:80",
		"BASE_DOMAIN":    "loopzero-drill.example",
		"REGISTRY_URL":   "registry.digitalocean.com/loopzero-system",
		"SOURCE_BUCKET":  drillSourceBucket,
		"MINIO_ENDPOINT": "https://sfo3.digitaloceanspaces.com",
	}
	for key, value := range want {
		if data[key] != value {
			t.Errorf("loopzero-api ConfigMap %s = %v, want %q", key, data[key], value)
		}
	}
}

// Bug #65: the object-store key pair the drill set on the Deployment from .env.
func TestDrillLoopZeroAPIObjectStoreCredentialsAreRendered(t *testing.T) {
	secret := renderedObject(t, renderDrillLoopZeroAPI(t), "Secret", "loopzero-api")
	if secret == nil {
		t.Fatal("no loopzero-api Secret in the drill render")
	}
	values, _ := secret["stringData"].(map[string]any)
	if values == nil {
		values, _ = secret["data"].(map[string]any)
	}
	for key, want := range map[string]string{
		"MINIO_ACCESS_KEY": "spaces-key-placeholder",
		"MINIO_SECRET_KEY": "spaces-secret-placeholder",
	} {
		if got, _ := values[key].(string); got != want && got != b64(want) {
			t.Errorf("loopzero-api Secret %s = %q, want the LOOPZERO_SPACES_* value", key, got)
		}
	}
}

// Bug #57: GITEA_URL set on the operators. Only the tenant operator reads it.
func TestTenantOperatorIsToldWhereCoreInstallsGitea(t *testing.T) {
	for _, env := range []string{"drill", "prod", "dev-public"} {
		t.Run(env, func(t *testing.T) {
			rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", env, "loopzero-tenant-operator",
				"charts/loopzero-tenant-operator", helmfileDrillEnv...)
			if got := configMapData(t, rendered, "loopzero-tenant-operator")["GITEA_URL"]; got != giteaInternalURL {
				t.Errorf("tenant operator GITEA_URL = %v, want %q", got, giteaInternalURL)
			}
		})
	}
}

// Bug #59: the operators' Gitea egress patched from namespace `gitea` to
// `loopzero-system`. Every environment's Gitea is in loopzero-system.
func TestOperatorsReachGiteaInTheNamespaceCoreInstallsItIn(t *testing.T) {
	for _, env := range []string{"drill", "prod", "dev-public"} {
		for _, release := range []string{"loopzero-tenant-operator", "loopzero-platform-operator"} {
			t.Run(env+"/"+release, func(t *testing.T) {
				rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", env, release,
					"charts/"+release, helmfileDrillEnv...)
				namespaces := giteaEgressNamespaces(t, rendered, release)
				if strings.Join(namespaces, ",") != "loopzero-system" {
					t.Errorf("%s egress on port 3000 reaches namespaces %v, want exactly [loopzero-system]", release, namespaces)
				}
			})
		}
	}
}

// Bug #64: the loopzero-clone ClusterWorkflowTemplate's bucket rewritten with sed.
func TestDrillCloneStepReadsTheDrillSourceBucket(t *testing.T) {
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", "drill", "loopzero-build-cwts",
		"charts/loopzero-build-cwts", helmfileDrillEnv...)
	cwt := renderedObject(t, rendered, "ClusterWorkflowTemplate", "loopzero-clone")
	if cwt == nil {
		t.Fatal("no loopzero-clone ClusterWorkflowTemplate in the drill render")
	}
	if !strings.Contains(renderYAML(t, cwt), "loopzero/"+drillSourceBucket+"/") {
		t.Errorf("loopzero-clone does not read loopzero/%s/", drillSourceBucket)
	}
}

// giteaEgressNamespaces returns the namespaces the release's NetworkPolicy
// lets it reach on Gitea's port.
func giteaEgressNamespaces(t *testing.T, rendered, name string) []string {
	t.Helper()
	np := renderedObject(t, rendered, "NetworkPolicy", name)
	if np == nil {
		t.Fatalf("no NetworkPolicy %q in the render", name)
	}
	var namespaces []string
	rules, _ := dig(np, "spec", "egress").([]any)
	for _, rule := range rules {
		if !rulePermitsPort(rule, 3000) {
			continue
		}
		peers, _ := dig(rule, "to").([]any)
		for _, peer := range peers {
			if ns, ok := dig(peer, "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name").(string); ok {
				namespaces = append(namespaces, ns)
			}
		}
	}
	return namespaces
}

func rulePermitsPort(rule any, port int) bool {
	ports, _ := dig(rule, "ports").([]any)
	for _, p := range ports {
		if dig(p, "port") == port {
			return true
		}
	}
	return false
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func renderYAML(t *testing.T, obj any) string {
	t.Helper()
	b, err := yaml.Marshal(obj)
	if err != nil {
		t.Fatalf("encode the rendered object: %v", err)
	}
	return string(b)
}
