package charts

import (
	"os/exec"
	"strings"
	"testing"
)

// TestLoopZeroCloudServiceIsNodePortForTheEdge locks the fix for the app-dev edge.
//
// Measured on the droplet 2026-08-10: `loopzero-cloud` was a ClusterIP service, so the
// Caddy edge could only reach it through `kubectl port-forward`. That forward is a
// debugging tool, not a transport, and it showed it — the systemd unit babysitting it
// reported NRestarts=126499, roughly 1.8 restarts per minute, and every restart is a
// window where `/engine/*` answers 502. That path carries deploy, whoami, list
// projects and build status, so the product was intermittently broken for as long as
// the unit had been running.
//
// Its sibling `loopzero-api` never had the problem, because it is a NodePort published by
// the k3d serverlb through a plain docker port mapping. This test makes `loopzero-cloud`
// hold the same shape.
//
// 30002 is deliberate: `docker port k3d-loopzero-core-serverlb` already publishes
// 30002 → host 3002, and 30002 was not taken by any other service in the cluster.
// Picking an unpublished NodePort would render fine here and still leave the edge
// unable to reach it.
func TestLoopZeroCloudServiceIsNodePortForTheEdge(t *testing.T) {
	rendered := renderLoopZeroCloudService(t)

	svc := extractServiceBlock(rendered, "loopzero-cloud")
	if svc == "" {
		t.Fatal("no Service named loopzero-cloud in the rendered output")
	}

	if !strings.Contains(svc, "type: NodePort") {
		t.Errorf("loopzero-cloud must be NodePort so the edge reaches it via the serverlb "+
			"instead of a kubectl port-forward; got:\n%s", svc)
	}
	if !strings.Contains(svc, "nodePort: 30002") {
		t.Errorf("loopzero-cloud must pin nodePort 30002 — the serverlb publishes it as host "+
			"port 3002, and an unpinned port would land somewhere the edge cannot reach; got:\n%s", svc)
	}
}

// TestLoopZeroCloudNodePortIsOptional keeps the chart usable where the edge does not apply.
//
// A hardcoded NodePort would force every consumer of this chart into a topology that
// only dev-public needs. The template must stay ClusterIP-capable, and emit the
// nodePort line only when one is configured.
func TestLoopZeroCloudNodePortIsOptional(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("helm", "template", "loopzero-cloud", "infra/helm/loopzero-cloud",
		"--set", "service.type=ClusterIP", "--set", "service.nodePort=null")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, out)
	}
	svc := extractServiceBlock(string(out), "loopzero-cloud")
	if strings.Contains(svc, "nodePort:") {
		t.Errorf("a ClusterIP render must not emit nodePort; got:\n%s", svc)
	}
}

func renderLoopZeroCloudService(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	cmd := exec.Command(
		"helmfile",
		"-f", "infra/helmfile/core.helmfile.yaml.gotmpl",
		"-e", "dev-public",
		"--selector", "name=loopzero-cloud",
		// B-156: sem isto o `helmfile template` roda `helm repo add/update` contra os
		// repositorios remotos declarados no arquivo, mesmo com o seletor apontando para um
		// unico release. Medido em 2026-08-19: 17 chamadas de repo e 10s com rede boa; sob
		// carga foram 123,89s e FALHA, barrando um commit que so' tocava CHANGELOG e BACKLOG.
		// Com a flag: 0s e zero chamadas.
		"--skip-deps",
		"template",
	)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helmfile template failed: %v\n%s", err, truncate(string(out), 1200))
	}
	return string(out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
