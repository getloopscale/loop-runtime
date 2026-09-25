// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// VaultCrossClusterAddrSeed is the Go port of
// infra/scripts/local/seed-vault-cross-cluster-addr-canonical.sh.
//
// Sets `OPENBAO_VAULT_ADDR=https://<serverlb-ip>:<nodeport>` on the mgmt/loopzero-system/
// loopzero-api Deployment env so it can reach OpenBao that runs in the SEPARATE
// k3d-loopzero-secrets cluster.
//
// Cross-cluster discovery:
//  1. Vault Service `openbao.secrets-system` in the vault cluster → https NodePort
//  2. `docker inspect` on `k3d-loopzero-secrets-serverlb` for the container IP
//
// D1 exemption: `docker inspect` is a READ-ONLY container introspection —
// docker is the only source-of-truth for the k3d loadbalancer's internal IP.
// Not shell-out for mutation.
type VaultCrossClusterAddrSeed struct{}

func NewVaultCrossClusterAddrSeed() *VaultCrossClusterAddrSeed {
	return &VaultCrossClusterAddrSeed{}
}
func (s *VaultCrossClusterAddrSeed) Name() string { return "vault-cross-cluster-addr" }

const (
	vaultContextName        = "k3d-loopzero-secrets"
	vaultNamespace          = "secrets-system"
	openbaoServiceName      = "openbao"
	openbaoHTTPSPortName    = "https"
	vaultServerLBContainer  = "k3d-loopzero-secrets-serverlb"
	openBaoVaultAddrEnvVar  = "OPENBAO_VAULT_ADDR"
	loopzeroAPINamespaceInCore  = "loopzero-system"
	loopzeroAPIDeploymentInCore = "loopzero-api"
)

func (s *VaultCrossClusterAddrSeed) Run(d Deps) Result {
	// Vault cluster is a separate context (`k3d-loopzero-secrets`).
	vaultCli, err := d.ClientFor(vaultContextName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("resolve vault context %s: %v", vaultContextName, err)}
	}

	// Step 1 — pull https NodePort from openbao Service.
	np, err := vaultServiceHTTPSNodePort(d.Ctx, vaultCli)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("openbao NodePort: %v (run task k3d:secrets:bootstrap first)", err)}
	}

	// Step 2 — docker inspect the k3d-loopzero-secrets serverlb IP.
	ip, err := d.Host.dockerContainerIP(vaultServerLBContainer)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("docker inspect %s: %v", vaultServerLBContainer, err)}
	}
	vaultAddr := fmt.Sprintf("https://%s:%d", ip, np)

	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would set %s=%s on %s/%s", openBaoVaultAddrEnvVar, vaultAddr, loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore)}
	}

	// EC-2 absorption: wait for any in-progress rollout to settle before
	// triggering the env change (avoids double-rollout race).
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore, d.settleTimeout(loopzeroAPISettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("loopzero-api rollout in progress did not settle before the env change: %v", err)}
	}

	// Step 3 — set env on loopzero-api Deployment.
	if err := setDeploymentContainerEnv(d.Ctx, d.CtrlClient,
		loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore, openBaoVaultAddrEnvVar, vaultAddr); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("set env on loopzero-api: %v", err)}
	}
	// Rollout settle post-change.
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore, d.settleTimeout(loopzeroAPIResettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("loopzero-api rollout: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("wired %s=%s on %s/%s", openBaoVaultAddrEnvVar, vaultAddr, loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore)}
}

func (s *VaultCrossClusterAddrSeed) Verify(d Deps) Result {
	vaultCli, err := d.ClientFor(vaultContextName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("resolve vault context: %v", err)}
	}
	np, err := vaultServiceHTTPSNodePort(d.Ctx, vaultCli)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	ip, err := d.Host.dockerContainerIP(vaultServerLBContainer)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	wantAddr := fmt.Sprintf("https://%s:%d", ip, np)
	got, err := getDeploymentContainerEnv(d.Ctx, d.CtrlClient, loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore, openBaoVaultAddrEnvVar)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if got != wantAddr {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s = %q, want %q", openBaoVaultAddrEnvVar, got, wantAddr)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s wired canonical %s", openBaoVaultAddrEnvVar, wantAddr)}
}

// vaultServiceHTTPSNodePort returns the openbao Service's https NodePort in the
// secrets-system namespace of the vault cluster.
func vaultServiceHTTPSNodePort(ctx context.Context, services Services) (int32, error) {
	svc, err := services.GetService(ctx, vaultNamespace, openbaoServiceName)
	if err != nil {
		return 0, err
	}
	for _, p := range svc.Spec.Ports {
		if p.Name == openbaoHTTPSPortName {
			if p.NodePort == 0 {
				return 0, fmt.Errorf("openbao Service https port has no NodePort (svc.type=%q)", svc.Spec.Type)
			}
			return p.NodePort, nil
		}
	}
	return 0, fmt.Errorf("openbao Service has no port named %q", openbaoHTTPSPortName)
}

