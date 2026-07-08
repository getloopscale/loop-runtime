// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
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
// k3d-loopzero-vault cluster (Jeito A per cleanroom-up-live-test-mandate.md).
//
// Cross-cluster discovery:
//  1. Vault Service `openbao.vault-system` in the vault cluster → https NodePort
//  2. `docker inspect` on `k3d-loopzero-vault-serverlb` for the container IP
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
	vaultContextName        = "k3d-loopzero-vault"
	vaultNamespace          = "vault-system"
	openbaoServiceName      = "openbao"
	openbaoHTTPSPortName    = "https"
	vaultServerLBContainer  = "k3d-loopzero-vault-serverlb"
	openBaoVaultAddrEnvVar  = "OPENBAO_VAULT_ADDR"
	loopzeroAPINamespaceInMgmt  = "loopzero-system"
	loopzeroAPIDeploymentInMgmt = "loopzero-api"
)

func (s *VaultCrossClusterAddrSeed) Run(d Deps) Result {
	// Vault cluster is a separate context (`k3d-loopzero-vault`).
	vaultCli, err := d.ClientFor(vaultContextName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("resolve vault context %s: %v", vaultContextName, err)}
	}

	// Step 1 — pull https NodePort from openbao Service.
	np, err := vaultServiceHTTPSNodePort(d.Ctx, vaultCli)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("openbao NodePort: %v (run task k3d:vault:bootstrap first)", err)}
	}

	// Step 2 — docker inspect the k3d-loopzero-vault serverlb IP.
	ip, err := dockerInspectContainerIP(vaultServerLBContainer)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("docker inspect %s: %v", vaultServerLBContainer, err)}
	}
	vaultAddr := fmt.Sprintf("https://%s:%d", ip, np)

	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would set %s=%s on %s/%s", openBaoVaultAddrEnvVar, vaultAddr, loopzeroAPINamespaceInMgmt, loopzeroAPIDeploymentInMgmt)}
	}

	// EC-2 absorption: wait for any in-progress rollout to settle before
	// triggering the env change (avoids double-rollout race).
	_ = waitForRolloutSettled(d.Ctx, d.CtrlClient, loopzeroAPINamespaceInMgmt, loopzeroAPIDeploymentInMgmt, 120*time.Second)

	// Step 3 — set env on loopzero-api Deployment.
	if err := setDeploymentContainerEnv(d.Ctx, d.CtrlClient,
		loopzeroAPINamespaceInMgmt, loopzeroAPIDeploymentInMgmt, openBaoVaultAddrEnvVar, vaultAddr); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("set env on loopzero-api: %v", err)}
	}
	// Rollout settle post-change.
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, loopzeroAPINamespaceInMgmt, loopzeroAPIDeploymentInMgmt, 60*time.Second); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("loopzero-api rollout: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("wired %s=%s on %s/%s", openBaoVaultAddrEnvVar, vaultAddr, loopzeroAPINamespaceInMgmt, loopzeroAPIDeploymentInMgmt)}
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
	ip, err := dockerInspectContainerIP(vaultServerLBContainer)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	wantAddr := fmt.Sprintf("https://%s:%d", ip, np)
	got, err := getDeploymentContainerEnv(d.Ctx, d.CtrlClient, loopzeroAPINamespaceInMgmt, loopzeroAPIDeploymentInMgmt, openBaoVaultAddrEnvVar)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if got != wantAddr {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s = %q, want %q", openBaoVaultAddrEnvVar, got, wantAddr)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s wired canonical %s", openBaoVaultAddrEnvVar, wantAddr)}
}

// vaultServiceHTTPSNodePort returns the openbao Service's https NodePort in the
// vault-system namespace of the vault cluster.
func vaultServiceHTTPSNodePort(ctx context.Context, k KubeClient) (int32, error) {
	// KubeClient has no Get-Service today; use the underlying ctrl.Client via a
	// tiny helper. We shim through the interface by asking for the raw client.
	rawCli, ok := k.(*kubeClient)
	if !ok {
		return 0, fmt.Errorf("KubeClient impl does not expose ctrl.Client")
	}
	var svc corev1.Service
	if err := rawCli.c.Get(ctx, ctrlclient.ObjectKey{Namespace: vaultNamespace, Name: openbaoServiceName}, &svc); err != nil {
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

// dockerInspectContainerIP returns the first non-empty IPAddress from
// `docker inspect --format '{{...}}' <container>`. D1 exemption applies:
// this is READ-ONLY container introspection, not shell-out for mutation.
var dockerInspectFn = defaultDockerInspect

func dockerInspectContainerIP(container string) (string, error) {
	return dockerInspectFn(container)
}

func defaultDockerInspect(container string) (string, error) {
	// docker inspect exits non-zero if the container is missing.
	out, err := exec.Command("docker", "inspect",
		container, "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}").Output()
	if err != nil {
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

// waitForRolloutSettled polls the Deployment until readyReplicas == desired
// OR timeout. Errors are non-fatal on timeout: the seed can proceed and Kubernetes
// will converge.
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
			return d.Status.ReadyReplicas == want && d.Status.UpdatedReplicas == want, nil
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
