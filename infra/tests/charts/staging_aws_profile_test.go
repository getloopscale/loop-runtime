// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The infra-live outputs `task staging:platform` exports before every sync, with the
// shapes infra-live gives them. The tests pass them explicitly instead of inheriting a
// shell's.
var stagingOutputs = []string{
	"LOOPZERO_DB_HOST=172.29.0.1",
	"LOOPZERO_DB_PORT=5432",
	"LOOPZERO_DB_NAME=loopzero",
	"LOOPZERO_DB_MASTER_SECRET=arn:aws:secretsmanager:us-east-1:000000000000:secret:rds!db-test",
	"LOOPZERO_CACHE_HOST=172.29.0.1",
	"LOOPZERO_CACHE_PORT=6379",
	"LOOPZERO_CACHE_AUTH_SECRET=arn:aws:secretsmanager:us-east-1:000000000000:secret:loopzero-staging/valkey-auth-token-test",
	"LOOPZERO_VPC_ID=vpc-test",
	"LOOPZERO_CELL_CONTEXT=cell-0",
}

func stagingInstalledReleases(t *testing.T, helmfile string) map[string]bool {
	t.Helper()
	out := helmfileRun(t, stagingOutputs, "-f", "infra/helmfile/"+helmfile, "-e", "staging", "list", "--output=json")
	var releases []listedRelease
	if err := json.Unmarshal([]byte(out), &releases); err != nil {
		t.Fatalf("decode helmfile list output: %v\n%s", err, truncate(out, 500))
	}
	installed := map[string]bool{}
	for _, r := range releases {
		installed[r.Namespace+"/"+r.Name] = r.Installed
	}
	if len(installed) == 0 {
		t.Fatalf("helmfile list returned no releases for staging/%s", helmfile)
	}
	return installed
}

// Staging runs production's managed data services: RDS, ElastiCache and S3 replace the
// in-cluster PostgreSQL, Redis and MinIO, and AWS Secrets Manager replaces OpenBao.
func TestStagingRunsNoInClusterDataServices(t *testing.T) {
	installed := stagingInstalledReleases(t, "core.helmfile.yaml.gotmpl")
	for _, release := range []string{
		"loopzero-system/postgresql",
		"loopzero-system/gitea-redis",
		"loopzero-system/minio",
	} {
		if installed[release] {
			t.Errorf("%s is installed on staging; the AWS profile uses the managed service instead", release)
		}
	}
	for _, release := range []string{
		"external-secrets/external-secrets",
		"external-secrets/aws-secrets-manager",
		"loopzero-system/database-bootstrap",
		"kube-system/aws-load-balancer-controller",
		"loopzero-system/loopzero-api",
	} {
		if !installed[release] {
			t.Errorf("%s is not installed on staging", release)
		}
	}
	for release, on := range installed {
		if on && strings.Contains(release, "openbao") {
			t.Errorf("%s is installed on staging; secrets come from AWS Secrets Manager", release)
		}
	}
}

// Every workload that calls AWS carries the IRSA role infra-live creates for it, named
// `<namePrefix>-<cluster>-<workload>` and trusting exactly that ServiceAccount.
func TestStagingWorkloadsCarryTheirIRSARoles(t *testing.T) {
	const prefix = "arn:aws:iam::000000000000:role/loopzero-staging-"
	cases := []struct {
		helmfile, release string
		path              []any
		role              string
	}{
		{"core.helmfile.yaml.gotmpl", "external-secrets", []any{"serviceAccount", "annotations"}, "core-external-secrets"},
		{"core.helmfile.yaml.gotmpl", "aws-load-balancer-controller", []any{"serviceAccount", "annotations"}, "core-lb-controller"},
		{"core.helmfile.yaml.gotmpl", "velero", []any{"velero", "serviceAccount", "server", "annotations"}, "core-velero"},
		{"cell.helmfile.yaml.gotmpl", "external-secrets", []any{"serviceAccount", "annotations"}, "cell-0-external-secrets"},
		{"cell.helmfile.yaml.gotmpl", "aws-load-balancer-controller", []any{"serviceAccount", "annotations"}, "cell-0-lb-controller"},
		{"build.helmfile.yaml.gotmpl", "external-secrets", []any{"serviceAccount", "annotations"}, "build-external-secrets"},
		{"build.helmfile.yaml.gotmpl", "argo-workflows", []any{"workflow", "serviceAccount", "annotations"}, "build-workflows"},
		{"build.helmfile.yaml.gotmpl", "loki", []any{"serviceAccount", "annotations"}, "build-loki"},
	}
	for _, c := range cases {
		t.Run(c.helmfile+"/"+c.release, func(t *testing.T) {
			values := helmfileValues(t, c.helmfile, "staging", c.release, stagingOutputs...)
			annotations, _ := dig(values, c.path...).(map[string]any)
			if got := annotations["eks.amazonaws.com/role-arn"]; got != prefix+c.role {
				t.Errorf("%s: role-arn annotation = %v, want %s", c.release, got, prefix+c.role)
			}
		})
	}

	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopzero-api", "charts/loopzero-api", stagingOutputs...)
	sa := renderedObject(t, rendered, "ServiceAccount", "loopzero-api")
	if sa == nil {
		t.Fatal("loopzero-api renders no ServiceAccount loopzero-api")
	}
	annotations, _ := dig(sa, "metadata", "annotations").(map[string]any)
	if got := annotations["eks.amazonaws.com/role-arn"]; got != prefix+"core-loopzero-api" {
		t.Errorf("loopzero-api ServiceAccount role-arn = %v, want %score-loopzero-api", got, prefix)
	}
}

