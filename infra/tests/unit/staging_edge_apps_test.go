// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Tenant apps are public under *.apps.staging.getloopscale.com (#68). Caddy terminates their
// TLS with a wildcard certificate that lego issues over DNS-01 (infra/staging/edge/
// apps-certificate.sh), and hands the request to the emulator with the viewer's Host, where the
// app-router distribution claims it by its wildcard alias and its function sends it to the
// cell. The function finds a cell's load balancer under origin:<cell> in the routes store,
// which infra/staging/edge/apps-origins.sh writes from the cells' traefik-edge ALBs.

// fakeBin writes an executable named name into dir that logs its arguments to log and then
// runs body.
func fakeBin(t *testing.T, dir, name, log, body string) {
	t.Helper()
	script := "#!/usr/bin/env bash\necho \"" + name + " $*\" >> " + log + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runEdgeScript(t *testing.T, script, bin string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra/staging/edge", script))
	cmd.Env = append(os.Environ(), append([]string{"PATH=" + bin + ":" + os.Getenv("PATH")}, env...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const fakeELB = `case "$1 $2" in
  "elbv2 describe-load-balancers")
    if [[ "$*" == *--load-balancer-arns* ]]; then
      case "$*" in
        *arn-cell-0*) echo "cell0.us-east-1.elb.amazonaws.com" ;;
        *arn-cell-1*) echo "cell1.us-east-1.elb.amazonaws.com" ;;
      esac
    else
      echo "arn-core arn-cell-0 arn-cell-1 arn-other"
    fi ;;
  "elbv2 describe-tags")
    cat <<'JSON'
{"TagDescriptions":[
 {"ResourceArn":"arn-core","Tags":[{"Key":"ingress.k8s.aws/stack","Value":"traefik/traefik-edge"},{"Key":"elbv2.k8s.aws/cluster","Value":"loopzero-staging-core"}]},
 {"ResourceArn":"arn-cell-0","Tags":[{"Key":"ingress.k8s.aws/stack","Value":"traefik/traefik-edge"},{"Key":"elbv2.k8s.aws/cluster","Value":"loopzero-staging-cell-0"}]},
 {"ResourceArn":"arn-cell-1","Tags":[{"Key":"ingress.k8s.aws/stack","Value":"traefik/traefik-edge"},{"Key":"elbv2.k8s.aws/cluster","Value":"loopzero-staging-cell-1"}]},
 {"ResourceArn":"arn-other","Tags":[{"Key":"elbv2.k8s.aws/cluster","Value":"loopzero-staging-cell-0"}]}]}
JSON
    ;;
  "cloudfront-keyvaluestore describe-key-value-store") echo "etag-1" ;;
  "cloudfront-keyvaluestore put-key") echo '{"ETag":"etag-2"}' ;;
esac`

func TestTheCellsLoadBalancersAreWrittenAsTheirOrigins(t *testing.T) {
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	fakeBin(t, bin, "aws", log, fakeELB)

	out, err := runEdgeScript(t, "apps-origins.sh", bin, "EDGE_KVS_ARN=arn:kvs", "EDGE_CLUSTER_PREFIX=loopzero-staging-")
	if err != nil {
		t.Fatalf("apps-origins.sh: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	puts := regexp.MustCompile(`(?m)^aws cloudfront-keyvaluestore put-key .*$`).FindAllString(string(calls), -1)
	joined := strings.Join(puts, "\n")
	for _, want := range []string{
		"--key origin:cell-0 --value cell0.us-east-1.elb.amazonaws.com",
		"--key origin:cell-1 --value cell1.us-east-1.elb.amazonaws.com",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no put-key with %q in:\n%s", want, joined)
		}
	}
	if len(puts) != 2 {
		t.Errorf("%d put-key calls, want one per cell (the core ALB serves no tenant app):\n%s", len(puts), joined)
	}
	if !strings.Contains(joined, "--kvs-arn arn:kvs") || !strings.Contains(joined, "--if-match etag-1") {
		t.Errorf("put-key does not name the store and its current ETag:\n%s", joined)
	}
	if !strings.Contains(string(calls), "--if-match etag-2") {
		t.Errorf("the second put-key does not use the ETag the first one answered:\n%s", calls)
	}
}

func TestNoCellLoadBalancerFailsTheOrigins(t *testing.T) {
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	fakeBin(t, bin, "aws", log, `case "$1 $2" in
  "elbv2 describe-load-balancers") echo "arn-core" ;;
  "elbv2 describe-tags") echo '{"TagDescriptions":[]}' ;;
esac`)
	if out, err := runEdgeScript(t, "apps-origins.sh", bin, "EDGE_KVS_ARN=arn:kvs", "EDGE_CLUSTER_PREFIX=loopzero-staging-"); err == nil {
		t.Fatalf("apps-origins.sh succeeded with no cell load balancer:\n%s", out)
	}
}

// lego 5 has one command, run, that issues the certificate when there is none and renews it
// when it is due, so the same call serves the rebuild and the renewal timer.
func TestTheWildcardCertificateIsRequestedOverDNSAndRestartsCaddyWhenItChanges(t *testing.T) {
	bin, log, dir := t.TempDir(), filepath.Join(t.TempDir(), "calls"), t.TempDir()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin(t, bin, "lego", log, `[ "$HOSTINGER_API_TOKEN" = x ] || { echo "lego got no HOSTINGER_API_TOKEN" >&2; exit 1; }`)
	env := []string{"EDGE_APPS_DOMAIN=apps.example.com", "EDGE_LEGO_DIR=" + dir, "EDGE_DNS_TOKEN_FILE=" + token, "EDGE_CERT_EMAIL=ops@example.com"}

	if out, err := runEdgeScript(t, "apps-certificate.sh", bin, env...); err != nil {
		t.Fatalf("apps-certificate.sh: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	call := strings.TrimSpace(string(calls))
	for _, want := range []string{"--dns hostinger", "--domains *.apps.example.com", "--accept-tos", "--email ops@example.com",
		"--path " + dir, "--deploy-hook systemctl try-restart loopzero-edge.service", " run"} {
		if !strings.Contains(call, want) {
			t.Errorf("lego call %q lacks %q", call, want)
		}
	}
}

func TestTheCertificateIsNotRequestedWithoutTheDNSToken(t *testing.T) {
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	fakeBin(t, bin, "lego", log, "")
	out, err := runEdgeScript(t, "apps-certificate.sh", bin, "EDGE_APPS_DOMAIN=apps.example.com", "EDGE_LEGO_DIR="+t.TempDir(),
		"EDGE_DNS_TOKEN_FILE=/nonexistent", "EDGE_CERT_EMAIL=ops@example.com")
	if err == nil || !strings.Contains(out, "/nonexistent") {
		t.Fatalf("apps-certificate.sh without the token file: err=%v\n%s", err, out)
	}
	if calls, _ := os.ReadFile(log); len(calls) != 0 {
		t.Errorf("lego ran without the token: %s", calls)
	}
}

func TestCaddyServesTheAppsWithTheWildcardCertificateAndTheViewersHost(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra/staging/edge/Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "*.{$EDGE_APPS_DOMAIN} {")
	if start < 0 {
		t.Fatal("the Caddyfile has no *.{$EDGE_APPS_DOMAIN} site block")
	}
	block := text[start:]
	if end := strings.Index(block, "\n}\n"); end > 0 {
		block = block[:end]
	}
	for _, want := range []string{"tls {$CREDENTIALS_DIRECTORY}/apps.crt {$CREDENTIALS_DIRECTORY}/apps.key", "reverse_proxy {$EDGE_ORIGIN}", "header_up Host {host}", "tls_trust_pool file {$EDGE_ORIGIN_CA}"} {
		if !strings.Contains(block, want) {
			t.Errorf("the apps block lacks %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "EDGE_ALB_DNS_NAME") {
		t.Error("the apps block rewrites Host to the core ALB; the distribution claims the request by the viewer's host")
	}
}

func TestTheStagingAPIPublishesAppsUnderTheAppsDomain(t *testing.T) {
	values := readRepoFile(t, "infra", "helmfile", "values", "staging", "loopzero-api.yaml.gotmpl")
	if !regexp.MustCompile(`(?m)^\s+loopzeroPublicDomain: "apps\.staging\.getloopscale\.com"$`).MatchString(values) {
		t.Error("the staging API does not set loopzeroPublicDomain to apps.staging.getloopscale.com")
	}
}

func TestTheEdgeUnitHandsCaddyTheWildcardCertificateAsACredential(t *testing.T) {
	unit := readRepoFile(t, "infra", "staging", "systemd", "loopzero-edge.service")
	for _, want := range []string{"LoadCredential=apps.crt:", "LoadCredential=apps.key:"} {
		if !strings.Contains(unit, want) {
			t.Errorf("loopzero-edge.service lacks %s", want)
		}
	}
	timer := readRepoFile(t, "infra", "staging", "systemd", "loopzero-edge-certificate.timer")
	if !strings.Contains(timer, "OnCalendar=") {
		t.Error("loopzero-edge-certificate.timer has no schedule")
	}
}
