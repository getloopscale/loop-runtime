package charts

import (
	"strings"
	"testing"
)

// TestLoopRuntimeCloudServiceIsNodePortForTheEdge locks the fix for the app-dev edge.
//
// Measured on the droplet 2026-08-10: `loopruntime-cloud` was a ClusterIP service, so the
// Caddy edge could only reach it through `kubectl port-forward`. That forward is a
// debugging tool, not a transport, and it showed it — the systemd unit babysitting it
// reported NRestarts=126499, roughly 1.8 restarts per minute, and every restart is a
// window where `/engine/*` answers 502. That path carries deploy, whoami, list
// projects and build status, so the product was intermittently broken for as long as
// the unit had been running.
//
// Its sibling `loopruntime-api` never had the problem, because it is a NodePort published by
// the k3d serverlb through a plain docker port mapping. This test makes `loopruntime-cloud`
// hold the same shape.
//
// 30002 is deliberate: `docker port k3d-loopruntime-core-serverlb` already publishes
// 30002 → host 3002, and 30002 was not taken by any other service in the cluster.
// Picking an unpublished NodePort would render fine here and still leave the edge
// unable to reach it.
func TestLoopRuntimeCloudServiceIsNodePortForTheEdge(t *testing.T) {
	rendered := renderDevPublicLoopRuntimeCloud(t)

	svc := extractServiceBlock(rendered, "loopruntime-cloud")
	if svc == "" {
		t.Fatal("no Service named loopruntime-cloud in the rendered output")
	}

	if !strings.Contains(svc, "type: NodePort") {
		t.Errorf("loopruntime-cloud must be NodePort so the edge reaches it via the serverlb "+
			"instead of a kubectl port-forward; got:\n%s", svc)
	}
	if !strings.Contains(svc, "nodePort: 30002") {
		t.Errorf("loopruntime-cloud must pin nodePort 30002 — the serverlb publishes it as host "+
			"port 3002, and an unpinned port would land somewhere the edge cannot reach; got:\n%s", svc)
	}
}

// TestLoopRuntimeCloudNodePortIsOptional keeps the chart usable where the edge does not apply.
//
// A hardcoded NodePort would force every consumer of this chart into a topology that
// only dev-public needs. The template must stay ClusterIP-capable, and emit the
// nodePort line only when one is configured.
func TestLoopRuntimeCloudNodePortIsOptional(t *testing.T) {
	out := renderChart(t, publishedChart(t, "loopruntime-cloud"), "loopruntime-cloud",
		map[string]string{"service.type": "ClusterIP", "service.nodePort": "null"})
	svc := extractServiceBlock(out, "loopruntime-cloud")
	if strings.Contains(svc, "nodePort:") {
		t.Errorf("a ClusterIP render must not emit nodePort; got:\n%s", svc)
	}
}

// renderDevPublicLoopRuntimeCloud renders the loopruntime-cloud release of the dev-public core.
func renderDevPublicLoopRuntimeCloud(t *testing.T) string {
	t.Helper()
	return helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopruntime-cloud", "charts/loopruntime-cloud")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
