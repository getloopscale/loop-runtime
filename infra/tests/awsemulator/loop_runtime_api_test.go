// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

//go:build awsemulator

// Package awsemulator runs loop-runtime-api on the AWS emulator (github.com/paulohenriquevn/aws-emulator)
// the way it runs on AWS: in a pod of an EKS cluster, on an RDS for PostgreSQL instance, behind an
// Application Load Balancer the AWS Load Balancer Controller makes from an Ingress.
//
// The emulator's own harness (fork/kubernetes/run.sh, run with KEEP_CLUSTER=1) creates the EKS
// cluster as a kind cluster whose nodes are EC2 instances, installs the AWS Load Balancer
// Controller, and writes environment.sh. Source that file, load the loop-runtime-api image into the kind
// cluster, and run:
//
//	LOOP_RUNTIME_API_IMAGE=localhost/loop-runtime-api:e2e go test -tags=awsemulator -v -timeout 30m ./awsemulator/
//
// Argo Workflows, ArgoCD, Gitea, MinIO and the other cells are not installed: the test covers the
// database and the load balancer path only.
package awsemulator

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"
)

const (
	namespace = "loop-runtime-system"
	database  = "loop_runtime"
	// The master user runs the migrations, and migration 000023 requires it to be named loop-runtime.
	masterUser = "loop-runtime"
	psqlImage  = "postgres:16.11-alpine"
)

// environment is what the emulator's harness exports in environment.sh, plus the image.
type environment struct {
	endpoint, ca, cluster, vpc, image string
	subnets, nodeGroups               []string
}

func readEnvironment(t *testing.T) environment {
	t.Helper()
	get := func(name string) string {
		value := os.Getenv(name)
		if value == "" {
			t.Fatalf("%s is not set: source the emulator harness's environment.sh and set LOOP_RUNTIME_API_IMAGE", name)
		}
		return value
	}
	return environment{
		endpoint:   get("AWS_ENDPOINT_URL"),
		ca:         get("EMULATOR_CA"),
		cluster:    get("EKS_CLUSTER"),
		vpc:        get("VPC_ID"),
		image:      get("LOOP_RUNTIME_API_IMAGE"),
		subnets:    strings.Fields(get("SUBNET_IDS")),
		nodeGroups: strings.Fields(get("NODE_SECURITY_GROUP_IDS")),
	}
}

func TestLoopRuntimeAPIAnswersThroughTheLoadBalancerWithItsRDSDatabaseConnected(t *testing.T) {
	// Arrange: an RDS instance the nodes may reach, its roles and schema, and loop-runtime-api.
	env := readEnvironment(t)
	t.Cleanup(func() {
		if t.Failed() {
			diagnose(t)
		}
	})
	passwords := map[string]string{"master": secret(t), "api": secret(t), "worker": secret(t), "cloud": secret(t)}
	host, port := createDatabase(t, env, passwords["master"])
	dsn := func(user, password string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=require", user, password, host, port, database)
	}
	run(t, nil, "kubectl", "create", "namespace", namespace)
	prepareDatabase(t, env, host, port, dsn(masterUser, passwords["master"]), passwords)
	installCell(t, env)
	installLoopRuntimeAPI(t, env, dsn("loop_runtime_api", passwords["api"]), dsn("loop_runtime_worker", passwords["worker"]))

	// Act: ask loop-runtime-api, through the ALB the Ingress gets, for its health.
	hostname := loadBalancerFor(t)
	client := emulatorClient(t, env.ca)
	status, _ := eventually(t, 2*time.Minute, func() (int, []byte) {
		return getThroughALB(t, client, env.endpoint, hostname, "/api/v1/health")
	}, http.StatusOK)
	_, deep := getThroughALB(t, client, env.endpoint, hostname, "/api/v1/health/deep")

	// Assert: loop-runtime-api answers, and its deep check reports the RDS database and the cell.
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/health through the ALB answered %d, want 200", status)
	}
	var report map[string]string
	if err := json.Unmarshal(deep, &report); err != nil {
		t.Fatalf("GET /api/v1/health/deep answered %q, not a JSON object: %v", deep, err)
	}
	if report["database"] != "connected" {
		t.Errorf("deep health reports the database %q, want connected (body %s)", report["database"], deep)
	}
	if report["cells"] != "1 discovered" {
		t.Errorf("deep health reports cells %q, want 1 discovered (body %s)", report["cells"], deep)
	}
}

