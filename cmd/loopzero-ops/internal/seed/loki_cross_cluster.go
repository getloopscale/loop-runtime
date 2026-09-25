// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// LokiCrossClusterSeed is the Go port of
// infra/scripts/local/seed-loki-cross-cluster.sh.
//
// 3 layers:
//
//  1. Patch build cluster Promtail Secret clients.url → canonical
//     http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push
//     (rewrites any stale localhost/NodePort URL); rollout Promtail DS.
//  2. Convert build cluster loki Service to NodePort (fixed 30120) if not
//     already; harvest existing NodePort otherwise.
//  3. Set loopzero-api LOKI_URL env → http://<build-server-0 IP>:<NodePort>;
//     rollout loopzero-api.
type LokiCrossClusterSeed struct{}

func NewLokiCrossClusterSeed() *LokiCrossClusterSeed { return &LokiCrossClusterSeed{} }
func (s *LokiCrossClusterSeed) Name() string         { return "loki-cross-cluster" }

const (
	lokiBuildContext         = "k3d-loopzero-build"
	lokiMonitoringNS         = "monitoring"
	lokiPromtailSecret       = "promtail"
	lokiPromtailKey          = "promtail.yaml"
	lokiSvcName              = "loki"
	lokiSvcPort              = 3100
	lokiDefaultNodePort      = 30120
	lokiCanonicalPushURL     = "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push"
	lokiLoopZeroAPILokiURLEnvVar = "LOKI_URL"
	// `lokiBuildServerContainer = "k3d-loopzero-build-server-0"` foi REMOVIDA junto com o
	// defeito: era o container cujo IP a Layer 3 gravava. Deixa-la seria convidar a
	// proxima pessoa a voltar a usa-la — e um simbolo morto que aponta para o
	// comportamento errado e' pior que nenhum.
)

// pushURLRE matches any http://... URL followed by /loki/api/v1/push. Used to
// rewrite stale localhost/NodePort URLs in the Promtail config.
var pushURLRE = regexp.MustCompile(`http://[0-9.]+:[0-9]+/loki/api/v1/push`)

func (s *LokiCrossClusterSeed) Run(d Deps) Result {
	buildCli, err := d.ClientFor(lokiBuildContext)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("resolve build ctx: %v", err)}
	}

	// Layer 1 — Promtail Secret rewrite (if drift).
	promtail, err := buildCli.GetSecret(d.Ctx, lokiMonitoringNS, lokiPromtailSecret)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 get Promtail Secret: %v", err)}
	}
	current := string(promtail.Data[lokiPromtailKey])
	if current == "" {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 Promtail Secret has no data.%s", lokiPromtailKey)}
	}
	updated := pushURLRE.ReplaceAllString(current, lokiCanonicalPushURL)
	if updated != current {
		if d.DryRun {
			return Result{Verdict: PASS, Reason: "dry-run: would patch Promtail + Loki + LOKI_URL"}
		}
		if err := buildCli.PatchSecretData(d.Ctx, lokiMonitoringNS, lokiPromtailSecret,
			map[string][]byte{lokiPromtailKey: []byte(updated)}); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 patch Promtail: %v", err)}
		}
		if err := buildCli.RolloutRestartDaemonSet(d.Ctx, lokiMonitoringNS, "promtail"); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1 rollout DS: %v", err)}
		}
	}

	// Layer 2 — Loki Service → NodePort.
	nodePort, err := lokiEnsureNodePort(d.Ctx, buildCli, lokiDefaultNodePort)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 loki NodePort: %v", err)}
	}

	// Layer 3 — set loopzero-api LOKI_URL.
	//
	// GATEWAY, nao o IP do container (loopzero-workspace#23 item 2). O endereco de
	// `k3d-loopzero-build-server-0` e' distribuido por DHCP e EMBARALHA a cada
	// `docker restart` / `k3d cluster create`: no mapa medido em 2026-08-01,
	// `172.18.0.4` era o server de build e `172.18.0.7` o de mgmt — depois de um
	// reboot o LOKI_URL gravado podia apontar para OUTRO cluster, e o sintoma
	// (`loopzero logs` vazio) le-se como "Loki fora do ar", nunca como "cluster errado".
	//
	// O par estavel e' gateway + porta PUBLICADA no serverlb, declarada em
	// `infra/k3d/build-cluster.yaml`. Sem a publicacao o gateway sozinho nao alcanca
	// a NodePort — por isso as duas mudancas andam juntas.
	//
	// FALHA ALTO se o gateway nao resolve. Cair no IP do container como fallback
	// reintroduziria o defeito EM SILENCIO, e um fallback silencioso para o
	// comportamento errado e' pior que a falha: nunca e' investigado.
	gateway, err := d.Host.dockerGateway(brokerNetworkName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf(
			"Layer 3 gateway da rede %s: %v — sem gateway nao ha endereco estavel para LOKI_URL "+
				"(gravar IP de container reintroduz loopzero-workspace#23)", brokerNetworkName, err)}
	}
	canonicalURL := fmt.Sprintf("http://%s:%d", gateway, nodePort)
	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would set LOKI_URL=%s on loopzero-api", canonicalURL)}
	}
	got, err := getDeploymentContainerEnv(d.Ctx, d.CtrlClient,
		"loopzero-system", "loopzero-api", lokiLoopZeroAPILokiURLEnvVar)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 read LOKI_URL: %v", err)}
	}
	if got != canonicalURL {
		if err := setDeploymentContainerEnv(d.Ctx, d.CtrlClient,
			"loopzero-system", "loopzero-api", lokiLoopZeroAPILokiURLEnvVar, canonicalURL); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 set env: %v", err)}
		}
		if err := waitForRolloutSettled(d.Ctx, d.CtrlClient, "loopzero-system", "loopzero-api", d.settleTimeout(loopzeroAPISettle)); err != nil {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3 loopzero-api did not settle: %v", err)}
		}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("canonical Loki cross-cluster chain (LOKI_URL=%s)", canonicalURL)}
}