// errContainerAbsent marks a docker inspect of a container that does not
// exist — the one docker failure a caller may treat as "skip".
var errContainerAbsent = errors.New("container does not exist")

// defaultDockerInspect returns the first non-empty IPAddress from
// `docker inspect --format '{{...}}' <container>`. D1 exemption applies:
// this is READ-ONLY container introspection, not shell-out for mutation.
func defaultDockerInspect(container string) (string, error) {
	// docker inspect exits non-zero if the container is missing.
	out, err := exec.Command("docker", "inspect",
		container, "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && strings.Contains(string(ee.Stderr), "No such object") {
			return "", fmt.Errorf("%s: %w", container, errContainerAbsent)
		}
		return "", fmt.Errorf("docker inspect %s: %w", container, err)
	}
	for _, ip := range strings.Fields(string(out)) {
		if ip != "" && ip != "<no value>" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no IPAddress found for %s (State.Running?)", container)
}

// setDeploymentContainerEnv upserts an env var on the first container of a
// Deployment (`kubectl set env` equivalent). Bumps the restartedAt annotation
// as a side effect via RolloutRestart-shaped pod-template mutation.
func setDeploymentContainerEnv(ctx context.Context, cli ctrlclient.Client,
	namespace, name, envName, envVal string,
) error {
	var d appsv1.Deployment
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, &d); err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	if len(d.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("deployment %s/%s has no containers", namespace, name)
	}
	c := &d.Spec.Template.Spec.Containers[0]
	found := false
	for i, e := range c.Env {
		if e.Name == envName {
			c.Env[i].Value = envVal
			c.Env[i].ValueFrom = nil
			found = true
			break
		}
	}
	if !found {
		c.Env = append(c.Env, corev1.EnvVar{Name: envName, Value: envVal})
	}
	// Bumping restartedAt ensures the pod restarts if only env changed
	// (`kubectl set env` behavior). Idempotent.
	if d.Spec.Template.ObjectMeta.Annotations == nil {
		d.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	d.Spec.Template.ObjectMeta.Annotations["kubectl.kubernetes.io/restartedAt"] =
		time.Now().UTC().Format(time.RFC3339)
	return cli.Update(ctx, &d)
}

// getDeploymentContainerEnv returns the value of an env var on the first
// container of a Deployment. Returns "" when the var is absent.
func getDeploymentContainerEnv(ctx context.Context, cli ctrlclient.Client,
	namespace, name, envName string,
) (string, error) {
	var d appsv1.Deployment
	if err := cli.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, &d); err != nil {
		return "", fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}
	if len(d.Spec.Template.Spec.Containers) == 0 {
		return "", fmt.Errorf("deployment %s/%s has no containers", namespace, name)
	}
	for _, e := range d.Spec.Template.Spec.Containers[0].Env {
		if e.Name == envName {
			return e.Value, nil
		}
	}
	return "", nil
}

// The rollout settle deadlines, named. They DIFFER on purpose, and each
// difference has a reason that only survives if written down:
//
//   - loopzeroAPISettle — loopzero-api starts heavy (migrations plus connections to
//     several clusters), so it is the longest.
//   - loopzeroAPIResettle — the SAME seed's second wait, after the env change has
//     been applied. It is half because the Deployment just settled on the
//     first; if it takes longer than that, waiting will not fix it.
//   - kratosSettle — a Deployment plus a StatefulSet, lighter than loopzero-api.
//   - corednsSettle — the shortest: a light, stateless system Deployment that
//     restarts fast.
//
// They used to be seven literals across six files, and an eighth caller would
// have written its own — the shape spreads by being read. None is the
// EFFECTIVE deadline: Deps.settleTimeout resolves that, so a hermetic
// environment can inject its own without changing the production value.
const (
	loopzeroAPISettle   = 120 * time.Second
	loopzeroAPIResettle = 60 * time.Second
	kratosSettle    = 90 * time.Second
	corednsSettle   = 60 * time.Second
)

// waitForRolloutSettled polls the Deployment until the controller has observed
// the latest spec and ready/updated replicas equal the desired count, or the
// timeout elapses. A timeout is returned as an error: a seed that restarted a
// workload has not done its job until the workload is ready again.
func waitForRolloutSettled(ctx context.Context, cli ctrlclient.Client,
	namespace, name string, timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true,
		func(ctx context.Context) (bool, error) {
			var d appsv1.Deployment
			if err := cli.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, &d); err != nil {
				return false, err
			}
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			return d.Status.ObservedGeneration >= d.Generation &&
				d.Status.ReadyReplicas == want && d.Status.UpdatedReplicas == want, nil
		})
}

// makeVaultService is a small helper used by tests to seed the openbao Service.
func makeVaultService(np int32) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: vaultNamespace, Name: openbaoServiceName},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{Name: openbaoHTTPSPortName, Port: 8200, NodePort: np},
			},
		},
	}
}
