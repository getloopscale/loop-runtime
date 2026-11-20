// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package e2e provides shared test helpers for all E2E test suites.
package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/infra/scripts/seed"
	"github.com/getloopscale/loopzero/pkg/naming"
	. "github.com/onsi/gomega"
)

// e2eNamespace is the workload namespace of the E2E tenant's (project, env),
// derived by the platform's own naming rule. Specs never spell it by hand: the
// two-segment "loopzero-<tenant>-<env>" they used to build names a namespace no
// deploy creates, and an absence check there passes vacuously.
func e2eNamespace(project, env string) (string, error) {
	return naming.Namespace(seed.TenantID, project, env)
}

// mustE2ENamespace is e2eNamespace for spec bodies, failing the spec on error.
func mustE2ENamespace(project, env string) string {
	ns, err := e2eNamespace(project, env)
	Expect(err).NotTo(HaveOccurred(), "workload namespace for %s/%s", project, env)
	return ns
}

// proveDead verifies that all resources for a project are cleaned up
// (namespace gone or zero resources with loopzero.io/project label).
// This is the "proof of death" assertion from the bash journeys.
// 2026-05-20 drill-parity: uses kubectlForCluster(ClusterWorkload, ...).
func proveDead(tenant, project, env string) {
	// The namespace is the (tenant, project, env) triple. While this computed two
	// segments, the proof of death checked a namespace that never existed — and
	// always passed, proving nothing.
	ns, nsErr := naming.Namespace(tenant, project, env)
	Expect(nsErr).NotTo(HaveOccurred(), "proveDead: invalid namespace for %s/%s/%s", tenant, project, env)

	Eventually(func() bool {
		// Check if namespace still exists.
		checkNS := kubectlForCluster(ClusterWorkload,
			"get", "namespace", ns, "--no-headers", "-o", "name")
		if checkNS.Run() != nil {
			// Namespace doesn't exist — fully cleaned up.
			return true
		}

		// Namespace exists — check for remaining labeled resources.
		// Include both Deployment (legacy) and Rollout (progressive
		// delivery, default-on per argo-rollouts plan) so a stuck
		// Rollout/TraefikService doesn't yield a false-positive "cleaned".
		label := fmt.Sprintf("loopzero.io/project=%s", project)
		for _, resource := range []string{
			"deployment",
			"rollout.argoproj.io",
			"service",
			"ingressroute.traefik.io",
			"traefikservice.traefik.io",
		} {
			checkRes := kubectlForCluster(ClusterWorkload,
				"-n", ns, "get", resource, "-l", label,
				"--no-headers", "--ignore-not-found", "-o", "name")
			output, err := checkRes.CombinedOutput()
			if err == nil && strings.TrimSpace(string(output)) != "" {
				return false
			}
		}
		return true
	}, cleanupTimeout, cleanupPollInterval).Should(BeTrue(),
		"proof of death failed: resources still exist for project %s in %s", project, ns)
}