// Verify probes: (a) Promtail Secret has canonical push URL,
// (b) Loki Service is NodePort, (c) loopzero-api LOKI_URL != empty.
func (s *LokiCrossClusterSeed) Verify(d Deps) Result {
	buildCli, err := d.ClientFor(lokiBuildContext)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	pt, err := buildCli.GetSecret(d.Ctx, lokiMonitoringNS, lokiPromtailSecret)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if !strings.Contains(string(pt.Data[lokiPromtailKey]), lokiCanonicalPushURL) {
		return Result{Verdict: FAIL, Reason: "Promtail push URL not canonical"}
	}
	got, err := getDeploymentContainerEnv(d.Ctx, d.CtrlClient, "loopzero-system", "loopzero-api", lokiLoopZeroAPILokiURLEnvVar)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if got == "" {
		return Result{Verdict: FAIL, Reason: "loopzero-api LOKI_URL empty"}
	}
	return Result{Verdict: PASS, Reason: "canonical Loki chain: Promtail + LOKI_URL wired"}
}

// lokiEnsureNodePort ensures the loki Service is of type NodePort. Returns
// the assigned NodePort. If already NodePort, returns the existing value.
// If ClusterIP, patches to NodePort with the given desired NodePort.
func lokiEnsureNodePort(ctx context.Context, services Services, desired int32) (int32, error) {
	svc, err := services.GetService(ctx, lokiMonitoringNS, lokiSvcName)
	if err != nil {
		return 0, err
	}
	if svc.Spec.Type == corev1.ServiceTypeNodePort {
		for _, p := range svc.Spec.Ports {
			if p.Port == lokiSvcPort && p.NodePort != 0 {
				return p.NodePort, nil
			}
		}
		return desired, nil
	}
	svc.Spec.Type = corev1.ServiceTypeNodePort
	for i, p := range svc.Spec.Ports {
		if p.Port == lokiSvcPort {
			svc.Spec.Ports[i].NodePort = desired
		}
	}
	if len(svc.Spec.Ports) == 0 {
		svc.Spec.Ports = []corev1.ServicePort{
			{Port: lokiSvcPort, TargetPort: intstr.FromInt(int(lokiSvcPort)), NodePort: desired},
		}
	}
	if err := services.UpdateService(ctx, svc); err != nil {
		return 0, err
	}
	return desired, nil
}
