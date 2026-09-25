// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BrokerSeed is the Go port of infra/scripts/local/seed-broker-canonical.sh.
//
// 4-layer canonical broker chain:
//
//  1. Generate/reuse canonical token; apply mgmt loopzero-system/loopzero-broker-token
//     Secret with keys {token, LOOPZERO_BROKER_TOKEN}.
//  2. Mirror to workload cluster's external-secrets ns with the
//     `external-secrets.io/type=webhook` canonical label.
//  3. Resolve k3d-loopzero-core-server-0 container IP → install/upgrade
//     loopzero-external-secrets chart in workload cluster (broker.apiURL points
//     at mgmt server-0:30001 for cross-cluster).
//  4. Rollout restart mgmt loopzero-api so envFrom picks up LOOPZERO_BROKER_TOKEN.
//
// D1 exemptions:
//   - `docker network inspect` = READ-ONLY container discovery.
//   - `helm upgrade --install` = canonical chart lifecycle tool (declarative;
//     the chart itself lives in-tree).
type BrokerSeed struct{}

func NewBrokerSeed() *BrokerSeed   { return &BrokerSeed{} }
func (s *BrokerSeed) Name() string { return "broker" }

const (
	brokerContextWorkload   = "k3d-loopzero-cell-0"
	brokerESONamespace      = "external-secrets"
	brokerSecretName        = "loopzero-broker-token"
	brokerTokenKey          = "token"
	brokerCanonicalEnvKey   = "LOOPZERO_BROKER_TOKEN"
	brokerChartRelativePath = "infra/helm/loopzero-external-secrets"
	brokerHelmReleaseName   = "loopzero-broker"
	brokerAPIPort           = 30001
	brokerCellID            = "cell-0"
	brokerEnvName           = "dev-public"
	brokerMgmtContainer     = "k3d-loopzero-core-server-0"
	brokerNetworkName       = "k3d-loopzero"
)

func (s *BrokerSeed) Run(d Deps) Result {
	// Layer 1: get-or-generate canonical token.
	token, reused, err := brokerAcquireToken(d)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 token: %v", err)}
	}
	tokenB := []byte(token)

	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would %s token + apply Secrets + helm upgrade + restart loopzero-api", tokenAction(reused))}
	}

	// Layer 1 apply — mgmt Secret with both `token` (legacy) + canonical env key.
	mgmtSec := brokerMgmtSecret(tokenB)
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx, mgmtSec); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 apply mgmt Secret: %v", err)}
	}

	// Layer 2: workload cluster mirror with canonical webhook label.
	workloadCli, err := d.ClientFor(brokerContextWorkload)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 resolve workload: %v", err)}
	}
	if err := workloadCli.EnsureNamespace(d.Ctx, brokerESONamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 ensure ns: %v", err)}
	}
	workloadSec := brokerWorkloadSecret(tokenB)
	if _, err := workloadCli.CreateOrUpdateSecret(d.Ctx, workloadSec); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 apply workload Secret: %v", err)}
	}

	// Layer 3: resolve mgmt server-0 IP + helm upgrade the chart.
	mgmtIP, err := dockerNetworkContainerIP(brokerNetworkName, brokerMgmtContainer)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 mgmt IP: %v", err)}
	}
	apiURL := fmt.Sprintf("http://%s:%d", mgmtIP, brokerAPIPort)
	chartPath, err := brokerChartAbsPath()
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 chart path: %v", err)}
	}
	// Ensure loopzero-build ns exists on workload (chart post-render expects it).
	if err := workloadCli.EnsureNamespace(d.Ctx, "loopzero-build"); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 ensure ns loopzero-build: %v", err)}
	}

	if err := helmUpgradeBrokerChart(chartPath, apiURL); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 helm upgrade: %v", err)}
	}

	// Layer 4: rollout mgmt loopzero-api.
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, "loopzero-system", "loopzero-api"); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 4 rollout loopzero-api: %v", err)}
	}
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, "loopzero-system", "loopzero-api", d.settleTimeout(loopzeroAPISettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 4 loopzero-api did not settle: %v", err)}
	}

	return Result{Verdict: PASS, Reason: fmt.Sprintf("canonical broker chain seeded (%s token; apiURL=%s)", tokenAction(reused), apiURL)}
}

