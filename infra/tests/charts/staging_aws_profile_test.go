// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The infra-live outputs `task staging:platform` exports before every sync, with the
// shapes infra-live gives them. The tests pass them explicitly instead of inheriting a
// shell's.
var stagingOutputs = []string{
	"LOOPRUNTIME_DB_HOST=172.29.0.1",
	"LOOPRUNTIME_DB_PORT=5432",
	"LOOPRUNTIME_DB_NAME=loopruntime",
	"LOOPRUNTIME_DB_MASTER_SECRET=arn:aws:secretsmanager:us-east-1:000000000000:secret:rds!db-test",
	"LOOPRUNTIME_CACHE_HOST=172.29.0.1",
	"LOOPRUNTIME_CACHE_PORT=6379",
	"LOOPRUNTIME_CACHE_AUTH_SECRET=arn:aws:secretsmanager:us-east-1:000000000000:secret:loopruntime-staging/valkey-auth-token-test",
	"LOOPRUNTIME_VPC_ID=vpc-test",
	"LOOPRUNTIME_CELL_CONTEXT=cell-0",
	"LOOPRUNTIME_EDGE_KVS_ARN=" + stagingEdgeKVSArn,
	// Not an infra-live output: core's secret-fetch ALB, which the task reads from its
	// Ingress after the core sync and before the cells (secret_fetch_broker_test.go).
	"LOOPRUNTIME_CORE_API_ALB_DNS_NAME=" + stagingCoreAPIALB,
}

const stagingEdgeKVSArn = "arn:aws:cloudfront::000000000000:key-value-store/edge-routes-test"

// loopruntime-api publishes each app's public route to the edge's key value store, the
// app-routes output staging exports before the core sync.
func TestStagingApiPublishesRoutesToTheEdgeKeyValueStore(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-api", "charts/loopruntime-api", stagingOutputs...)
	config := renderedObject(t, rendered, "ConfigMap", "loopruntime-api")
	if got := dig(config, "data", "LOOPRUNTIME_EDGE_KVS_ARN"); got != stagingEdgeKVSArn {
		t.Errorf("LOOPRUNTIME_EDGE_KVS_ARN = %v, want %s", got, stagingEdgeKVSArn)
	}
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
		"loopruntime-system/postgresql",
		"loopruntime-system/gitea-redis",
		"loopruntime-system/minio",
	} {
		if installed[release] {
			t.Errorf("%s is installed on staging; the AWS profile uses the managed service instead", release)
		}
	}
	for _, release := range []string{
		"external-secrets/external-secrets",
		"external-secrets/aws-secrets-manager",
		"loopruntime-system/database-bootstrap",
		"kube-system/aws-load-balancer-controller",
		"loopruntime-system/loopruntime-api",
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
	const prefix = "arn:aws:iam::000000000000:role/loopruntime-staging-"
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

	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-api", "charts/loopruntime-api", stagingOutputs...)
	sa := renderedObject(t, rendered, "ServiceAccount", "loopruntime-api")
	if sa == nil {
		t.Fatal("loopruntime-api renders no ServiceAccount loopruntime-api")
	}
	annotations, _ := dig(sa, "metadata", "annotations").(map[string]any)
	if got := annotations["eks.amazonaws.com/role-arn"]; got != prefix+"core-loopruntime-api" {
		t.Errorf("loopruntime-api ServiceAccount role-arn = %v, want %score-loopruntime-api", got, prefix)
	}
}

