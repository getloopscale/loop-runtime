package charts

import (
	"strings"
	"testing"
)

// TestLoopConsoleServiceIsNodePortForTheEdge locks the fix for the app-dev edge.
//
// Measured on the droplet 2026-08-10: `loop-console` was a ClusterIP service, so the
// Caddy edge could only reach it through `kubectl port-forward`. That forward is a
// debugging tool, not a transport, and it showed it — the systemd unit babysitting it
// reported NRestarts=126499, roughly 1.8 restarts per minute, and every restart is a
// window where `/engine/*` answers 502. That path carries deploy, whoami, list
// projects and build status, so the product was intermittently broken for as long as
// the unit had been running.
//
// Its sibling `loop-runtime-api` never had the problem, because it is a NodePort published by
// the k3d serverlb through a plain docker port mapping. This test makes `loop-console`
// hold the same shape.
//
// 30002 is deliberate: `docker port k3d-loop-runtime-core-serverlb` already publishes
// 30002 → host 3002, and 30002 was not taken by any other service in the cluster.
// Picking an unpublished NodePort would render fine here and still leave the edge
// unable to reach it.
func TestLoopConsoleServiceIsNodePortForTheEdge(t *testing.T) {
	rendered := renderDevPublicLoopConsole(t)

	svc := extractServiceBlock(rendered, "loop-console")
	if svc == "" {
		t.Fatal("no Service named loop-console in the rendered output")
	}

	if !strings.Contains(svc, "type: NodePort") {
		t.Errorf("loop-console must be NodePort so the edge reaches it via the serverlb "+
			"instead of a kubectl port-forward; got:\n%s", svc)
	}
	if !strings.Contains(svc, "nodePort: 30002") {
		t.Errorf("loop-console must pin nodePort 30002 — the serverlb publishes it as host "+
			"port 3002, and an unpinned port would land somewhere the edge cannot reach; got:\n%s", svc)
	}
}

// TestLoopConsoleNodePortIsOptional keeps the chart usable where the edge does not apply.
//
// A hardcoded NodePort would force every consumer of this chart into a topology that
// only dev-public needs. The template must stay ClusterIP-capable, and emit the
// nodePort line only when one is configured.
func TestLoopConsoleNodePortIsOptional(t *testing.T) {
	out := renderChart(t, publishedChart(t, "loop-console"), "loop-console",
		map[string]string{"service.type": "ClusterIP", "service.nodePort": "null"})
	svc := extractServiceBlock(out, "loop-console")
	if strings.Contains(svc, "nodePort:") {
		t.Errorf("a ClusterIP render must not emit nodePort; got:\n%s", svc)
	}
}

// renderDevPublicLoopConsole renders the loop-console release of the dev-public core.
func renderDevPublicLoopConsole(t *testing.T) string {
	t.Helper()
	return helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev-public", "loop-console", "charts/loop-console")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