// Verify probes: (a) mgmt Secret has canonical env key non-empty,
// (b) workload Secret has canonical label + token, (c) CSS Ready optional.
func (s *BrokerSeed) Verify(d Deps) Result {
	sec, err := d.Kube.GetSecret(d.Ctx, "loopzero-system", brokerSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get mgmt Secret: %v", err)}
	}
	if len(sec.Data[brokerCanonicalEnvKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "mgmt Secret missing LOOPZERO_BROKER_TOKEN"}
	}
	workloadCli, err := d.ClientFor(brokerContextWorkload)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	wsec, err := workloadCli.GetSecret(d.Ctx, brokerESONamespace, brokerSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get workload Secret: %v", err)}
	}
	if wsec.Labels["external-secrets.io/type"] != "webhook" {
		return Result{Verdict: FAIL, Reason: "workload Secret missing canonical `external-secrets.io/type=webhook` label"}
	}
	if len(wsec.Data[brokerTokenKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "workload Secret token key empty"}
	}
	return Result{Verdict: PASS, Reason: "mgmt + workload broker Secrets canonical"}
}

// brokerAcquireToken returns the canonical token — reusing the existing one
// unless the Secret is absent or carries no token. Any other read error is
// returned: regenerating on it would silently rotate the broker token on a
// transient API failure.
func brokerAcquireToken(d Deps) (token string, reused bool, err error) {
	sec, err := d.Kube.GetSecret(d.Ctx, "loopzero-system", brokerSecretName)
	switch {
	case err == nil:
		if v := string(sec.Data[brokerTokenKey]); v != "" {
			return v, true, nil
		}
	case !apierrors.IsNotFound(err):
		return "", false, fmt.Errorf("read broker token: %w", err)
	}
	// Generate new (32 bytes hex = 64 chars — matches `openssl rand -hex 32`).
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false, err
	}
	return hex.EncodeToString(buf), false, nil
}

func tokenAction(reused bool) string {
	if reused {
		return "reused"
	}
	return "generated"
}

// brokerMgmtSecret builds the mgmt/loopzero-system/loopzero-broker-token Secret with
// both `token` (legacy alias) and `LOOPZERO_BROKER_TOKEN` (canonical envFrom key)
// keys. Labels record the seed as manager (managed-by renamed to Go form).
func brokerMgmtSecret(token []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "loopzero-system",
			Name:      brokerSecretName,
			Labels: map[string]string{
				"app.kubernetes.io/name":       brokerSecretName,
				"app.kubernetes.io/managed-by": "loopzero-ops-seed-broker",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			brokerTokenKey:        token,
			brokerCanonicalEnvKey: token,
		},
	}
}

// brokerWorkloadSecret builds the workload/external-secrets/loopzero-broker-token
// Secret mirror with the canonical webhook label.
func brokerWorkloadSecret(token []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: brokerESONamespace,
			Name:      brokerSecretName,
			Labels: map[string]string{
				"external-secrets.io/type":     "webhook",
				"app.kubernetes.io/name":       brokerSecretName,
				"app.kubernetes.io/managed-by": "loopzero-ops-seed-broker",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{brokerTokenKey: token},
	}
}

// dockerNetworkContainerIP returns the IPv4 address (no CIDR) of the given
// container inside the docker network. D1 exemption: read-only introspection.
var dockerNetworkFn = defaultDockerNetworkInspect

func dockerNetworkContainerIP(network, container string) (string, error) {
	return dockerNetworkFn(network, container)
}

func defaultDockerNetworkInspect(network, container string) (string, error) {
	out, err := exec.Command("docker", "network", "inspect", network).Output()
	if err != nil {
		return "", fmt.Errorf("docker network inspect %s: %w", network, err)
	}
	// Response is `[{...}]` (an array of network entries).
	var nets []struct {
		Containers map[string]struct {
			Name string `json:"Name"`
			IPv4 string `json:"IPv4Address"`
		} `json:"Containers"`
	}
	if err := json.Unmarshal(out, &nets); err != nil {
		return "", fmt.Errorf("parse docker network inspect: %w", err)
	}
	for _, n := range nets {
		for _, c := range n.Containers {
			if c.Name == container {
				ip, _, _ := strings.Cut(c.IPv4, "/")
				if ip != "" {
					return ip, nil
				}
			}
		}
	}
	return "", fmt.Errorf("container %s not found in network %s", container, network)
}