// waitAppHealthyViaKubectl is the V9 Option-B bypass for local k3d
// HTTP probes that flake on Traefik+DNS routing under load. It uses
// `kubectl port-forward <pod>` to tunnel an HTTP request from the
// Ginkgo process directly to the pod — completely sidestepping the
// local k3d DNS+TLS+Traefik convergence path AND the in-container
// busybox/distroless tooling assumption (apps with scratch/distroless
// bases have no wget/curl, so kubectl exec probe returns exit 127).
//
// Parameters:
//   - project, app, env: identify the workload (namespace
//     e2eNamespace(project, env), label "app=<project>-<app>")
//   - path: HTTP path to GET (e.g., "/health")
//   - timeout: Eventually() budget
//
// Returns silently on success; fails the spec via Gomega Expect
// on failure (mirrors waitHTTP200WithHost behavior).
//
// Resilient to:
//   - Pod not yet provisioned (Eventually retries)
//   - Pod not yet Ready (port-forward fails, retry)
//   - Distroless/scratch base image (port-forward works regardless)
//
// V9 Final fix (post-L11): kubectl exec assumed wget/curl exist in the
// app image, but loopzero-packs uses distroless base → exit 127. port-forward
// works for any image because the kubelet tunnel runs OUTSIDE the
// container's filesystem.
func waitAppHealthyViaKubectl(project, app, env, path string, timeout time.Duration) {
	resName := project + "-" + app
	ns := mustE2ENamespace(project, env)

	Eventually(func() error {
		// Find a Running+Ready pod for this app.
		out, err := kubectlForCluster(ClusterWorkload,
			"get", "pods", "-n", ns,
			"-l", "app="+resName,
			"-o", "jsonpath={range .items[*]}{.metadata.name}|{.status.phase}|{range .status.containerStatuses[*]}{.ready},{end}{\"\\n\"}{end}",
		).CombinedOutput()
		if err != nil {
			return fmt.Errorf("list pods in %s: %s", ns, strings.TrimSpace(string(out)))
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) == 0 || lines[0] == "" {
			return fmt.Errorf("no pods with label app=%s in namespace %s", resName, ns)
		}
		var podName string
		for _, line := range lines {
			parts := strings.Split(line, "|")
			if len(parts) < 3 {
				continue
			}
			if parts[1] != "Running" {
				continue
			}
			// containerStatuses ready comma-separated: "true,true,"
			if strings.Contains(parts[2], "false") {
				continue
			}
			podName = parts[0]
			break
		}
		if podName == "" {
			return fmt.Errorf("no Running+Ready pod for app=%s in %s (status: %s)", resName, ns, lines[0])
		}

		// Discover the container port the app listens on. loopzero-packs
		// app images typically expose 3000 (node) or 8080 (other). We
		// try the first containerPort declared.
		portOut, perr := kubectlForCluster(ClusterWorkload,
			"get", "pod", "-n", ns, podName,
			"-o", "jsonpath={.spec.containers[0].ports[0].containerPort}",
		).CombinedOutput()
		appPort := strings.TrimSpace(string(portOut))
		if perr != nil || appPort == "" {
			appPort = "3000" // safe default for loopzero-packs node template
		}

		// kubectl port-forward to a random local port, run HTTP GET,
		// then kill the port-forward. Random port avoids contention
		// when multiple journeys run sequentially.
		// We pick a port in the range 39000-39999 using crypto/rand.
		var portBuf [2]byte
		_, _ = rand.Read(portBuf[:])
		localPort := 39000 + (int(portBuf[0])<<8|int(portBuf[1]))%1000

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pfCmd := kubectlForCluster(ClusterWorkload,
			"port-forward", "-n", ns, "pod/"+podName,
			fmt.Sprintf("%d:%s", localPort, appPort),
		)
		// Start port-forward as background subprocess.
		stderr := &strings.Builder{}
		pfCmd.Stderr = stderr
		if err := pfCmd.Start(); err != nil {
			return fmt.Errorf("port-forward start failed: %v", err)
		}
		// Kill port-forward on function exit (deferred close).
		killCh := make(chan struct{})
		go func() {
			select {
			case <-killCh:
			case <-ctx.Done():
			}
			_ = pfCmd.Process.Kill()
			_, _ = pfCmd.Process.Wait()
		}()
		defer close(killCh)

		// Wait briefly for port-forward to bind.
		// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
		time.Sleep(500 * time.Millisecond)

		// HTTP GET via the port-forwarded localhost tunnel.
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d%s", localPort, path), nil)
		resp, herr := client.Do(req)
		if herr != nil {
			return fmt.Errorf("port-forward HTTP probe to %s:%d%s failed: %v (pf stderr: %s)",
				podName, localPort, path, herr, strings.TrimSpace(stderr.String()))
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("port-forward HTTP probe to %s:%d%s returned %d", podName, localPort, path, resp.StatusCode)
		}
		return nil
	}, timeout, 5*time.Second).Should(Succeed(),
		"expected /health probe to succeed via kubectl port-forward on app=%s in namespace=%s within %s",
		resName, ns, timeout,
	)
}
