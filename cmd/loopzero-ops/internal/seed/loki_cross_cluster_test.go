// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPushURLRE_RewritesStaleLocalhostToCanonical(t *testing.T) {
	stale := `clients:
  - url: http://127.0.0.1:30094/loki/api/v1/push
`
	got := pushURLRE.ReplaceAllString(stale, lokiCanonicalPushURL)
	if strings.Contains(got, "127.0.0.1") {
		t.Errorf("stale IP survived: %q", got)
	}
}

func TestPushURLRE_LeavesUnrelatedURLsAlone(t *testing.T) {
	// The regex is anchored to `/loki/api/v1/push` — other URLs are preserved.
	other := "http://example.com/other/path"
	got := pushURLRE.ReplaceAllString(other, lokiCanonicalPushURL)
	if got != other {
		t.Errorf("unrelated URL modified: got %q, want %q", got, other)
	}
}

// buildClientCtorFor returns a ClientCtor stub that returns `k` for the given
// context name; anything else returns ErrNoClientCtor.
func buildClientCtorFor(name string, k KubeClient) func(string) (KubeClient, error) {
	return func(ctx string) (KubeClient, error) {
		if ctx == name {
			return k, nil
		}
		return nil, ErrNoClientCtor
	}
}

func TestLokiCrossClusterSeed_Run_PatchesPromtailAndSetsLokiURL(t *testing.T) {
	var host Host
	// Stub docker network so Layer 3 can resolve the stable address.
	//
	// O GATEWAY passou a ser stubado aqui por duas razoes, e a segunda so apareceu
	// ao rodar: (a) a Layer 3 deixou de gravar IP de container (loopzero-workspace#23
	// item 2), entao e' o gateway que ela consulta; (b) sem o stub, este teste
	// chamava `docker network inspect` DE VERDADE e passou a depender da maquina —
	// mediu `10.0.5.1`, o gateway real deste host. Um teste de unidade que alcanca o
	// daemon do Docker e' nao-deterministico por construcao.
	host.DockerNetworkIP = func(network, container string) (string, error) {
		return "172.20.0.99", nil
	}
	host.DockerGateway = func(network string) (string, error) {
		return "172.20.0.1", nil
	}

	// Build-cluster fake client — Promtail Secret with a stale URL + loki Service + DS.
	pt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiPromtailSecret},
		Data: map[string][]byte{
			lokiPromtailKey: []byte("clients:\n  - url: http://127.0.0.1:30094/loki/api/v1/push\n"),
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiSvcName},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{Port: lokiSvcPort},
			},
		},
	}
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: "promtail"},
	}
	buildC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(pt, svc, ds).Build()

	// Mgmt cluster fake client — loopzero-api Deployment for LOKI_URL env write.
	apiDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Status:     settledDeploymentStatus,
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "loopzero-api"}},
				},
			},
		},
	}
	mgmtC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(apiDep).Build()

	deps := Deps{Host: host,
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmtC),
		CtrlClient:           mgmtC,
		ClientCtor:           buildClientCtorFor(lokiBuildContext, NewKubeClient(buildC)),
	}

	got := NewLokiCrossClusterSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}

	// Assert Layer 1: Promtail Secret rewritten to canonical.
	var patched corev1.Secret
	_ = buildC.Get(context.Background(), fakeKey(lokiMonitoringNS, lokiPromtailSecret), &patched)
	if !strings.Contains(string(patched.Data[lokiPromtailKey]), lokiCanonicalPushURL) {
		t.Errorf("Promtail Secret NOT canonical: %q", patched.Data[lokiPromtailKey])
	}
	if strings.Contains(string(patched.Data[lokiPromtailKey]), "127.0.0.1") {
		t.Errorf("stale localhost survived: %q", patched.Data[lokiPromtailKey])
	}

	// Assert Layer 2: Loki Service is NodePort.
	var lokiSvc corev1.Service
	_ = buildC.Get(context.Background(), fakeKey(lokiMonitoringNS, lokiSvcName), &lokiSvc)
	if lokiSvc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("Loki Service type = %s, want NodePort", lokiSvc.Spec.Type)
	}

	// Assert Layer 3: loopzero-api LOKI_URL env upserted to GATEWAY + NodePort.
	//
	// Este assert dizia `172.20.0.99` — o IP do CONTAINER. Ele nao estava errado por
	// descuido: travava fielmente o comportamento de entao, que era o defeito do
	// loopzero-workspace#23 item 2. Trocar o alvo aqui e' a mudanca de contrato, nao um
	// ajuste de teste: o endereco estavel e' o gateway da rede (declarado no IPAM)
	// mais a porta publicada no serverlb, nunca um IP distribuido por DHCP.
	env, err := getDeploymentContainerEnv(context.Background(), mgmtC, "loopzero-system", "loopzero-api", lokiLoopZeroAPILokiURLEnvVar)
	if err != nil {
		t.Fatalf("getDeploymentContainerEnv: %v", err)
	}
	if !strings.HasPrefix(env, "http://172.20.0.1:") {
		t.Errorf("LOKI_URL = %q, want prefix http://172.20.0.1: (gateway, nao IP de container)", env)
	}
}

