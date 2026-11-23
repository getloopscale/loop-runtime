// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The shell `task staging:platform` runs between its syncs for the monitoring of the four
// clusters: infra/staging/monitoring-addresses.sh reads the internal ALBs' DNSNames from
// their Ingress status, and infra/staging/restart-uninjected-pods.sh replaces the pods
// the emulator's EKS layer never wired to the emulator.

// fakeIngressKubectl answers `kubectl --context <ctx> -n monitoring get ingress <name> -o
// jsonpath=...` with the file $FAKE_STATE/<ctx>/<name>, empty when there is none. A file
// <name>.after holding N makes the first N reads answer empty, as an Ingress whose load
// balancer is still being created does.
const fakeIngressKubectl = `#!/usr/bin/env bash
set -euo pipefail
ctx=""; name=""
while [ $# -gt 0 ]; do
  case "$1" in
    --context) ctx="$2"; shift 2 ;;
    ingress) name="$2"; shift 2 ;;
    *) shift ;;
  esac
done
dir="$FAKE_STATE/$ctx"
if [ -f "$dir/$name.after" ]; then
  left=$(cat "$dir/$name.after")
  if [ "$left" -gt 0 ]; then echo $((left - 1)) >"$dir/$name.after"; exit 0; fi
fi
[ -f "$dir/$name" ] && cat "$dir/$name"
exit 0
`

type fakeCluster struct {
	t     *testing.T
	bin   string
	state string
}

func newFakeCluster(t *testing.T, kubectl string) fakeCluster {
	t.Helper()
	f := fakeCluster{t: t, bin: t.TempDir(), state: t.TempDir()}
	if err := os.WriteFile(filepath.Join(f.bin, "kubectl"), []byte(kubectl), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fakeCluster) publish(context, ingress, dnsName string) {
	f.t.Helper()
	dir := filepath.Join(f.state, context)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ingress), []byte(dnsName), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f fakeCluster) run(script string, args ...string) (stdout, stderr string, err error) {
	f.t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(), "infra", "staging", script)}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+f.bin+":"+os.Getenv("PATH"), "FAKE_STATE="+f.state, "MONITORING_ADDRESSES_INTERVAL=0")
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// exported evaluates the script's output as `task staging:platform` does and returns the
// variable's value.
func exported(t *testing.T, output, variable string) string {
	t.Helper()
	out, err := exec.Command("bash", "-c", output+"\nprintf '%s' \"${"+variable+":-}\"").Output()
	if err != nil {
		t.Fatalf("eval %q: %v", output, err)
	}
	return string(out)
}

const (
	lokiALB  = "internal-k8s-monitori-lokiinte-0123456789.us-east-1.elb.amazonaws.com"
	buildALB = "internal-k8s-monitori-promethe-1111111111.us-east-1.elb.amazonaws.com"
	cell0ALB = "internal-k8s-monitori-promethe-2222222222.us-east-1.elb.amazonaws.com"
	cell1ALB = "internal-k8s-monitori-promethe-3333333333.us-east-1.elb.amazonaws.com"
)

func TestMonitoringAddressesExportsTheLokiALB(t *testing.T) {
	f := newFakeCluster(t, fakeIngressKubectl)
	f.publish("build", "loki-internal", lokiALB)

	out, stderr, err := f.run("monitoring-addresses.sh", "loki", "--wait", "5")

	if err != nil {
		t.Fatalf("monitoring-addresses.sh loki: %v\n%s", err, stderr)
	}
	if got := exported(t, out, "LOOPZERO_LOKI_ALB_DNS_NAME"); got != lokiALB {
		t.Errorf("LOOPZERO_LOKI_ALB_DNS_NAME = %q, want %q", got, lokiALB)
	}
}

