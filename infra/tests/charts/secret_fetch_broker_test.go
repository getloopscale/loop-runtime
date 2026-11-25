// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

// A secret a tenant sets reaches the app through the cells' External Secrets, which ask
// loopzero-api on core for it (the loopzero-broker ClusterSecretStore, POST
// /internal/v1/secret-fetch/...). On staging and in production the cells reach core
// through an internal ALB that forwards that prefix alone, over HTTPS; dev-public keeps
// its k3d host address.

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	stagingCoreAPIALB      = "internal-k8s-loopzero-loopzero-4444444444.us-east-1.elb.amazonaws.com"
	prodCoreAPIInternalURL = "https://secret-fetch.core.internal.getloopscale.com"
	prodCoreAPICertificate = "arn:aws:acm:us-east-1:123456789012:certificate/0f0e0d0c-0b0a-0908-0706-050403020100"
)

// brokerStoreURL renders the cell's loopzero-broker release and returns the webhook URL
// of its ClusterSecretStore.
func brokerStoreURL(t *testing.T, env string, extraEnv ...string) string {
	t.Helper()
	rendered := helmfileTemplate(t, "cell.helmfile.yaml.gotmpl", env, "loopzero-broker", "infra/helm/loopzero-external-secrets", extraEnv...)
	store := renderedObject(t, rendered, "ClusterSecretStore", "loopzero-broker")
	if store == nil {
		t.Fatalf("the %s cell renders no ClusterSecretStore loopzero-broker", env)
	}
	url, _ := dig(store, "spec", "provider", "webhook", "url").(string)
	return url
}

// renderFails runs a helmfile render that must refuse its input and returns stderr.
func renderFails(t *testing.T, helmfile, env, release string, extraEnv ...string) string {
	t.Helper()
	_, stderr, err := helmfileRunResult(t, extraEnv, "-f", "infra/helmfile/"+helmfile, "-e", env,
		"--selector", "name="+release, "--output-file-template", filepath.Join(t.TempDir(), "{{ .Release.Name }}.yaml"), "write-values")
	if err == nil {
		t.Fatalf("%s -e %s rendered release %s; it must refuse", helmfile, env, release)
	}
	return stderr
}

func TestStagingCellsFetchSecretsFromCoreThroughTheEmulatorALBOverHTTPS(t *testing.T) {
	url := brokerStoreURL(t, "staging", stagingOutputs...)

	want := "https://172.29.0.1:9597/_aws/elasticloadbalancing/" + stagingCoreAPIALB + "/internal/v1/secret-fetch/"
	if !strings.HasPrefix(url, want) {
		t.Errorf("the staging broker store calls %q, want the core API's ALB through the emulator's HTTPS endpoint (%s...)", url, want)
	}
}

func TestStagingCellRenderRefusesWithoutTheCoreAPIALB(t *testing.T) {
	var env []string
	for _, kv := range stagingOutputs {
		if !strings.HasPrefix(kv, "LOOPZERO_CORE_API_ALB_DNS_NAME=") {
			env = append(env, kv)
		}
	}
	stderr := renderFails(t, "cell.helmfile.yaml.gotmpl", "staging", "loopzero-broker", env...)

	if !strings.Contains(stderr, "LOOPZERO_CORE_API_ALB_DNS_NAME") {
		t.Errorf("the staging cell render failed without naming LOOPZERO_CORE_API_ALB_DNS_NAME:\n%s", truncate(stderr, 1200))
	}
}

func TestProdCellsRefuseToRenderWithoutTheInternalHTTPSAddress(t *testing.T) {
	stderr := renderFails(t, "cell.helmfile.yaml.gotmpl", "prod", "loopzero-broker", "LOOPZERO_CELL_CONTEXT=cell-0")

	if !strings.Contains(stderr, "LOOPZERO_CORE_API_INTERNAL_URL") {
		t.Errorf("the prod cell render failed without naming LOOPZERO_CORE_API_INTERNAL_URL:\n%s", truncate(stderr, 1200))
	}
}

func TestProdCellsRefuseAPlaintextCoreAPIAddress(t *testing.T) {
	_, stderr, err := helmfileRunResult(t, []string{"LOOPZERO_CELL_CONTEXT=cell-0", "LOOPZERO_CORE_API_INTERNAL_URL=http://secret-fetch.core.internal"},
		"-f", "infra/helmfile/cell.helmfile.yaml.gotmpl", "-e", "prod", "--selector", "name=loopzero-broker", "template")
	if err == nil {
		t.Fatal("the prod cell rendered a broker store that sends its bearer over http://")
	}
	if !strings.Contains(stderr, "insecure broker.apiURL") {
		t.Errorf("the prod render failed, but not on the plaintext address:\n%s", truncate(stderr, 1200))
	}
}

func TestProdCellsFetchSecretsFromTheInternalHTTPSAddress(t *testing.T) {
	url := brokerStoreURL(t, "prod", "LOOPZERO_CELL_CONTEXT=cell-0", "LOOPZERO_CORE_API_INTERNAL_URL="+prodCoreAPIInternalURL)

	if !strings.HasPrefix(url, prodCoreAPIInternalURL+"/internal/v1/secret-fetch/") {
		t.Errorf("the prod broker store calls %q, want %s/internal/v1/secret-fetch/...", url, prodCoreAPIInternalURL)
	}
}

