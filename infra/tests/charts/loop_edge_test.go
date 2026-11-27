// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

// loop-edge is the Caddy fleet that serves tenants' custom domains in production
// (docs/decisions/2026-11-26-customer-domains-at-the-edge.md): it obtains a certificate on
// demand for a host the API knows as an active custom domain, keeps the certificates in
// storage every pod shares, and proxies the request to the cell that serves the host.

import (
	"strings"
	"testing"
)

const loopEdgeValues = `
image:
  registry: registry.example.test
  tag: "test"
platformDomains: ["platform.example", "apps.example"]
acme:
  email: ops@platform.example
cells:
  cell-0: origin-cell-0.platform.example
  cell-1: origin-cell-1.platform.example
storage:
  host: cache.internal.example
  passwordSecret: { name: edge-cache, key: token }
`

func renderLoopEdge(t *testing.T, values string) []workloadDoc {
	t.Helper()
	out := renderChart(t, uniqueChart(t, "loop-edge"), "loop-edge", nil, writeValues(t, values))
	return parseDocs(t, out)
}

func loopEdgeDoc(t *testing.T, docs []workloadDoc, kind string) string {
	t.Helper()
	for _, d := range docs {
		if d.obj.Kind == kind {
			return d.raw
		}
	}
	t.Fatalf("the chart renders no %s", kind)
	return ""
}

func wantAll(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, text)
		}
	}
}

// Anyone can point a name at the fleet. A certificate is requested only for a host the API
// answers for, and never for a name under the platform's own domains.
func TestLoopEdgeAsksTheAPIBeforeItObtainsACertificate(t *testing.T) {
	caddyfile := loopEdgeDoc(t, renderLoopEdge(t, loopEdgeValues), "ConfigMap")
	wantAll(t, "the Caddyfile", caddyfile,
		"on_demand_tls {",
		"ask http://127.0.0.1:9123/ask",
		"rewrite * /internal/edge/route?{query}",
		`header_up Authorization "Bearer {env.EDGE_ROUTE_TOKEN}"`,
		`{query.domain} == "platform.example"`,
		`{query.domain}.endsWith(".platform.example")`,
		`{query.domain}.endsWith(".apps.example")`,
		"respond @platform 404",
	)
	// The site that answers the question names no host: forward_auth passes the viewer's
	// Host on, and Caddy answers a Host no site names with an empty 200
	// (getloopscale/loop-runtime#177).
	wantAll(t, "the Caddyfile", caddyfile, "http://:9123 {", "bind 127.0.0.1")
}

// Two pods that each kept their certificates would each ask Let's Encrypt for the same host.
func TestLoopEdgeKeepsCertificatesInTheSharedStore(t *testing.T) {
	docs := renderLoopEdge(t, loopEdgeValues)
	wantAll(t, "the Caddyfile", loopEdgeDoc(t, docs, "ConfigMap"),
		"storage redis {",
		"host cache.internal.example",
		"port 6379",
		`password "{$EDGE_STORAGE_PASSWORD}"`,
		"key_prefix loop-edge",
		"tls_enabled true",
	)
	deployment := loopEdgeDoc(t, docs, "Deployment")
	wantAll(t, "the Deployment", deployment,
		"name: EDGE_STORAGE_PASSWORD",
		"name: edge-cache",
		"key: token",
		"name: EDGE_ROUTE_TOKEN",
		"name: loop-runtime-edge-route-token",
		"key: LOOP_RUNTIME_EDGE_ROUTE_TOKEN",
	)
	if strings.Contains(deployment, "EDGE_STORAGE_ENCRYPTION_KEY") {
		t.Error("the Deployment reads an encryption key no value names")
	}
}

// A store whose certificate a private authority signs: Caddy trusts that authority for the
// store alone, and the system's authorities stay as they are for ACME.
func TestLoopEdgeTrustsAPrivateAuthorityForTheStore(t *testing.T) {
	docs := renderLoopEdge(t, loopEdgeValues+`
  caConfigMap: { name: private-ca, key: ca.pem }
`)
	wantAll(t, "the Caddyfile", loopEdgeDoc(t, docs, "ConfigMap"),
		"tls_server_certs_path /etc/loop-edge/storage-ca/ca.pem")
	wantAll(t, "the Deployment", loopEdgeDoc(t, docs, "Deployment"),
		"mountPath: /etc/loop-edge/storage-ca", "name: private-ca")

	plain := loopEdgeDoc(t, renderLoopEdge(t, loopEdgeValues), "ConfigMap")
	if strings.Contains(plain, "tls_server_certs_path") {
		t.Error("the Caddyfile names an authority file no value asked for")
	}
}