// loopzero-api's database credentials are an ExternalSecret over the RDS master secret
// and the platform secret, and its request pool connects as the non-owner role.
func TestStagingDatabaseCredentialsComeFromSecretsManager(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "loopzero-db-credentials")
	if es == nil {
		t.Fatal("no ExternalSecret loopzero-db-credentials")
	}
	keys := map[string]bool{}
	data, _ := dig(es, "spec", "data").([]any)
	for _, d := range data {
		entry, _ := d.(map[string]any)
		ref, _ := entry["remoteRef"].(map[string]any)
		keys[ref["key"].(string)] = true
	}
	for _, want := range []string{"arn:aws:secretsmanager:us-east-1:000000000000:secret:rds!db-test", "loopzero/staging/platform"} {
		if !keys[want] {
			t.Errorf("loopzero-db-credentials does not read %s; reads %v", want, keys)
		}
	}
	apiURI, _ := dig(es, "spec", "target", "template", "data", "api-uri").(string)
	if !strings.HasPrefix(apiURI, "postgresql://loopzero_api:") || !strings.Contains(apiURI, "@172.29.0.1:5432/loopzero?sslmode=require") {
		t.Errorf("api-uri = %q, want the loopzero_api role on the RDS endpoint with TLS", apiURI)
	}
}

// On AWS S3 Velero's BackupStorageLocation names no s3Url: the SDK resolves the regional
// endpoint, or the one AWS_ENDPOINT_URL names. The chart's schema rejects its placeholder,
// so the location must replace the chart's whole instead of patching one field.
func TestStagingVeleroBackupLocationIsAWSS3(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "staging", "velero", stagingOutputs...)
	locations, _ := dig(values, "velero", "configuration", "backupStorageLocation").([]any)
	if len(locations) != 1 {
		t.Fatalf("backupStorageLocation = %v, want exactly one location", locations)
	}
	location, _ := locations[0].(map[string]any)
	if got := location["bucket"]; got != "loopzero-staging-velero-backups-000000000000" {
		t.Errorf("bucket = %v, want loopzero-staging-velero-backups-000000000000", got)
	}
	config, _ := location["config"].(map[string]any)
	if url, ok := config["s3Url"]; ok {
		t.Errorf("config.s3Url = %v, want no s3Url on AWS S3", url)
	}
	if got := config["region"]; got != "us-east-1" {
		t.Errorf("config.region = %v, want us-east-1", got)
	}
}

// The ALB in front of Traefik registers the node group's instances on Traefik's NodePort.
// An ip target is a pod address the controller can register only when the pod has a VPC
// network interface (the Amazon VPC CNI); with any other CNI the controller never
// registers a target ("cannot resolve pod ENI for pods") and the ALB has nothing to send
// traffic to. Instance targets need a NodePort (or LoadBalancer) Service.
func TestStagingEdgeIngressRegistersInstanceTargets(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "staging", "traefik", stagingOutputs...)
	if got := dig(values, "service", "type"); got != "NodePort" {
		t.Errorf("traefik service.type = %v, want NodePort for instance targets", got)
	}
	objects, _ := values["extraObjects"].([]any)
	var edge map[string]any
	for _, object := range objects {
		if o, ok := object.(map[string]any); ok && o["kind"] == "Ingress" && dig(o, "metadata", "name") == "traefik-edge" {
			edge = o
		}
	}
	if edge == nil {
		t.Fatal("traefik renders no Ingress traefik-edge")
	}
	annotations, _ := dig(edge, "metadata", "annotations").(map[string]any)
	if got := annotations["alb.ingress.kubernetes.io/target-type"]; got != "instance" {
		t.Errorf("traefik-edge target-type = %v, want instance", got)
	}
}

