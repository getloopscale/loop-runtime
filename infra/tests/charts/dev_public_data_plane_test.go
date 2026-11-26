// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// dev-public's data plane — memory, RAG, lens — moves to the core cluster from Helmfile
// (#446), one service at a time: a service is listed in `dataPlane.services` once its
// repository publishes to ECR. They ran as compose containers started from directories that
// no longer exist, on host ports that had drifted from what the engine configured: memory
// answered on 8080 while the seed chain probed 18081, and loop-console reached RAG at :8787, a
// port the host never published.
//
// Three ends must agree for a service to be reachable, and each was once the one that
// drifted: the Service's pinned nodePort, the host port the k3d load balancer publishes for
// it, and the address its consumer uses.
var dataPlanePorts = map[string]struct {
	nodePort, hostPort, servicePort int
	consumerKey                     string // loop-console config key, "" when loop-console does not call it
}{
	"loop-runtime-memory": {30181, 18081, 8080, "memoryBaseUrl"},
	"loop-runtime-rag":    {30182, 18082, 8787, "ragBaseUrl"},
	"loop-runtime-lens":   {30184, 18084, 4318, "lensBaseUrl"},
}

// enabledDataPlane reads `dataPlane.services` from the dev-public environment.
func enabledDataPlane(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "infra", "helmfile", "environments", "dev-public.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		DataPlane struct {
			Services []string `yaml:"services"`
		} `yaml:"dataPlane"`
		PlatformImages struct {
			Tags map[string]string `yaml:"tags"`
		} `yaml:"platformImages"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	enabled := map[string]bool{}
	for _, s := range env.DataPlane.Services {
		if _, known := dataPlanePorts[s]; !known {
			t.Errorf("dataPlane.services lists %q, which is not a data-plane service", s)
		}
		if !commitTag.MatchString(env.PlatformImages.Tags[s]) {
			t.Errorf("%s is enabled but platformImages.tags.%s is not a commit SHA", s, s)
		}
		enabled[s] = true
	}
	return enabled
}

func TestDevPublicDataPlaneIsPublishedOnItsHostPort(t *testing.T) {
	cluster := readCoreClusterPorts(t)
	enabled := enabledDataPlane(t)
	if len(enabled) == 0 {
		t.Fatal("no data-plane service is enabled on dev-public")
	}
	for svc, p := range dataPlanePorts {
		mapping := fmt.Sprintf("%d:%d", p.hostPort, p.nodePort)
		if !enabled[svc] {
			// The host port still belongs to the compose container being replaced; mapping it
			// on the cluster would fail the next cluster creation.
			if cluster[mapping] {
				t.Errorf("%s is not enabled but core-cluster.yaml already publishes %s", svc, mapping)
			}
			continue
		}
		t.Run(svc, func(t *testing.T) {
			rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", svc,
				"infra/helm/data-plane-service", helmfileCoreEnv...)
			var found bool
			for _, s := range renderedObjectsOfKind(t, rendered, "Service") {
				if name, _ := dig(s, "metadata", "name").(string); name != svc {
					continue
				}
				found = true
				ports, _ := dig(s, "spec", "ports").([]any)
				port, _ := ports[0].(map[string]any)
				if got := fmt.Sprint(port["nodePort"]); got != fmt.Sprint(p.nodePort) {
					t.Errorf("nodePort = %s, want %d", got, p.nodePort)
				}
			}
			if !found {
				t.Fatalf("no Service %s rendered", svc)
			}
			if !cluster[mapping] {
				t.Errorf("infra/k3d/core-cluster.yaml does not publish %s on the load balancer", mapping)
			}
		})
	}
}

// loop-console reaches a service that runs on its cluster by Service name. One not moved yet
// is reached where its compose container publishes it — measured 2026-09-29, RAG's
// pipeline container `therag_api` publishes host :18082 → :8787 and nothing listens on host
// :8787. The address drifted twice before: in July :18082 was a `node` process started by
// hand that answered 200 while reporting built_at "unknown", and the fix moved RAG to
// :8787, a port the host never published. A Service name cannot point at the wrong
// instance, which is why a moved service never goes through the host.
func TestDevPublicControlPlaneReachesTheDataPlaneWhereItRuns(t *testing.T) {
	enabled := enabledDataPlane(t)
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loop-console", "charts/loop-console", helmfileCoreEnv...)
	for svc, p := range dataPlanePorts {
		if p.consumerKey == "" {
			continue
		}
		want := fmt.Sprintf("http://host.k3d.internal:%d", p.hostPort)
		if enabled[svc] {
			want = fmt.Sprintf("http://%s.loop-runtime-data-plane.svc.cluster.local:%d", svc, p.servicePort)
		}
		if !strings.Contains(rendered, want) {
			t.Errorf("loop-console does not reach %s at %s (%s)", svc, want, p.consumerKey)
		}
	}
	if strings.Contains(rendered, "host.k3d.internal:8787") {
		t.Error("loop-console reaches RAG at :8787, a port the host never published")
	}
}

func readCoreClusterPorts(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "infra", "k3d", "core-cluster.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Ports []struct {
			Port        string   `yaml:"port"`
			NodeFilters []string `yaml:"nodeFilters"`
		} `yaml:"ports"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	published := map[string]bool{}
	for _, p := range cfg.Ports {
		for _, f := range p.NodeFilters {
			if f == "loadbalancer" {
				published[p.Port] = true
			}
		}
	}
	return published
}

// loop-runtime-rag refuses to serve as a superuser and its worker refuses any role that cannot
// cross workspaces, while its migrations need the owner (#474). dev-public connects each
// process as the role it insists on and migrates as the owner before the app starts.
func TestDevPublicRAGConnectsEachProcessAsItsRoleAndMigratesAsTheOwner(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loop-runtime-rag",
		"infra/helm/data-plane-service", helmfileCoreEnv...)
	docs := parseDocs(t, rendered)

	for deploy, role := range map[string]string{"loop-runtime-rag": "rag_app", "loop-runtime-rag-worker": "rag_worker"} {
		env := asWorkload(t, findDoc(t, docs, "Deployment", deploy)).Spec.Template.Spec.Containers[0].Env
		i := envIndex(env, "LOOP_RUNTIMERAG_PG_URI")
		if i < 0 || !strings.HasPrefix(env[i].Value, "postgres://"+role+":") {
			t.Errorf("%s: LOOP_RUNTIMERAG_PG_URI does not connect as %s: %+v", deploy, role, env)
		}
	}

	init := asWorkload(t, findDoc(t, docs, "Deployment", "loop-runtime-rag")).Spec.Template.Spec.InitContainers
	last := init[len(init)-1]
	if last.Name != "migrate" || strings.Join(last.Command, " ") != "node_modules/.bin/tsx src/migrate.ts" {
		t.Fatalf("loop-runtime-rag's last init container = %s %v, want migrate running src/migrate.ts", last.Name, last.Command)
	}
	if i := envIndex(last.Env, "LOOP_RUNTIMERAG_MIGRATE_PG_URI"); i < 0 || !strings.HasPrefix(last.Env[i].Value, "postgres://therag:") {
		t.Errorf("migrate does not connect as the owner: %+v", last.Env)
	}
}