func TestLoopEdgeProxiesAHostToTheCellThatServesIt(t *testing.T) {
	caddyfile := loopEdgeDoc(t, renderLoopEdge(t, loopEdgeValues), "ConfigMap")
	wantAll(t, "the Caddyfile", caddyfile,
		"https:// {",
		"on_demand",
		"uri /ask?domain={host}",
		"copy_headers X-Loop-Runtime-Cell",
		"cell-0 origin-cell-0.platform.example",
		"cell-1 origin-cell-1.platform.example",
		"default cell-without-an-origin.invalid",
		"reverse_proxy {cell_origin}:443 {",
		"header_up -X-Loop-Runtime-Cell",
		"tls_server_name {cell_origin}",
	)
	if strings.Contains(caddyfile, "issuer internal") {
		t.Error("the default issuer is Caddy's own authority; a browser trusts no certificate from it")
	}
}

// An emulator serves every load balancer at one endpoint and tells them apart by Host.
func TestLoopEdgeCanReachEveryCellThroughOneAddress(t *testing.T) {
	caddyfile := loopEdgeDoc(t, renderLoopEdge(t, loopEdgeValues+`
upstream:
  address: emulator.internal.example:9297
  hostHeader: origin
  tls:
    serverName: localhost
    caConfigMap: { name: emulator-ca, key: ca.pem }
`), "ConfigMap")
	wantAll(t, "the Caddyfile", caddyfile,
		"reverse_proxy emulator.internal.example:9297 {",
		"header_up Host {cell_origin}",
		"header_up X-Forwarded-Host {host}",
		"tls_server_name localhost",
		"tls_trust_pool file /etc/loop-edge/upstream-ca/ca.pem",
	)
}

func TestLoopEdgeIsAFleetBehindOneLoadBalancer(t *testing.T) {
	docs := renderLoopEdge(t, loopEdgeValues)
	deployment := loopEdgeDoc(t, docs, "Deployment")
	wantAll(t, "the Deployment", deployment,
		"replicas: 2",
		"image: registry.example.test/loop-edge:test",
		"runAsNonRoot: true",
		"readOnlyRootFilesystem: true",
		"containerPort: 8443",
		"containerPort: 8080",
	)
	service := loopEdgeDoc(t, docs, "Service")
	wantAll(t, "the Service", service,
		"type: LoadBalancer",
		"port: 443",
		"targetPort: https",
		"port: 80",
		"targetPort: http",
		"service.beta.kubernetes.io/aws-load-balancer-type: external",
	)
	wantAll(t, "the PodDisruptionBudget", loopEdgeDoc(t, docs, "PodDisruptionBudget"), "minAvailable: 1")
}

func TestLoopEdgeRefusesToRenderWithoutWhatItCannotGuess(t *testing.T) {
	for name, values := range map[string]string{
		"no platform domain": strings.Replace(loopEdgeValues, `platformDomains: ["platform.example", "apps.example"]`, "platformDomains: []", 1),
		"no storage host":    strings.Replace(loopEdgeValues, "host: cache.internal.example", `host: ""`, 1),
		"no ACME contact":    strings.Replace(loopEdgeValues, "email: ops@platform.example", `email: ""`, 1),
		"no image tag":       strings.Replace(loopEdgeValues, `tag: "test"`, `tag: ""`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			out, err := renderChartE(t, uniqueChart(t, "loop-edge"), "loop-edge", nil, writeValues(t, values))
			if err == nil {
				t.Fatalf("the chart rendered:\n%s", out)
			}
		})
	}
}

// Staging has no public address for the fleet, so it issues from Caddy's own authority
// there and needs no ACME contact.
func TestLoopEdgeCanIssueFromItsOwnAuthority(t *testing.T) {
	values := strings.Replace(loopEdgeValues, "email: ops@platform.example", "issuer: internal", 1)
	caddyfile := loopEdgeDoc(t, renderLoopEdge(t, values), "ConfigMap")
	// Caddy otherwise tries to add its authority to the system's trust store at start, which
	// a read-only container refuses with an error in the log.
	wantAll(t, "the Caddyfile", caddyfile, "issuer internal", "skip_install_trust")
}
