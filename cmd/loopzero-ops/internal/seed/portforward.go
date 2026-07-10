// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Tunnel is an open kubectl-port-forward-equivalent to a Pod. Callers MUST
// invoke Close (typically via defer) to tear down the goroutine + socket.
type Tunnel struct {
	LocalPort int
	Close     func()
}

// OpenTunnelForService picks the FIRST Ready pod matching podLabels in
// namespace, opens a port-forward to remotePort, and (optionally) probes
// probePath until it returns 200 OR 401. Returns the bound local port so
// the caller can build "http://127.0.0.1:<port>" URLs for its API client.
//
// probePath is joined onto "http://localhost:<localPort>" — pass "" to
// skip the probe (the tunnel may be dead but ready == "socket accepted").
// A probe that never returns 200|401 within 30s is FAIL.
func OpenTunnelForService(ctx context.Context, restCfg *rest.Config, cli ctrlclient.Client,
	namespace string, podLabels map[string]string, remotePort int, probePath string,
) (*Tunnel, error) {
	if restCfg == nil {
		return nil, fmt.Errorf("port-forward: restCfg is nil (composition root did not wire it)")
	}
	podName, err := firstReadyPod(ctx, cli, namespace, podLabels)
	if err != nil {
		return nil, err
	}
	transport, upgrader, err := spdy.RoundTripperFor(restCfg)
	if err != nil {
		return nil, fmt.Errorf("spdy round-tripper: %w", err)
	}
	u, err := url.Parse(restCfg.Host)
	if err != nil {
		return nil, fmt.Errorf("parse host: %w", err)
	}
	u.Path = fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, u)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)
	pf, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", remotePort)},
		stopCh, readyCh, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("portforward: %w", err)
	}
	go func() {
		if err := pf.ForwardPorts(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-readyCh:
	case e := <-errCh:
		return nil, fmt.Errorf("port-forward failed before ready: %w", e)
	case <-ctx.Done():
		close(stopCh)
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		close(stopCh)
		return nil, fmt.Errorf("port-forward not ready after 15s")
	}
	ports, perr := pf.GetPorts()
	if perr != nil || len(ports) == 0 {
		close(stopCh)
		return nil, fmt.Errorf("port-forward: no ports bound")
	}
	local := int(ports[0].Local)
	closed := false
	close1 := func() {
		if closed {
			return
		}
		closed = true
		close(stopCh)
	}
	if probePath != "" {
		if err := probeTunnelReady(ctx, local, probePath); err != nil {
			close1()
			return nil, err
		}
	}
	return &Tunnel{LocalPort: local, Close: close1}, nil
}

// firstReadyPod lists pods matching labels in namespace and returns the name
// of the FIRST pod in Ready state. A missing pod is a FAIL-shaped error.
func firstReadyPod(ctx context.Context, cli ctrlclient.Client,
	namespace string, labels map[string]string,
) (string, error) {
	var list corev1.PodList
	sel := ctrlclient.MatchingLabels(labels)
	if err := cli.List(ctx, &list, ctrlclient.InNamespace(namespace), sel); err != nil {
		return "", fmt.Errorf("list pods %s/%v: %w", namespace, labels, err)
	}
	for _, p := range list.Items {
		if isPodReady(&p) {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no Ready pod in %s matching %v", namespace, labels)
}

func isPodReady(p *corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// probeTunnelReady polls http://localhost:<port><path> until it returns 200
// OR 401 (auth-gated APIs return 401 unauthed but the tunnel is warm). Matches
// the bash `for i in $(seq 1 30); do curl … done` canonical pattern.
func probeTunnelReady(ctx context.Context, port int, path string) error {
	target := fmt.Sprintf("http://localhost:%d%s", port, path)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err == nil {
			code := resp.StatusCode
			_ = resp.Body.Close()
			if code == http.StatusOK || code == http.StatusUnauthorized {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return fmt.Errorf("tunnel probe %q never returned 200|401 within 30s", target)
}

// PodLabels is a tiny type alias so seed code reads naturally when the
// selector is inline: `PodLabels{"app.kubernetes.io/name": "argocd-server"}`.
type PodLabels = map[string]string

// PodMetaLabels selects on the standard app.kubernetes.io/name label alone.
func PodMetaLabels(name string) PodLabels {
	return PodLabels{"app.kubernetes.io/name": name}
}

// asObjectMeta is unused but reserved for future adjacent helpers (avoids
// import-cycle temptation to import metav1 in seed .go files directly).
var _ = metav1.ObjectMeta{}