// loopruntime-api's database credentials are an ExternalSecret over the RDS master secret
// and the platform secret, and its request pool connects as the non-owner role.
func TestStagingDatabaseCredentialsComeFromSecretsManager(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "loopruntime-db-credentials")
	if es == nil {
		t.Fatal("no ExternalSecret loopruntime-db-credentials")
	}
	keys := map[string]bool{}
	data, _ := dig(es, "spec", "data").([]any)
	for _, d := range data {
		entry, _ := d.(map[string]any)
		ref, _ := entry["remoteRef"].(map[string]any)
		keys[ref["key"].(string)] = true
	}
	for _, want := range []string{"arn:aws:secretsmanager:us-east-1:000000000000:secret:rds!db-test", "loopruntime/staging/platform"} {
		if !keys[want] {
			t.Errorf("loopruntime-db-credentials does not read %s; reads %v", want, keys)
		}
	}
	apiURI, _ := dig(es, "spec", "target", "template", "data", "api-uri").(string)
	if !strings.HasPrefix(apiURI, "postgresql://loopruntime_api:") || !strings.Contains(apiURI, "@172.29.0.1:5432/loopruntime?sslmode=require") {
		t.Errorf("api-uri = %q, want the loopruntime_api role on the RDS endpoint with TLS", apiURI)
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
	if got := location["bucket"]; got != "loopruntime-staging-velero-backups-000000000000" {
		t.Errorf("bucket = %v, want loopruntime-staging-velero-backups-000000000000", got)
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
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-api", "charts/loopruntime-api", stagingOutputs...)
	if cert := renderedObject(t, rendered, "Certificate", "loopruntime-api-tls"); cert != nil {
		t.Errorf("loopruntime-api renders Certificate loopruntime-api-tls (issuer %v); the edge holds the public certificate",
			dig(cert, "spec", "issuerRef", "name"))
	}
	route := renderedObject(t, rendered, "IngressRoute", "loopruntime-api")
	if route == nil {
		t.Fatal("loopruntime-api renders no IngressRoute")
	}
	if secret := dig(route, "spec", "tls", "secretName"); secret != nil {
		t.Errorf("loopruntime-api IngressRoute tls.secretName = %v, want none", secret)
	}
}

// loopruntime-cloud signs its session tokens with SESSION_TOKEN_SECRET. Without one it makes
// up a random secret per pod, and every token dies with the pod that issued it (#523 of
// getloopscale/console). Staging reads it from the platform secret, through External
// Secrets, into the Secret the chart mounts as existingSecret.
func TestStagingCloudSessionSecretComesFromSecretsManager(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "loopruntime-cloud-secrets")
	if es == nil {
		t.Fatal("no ExternalSecret loopruntime-cloud-secrets")
	}
	if ns, _ := dig(es, "metadata", "namespace").(string); ns != "loopruntime-system" {
		t.Errorf("loopruntime-cloud-secrets is in namespace %q, want loopruntime-system", ns)
	}
	found := false
	data, _ := dig(es, "spec", "data").([]any)
	for _, d := range data {
		entry, _ := d.(map[string]any)
		ref, _ := entry["remoteRef"].(map[string]any)
		if entry["secretKey"] == "SESSION_TOKEN_SECRET" && ref["key"] == "loopruntime/staging/platform" && ref["property"] == "session-token-secret" {
			found = true
		}
	}
	if !found {
		t.Errorf("loopruntime-cloud-secrets does not map session-token-secret of loopruntime/staging/platform onto SESSION_TOKEN_SECRET: %v", data)
	}

	cloud := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-cloud", "charts/loopruntime-cloud", stagingOutputs...)
	deployment := renderedObject(t, cloud, "Deployment", "loopruntime-cloud")
	if deployment == nil {
		t.Fatal("no Deployment loopruntime-cloud")
	}
	containers, _ := dig(deployment, "spec", "template", "spec", "containers").([]any)
	mounted := false
	for _, c := range containers {
		sources, _ := dig(c, "envFrom").([]any)
		for _, s := range sources {
			if dig(s, "secretRef", "name") == "loopruntime-cloud-secrets" && dig(s, "secretRef", "optional") != true {
				mounted = true
			}
		}
	}
	if !mounted {
		t.Error("the loopruntime-cloud Deployment does not mount loopruntime-cloud-secrets as a required envFrom source")
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
	if got := externalSecretRefs(t, es)["apiKey"]; got != "loopruntime/staging/resend#api-key" {
		t.Errorf("ory-kratos-resend reads the key from %q, want loopruntime/staging/resend#api-key", got)
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

// loopruntime-cloud runs in production mode on staging, which refuses to start without its
// Stripe keys; staging's are test-mode keys, from their own secret.
func TestStagingCloudRunsInProductionModeWithStripeTestKeys(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "loopruntime-cloud-secrets")
	if es == nil {
		t.Fatal("no ExternalSecret loopruntime-cloud-secrets")
	}
	refs := externalSecretRefs(t, es)
	for key, want := range map[string]string{
		"STRIPE_API_KEY":        "loopruntime/staging/stripe#api-key",
		"STRIPE_WEBHOOK_SECRET": "loopruntime/staging/stripe#webhook-secret",
		"STRIPE_PRICE_STARTER":  "loopruntime/staging/stripe#price-starter",
		"STRIPE_PRICE_PRO":      "loopruntime/staging/stripe#price-pro",
		"STRIPE_PRICE_TEAM":     "loopruntime/staging/stripe#price-team",
	} {
		if refs[key] != want {
			t.Errorf("loopruntime-cloud-secrets maps %s from %q, want %s", key, refs[key], want)
		}
	}

	cloud := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-cloud", "charts/loopruntime-cloud", stagingOutputs...)
	config := renderedObject(t, cloud, "ConfigMap", "loopruntime-cloud")
	if got := dig(config, "data", "LOOPRUNTIME_ENV"); got != "prod" {
		t.Errorf("LOOPRUNTIME_ENV = %v, want prod (the console's production mode)", got)
	}
}

// The control plane's traces reach the collector the core helmfile installs, which hands
// them to Tempo. Unset, loopruntime-cloud dialled the console's default address
// (otel-collector.theo-system), which no cluster resolves, every five seconds.
func TestStagingControlPlaneTracesReachTheCollector(t *testing.T) {
	const collector = "otel-collector-opentelemetry-collector.monitoring.svc.cluster.local:4317"
	for _, release := range []struct{ name, chart string }{
		{"loopruntime-api", "charts/loopruntime-api"},
		{"loopruntime-cloud", "charts/loopruntime-cloud"},
	} {
		rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", release.name, release.chart, stagingOutputs...)
		config := renderedObject(t, rendered, "ConfigMap", release.name)
		if got := dig(config, "data", "OTEL_EXPORTER_OTLP_ENDPOINT"); got != collector {
			t.Errorf("%s: OTEL_EXPORTER_OTLP_ENDPOINT = %v, want %s", release.name, got, collector)
		}
	}
}

// loopruntime-api runs on core and reads the build cluster's Loki, which staging publishes
// through an internal ALB the emulator serves at its endpoint with the ALB's DNSName in
// the path. Before the ALB exists (a rebuild's first core sync) the chart's default
// stands, and the core sync after the build one renders the ALB address.
func TestStagingApiReadsLokiThroughTheBuildClusterALB(t *testing.T) {
	const alb = "internal-k8s-monitoring-lokiinte-0123456789.us-east-1.elb.amazonaws.com"
	env := append(append([]string{}, stagingOutputs...), "LOOPRUNTIME_LOKI_ALB_DNS_NAME="+alb)
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-api", "charts/loopruntime-api", env...)
	config := renderedObject(t, rendered, "ConfigMap", "loopruntime-api")
	want := "https://172.29.0.1:9597/_aws/elasticloadbalancing/" + alb
	if got := dig(config, "data", "LOKI_URL"); got != want {
		t.Errorf("LOKI_URL = %v, want %s", got, want)
	}
}

// The build signs tenant images with the key in Secret argo/cosign-signing-key. On the AWS
// path External Secrets writes it into the build cluster from the secret
// seed-aws-cosign-key.sh creates; without it the signing step cannot start (#53).
func TestStagingBuildSigningKeyComesFromSecretsManager(t *testing.T) {
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "cosign-signing-key")
	if es == nil {
		t.Fatal("no ExternalSecret cosign-signing-key")
	}
	if ns := dig(es, "metadata", "namespace"); ns != "argo" {
		t.Errorf("cosign-signing-key is written into %v, want argo", ns)
	}
	refs := externalSecretRefs(t, es)
	for _, key := range []string{"cosign.key", "cosign.pub", "cosign.password"} {
		if got := refs[key]; got != "loopruntime/staging/cosign-signing-key#"+key {
			t.Errorf("%s reads %q, want loopruntime/staging/cosign-signing-key#%s", key, got, key)
		}
	}
}

// Builds push tenant app images under the ECR prefix infra-live's registry/ecr creates
// repositories for on the first push, at the address the pods reach the registry by.
func TestStagingBuildsPushTenantImagesToTheECRPrefix(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-api", "charts/loopruntime-api", stagingOutputs...)
	cm := renderedObject(t, rendered, "ConfigMap", "loopruntime-api")
	if cm == nil {
		t.Fatal("loopruntime-api renders no ConfigMap loopruntime-api")
	}
	if got := dig(cm, "data", "REGISTRY_URL"); got != "172.29.0.1:9597/loopruntime-tenants" {
		t.Errorf("REGISTRY_URL = %v, want 172.29.0.1:9597/loopruntime-tenants", got)
	}
}

// The cells admit tenant pods whose images come from the tenant registry prefix, and no
// longer from the DigitalOcean registry the policy named before ECR.
func TestStagingCellsAdmitTenantImagesFromTheECRPrefix(t *testing.T) {
	rendered := helmfileTemplate(t, "cell.helmfile.yaml.gotmpl", "staging", "admission-policies", "charts/loopruntime-admission-policies", stagingOutputs...)
	vap := renderedObject(t, rendered, "ValidatingAdmissionPolicy", "loopruntime-restrict-image-registries")
	if vap == nil {
		t.Fatal("no ValidatingAdmissionPolicy loopruntime-restrict-image-registries")
	}
	expr, _ := dig(vap, "spec", "validations", 0, "expression").(string)
	if !strings.Contains(expr, `c.image.startsWith("172.29.0.1:9597/loopruntime-tenants/")`) {
		t.Errorf("container rule %q does not admit 172.29.0.1:9597/loopruntime-tenants/", expr)
	}
	if strings.Contains(expr, "registry.digitalocean.com") {
		t.Errorf("container rule %q still admits the DigitalOcean registry", expr)
	}
}

// The build pushes with argo/buildkit-registry-config, an ECR authorization token that a
// CronJob on the build cluster refreshes under the role infra-live grants push to
// (identity/build-registry-push). The token lasts 12 hours.
func TestStagingBuildRegistryCredentialIsAnECRTokenRefreshedByTheBuildCluster(t *testing.T) {
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", "staging", "ecr-registry-auth", "infra/helm/ecr-registry-auth", stagingOutputs...)
	sa := renderedObject(t, rendered, "ServiceAccount", "loopruntime-registry-auth")
	if sa == nil {
		t.Fatal("no ServiceAccount loopruntime-registry-auth")
	}
	if ns := dig(sa, "metadata", "namespace"); ns != "argo" {
		t.Errorf("ServiceAccount namespace = %v, want argo", ns)
	}
	annotations, _ := dig(sa, "metadata", "annotations").(map[string]any)
	if got := annotations["eks.amazonaws.com/role-arn"]; got != "arn:aws:iam::000000000000:role/loopruntime-staging-build-registry-push" {
		t.Errorf("role-arn = %v, want the build-registry-push role", got)
	}
	cron := renderedObject(t, rendered, "CronJob", "loopruntime-registry-auth")
	if cron == nil {
		t.Fatal("no CronJob loopruntime-registry-auth")
	}
	if got := dig(cron, "spec", "jobTemplate", "spec", "template", "spec", "serviceAccountName"); got != "loopruntime-registry-auth" {
		t.Errorf("CronJob runs as %v, want loopruntime-registry-auth", got)
	}
	env := map[string]any{}
	for _, c := range []string{"initContainers", "containers"} {
		list, _ := dig(cron, "spec", "jobTemplate", "spec", "template", "spec", c).([]any)
		for _, container := range list {
			vars, _ := dig(container, "env").([]any)
			for _, v := range vars {
				env[fmt.Sprint(dig(v, "name"))] = dig(v, "value")
			}
		}
	}
	if env["REGISTRY_HOST"] != "172.29.0.1:9597" || env["SECRET_NAME"] != "buildkit-registry-config" {
		t.Errorf("CronJob env REGISTRY_HOST=%v SECRET_NAME=%v, want 172.29.0.1:9597 and buildkit-registry-config", env["REGISTRY_HOST"], env["SECRET_NAME"])
	}
	if renderedObject(t, rendered, "Job", "loopruntime-registry-auth-initial") == nil {
		t.Error("no Job loopruntime-registry-auth-initial: the first build would wait for the first schedule")
	}
}

// Staging has no MinIO and no loopruntime-minio-credentials Secret: the clone and cache
// steps of every build reach S3 as the IRSA role of loopruntime-build-sa. The first tenant
// build on staging waited on that missing Secret (CreateContainerConfigError) until it
// was cancelled. The role reads the builds bucket only, so the dependency cache goes to
// the artifacts bucket.
func TestStagingBuildStepsReachS3AsTheBuildWorkflowsRole(t *testing.T) {
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", "staging", "loopruntime-build-cwts", "charts/loopruntime-build-cwts", stagingOutputs...)
	for _, cwt := range []string{"loopruntime-clone", "loopruntime-cache-restore", "loopruntime-cache-save"} {
		t.Run(cwt, func(t *testing.T) {
			obj := renderedObject(t, rendered, "ClusterWorkflowTemplate", cwt)
			if obj == nil {
				t.Fatalf("no ClusterWorkflowTemplate %s", cwt)
			}
			templates, _ := dig(obj, "spec", "templates").([]any)
			if len(templates) == 0 {
				t.Fatalf("%s has no templates", cwt)
			}
			for _, tmpl := range templates {
				container := dig(tmpl, "container")
				if refs := dig(container, "envFrom"); refs != nil {
					t.Errorf("%s reads a Secret through envFrom %v; staging has none", cwt, refs)
				}
				script := strings.Join(func() []string {
					var out []string
					for _, part := range []any{dig(container, "command"), dig(container, "args")} {
						list, _ := part.([]any)
						for _, s := range list {
							out = append(out, fmt.Sprint(s))
						}
					}
					return out
				}(), "\n")
				for _, want := range []string{"MC_WEB_IDENTITY_TOKEN_FILE_loopruntime", "MC_ROLE_ARN_loopruntime", "MC_STS_ENDPOINT_loopruntime"} {
					if !strings.Contains(script, want) {
						t.Errorf("%s does not set %s: mc would sign with no credentials", cwt, want)
					}
				}
				if regexp.MustCompile(`(?m)^\s*--insecure\b`).MatchString(script) {
					t.Errorf("%s passes --insecure to mc; staging's S3 has a verifiable certificate", cwt)
				}
				if cwt != "loopruntime-clone" && !strings.Contains(script, "loopruntime/loopruntime-staging-artifacts-000000000000/") {
					t.Errorf("%s does not keep the cache in the artifacts bucket, the one the build role can write", cwt)
				}
			}
		})
	}
}

// The SBOM and static app steps upload to the staging static assets bucket; the
// pipeline's default name, loopruntime-static-assets, is no bucket on staging.
func TestStagingAPINamesTheStaticAssetsBucket(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-api", "charts/loopruntime-api", stagingOutputs...)
	cm := renderedObject(t, rendered, "ConfigMap", "loopruntime-api")
	if cm == nil {
		t.Fatal("no ConfigMap loopruntime-api")
	}
	if got := dig(cm, "data", "STATIC_ASSETS_BUCKET"); got != "loopruntime-staging-static-assets-000000000000" {
		t.Errorf("STATIC_ASSETS_BUCKET = %v, want loopruntime-staging-static-assets-000000000000", got)
	}
}

// Staging's S3, STS and ECR are the emulator at 172.29.0.1:9597, a port the build pods'
// default-deny egress did not open: the first IRSA clone timed out dialling it. Only
// that host and port open; production has no emulator block and keeps 443.
func TestStagingBuildPodsReachTheEmulator(t *testing.T) {
	rendered := helmfileTemplate(t, "build.helmfile.yaml.gotmpl", "staging", "loopruntime-build-cwts", "charts/loopruntime-build-cwts", stagingOutputs...)
	np := renderedObject(t, rendered, "NetworkPolicy", "build-pod-egress")
	if np == nil {
		t.Fatal("no NetworkPolicy build-pod-egress")
	}
	rules, _ := dig(np, "spec", "egress").([]any)
	for _, rule := range rules {
		to, _ := dig(rule, "to").([]any)
		ports, _ := dig(rule, "ports").([]any)
		if len(to) == 1 && len(ports) == 1 &&
			dig(to[0], "ipBlock", "cidr") == "172.29.0.1/32" &&
			fmt.Sprint(dig(ports[0], "port")) == "9597" && dig(ports[0], "protocol") == "TCP" {
			return
		}
	}
	t.Errorf("build-pod-egress has no rule for TCP 9597 to 172.29.0.1/32:\n%v", rules)
}

// Staging's GitHub App (id, private key, webhook secret) lives in a secret of its own,
// seeded from the host like Resend's and Stripe's, and reaches loopruntime-api as the
// optional loopruntime-api-github source its values-bootstrap declares. The console's
// install button sends the browser to that App's slug.
func TestStagingGitHubAppReachesTheAPIFromItsOwnSecret(t *testing.T) {
	rendered := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "aws-secrets-manager", "infra/helm/aws-secrets-manager", stagingOutputs...)
	es := renderedObject(t, rendered, "ExternalSecret", "loopruntime-api-github")
	if es == nil {
		t.Fatal("no ExternalSecret loopruntime-api-github")
	}
	if ns, _ := dig(es, "metadata", "namespace").(string); ns != "loopruntime-system" {
		t.Errorf("loopruntime-api-github is in namespace %q, want loopruntime-system", ns)
	}
	refs := externalSecretRefs(t, es)
	for key, want := range map[string]string{
		"GITHUB_APP_ID":          "loopruntime/staging/github-app#app-id",
		"GITHUB_APP_PRIVATE_KEY": "loopruntime/staging/github-app#private-key",
		"GITHUB_WEBHOOK_SECRET":  "loopruntime/staging/github-app#webhook-secret",
		// The App's OAuth client: the API asks GitHub which installations a
		// user can access before it binds one to a tenant.
		"GITHUB_APP_CLIENT_ID":     "loopruntime/staging/github-app#client-id",
		"GITHUB_APP_CLIENT_SECRET": "loopruntime/staging/github-app#client-secret",
	} {
		if refs[key] != want {
			t.Errorf("loopruntime-api-github maps %s from %q, want %s", key, refs[key], want)
		}
	}

	cloud := helmfileTemplate(t, "core.helmfile.yaml.gotmpl", "staging", "loopruntime-cloud", "charts/loopruntime-cloud", stagingOutputs...)
	config := renderedObject(t, cloud, "ConfigMap", "loopruntime-cloud")
	if got := dig(config, "data", "GITHUB_APP_SLUG"); got != "loopscale-staging" {
		t.Errorf("GITHUB_APP_SLUG = %v, want loopscale-staging", got)
	}

	taskfile, err := os.ReadFile("../../../taskfiles/staging.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`for name in [^;\n]*\bgithub-app\b`).Match(taskfile) {
		t.Error("staging:platform does not seed loopruntime/staging/github-app from the host")
	}
}