// Public TLS terminates at the edge in front of the ALB (CloudFront and ACM, Caddy on
// staging), and an ALB does not validate a target's certificate. The API's route asks for
// no cert-manager Certificate: there is no ACME issuer in the cluster, and a route whose
// TLS Secret never appears is dropped by Traefik ("secret ... does not exist"), which
// leaves the host answering 404.
func TestStagingAPIRouteLeavesTLSToTheEdge(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopzero-api", "charts/loopzero-api", stagingOutputs...)
	if cert := renderedObject(t, rendered, "Certificate", "loopzero-api-tls"); cert != nil {
		t.Errorf("loopzero-api renders Certificate loopzero-api-tls (issuer %v); the edge holds the public certificate",
			dig(cert, "spec", "issuerRef", "name"))
	}
	route := renderedObject(t, rendered, "IngressRoute", "loopzero-api")
	if route == nil {
		t.Fatal("loopzero-api renders no IngressRoute")
	}
	if secret := dig(route, "spec", "tls", "secretName"); secret != nil {
		t.Errorf("loopzero-api IngressRoute tls.secretName = %v, want none", secret)
	}
}

// loopzero-cloud signs its session tokens with SESSION_TOKEN_SECRET. Without one it makes
// up a random secret per pod, and every token dies with the pod that issued it (#523 of
// getloopscale/console). Staging reads it from the platform secret, through External
// Secrets, into the Secret the chart mounts as existingSecret.
func TestStagingCloudSessionSecretComesFromSecretsManager(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "loopzero-cloud-secrets")
	if es == nil {
		t.Fatal("no ExternalSecret loopzero-cloud-secrets")
	}
	if ns, _ := dig(es, "metadata", "namespace").(string); ns != "loopzero-system" {
		t.Errorf("loopzero-cloud-secrets is in namespace %q, want loopzero-system", ns)
	}
	found := false
	data, _ := dig(es, "spec", "data").([]any)
	for _, d := range data {
		entry, _ := d.(map[string]any)
		ref, _ := entry["remoteRef"].(map[string]any)
		if entry["secretKey"] == "SESSION_TOKEN_SECRET" && ref["key"] == "loopzero/staging/platform" && ref["property"] == "session-token-secret" {
			found = true
		}
	}
	if !found {
		t.Errorf("loopzero-cloud-secrets does not map session-token-secret of loopzero/staging/platform onto SESSION_TOKEN_SECRET: %v", data)
	}

	cloud := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopzero-cloud", "charts/loopzero-cloud", stagingOutputs...)
	deployment := renderedObject(t, cloud, "Deployment", "loopzero-cloud")
	if deployment == nil {
		t.Fatal("no Deployment loopzero-cloud")
	}
	containers, _ := dig(deployment, "spec", "template", "spec", "containers").([]any)
	mounted := false
	for _, c := range containers {
		sources, _ := dig(c, "envFrom").([]any)
		for _, s := range sources {
			if dig(s, "secretRef", "name") == "loopzero-cloud-secrets" && dig(s, "secretRef", "optional") != true {
				mounted = true
			}
		}
	}
	if !mounted {
		t.Error("the loopzero-cloud Deployment does not mount loopzero-cloud-secrets as a required envFrom source")
	}
}

// externalSecretRefs maps each secretKey of an ExternalSecret onto "<key>#<property>".
func externalSecretRefs(t *testing.T, es map[string]any) map[string]string {
	t.Helper()
	refs := map[string]string{}
	data, _ := dig(es, "spec", "data").([]any)
	for _, d := range data {
		entry, _ := d.(map[string]any)
		ref, _ := entry["remoteRef"].(map[string]any)
		key, _ := entry["secretKey"].(string)
		refs[key] = fmt.Sprintf("%v#%v", ref["key"], ref["property"])
	}
	return refs
}

