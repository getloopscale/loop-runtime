package seed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// loopzero-workspace#23, item 2 — LOKI_URL deixa de gravar IP de container.
//
// O DEFEITO. A Layer 3 derivava a URL do IP do container `k3d-loopzero-build-server-0`.
// IPs de rede Docker sao atribuidos por DHCP e EMBARALHAM a cada `docker restart` /
// `k3d cluster create`: no mapa medido em 2026-08-01, `172.18.0.4` era o server de
// build, `172.18.0.7` o de mgmt e `172.18.0.10` o serverlb de workload — e nada
// garante que continuem assim depois de um reboot.
//
// POR QUE E' PIOR QUE UM ENDERECO ERRADO. O sintoma nunca aponta para a causa. Um
// `LOKI_URL` que passou a apontar para OUTRO cluster nao devolve "cluster errado":
// devolve `loopzero logs` vazio, ou `connection refused`, que se le como "Loki fora do
// ar". A mesma classe fez duas investigacoes do #114 gastarem esforco rotacionando
// credencial — o que funciona ate' o proximo restart, e por isso o defeito parece
// periodico e insoluvel.
//
// A SAIDA e' a que ja tirou o item 1 da classe: o GATEWAY da rede (`.0.1`) mais uma
// porta PUBLICADA no serverlb. O gateway nao e' distribuido por DHCP — e' o endereco
// da propria bridge, declarado no IPAM da rede — e a porta publicada esta' na config
// versionada do cluster (`infra/k3d/build-cluster.yaml`), nao no sorteio do Docker.
//
// Derivado, nunca fixado em texto: `172.18.0.1` e' verdade para a rede `k3d-loopzero`
// de hoje: outra rede tem outra bridge. Ler do IPAM mantem a propriedade
// (estabilidade) sem herdar o literal.

// rede sem gateway declarado no IPAM — o caso que nao pode virar fallback silencioso.
var errNoGatewayForTest = errors.New("network declares no IPAM gateway")

func TestLokiCrossCluster_URLDerivaDoGatewayNaoDoContainer(t *testing.T) {
	var host Host
	// Arrange — o fake distingue as duas fontes: se o codigo perguntar pelo IP do
	// CONTAINER, recebe 172.20.0.99; se perguntar pelo GATEWAY, recebe 172.20.0.1.
	host.DockerNetworkIP = func(network, container string) (string, error) { return "172.20.0.99", nil }
	host.DockerGateway = func(network string) (string, error) { return "172.20.0.1", nil }

	pt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiPromtailSecret},
		Data:       map[string][]byte{lokiPromtailKey: []byte("clients:\n  - url: http://127.0.0.1:3100/loki/api/v1/push\n")},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiSvcName},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: lokiSvcPort}},
		},
	}
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: "promtail"}}
	buildC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(pt, svc, ds).Build()

	apiDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Status:     settledDeploymentStatus,
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "loopzero-api"}}},
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

	// Act
	got := NewLokiCrossClusterSeed().Run(deps)
	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}

	// Assert
	env, err := getDeploymentContainerEnv(context.Background(), mgmtC, "loopzero-system", "loopzero-api", lokiLoopZeroAPILokiURLEnvVar)
	if err != nil {
		t.Fatalf("getDeploymentContainerEnv: %v", err)
	}
	if strings.Contains(env, "172.20.0.99") {
		t.Errorf("LOKI_URL = %q — gravou o IP do CONTAINER, que embaralha a cada restart do k3d "+
			"(loopzero-workspace#23 item 2)", env)
	}
	if !strings.HasPrefix(env, "http://172.20.0.1:") {
		t.Errorf("LOKI_URL = %q, esperado derivar do gateway da rede (http://172.20.0.1:<porta>)", env)
	}
}

func TestLokiCrossCluster_FalhaAltoQuandoOGatewayNaoResolve(t *testing.T) {
	var host Host
	// Fail-fast: sem gateway nao existe endereco estavel. Cair no IP do container
	// como "fallback" reintroduziria o defeito EM SILENCIO — e um fallback silencioso
	// para o comportamento errado e' pior que a falha, porque nunca e' investigado.
	host.DockerNetworkIP = func(network, container string) (string, error) { return "172.20.0.99", nil }
	host.DockerGateway = func(network string) (string, error) { return "", errNoGatewayForTest }

	pt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiPromtailSecret},
		Data:       map[string][]byte{lokiPromtailKey: []byte("clients:\n  - url: http://127.0.0.1:3100/loki/api/v1/push\n")},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: lokiSvcName},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: lokiSvcPort}},
		},
	}
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: lokiMonitoringNS, Name: "promtail"}}
	buildC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(pt, svc, ds).Build()
	apiDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Status:     settledDeploymentStatus,
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "loopzero-api"}}},
			},
		},
	}
	mgmtC := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(apiDep).Build()

	got := NewLokiCrossClusterSeed().Run(Deps{Host: host,
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmtC),
		CtrlClient:           mgmtC,
		ClientCtor:           buildClientCtorFor(lokiBuildContext, NewKubeClient(buildC)),
	})

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL — sem gateway nao ha endereco estavel", got.Verdict, got.Reason)
	}
	env, _ := getDeploymentContainerEnv(context.Background(), mgmtC, "loopzero-system", "loopzero-api", lokiLoopZeroAPILokiURLEnvVar)
	if strings.Contains(env, "172.20.0.99") {
		t.Errorf("caiu no IP do container como fallback silencioso: LOKI_URL = %q", env)
	}
}