func TestMonitoringAddressesExportsEveryRemoteClusterMetricsALB(t *testing.T) {
	f := newFakeCluster(t, fakeIngressKubectl)
	f.publish("build", "prometheus-federation", buildALB)
	f.publish("cell-0", "prometheus-federation", cell0ALB)
	f.publish("cell-1", "prometheus-federation", cell1ALB)

	out, stderr, err := f.run("monitoring-addresses.sh", "metrics", "--wait", "5")

	if err != nil {
		t.Fatalf("monitoring-addresses.sh metrics: %v\n%s", err, stderr)
	}
	want := "build=" + buildALB + ",cell-0=" + cell0ALB + ",cell-1=" + cell1ALB
	if got := exported(t, out, "LOOPZERO_METRICS_ALB_DNS_NAMES"); got != want {
		t.Errorf("LOOPZERO_METRICS_ALB_DNS_NAMES = %q, want %q", got, want)
	}
}

// Before the syncs of a rebuild no Ingress has an ALB yet: without --wait the script
// exports nothing and succeeds, so the first core sync renders without the addresses.
func TestMonitoringAddressesExportsNothingWhileAnALBIsMissing(t *testing.T) {
	f := newFakeCluster(t, fakeIngressKubectl)
	f.publish("build", "prometheus-federation", buildALB)
	f.publish("cell-0", "prometheus-federation", cell0ALB)

	for _, what := range []string{"loki", "metrics"} {
		out, stderr, err := f.run("monitoring-addresses.sh", what)
		if err != nil {
			t.Fatalf("monitoring-addresses.sh %s: %v\n%s", what, err, stderr)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("monitoring-addresses.sh %s printed %q with an ALB missing, want nothing", what, out)
		}
	}
}