// The third-party credentials staging cannot generate (Resend's key, Stripe's test keys)
// live in secrets of their own, seeded from files on the host that outlive a rebuild of
// the emulator. Kratos's courier reads the Resend key as a config override from the
// Secret External Secrets writes, so it never passes through Helm.
func TestStagingResendKeyReachesTheCourierFromItsOwnSecret(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "ory-kratos-resend")
	if es == nil {
		t.Fatal("no ExternalSecret ory-kratos-resend")
	}
	if got := externalSecretRefs(t, es)["apiKey"]; got != "loopzero/staging/resend#api-key" {
		t.Errorf("ory-kratos-resend reads the key from %q, want loopzero/staging/resend#api-key", got)
	}
	override, _ := dig(es, "spec", "target", "template", "data", "COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_VALUE").(string)
	if !strings.HasPrefix(override, "Bearer ") || !strings.Contains(override, ".apiKey") {
		t.Errorf("COURIER_HTTP_REQUEST_CONFIG_AUTH_CONFIG_VALUE = %q, want the bearer of the Resend key", override)
	}

	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "staging", "ory-kratos", stagingOutputs...)
	request, _ := dig(values, "kratos", "config", "courier", "http", "request_config").(map[string]any)
	if header := dig(request, "headers", "Authorization"); header != nil {
		t.Errorf("the courier config still renders an Authorization header: %v", header)
	}
	if dig(request, "auth", "type") != "api_key" || dig(request, "auth", "config", "name") != "Authorization" || dig(request, "auth", "config", "in") != "header" {
		t.Errorf("the courier authenticates with %v, want an api_key in the Authorization header", request["auth"])
	}
	body, _ := request["body"].(string)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(body, "base64://"))
	if err != nil || !strings.Contains(string(decoded), `from: "LoopScale <noreply@getloopscale.com>"`) {
		t.Errorf("the courier does not send as LoopScale <noreply@getloopscale.com>: %q (%v)", decoded, err)
	}
}

// loopzero-cloud runs in production mode on staging, which refuses to start without its
// Stripe keys; staging's are test-mode keys, from their own secret.
func TestStagingCloudRunsInProductionModeWithStripeTestKeys(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "loopzero-cloud-secrets")
	if es == nil {
		t.Fatal("no ExternalSecret loopzero-cloud-secrets")
	}
	refs := externalSecretRefs(t, es)
	for key, want := range map[string]string{
		"STRIPE_API_KEY":        "loopzero/staging/stripe#api-key",
		"STRIPE_WEBHOOK_SECRET": "loopzero/staging/stripe#webhook-secret",
		"STRIPE_PRICE_STARTER":  "loopzero/staging/stripe#price-starter",
		"STRIPE_PRICE_PRO":      "loopzero/staging/stripe#price-pro",
		"STRIPE_PRICE_TEAM":     "loopzero/staging/stripe#price-team",
	} {
		if refs[key] != want {
			t.Errorf("loopzero-cloud-secrets maps %s from %q, want %s", key, refs[key], want)
		}
	}

	cloud := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopzero-cloud", "charts/loopzero-cloud", stagingOutputs...)
	config := renderedObject(t, cloud, "ConfigMap", "loopzero-cloud")
	if got := dig(config, "data", "LOOPZERO_ENV"); got != "prod" {
		t.Errorf("LOOPZERO_ENV = %v, want prod (the console's production mode)", got)
	}
}

// The control plane's traces reach the collector the core helmfile installs, which hands
// them to Tempo. Unset, loopzero-cloud dialled the console's default address
// (otel-collector.theo-system), which no cluster resolves, every five seconds.
func TestStagingControlPlaneTracesReachTheCollector(t *testing.T) {
	const collector = "otel-collector-opentelemetry-collector.monitoring.svc.cluster.local:4317"
	for _, release := range []struct{ name, chart string }{
		{"loopzero-api", "charts/loopzero-api"},
		{"loopzero-cloud", "charts/loopzero-cloud"},
	} {
		rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", release.name, release.chart, stagingOutputs...)
		config := renderedObject(t, rendered, "ConfigMap", release.name)
		if got := dig(config, "data", "OTEL_EXPORTER_OTLP_ENDPOINT"); got != collector {
			t.Errorf("%s: OTEL_EXPORTER_OTLP_ENDPOINT = %v, want %s", release.name, got, collector)
		}
	}
}

// loopzero-api runs on core and reads the build cluster's Loki, which staging publishes
// through an internal ALB the emulator serves at its endpoint with the ALB's DNSName in
// the path. Before the ALB exists (a rebuild's first core sync) the chart's default
// stands, and the core sync after the build one renders the ALB address.
func TestStagingApiReadsLokiThroughTheBuildClusterALB(t *testing.T) {
	const alb = "internal-k8s-monitoring-lokiinte-0123456789.us-east-1.elb.amazonaws.com"
	env := append(append([]string{}, stagingOutputs...), "LOOPZERO_LOKI_ALB_DNS_NAME="+alb)
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopzero-api", "charts/loopzero-api", env...)
	config := renderedObject(t, rendered, "ConfigMap", "loopzero-api")
	want := "https://172.29.0.1:9597/_aws/elasticloadbalancing/" + alb
	if got := dig(config, "data", "LOKI_URL"); got != want {
		t.Errorf("LOKI_URL = %v, want %s", got, want)
	}
}