func TestLokiCrossClusterSeed_Run_DryRunReports(t *testing.T) {
	pt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiPromtailSecret},
		Data: map[string][]byte{
			lokiPromtailKey: []byte("clients:\n  - url: http://127.0.0.1:30094/loki/api/v1/push\n"),
		},
	}
	buildC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(pt).Build()

	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		DryRun:               true,
		ClientCtor:           buildClientCtorFor(lokiBuildContext, NewKubeClient(buildC)),
	}

	got := NewLokiCrossClusterSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "dry-run") {
		t.Errorf("Reason = %q, want dry-run marker", got.Reason)
	}
}

func TestLokiCrossClusterSeed_Run_FailsWhenPromtailSecretMissing(t *testing.T) {
	buildC := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		ClientCtor:           buildClientCtorFor(lokiBuildContext, NewKubeClient(buildC)),
	}
	got := NewLokiCrossClusterSeed().Run(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "Promtail Secret") {
		t.Errorf("Reason = %q, want 'Promtail Secret' marker", got.Reason)
	}
}

func TestLokiEnsureNodePort_PreservesExisting(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiSvcName},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{Port: lokiSvcPort, NodePort: 31234},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(svc).Build()
	got, err := lokiEnsureNodePort(context.Background(), NewKubeClient(c), lokiDefaultNodePort)
	if err != nil {
		t.Fatalf("lokiEnsureNodePort: %v", err)
	}
	if got != 31234 {
		t.Errorf("NodePort = %d, want 31234 (preserved)", got)
	}
}

func TestLokiEnsureNodePort_ConvertsClusterIP(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiSvcName},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: lokiSvcPort}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(svc).Build()
	got, err := lokiEnsureNodePort(context.Background(), NewKubeClient(c), lokiDefaultNodePort)
	if err != nil {
		t.Fatalf("lokiEnsureNodePort: %v", err)
	}
	if got != lokiDefaultNodePort {
		t.Errorf("NodePort = %d, want %d", got, lokiDefaultNodePort)
	}
	var updated corev1.Service
	_ = c.Get(context.Background(), fakeKey(lokiMonitoringNS, lokiSvcName), &updated)
	if updated.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("Service type = %s, want NodePort", updated.Spec.Type)
	}
}

func TestLokiCrossClusterSeed_Verify_PassWhenCanonical(t *testing.T) {
	pt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiPromtailSecret},
		Data:       map[string][]byte{lokiPromtailKey: []byte(lokiCanonicalPushURL)},
	}
	buildC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(pt).Build()

	apiDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Status:     settledDeploymentStatus,
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "loopzero-api",
						Env: []corev1.EnvVar{
							{Name: lokiLoopZeroAPILokiURLEnvVar, Value: "http://172.20.0.99:30120"},
						},
					}},
				},
			},
		},
	}
	mgmtC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(apiDep).Build()

	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		CtrlClient:           mgmtC,
		ClientCtor:           buildClientCtorFor(lokiBuildContext, NewKubeClient(buildC)),
	}
	got := NewLokiCrossClusterSeed().Verify(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestLokiCrossClusterSeed_Verify_FailWhenEnvEmpty(t *testing.T) {
	pt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiPromtailSecret},
		Data:       map[string][]byte{lokiPromtailKey: []byte(lokiCanonicalPushURL)},
	}
	buildC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(pt).Build()

	apiDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Status:     settledDeploymentStatus,
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "loopzero-api"}},
				},
			},
		},
	}
	mgmtC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(apiDep).Build()

	deps := Deps{
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		CtrlClient:           mgmtC,
		ClientCtor:           buildClientCtorFor(lokiBuildContext, NewKubeClient(buildC)),
	}
	got := NewLokiCrossClusterSeed().Verify(deps)
	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestLokiCrossClusterSeed_Name(t *testing.T) {
	if got := NewLokiCrossClusterSeed().Name(); got != "loki-cross-cluster" {
		t.Errorf("Name = %q", got)
	}
}

func TestContains(t *testing.T) {
	if !strings.Contains("hello world", "world") {
		t.Error("strings.Contains(hello world, world) = false")
	}
	if strings.Contains("abc", "xyz") {
		t.Error("strings.Contains(abc, xyz) = true")
	}
	if !strings.Contains("anything", "") {
		t.Error("strings.Contains(anything, empty) = false")
	}
	if strings.Contains("short", "toolong") {
		t.Error("strings.Contains(short, toolong) = true")
	}
}