// dockerNetworkGateway returns the IPv4 gateway of a docker network — the address
// of the bridge itself.
//
// POR QUE ELE EXISTE (loopzero-workspace#23). O IP de um CONTAINER e' atribuido por
// DHCP e EMBARALHA a cada `docker restart` / `k3d cluster create`: qualquer config
// que o tenha gravado passa a apontar para outro cluster, EM SILENCIO — e o sintoma
// nunca acusa a causa (`loopzero logs` vazio le-se como "Loki fora do ar", nunca como
// "voce esta' falando com o cluster errado"). O gateway nao participa desse sorteio:
// e' declarado no IPAM da rede e vive enquanto a rede viver. Combinado com uma porta
// PUBLICADA — declarada na config versionada do cluster, nao sorteada — forma o unico
// endereco estavel que um processo fora do cluster tem para falar com ele. Foi o que
// tirou o item 1 do issue da classe.
//
// DERIVADO, nunca fixado em texto: `172.18.0.1` e' verdade para a rede `k3d-loopzero` de
// hoje; outra rede tem outra bridge. Ler do IPAM preserva a PROPRIEDADE (estabilidade)
// sem herdar o literal — e literal de IP e' exatamente o que este issue combate.
//
// D1 exemption: read-only introspection, mesmo caso do vizinho acima.
var dockerGatewayFn = defaultDockerNetworkGateway

func dockerNetworkGateway(network string) (string, error) { return dockerGatewayFn(network) }

func defaultDockerNetworkGateway(network string) (string, error) {
	out, err := exec.Command("docker", "network", "inspect", network).Output()
	if err != nil {
		return "", fmt.Errorf("docker network inspect %s: %w", network, err)
	}
	var nets []struct {
		IPAM struct {
			Config []struct {
				Gateway string `json:"Gateway"`
			} `json:"Config"`
		} `json:"IPAM"`
	}
	if err := json.Unmarshal(out, &nets); err != nil {
		return "", fmt.Errorf("parse docker network inspect: %w", err)
	}
	for _, n := range nets {
		for _, c := range n.IPAM.Config {
			// Sem CIDR: algumas versoes reportam `172.18.0.1/16` neste campo.
			if gw, _, _ := strings.Cut(c.Gateway, "/"); gw != "" {
				return gw, nil
			}
		}
	}
	return "", fmt.Errorf("network %s declares no IPAM gateway", network)
}

// brokerChartAbsPath resolves the chart path relative to the running binary.
// Overridable via LOOPZERO_CHART_PATH env for operator custom locations.
func brokerChartAbsPath() (string, error) {
	if p := osGetenv("LOOPZERO_CHART_PATH"); p != "" {
		return p, nil
	}
	// Resolve relative to the CWD — the seed is expected to run from the repo
	// root (halted-loop) or the cmd/loopzero-ops dir (dev). Try both.
	for _, candidate := range []string{
		brokerChartRelativePath,
		filepath.Join("..", "..", brokerChartRelativePath),
	} {
		if info, err := statPath(candidate); err == nil && info.IsDir() {
			abs, aerr := filepath.Abs(candidate)
			if aerr == nil {
				return abs, nil
			}
		}
	}
	return "", fmt.Errorf("chart dir not found (tried %s and ../../%s; set LOOPZERO_CHART_PATH)", brokerChartRelativePath, brokerChartRelativePath)
}

// helmUpgradeBrokerChart runs `helm upgrade --install` for the broker chart.
// D1 exemption: helm is a canonical operator-tool for chart lifecycle.
var helmUpgradeFn = defaultHelmUpgrade

func helmUpgradeBrokerChart(chartPath, apiURL string) error {
	return helmUpgradeFn(chartPath, apiURL)
}

func defaultHelmUpgrade(chartPath, apiURL string) error {
	cmd := exec.Command("helm", "upgrade", "--install", brokerHelmReleaseName,
		chartPath,
		"--kube-context", brokerContextWorkload,
		"-n", brokerESONamespace,
		"--create-namespace",
		"--set", "envName="+brokerEnvName,
		"--set", "broker.enabled=true",
		"--set", "broker.apiURL="+apiURL,
		"--set", "broker.tokenSecret="+brokerSecretName,
		"--set", "broker.cellID="+brokerCellID,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("helm upgrade: %w: %s", err, snippet(out))
	}
	return nil
}