// createDatabase creates the RDS instance behind a security group that admits the node
// instances' groups on 5432, and returns its endpoint.
func createDatabase(t *testing.T, env environment, masterPassword string) (host, port string) {
	t.Helper()
	run(t, nil, "aws", append([]string{"rds", "create-db-subnet-group", "--db-subnet-group-name", "loop-runtime",
		"--db-subnet-group-description", "loop-runtime-api database", "--subnet-ids"}, env.subnets...)...)
	group := run(t, nil, "aws", "ec2", "create-security-group", "--group-name", "loop-runtime-database",
		"--description", "loop-runtime-api database, admits the cluster", "--vpc-id", env.vpc,
		"--query", "GroupId", "--output", "text")
	for _, nodeGroup := range env.nodeGroups {
		run(t, nil, "aws", "ec2", "authorize-security-group-ingress", "--group-id", group,
			"--protocol", "tcp", "--port", "5432", "--source-group", nodeGroup)
	}
	run(t, nil, "aws", "rds", "create-db-instance", "--db-instance-identifier", "loop-runtime",
		"--engine", "postgres", "--engine-version", "16", "--db-instance-class", "db.t4g.micro",
		"--allocated-storage", "20", "--db-name", database, "--master-username", masterUser,
		"--master-user-password", masterPassword, "--port", "5432",
		"--db-subnet-group-name", "loop-runtime", "--vpc-security-group-ids", group)
	run(t, nil, "aws", "rds", "wait", "db-instance-available", "--db-instance-identifier", "loop-runtime")
	endpoint := strings.Fields(run(t, nil, "aws", "rds", "describe-db-instances", "--db-instance-identifier", "loop-runtime",
		"--query", "DBInstances[0].[Endpoint.Address,Endpoint.Port]", "--output", "text"))
	if len(endpoint) != 2 {
		t.Fatalf("RDS instance loop-runtime has endpoint %q, want an address and a port", endpoint)
	}
	t.Logf("RDS instance loop-runtime at %s:%s, security group %s admits %v", endpoint[0], endpoint[1], group, env.nodeGroups)
	return endpoint[0], endpoint[1]
}

// prepareDatabase creates the roles with the production script and runs the migrations, each
// from a pod, so both connections go through the instance's security group.
func prepareDatabase(t *testing.T, env environment, host, port, masterDSN string, passwords map[string]string) {
	t.Helper()
	apply(t, "kubectl", "-n", namespace, "create", "secret", "generic", "database-admin",
		"--from-literal=PGHOST="+host, "--from-literal=PGPORT="+port,
		"--from-literal=PGDATABASE="+database, "--from-literal=PGUSER="+masterUser,
		"--from-literal=PGPASSWORD="+passwords["master"], "--from-literal=PGSSLMODE=require",
		"--from-literal=API_PASSWORD="+passwords["api"], "--from-literal=WORKER_PASSWORD="+passwords["worker"],
		"--from-literal=CLOUD_PASSWORD="+passwords["cloud"],
		"--from-literal=MIGRATE_DSN="+masterDSN+"&x-migrations-table=schema_migrations")
	apply(t, "kubectl", "-n", namespace, "create", "configmap", "database-roles",
		"--from-file=provision-db-roles.sql="+filepath.Join(repoRoot(t), "infra/scripts/production/provision-db-roles.sql"))
	data := map[string]string{"PsqlImage": psqlImage, "Image": env.image}
	runJob(t, "database-roles", render(t, "database-roles-job.yaml", data))
	runJob(t, "database-migrations", render(t, "database-migrations-job.yaml", data))
}

// installCell installs the ClusterCredential CRD and one connected cell, and the build cluster's
// kubeconfig: loop-runtime-api does not start without either.
func installCell(t *testing.T, env environment) {
	t.Helper()
	crdChart := filepath.Join(repoRoot(t), "charts/loop-runtime-crds")
	run(t, nil, "helm", "dependency", "build", crdChart)
	crds := run(t, nil, "helm", "template", "loop-runtime-crds", crdChart)
	run(t, []byte(crds), "kubectl", "apply", "--server-side", "-f", "-")
	run(t, nil, "kubectl", "wait", "crd", "clustercredentials.runtime.getloopscale.com", "--for=condition=Established", "--timeout=60s")
	run(t, render(t, "cluster-credential.yaml", map[string]string{"ClusterName": env.cluster, "Namespace": namespace}),
		"kubectl", "-n", namespace, "apply", "-f", "-")
	run(t, nil, "kubectl", "-n", namespace, "patch", "clustercredential", "cell-0", "--subresource=status",
		"--type=merge", "-p", `{"status":{"connected":true}}`)
	run(t, render(t, "build-kubeconfig.yaml", nil), "kubectl", "-n", namespace, "apply", "-f", "-")
}

