// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

// Staging runs loop-edge on its core cluster, the way production does, with what the
// emulator changes: the cells' load balancers are all served at the emulator's endpoint,
// Valkey's certificate is signed by the emulator's authority, and the fleet has no public
// address (the host's Caddy holds ports 80 and 443), so it issues from its own authority.

import (
	"strings"
	"testing"
)

var stagingWithCellOrigins = append(append([]string{}, stagingOutputs...),
	"LOOP_RUNTIME_CELL_ORIGINS=cell-0=alb-0.elb.example.test,cell-1=alb-1.elb.example.test")

func TestStagingRunsTheCustomDomainFleetAgainstTheEmulator(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loop-edge", "charts/loop-edge", stagingWithCellOrigins...)

	config := renderedObject(t, rendered, "ConfigMap", "loop-edge")
	if config == nil {
		t.Fatalf("staging renders no loop-edge ConfigMap:\n%s", rendered)
	}
	caddyfile, _ := dig(config, "data", "Caddyfile").(string)
	for _, want := range []string{
		"host 172.29.0.1",
		"port 6379",
		`password "{$EDGE_STORAGE_PASSWORD}"`,
		"tls_server_certs_path /etc/loop-edge/storage-ca/ca.pem",
		`{query.domain}.endsWith(".getloopscale.com")`,
		"issuer internal",
		"cell-0 alb-0.elb.example.test",
		"cell-1 alb-1.elb.example.test",
		"reverse_proxy 172.29.0.1:9597 {",
		"header_up Host {cell_origin}",
		"tls_server_name localhost",
		"tls_trust_pool file /etc/loop-edge/upstream-ca/ca.pem",
	} {
		if !strings.Contains(caddyfile, want) {
			t.Errorf("the staging Caddyfile lacks %q:\n%s", want, caddyfile)
		}
	}

	if renderedObject(t, rendered, "Deployment", "loop-edge") == nil {
		t.Fatal("staging renders no loop-edge Deployment")
	}
	if !strings.Contains(rendered, "image: ghcr.io/getloopscale/loop-edge:develop-") {
		t.Error("the fleet does not run the loop-edge image CI published to the platform registry")
	}
	if !strings.Contains(rendered, "name: loop-edge-emulator-ca") || !strings.Contains(rendered, "name: loop-edge-store") {
		t.Error("the fleet does not read the emulator's authority and the store's token")
	}
	service := renderedObject(t, rendered, "Service", "loop-edge")
	if got := dig(service, "spec", "type"); got != "ClusterIP" {
		t.Errorf("the staging Service is %v: the host's Caddy holds the public ports, so the fleet gets no load balancer", got)
	}
}

// The token of the store reaches the fleet the way every other credential does on AWS:
// an ExternalSecret of the Secrets Manager secret the Valkey unit created.
func TestStagingGivesTheFleetTheStoreToken(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	secret := renderedObject(t, rendered, "ExternalSecret", "loop-edge-store")
	if secret == nil {
		t.Fatal("no ExternalSecret loop-edge-store")
	}
	if got := dig(secret, "metadata", "namespace"); got != "loop-runtime-system" {
		t.Errorf("loop-edge-store is in %v, want the fleet's namespace", got)
	}
	data, _ := dig(secret, "spec", "data").([]any)
	if len(data) != 1 {
		t.Fatalf("loop-edge-store reads %d keys, want the token alone", len(data))
	}
	entry, _ := data[0].(map[string]any)
	if entry["secretKey"] != "token" || dig(entry, "remoteRef", "key") != "arn:aws:secretsmanager:us-east-1:000000000000:secret:loop-runtime-staging/valkey-auth-token-test" {
		t.Errorf("loop-edge-store does not read the Valkey AUTH token: %v", entry)
	}
}

// Production has no fleet configured yet (getloopscale/infra-live#39): the release stays
// out of an environment that does not enable it.
func TestTheFleetIsNotInstalledWhereItIsNotConfigured(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "dev", "loop-edge", "charts/loop-edge")
	if strings.Contains(rendered, "kind: Deployment") {
		t.Errorf("dev installs loop-edge without configuring it:\n%s", rendered)
	}
}