// Without githubAppSecret the chart renders no loopruntime-api-github, and the API keeps
// its repository features off instead of reading an empty secret.
func TestNoGitHubAppSecretRendersNoGitHubExternalSecret(t *testing.T) {
	out := renderChart(t, uniqueChart(t, "aws-secrets-manager"), "aws-secrets-manager", nil, writeValues(t, `
region: us-east-1
coreSecrets: true
platformSecret: loopruntime/test/platform
database: {host: db, masterSecret: m}
cache: {host: c, authTokenSecret: a}
`))
	if strings.Contains(out, "name: loopruntime-api-github") {
		t.Error("loopruntime-api-github rendered without githubAppSecret")
	}
}

// A tenant's custom domain reaches its cell under its own name (#174). An ALB rule with a
// host answers every other host itself, with an empty 404 that never gets to Traefik, so
// the rule in front of a cell's Traefik names no host. Traefik's IngressRoutes are what
// decide which hosts exist.
func TestTheCellLoadBalancerPassesEveryHostToTraefik(t *testing.T) {
	values := helmfileValues(t, "cell.helmfile.yaml.gotmpl", "staging", "traefik", stagingMonitoringOutputs()...)
	objects, _ := values["extraObjects"].([]any)
	var edge map[string]any
	for _, object := range objects {
		if o, ok := object.(map[string]any); ok && o["kind"] == "Ingress" && dig(o, "metadata", "name") == "traefik-edge" {
			edge = o
		}
	}
	if edge == nil {
		t.Fatal("the cell's traefik renders no Ingress traefik-edge")
	}
	rules, _ := dig(edge, "spec", "rules").([]any)
	if len(rules) != 1 {
		t.Fatalf("traefik-edge has %d rules, want one", len(rules))
	}
	rule, _ := rules[0].(map[string]any)
	if host, ok := rule["host"]; ok {
		t.Errorf("traefik-edge rule host = %v: the ALB answers a custom domain 404 before Traefik sees it", host)
	}
}