func installLoopRuntimeAPI(t *testing.T, env environment, apiDSN, workerDSN string) {
	t.Helper()
	apply(t, "kubectl", "-n", namespace, "create", "secret", "generic", "loop-runtime-api-env",
		"--from-literal=DATABASE_URL="+apiDSN, "--from-literal=LOOP_RUNTIME_WORKER_DATABASE_URL="+workerDSN)
	registry, rest, found := strings.Cut(env.image, "/")
	repository, tag, tagged := strings.Cut(rest, ":")
	if !found || !tagged {
		t.Fatalf("LOOP_RUNTIME_API_IMAGE %q is not <registry>/<repository>:<tag>", env.image)
	}
	chart := filepath.Join(repoRoot(t), "charts/loop-runtime-api")
	run(t, nil, "helm", "dependency", "build", chart)
	run(t, nil, "helm", "upgrade", "--install", "loop-runtime-api", chart, "-n", namespace,
		"-f", filepath.Join(testdata(t), "values.yaml"),
		"--set", "global.registry="+registry, "--set", "image.repository="+repository, "--set", "image.tag="+tag,
		"--wait", "--timeout", "300s")
	run(t, render(t, "ingress.yaml", nil), "kubectl", "-n", namespace, "apply", "-f", "-")
}

// loadBalancerFor waits for the Ingress to report the ALB's DNS name.
func loadBalancerFor(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("kubectl", "-n", namespace, "get", "ingress", "loop-runtime-api",
			"-o", "jsonpath={.status.loadBalancer.ingress[0].hostname}").Output()
		if hostname := strings.TrimSpace(string(out)); hostname != "" {
			t.Logf("load balancer %s", hostname)
			return hostname
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("the loop-runtime-api Ingress got no load balancer in 3m")
	return ""
}

func emulatorClient(t *testing.T, caPath string) *http.Client {
	t.Helper()
	pem, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("reading the emulator CA %s: %v", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatalf("the emulator CA %s holds no certificate", caPath)
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
}

// getThroughALB sends a request to the emulator's endpoint with the ALB's name as Host, which
// the emulator serves as that load balancer. A transport error reads as status 0.
func getThroughALB(t *testing.T, client *http.Client, endpoint, hostname, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint+path, nil)
	if err != nil {
		t.Fatalf("building GET %s: %v", path, err)
	}
	req.Host = hostname
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("GET http://%s%s: %v", hostname, path, err)
		return 0, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	t.Logf("GET http://%s%s: %d %s", hostname, path, resp.StatusCode, bytes.TrimSpace(body))
	return resp.StatusCode, body
}

func eventually(t *testing.T, within time.Duration, try func() (int, []byte), want int) (int, []byte) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		status, body := try()
		if status == want || time.Now().After(deadline) {
			return status, body
		}
		time.Sleep(3 * time.Second)
	}
}

// runJob applies a Job and waits for it to complete, failing with its log when it does not.
func runJob(t *testing.T, name string, manifest []byte) {
	t.Helper()
	run(t, manifest, "kubectl", "-n", namespace, "apply", "-f", "-")
	if err := exec.Command("kubectl", "-n", namespace, "wait", "job", name,
		"--for=condition=Complete", "--timeout=300s").Run(); err != nil {
		logs, _ := exec.Command("kubectl", "-n", namespace, "logs", "job/"+name, "--tail=50").CombinedOutput()
		t.Fatalf("job %s did not complete: %v\n%s", name, err, logs)
	}
	t.Logf("job %s completed", name)
}

// apply runs a `kubectl create ... ` as a dry run and applies what it prints, so a rerun on a
// kept cluster updates the object.
func apply(t *testing.T, args ...string) {
	t.Helper()
	manifest := run(t, nil, args[0], append(args[1:], "--dry-run=client", "-o", "yaml")...)
	run(t, []byte(manifest), "kubectl", "apply", "-f", "-")
}

// run executes a command and returns its trimmed stdout, failing the test with stderr when it
// exits non-zero. Arguments are not logged: some carry passwords.
func run(t *testing.T, stdin []byte, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if name == "kubectl" && len(args) > 0 && args[0] == "create" && strings.Contains(stderr.String(), "AlreadyExists") {
			return ""
		}
		subcommand := strings.Join(args[:min(2, len(args))], " ")
		t.Fatalf("%s %s: %v\n%s", name, subcommand, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func render(t *testing.T, name string, data any) []byte {
	t.Helper()
	tmpl, err := template.ParseFiles(filepath.Join(testdata(t), name))
	if err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		t.Fatalf("rendering %s: %v", name, err)
	}
	return out.Bytes()
}

func diagnose(t *testing.T) {
	for _, args := range [][]string{
		{"-n", namespace, "get", "pods,jobs,svc,ingress", "-o", "wide"},
		{"-n", namespace, "describe", "pods"},
		{"-n", namespace, "logs", "-l", "app.kubernetes.io/name=loop-runtime-api", "--all-containers", "--tail=200"},
		{"-n", "kube-system", "logs", "-l", "app.kubernetes.io/name=aws-load-balancer-controller", "--tail=100"},
	} {
		out, _ := exec.Command("kubectl", args...).CombinedOutput()
		t.Logf("kubectl %s:\n%s", strings.Join(args, " "), out)
	}
}

func secret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating a password: %v", err)
	}
	return hex.EncodeToString(b)
}

func testdata(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("locating testdata: %v", err)
	}
	return dir
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}
	return root
}
