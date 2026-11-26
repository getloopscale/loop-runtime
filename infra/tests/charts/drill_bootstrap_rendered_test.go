// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// The values drill-helmfile-bootstrap.sh used to patch into live objects after
// `helmfile sync`, proven to be rendered by the sync instead
// (principle_violations#234).
//
// The drill ran `kubectl set env` twice on loop-runtime-api (Bugs #52, #65/#73/#79), set
// env on all four operators and patched their NetworkPolicies (Bugs #57, #59),
// and rewrote the loop-runtime-clone ClusterWorkflowTemplate with sed (Bug #64). Each
// test below names the patch it replaces and asserts the value the patch wrote is
// now in the render, so the drill is the same install as the charts declare.
package charts

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
	"gopkg.in/yaml.v3"
)

// The drill overlay reads what drill-helmfile-bootstrap.sh exports before its
// Phase 3 sync: the DOKS context names and the DO Spaces key pair.
var helmfileDrillEnv = append(append([]string{}, helmfileCoreEnv...),
	"CORE_KUBE_CONTEXT=do-drill-core",
	"BUILD_KUBE_CONTEXT=do-drill-build",
	"CELL_0_KUBE_CONTEXT=do-drill-cell-0",
	"LOOP_RUNTIME_SPACES_KEY=spaces-key-placeholder",
	"LOOP_RUNTIME_SPACES_SECRET=spaces-secret-placeholder",
)

// Gitea is installed by core.helmfile into loop-runtime-system in every environment,
// which is the address gitearepo.DefaultInternalURL names. Asserting the
// helmfile's rendered value against the constant pins the helmfile's
// $giteaNamespace to it as well (issue #354).
const giteaInternalURL = gitearepo.DefaultInternalURL

// The drill's DO Spaces source bucket (infra/pulumi-drill `buckets.builds`).
const drillSourceBucket = "loop-runtime-drill-builds-sfo3"

func renderDrillLoopRuntimeAPI(t *testing.T) string {
	t.Helper()
	return helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "drill", "loop-runtime-api", "charts/loop-runtime-api", helmfileDrillEnv...)
}

// Bugs #52 and #65/#73/#79: `kubectl set env deployment/loop-runtime-api` after the sync.
func TestDrillLoopRuntimeAPIConfigIsRenderedNotPatched(t *testing.T) {
	data := configMapData(t, renderDrillLoopRuntimeAPI(t), "loop-runtime-api")

	want := map[string]string{
		"GITEA_URL":      giteaInternalURL,
		"GITEA_ORG":      "loop-runtime",
		"ARGOCD_URL":     "http://argocd-server.argocd.svc.cluster.local:80",
		"BASE_DOMAIN":    "drill.getloopscale.com",
		"REGISTRY_URL":   "registry.digitalocean.com/loop-runtime-system",
		"SOURCE_BUCKET":  drillSourceBucket,
		"MINIO_ENDPOINT": "https://sfo3.digitaloceanspaces.com",
	}
	for key, value := range want {
		if data[key] != value {
			t.Errorf("loop-runtime-api ConfigMap %s = %v, want %q", key, data[key], value)
		}
	}
}

// Bug #65: the object-store key pair the drill set on the Deployment from .env.
func TestDrillLoopRuntimeAPIObjectStoreCredentialsAreRendered(t *testing.T) {
	secret := renderedObject(t, renderDrillLoopRuntimeAPI(t), "Secret", "loop-runtime-api")
	if secret == nil {
		t.Fatal("no loop-runtime-api Secret in the drill render")
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
			t.Errorf("loop-runtime-api Secret %s = %q, want the LOOP_RUNTIME_SPACES_* value", key, got)
		}
	}
}

// Bug #57: GITEA_URL set on the operators. Only the tenant operator reads it.
func TestTenantOperatorIsToldWhereCoreInstallsGitea(t *testing.T) {
	for _, env := range []string{"drill", "prod", "dev-public"} {
		t.Run(env, func(t *testing.T) {
			rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", env, "loop-runtime-tenant-operator",
				"charts/loop-runtime-tenant-operator", helmfileDrillEnv...)
			if got := configMapData(t, rendered, "loop-runtime-tenant-operator")["GITEA_URL"]; got != giteaInternalURL {
				t.Errorf("tenant operator GITEA_URL = %v, want %q", got, giteaInternalURL)
			}
		})
	}
}

// Bug #59: the operators' Gitea egress patched from namespace `gitea` to
// `loop-runtime-system`. Every environment's Gitea is in loop-runtime-system.
func TestOperatorsReachGiteaInTheNamespaceCoreInstallsItIn(t *testing.T) {
	for _, env := range []string{"drill", "prod", "dev-public"} {
		for _, release := range []string{"loop-runtime-tenant-operator", "loop-runtime-platform-operator"} {
			t.Run(env+"/"+release, func(t *testing.T) {
				rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", env, release,
					"charts/"+release, helmfileDrillEnv...)
				namespaces := giteaEgressNamespaces(t, rendered, release)
				if strings.Join(namespaces, ",") != "loop-runtime-system" {
					t.Errorf("%s egress on port 3000 reaches namespaces %v, want exactly [loop-runtime-system]", release, namespaces)
				}
			})
		}
	}
}

// Bug #64: the loop-runtime-clone ClusterWorkflowTemplate's bucket rewritten with sed.
func TestDrillCloneStepReadsTheDrillSourceBucket(t *testing.T) {
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", "drill", "loop-runtime-build-cwts",
		"charts/loop-runtime-build-cwts", helmfileDrillEnv...)
	cwt := renderedObject(t, rendered, "ClusterWorkflowTemplate", "loop-runtime-clone")
	if cwt == nil {
		t.Fatal("no loop-runtime-clone ClusterWorkflowTemplate in the drill render")
	}
	if !strings.Contains(renderYAML(t, cwt), "loop-runtime/"+drillSourceBucket+"/") {
		t.Errorf("loop-runtime-clone does not read loop-runtime/%s/", drillSourceBucket)
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
