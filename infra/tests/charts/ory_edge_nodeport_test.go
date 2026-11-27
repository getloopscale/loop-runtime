package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The edge reaches Kratos and Hydra through a published port, not through `kubectl port-forward`.
//
// Measured on the app-dev droplet on 2026-08-10, during the full rebuild: `auth-dev` returned
// 502 with Kratos and Hydra PERFECTLY healthy in the cluster. The chart's Services are
// ClusterIP, and the edge reached them through two `kubectl port-forward` processes nobody
// supervised — no systemd unit, nothing. Destroying the clusters killed the processes, and login
// stayed down until someone restarted them by hand.
//
// It is exactly the B-010 defect, which cost `/engine/*` an error window on every restart of
// the forwarding (NRestarts=126499, ~1.8 per minute). There the answer was a NodePort
// published by the serverlb; here it is the same, for the same reason: port-forward is a
// debugging tool, not a transport.
//
// This guard locks the THREE ends together, because breaking any one of them reproduces the 502:
// the Service with a pinned nodePort, the cluster's port mapping, and the port the edge
// actually looks for. Pinning one without the other is the half-fix that has already cost us twice.
type edgeExposure struct {
	name        string // human label
	manifest    string // path of the NodePort Service
	nodePort    string // node port, pinned in the manifest
	hostPort    string // port published on the host, which the edge looks for
	appSelector string // app.kubernetes.io/name of the target pod
	targetPort  string // Service port (the chart's upstream)
}

var edgeExposures = []edgeExposure{
	{
		name:        "Kratos public",
		manifest:    "infra/helm/ory-kratos/nodeport-service.yaml",
		nodePort:    "30005",
		hostPort:    "14433",
		appSelector: "kratos",
		targetPort:  "80",
	},
	{
		name:        "Hydra public",
		manifest:    "infra/helm/ory-hydra/nodeport-service.yaml",
		nodePort:    "30006",
		hostPort:    "14444",
		appSelector: "hydra",
		targetPort:  "4444",
	},
}

// TestOryEdgeServicesArePinnedNodePorts — the Service exists, is a NodePort, and the port is PINNED.
//
// Without pinning, Kubernetes picks a free port between 30000-32767 on every recreation, and the
// cluster's mapping (which is static) stops pointing at it. An unpinned NodePort here is a 502
// waiting for the next recreation.
func TestOryEdgeServicesArePinnedNodePorts(t *testing.T) {
	root := repoRoot(t)
	for _, e := range edgeExposures {
		t.Run(e.name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Clean(filepath.Join(root, e.manifest)))
			if err != nil {
				t.Fatalf("%s does not exist — without it the edge goes back to depending on `kubectl port-forward`, "+
					"which dies with the cluster and takes login down: %v", e.manifest, err)
			}
			m := string(b)

			if !strings.Contains(m, "type: NodePort") {
				t.Errorf("%s does not declare `type: NodePort`", e.manifest)
			}
			if !regexp.MustCompile(`nodePort:\s*` + e.nodePort).MatchString(m) {
				t.Errorf("%s does not pin `nodePort: %s`; without pinning, Kubernetes draws the port on every "+
					"recreation and the cluster's static mapping ends up pointing at nothing",
					e.manifest, e.nodePort)
			}
			// The selector has to match the chart's pod, otherwise the Service exists with zero
			// endpoints — which responds exactly like not existing, and is harder to see.
			if !regexp.MustCompile(`app\.kubernetes\.io/name:\s*` + e.appSelector).MatchString(m) {
				t.Errorf("%s does not select `app.kubernetes.io/name: %s` — a Service without endpoints "+
					"fails exactly like an absent Service, only silently",
					e.manifest, e.appSelector)
			}
			if !regexp.MustCompile(`port:\s*` + e.targetPort).MatchString(m) {
				t.Errorf("%s does not forward to port %s of the chart's service", e.manifest, e.targetPort)
			}
		})
	}
}

// TestCoreClusterPublishesTheEdgePorts — the cluster publishes the host port the edge looks for.
//
// The NodePort Service alone is not enough: k3d only exposes on the host the ports declared in
// `infra/k3d/core-cluster.yaml`. A NodePort without a mapping is reachable from inside the cluster
// and from nowhere else.
func TestCoreClusterPublishesTheEdgePorts(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Clean(filepath.Join(root, "infra/k3d/core-cluster.yaml")))
	if err != nil {
		t.Fatalf("read core-cluster.yaml: %v", err)
	}
	cfg := string(b)

	for _, e := range edgeExposures {
		t.Run(e.name, func(t *testing.T) {
			mapping := e.hostPort + ":" + e.nodePort
			if !strings.Contains(cfg, mapping) {
				t.Errorf("core-cluster.yaml does not publish `%s` — the NodePort Service %s would exist "+
					"with nobody able to reach it from the host, which is where the edge runs",
					mapping, e.nodePort)
			}
		})
	}
}

// TestEdgeConfigUsesThePublishedPorts closes the triangle: the edge looks for the port the
// cluster publishes.
//
// This is the link nobody checks by eye, because the three ends live in different files —
// the Service in a chart, the mapping in a cluster YAML, and the port in a Caddyfile.
// It was the divergence between them that produced the 502 with everything healthy.
func TestEdgeConfigUsesThePublishedPorts(t *testing.T) {
	root := repoRoot(t)
	// The canonical Caddyfile is versioned, so its absence here is a defect and not the environment:
	// skipping would make this link — the only one tying the edge to the cluster — invisible
	// exactly when it disappears.
	caddy := filepath.Join(root, "infra/local/Caddyfile")
	b, err := os.ReadFile(filepath.Clean(caddy))
	if err != nil {
		t.Fatalf("canonical Caddyfile missing at %s: %v", caddy, err)
	}
	cfg := string(b)
	for _, e := range edgeExposures {
		t.Run(e.name, func(t *testing.T) {
			if !strings.Contains(cfg, "localhost:"+e.hostPort) {
				t.Errorf("the Caddyfile does not forward to `localhost:%s`; if the edge looks for another "+
					"port, publishing this one does not bring login back", e.hostPort)
			}
		})
	}
}

// TestEdgeDoesNotProxyToAPortForward closes B-010 where it was left open.
//
// B-010 switched the loop-console transport from `kubectl port-forward` to a published
// NodePort — but only on the droplet. This repository's canonical Caddyfile kept pointing
// at `localhost:13002`, the old forwarding port, with the comment
// "loop-console via port-forward" still in the header.
//
// The cost showed up on 2026-08-10: syncing the canonical file over the server
// REVERTED the fix, and `/engine/*` went back from 401 to 502 — nothing was listening on 13002.
// A fix that lives only on the server is not a fix; it is a correction waiting for the next
// `git pull` to be undone.
func TestEdgeDoesNotProxyToAPortForward(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Clean(filepath.Join(root, "infra/local/Caddyfile")))
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	cfg := string(b)

	if strings.Contains(cfg, "localhost:13002") {
		t.Error("the Caddyfile forwards to localhost:13002 — the `kubectl port-forward` port " +
			"that B-010 retired. Nobody listens on it; the cluster publishes loop-console on 3002 " +
			"through the loadbalancer. Syncing this file over the server reverts the fix.")
	}
	if !strings.Contains(cfg, "localhost:3002") {
		t.Error("the Caddyfile does not forward to localhost:3002, which is loop-console's published port")
	}
}