func TestDevPublicCellsStillFetchSecretsFromTheK3dHost(t *testing.T) {
	url := brokerStoreURL(t, "dev-public", "LOOPZERO_CELL_CONTEXT=k3d-loopzero-cell-0")

	if !strings.HasPrefix(url, "http://host.k3d.internal:13001/internal/v1/secret-fetch/") {
		t.Errorf("the dev-public broker store calls %q, want crossCluster.coreApi", url)
	}
}

// secretFetchIngress renders core's loopzero-api release and returns the Ingress of the
// secret-fetch ALB and the Service it forwards to.
func secretFetchIngress(t *testing.T, env string, extraEnv ...string) (ingress, service map[string]any) {
	t.Helper()
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", env, "loopzero-api", "charts/loopzero-api", extraEnv...)
	ingress = renderedObject(t, rendered, "Ingress", "loopzero-api-secret-fetch")
	if ingress == nil {
		return nil, nil
	}
	backend, _ := dig(ingress, "spec", "rules", 0, "http", "paths", 0, "backend", "service", "name").(string)
	return ingress, renderedObject(t, rendered, "Service", backend)
}

// assertOnlySecretFetchIsForwarded fails unless the Ingress forwards one path, the
// secret-fetch prefix: the rest of /internal and the public API stay off the ALB.
func assertOnlySecretFetchIsForwarded(t *testing.T, ingress map[string]any) {
	t.Helper()
	rules, _ := dig(ingress, "spec", "rules").([]any)
	paths, _ := dig(ingress, "spec", "rules", 0, "http", "paths").([]any)
	if len(rules) != 1 || len(paths) != 1 {
		t.Fatalf("the secret-fetch Ingress has %d rules and %d paths, want exactly one path", len(rules), len(paths))
	}
	if got := dig(paths[0], "path"); got != "/internal/v1/secret-fetch" {
		t.Errorf("the secret-fetch Ingress forwards %v, want /internal/v1/secret-fetch only", got)
	}
	if got := dig(paths[0], "pathType"); got != "Prefix" {
		t.Errorf("pathType = %v, want Prefix", got)
	}
}

func TestStagingCorePublishesOnlySecretFetchOnAnInternalALB(t *testing.T) {
	ingress, service := secretFetchIngress(t, "staging", stagingOutputs...)
	if ingress == nil {
		t.Fatal("staging's core renders no Ingress loopzero-api-secret-fetch: the cells have no way to reach the secret-fetch broker")
	}

	assertInternalInstanceALB(t, ingress)
	assertOnlySecretFetchIsForwarded(t, ingress)
	annotations, _ := dig(ingress, "metadata", "annotations").(map[string]any)
	if got := annotations["alb.ingress.kubernetes.io/listen-ports"]; got != `[{"HTTP": 80}]` {
		t.Errorf("listen-ports = %v, want HTTP 80 behind the emulator's HTTPS endpoint", got)
	}
	if service == nil || dig(service, "spec", "type") != "NodePort" {
		t.Errorf("the secret-fetch Ingress does not forward to a NodePort Service: %v", service)
	}
}

func TestProdCoreRefusesTheSecretFetchALBWithoutACertificate(t *testing.T) {
	_, stderr, err := helmfileRunResult(t, nil,
		"-f", "infra/helmfile/core.helmfile.yaml.gotmpl", "-e", "prod", "--selector", "name=loopzero-api", "template")
	if err == nil {
		t.Fatal("prod's core rendered the secret-fetch ALB with no ACM certificate")
	}
	if !strings.Contains(stderr, "certificateArn") {
		t.Errorf("the prod core render failed, but not on the missing certificate:\n%s", truncate(stderr, 1200))
	}
}

func TestProdCorePublishesSecretFetchOverHTTPSWithTheEnvironmentsCertificate(t *testing.T) {
	ingress, _ := secretFetchIngress(t, "prod", "LOOPZERO_CORE_API_INTERNAL_CERTIFICATE_ARN="+prodCoreAPICertificate)
	if ingress == nil {
		t.Fatal("prod's core renders no Ingress loopzero-api-secret-fetch")
	}

	assertInternalInstanceALB(t, ingress)
	assertOnlySecretFetchIsForwarded(t, ingress)
	annotations, _ := dig(ingress, "metadata", "annotations").(map[string]any)
	if got := annotations["alb.ingress.kubernetes.io/listen-ports"]; got != `[{"HTTPS": 443}]` {
		t.Errorf("listen-ports = %v, want HTTPS 443", got)
	}
	if got := annotations["alb.ingress.kubernetes.io/certificate-arn"]; got != prodCoreAPICertificate {
		t.Errorf("certificate-arn = %v, want the environment's %s", got, prodCoreAPICertificate)
	}
}

func TestDevPublicCoreHasNoSecretFetchALB(t *testing.T) {
	ingress, _ := secretFetchIngress(t, "dev-public")

	if ingress != nil {
		t.Error("dev-public's core renders a secret-fetch ALB; its cells reach the API on the k3d host")
	}
}