func TestMonitoringAddressesWaitsForTheALB(t *testing.T) {
	f := newFakeCluster(t, fakeIngressKubectl)
	f.publish("build", "loki-internal", lokiALB)
	if err := os.WriteFile(filepath.Join(f.state, "build", "loki-internal.after"), []byte("3"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, stderr, err := f.run("monitoring-addresses.sh", "loki", "--wait", "30")

	if err != nil {
		t.Fatalf("monitoring-addresses.sh loki --wait: %v\n%s", err, stderr)
	}
	if got := exported(t, out, "LOOPZERO_LOKI_ALB_DNS_NAME"); got != lokiALB {
		t.Errorf("LOOPZERO_LOKI_ALB_DNS_NAME = %q, want %q", got, lokiALB)
	}
}

func TestMonitoringAddressesWaitFailsNamingTheIngress(t *testing.T) {
	f := newFakeCluster(t, fakeIngressKubectl)
	f.publish("build", "prometheus-federation", buildALB)
	f.publish("cell-0", "prometheus-federation", cell0ALB)

	out, stderr, err := f.run("monitoring-addresses.sh", "metrics", "--wait", "0")

	if err == nil {
		t.Fatalf("monitoring-addresses.sh metrics --wait succeeded with cell-1's ALB missing; printed %q", out)
	}
	if !strings.Contains(stderr, "monitoring/prometheus-federation on cell-1") {
		t.Errorf("stderr = %q, want it to name Ingress monitoring/prometheus-federation on cell-1", stderr)
	}
}

// fakePodsKubectl answers `get pods` with $FAKE_STATE/pods.json (or fails when
// $FAKE_STATE/pods.fail exists) and records every other call in $FAKE_STATE/calls.
const fakePodsKubectl = `#!/usr/bin/env bash
set -euo pipefail
case " $* " in
  *" get pods "*)
    [ -f "$FAKE_STATE/pods.fail" ] && { echo "Unable to connect to the server" >&2; exit 1; }
    cat "$FAKE_STATE/pods.json" ;;
  *) echo "$*" >>"$FAKE_STATE/calls" ;;
esac
`

const podsWithOneUnwiredController = `{"items": [
  {"metadata": {"namespace": "kube-system", "name": "aws-load-balancer-controller-9cdfcfd7-pbcxt",
     "labels": {"pod-template-hash": "9cdfcfd7"},
     "ownerReferences": [{"kind": "ReplicaSet", "name": "aws-load-balancer-controller-9cdfcfd7"}]},
   "spec": {"containers": [{"name": "controller", "env": [
     {"name": "AWS_REGION", "value": "us-east-1"},
     {"name": "AWS_ROLE_ARN", "value": "arn:aws:iam::000000000000:role/loopzero-staging-cell-0-lb-controller"},
     {"name": "AWS_WEB_IDENTITY_TOKEN_FILE", "value": "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"}]}]}},
  {"metadata": {"namespace": "external-secrets", "name": "external-secrets-5d9f7c-abcde",
     "labels": {"pod-template-hash": "5d9f7c"},
     "ownerReferences": [{"kind": "ReplicaSet", "name": "external-secrets-5d9f7c"}]},
   "spec": {"containers": [{"name": "external-secrets", "env": [
     {"name": "AWS_ROLE_ARN", "value": "arn:aws:iam::000000000000:role/loopzero-staging-cell-0-external-secrets"},
     {"name": "AWS_ENDPOINT_URL", "value": "https://172.29.0.1:9597"}]}]}},
  {"metadata": {"namespace": "monitoring", "name": "loki-0",
     "ownerReferences": [{"kind": "StatefulSet", "name": "loki"}]},
   "spec": {"containers": [{"name": "loki", "env": [
     {"name": "AWS_ROLE_ARN", "value": "arn:aws:iam::000000000000:role/loopzero-staging-build-loki"}]}]}},
  {"metadata": {"namespace": "monitoring", "name": "promtail-cwm8q",
     "ownerReferences": [{"kind": "DaemonSet", "name": "promtail"}]},
   "spec": {"containers": [{"name": "promtail"}]}}
]}`

func TestRestartUninjectedPodsRestartsOnlyTheUnwiredDeployment(t *testing.T) {
	f := newFakeCluster(t, fakePodsKubectl)
	if err := os.WriteFile(filepath.Join(f.state, "pods.json"), []byte(podsWithOneUnwiredController), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := f.run("restart-uninjected-pods.sh", "cell-0")

	if err != nil {
		t.Fatalf("restart-uninjected-pods.sh: %v\n%s", err, stderr)
	}
	calls, _ := os.ReadFile(filepath.Join(f.state, "calls"))
	want := "--context cell-0 -n kube-system rollout restart deployment/aws-load-balancer-controller\n" +
		"--context cell-0 -n kube-system rollout status deployment/aws-load-balancer-controller --timeout=5m\n"
	if string(calls) != want {
		t.Errorf("kubectl calls:\n%s\nwant:\n%s", calls, want)
	}
}

// A cluster that cannot be listed fails the run instead of passing as one with nothing
// to restart.
func TestRestartUninjectedPodsFailsWhenThePodsCannotBeListed(t *testing.T) {
	f := newFakeCluster(t, fakePodsKubectl)
	if err := os.WriteFile(filepath.Join(f.state, "pods.fail"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.run("restart-uninjected-pods.sh", "build"); err == nil {
		t.Fatal("restart-uninjected-pods.sh succeeded although kubectl could not list the pods")
	}
}

// The platform task runs both scripts, and syncs the core Promtail and Prometheus again
// once the remote clusters publish their ALBs.
func TestStagingPlatformSyncsCoreMonitoringAfterTheRemoteClusters(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	order := []string{
		"bash infra/staging/restart-uninjected-pods.sh core build cell-0 cell-1",
		"helmfile --selector 'name!=promtail' -f infra/helmfile/core.helmfile.yaml.gotmpl",
		"infra/helmfile/build.helmfile.yaml.gotmpl",
		"monitoring-addresses.sh loki --wait",
		"infra/helmfile/cell.helmfile.yaml.gotmpl",
		"monitoring-addresses.sh metrics --wait",
		"helmfile --selector name=promtail --selector name=prometheus -f infra/helmfile/core.helmfile.yaml.gotmpl",
	}
	at := 0
	for _, step := range order {
		i := strings.Index(text[at:], step)
		if i < 0 {
			t.Fatalf("taskfiles/staging.yml: %q is missing or out of order (after offset %d)", step, at)
		}
		at += i + len(step)
	}
}
