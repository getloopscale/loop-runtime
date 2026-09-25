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

// loopzero-workspace#23, item 2 — LOKI_URL stops recording a container IP.
//
// THE DEFECT. Layer 3 derived the URL from the IP of container
// `k3d-loopzero-build-server-0`. Docker network IPs are assigned by DHCP and
// SHUFFLE on every `docker restart` / `k3d cluster create`: in the map measured
// on 2026-08-01, `172.18.0.4` was the build server, `172.18.0.7` mgmt and
// `172.18.0.10` the workload serverlb — and nothing keeps them so after a
// reboot.
//
// WHY IT IS WORSE THAN A WRONG ADDRESS. The symptom never points at the cause.
// A LOKI_URL now pointing at ANOTHER cluster does not return "wrong cluster": it
// returns an empty `loopzero logs`, or `connection refused`, which reads as "Loki is
// down". The same class made two #114 investigations spend effort rotating
// credentials — which works until the next restart, so the defect looks
// periodic and unsolvable.
//
// THE WAY OUT is the one that already closed item 1 of the class: the network
// GATEWAY (`.0.1`) plus a port PUBLISHED on the serverlb. The gateway is not
// assigned by DHCP — it is the bridge's own address, declared in the network's
// IPAM — and the published port is in the cluster's versioned config
// (`infra/k3d/build-cluster.yaml`), not in Docker's draw.
//
// Derived, never written as a literal: `172.18.0.1` is true for today's
// `k3d-loopzero` network; another network has another bridge.

// A network with no IPAM gateway — the case that must not become a silent
// fallback.
var errNoGatewayForTest = errors.New("network declares no IPAM gateway")

func TestLokiCrossCluster_URLDerivesFromTheGatewayNotTheContainer(t *testing.T) {
	var host Host
	// Arrange — the fake tells the two sources apart: asked for the CONTAINER IP
	// it answers 172.20.0.99; asked for the GATEWAY, 172.20.0.1.
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
		t.Errorf("LOKI_URL = %q — it recorded the CONTAINER IP, which shuffles on every k3d restart "+
			"(loopzero-workspace#23 item 2)", env)
	}
	if !strings.HasPrefix(env, "http://172.20.0.1:") {
		t.Errorf("LOKI_URL = %q, want it derived from the network gateway (http://172.20.0.1:<port>)", env)
	}
}

func TestLokiCrossCluster_FailsLoudWhenTheGatewayDoesNotResolve(t *testing.T) {
	var host Host
	// Fail fast: with no gateway there is no stable address. Falling back to the
	// container IP would reintroduce the defect IN SILENCE — and a silent fallback
	// to the wrong behaviour is worse than the failure, because it is never
	// investigated.
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
		t.Fatalf("Verdict = %s (%s), want FAIL — with no gateway there is no stable address", got.Verdict, got.Reason)
	}
	env, _ := getDeploymentContainerEnv(context.Background(), mgmtC, "loopzero-system", "loopzero-api", lokiLoopZeroAPILokiURLEnvVar)
	if strings.Contains(env, "172.20.0.99") {
		t.Errorf("fell back to the container IP in silence: LOKI_URL = %q", env)
	}
}
