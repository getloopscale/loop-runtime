// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

// Staging encrypts tenant secrets the way production does (ADR 2026-11-25 tenant
// secrets on OpenBao): loop-runtime-api on core uses OpenBao's transit engine, and OpenBao
// runs on the dedicated secrets cluster, unsealed by the awskms seal under an IRSA role.

import (
	"strings"
	"testing"
)

func TestStagingAPIEncryptsTenantSecretsWithOpenBaoTransit(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "staging", "loop-runtime-api", stagingOutputs...)
	if got := dig(values, "config", "openbaoTransitEnabled"); got != "true" {
		t.Errorf("config.openbaoTransitEnabled = %v, want \"true\": the API refuses every tenant secret write without transit", got)
	}
	if got := dig(values, "config", "openbaoVaultAddr"); got != "https://openbao.secrets-system.svc.cluster.local:8200" {
		t.Errorf("config.openbaoVaultAddr = %v, want the name the OpenBao certificate carries", got)
	}
}

func TestStagingOpenBaoUnsealsWithTheKMSKeyUnderItsOwnRole(t *testing.T) {
	values := helmfileValues(t, "secrets.helmfile.yaml.gotmpl", "staging", "openbao",
		"OPENBAO_AWS_KMS_KEY_ID=arn:aws:kms:us-east-1:000000000000:key/unseal")
	config, _ := dig(values, "server", "ha", "raft", "config").(string)
	for _, want := range []string{
		`seal "awskms"`,
		`kms_key_id = "arn:aws:kms:us-east-1:000000000000:key/unseal"`,
		`endpoint = "https://172.29.0.1:9597"`,
		`retry_join`,
	} {
		if !strings.Contains(config, want) {
			t.Errorf("the staging OpenBao config lacks %s:\n%s", want, config)
		}
	}
	if got := dig(values, "server", "serviceAccount", "annotations", "eks.amazonaws.com/role-arn"); got != "arn:aws:iam::000000000000:role/loop-runtime-staging-secrets-openbao" {
		t.Errorf("OpenBao's ServiceAccount role = %v, want infra-live's identity/secrets-openbao", got)
	}
	if got := dig(values, "server", "service", "nodePort"); got != float64(30200) && got != 30200 {
		t.Errorf("server.service.nodePort = %v (%T), want 30200, the port core's openbao Service targets", got, got)
	}
	creds, _ := dig(values, "server", "extraSecretEnvironmentVars").([]any)
	names := map[any]any{}
	for _, c := range creds {
		if m, ok := c.(map[string]any); ok && m["secretName"] == "openbao-aws-creds" {
			names[m["envName"]] = m["secretKey"]
		}
	}
	if names["AWS_ACCESS_KEY_ID"] != "access_key_id" || names["AWS_SECRET_ACCESS_KEY"] != "secret_access_key" {
		t.Errorf("on the emulator the seal does not read its access key from openbao-aws-creds: %v", creds)
	}
	if got := dig(values, "server", "ha", "disruptionBudget", "enabled"); got != false {
		t.Errorf("a one-voter staging OpenBao has a disruption budget (%v), which blocks every drain of its node", got)
	}
}
