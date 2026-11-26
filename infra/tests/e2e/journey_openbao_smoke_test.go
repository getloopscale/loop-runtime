// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Journey: OpenBao Smoke (F1.10 of plan 2026-06-02-openbao-adoption v1.2)
//
// Validates OpenBao Helm release deploys correctly on k3d standalone
// (single-pod dev-mode) before promoting to prod. Pattern mirrors
// journey_velero_dr_test.go — invariants over the LIVE chart on the
// mgmt cluster.
//
// Skip semantics:
//   - Skipped when LOOPRUNTIME_E2E unset (suite-level gate via skipIfNotE2E)
//   - Skipped when secrets-system namespace missing (chart not deployed)
//   - Skipped when openbaoEnabled flag false in env (default; chart
//     installed:false per F1.7 Helmfile gate)
//
// Coverage matrix (OB- prefix):
//   * OB-1: bank-vaults-operator pod Running (operator deployment)
//   * OB-2: openbao pod Running in secrets-system (instance deployment)
//   * OB-3: openbao Service ClusterIP exposes port 8200
//   * OB-4: cert-manager Certificate openbao-tls Ready
//   * OB-5: NetworkPolicy openbao-allow-egress exists with port 443 rule
//   * OB-6: NetworkPolicy openbao-allow-ingress lists loopruntime-system
//   * OB-7: ValidatingAdmissionPolicy openbao-no-devmode-in-prod exists
//   * OB-8: BaoConfiguration CR `openbao` exists in secrets-system
//   * OB-9: bao status returns Initialized=true Sealed=false (dev-mode)
//   * OB-10: kv-v2 engine enabled at path secret/ (post-init smoke)
//
// Specs that need `bao` CLI (OB-9, OB-10) skip individually when
// binary not in PATH so structural invariants still run on hosts
// without the CLI installed.

package e2e

import (
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	openbaoNamespace       = "secrets-system"
	bankVaultsOperatorNs   = "bank-vaults-operator"
	openbaoReleaseName     = "openbao"
	openbaoServiceName     = "openbao"
	openbaoCertificateName = "openbao-tls"
)

// openbaoInstalled reports whether the OpenBao release is present on
// the management cluster. Used to skip the entire Describe when
// running on minimal bootstraps that do not deploy OpenBao.
//
// Pattern from journey_velero_dr_test.go: check for actual CRD
// registration rather than namespace existence alone (namespace
// may exist as Helmfile artifact even when chart `installed: false`).
//
// BaoConfiguration CRD is registered by Bank-Vaults operator first
// reconcile — if absent, neither operator nor instance is up.
func openbaoInstalled() bool {
	cmd := kubectlForCluster(ClusterMgmt,
		"get", "crd", "baoconfigurations.bao.banzaicloud.io",
		"--no-headers", "-o", "name")
	return cmd.Run() == nil
}

// baoCLIAvailable returns true when `bao` binary is on PATH.
func baoCLIAvailable() bool {
	_, err := exec.LookPath("bao")
	return err == nil
}

// openbaoNs builds a kubectl command scoped to secrets-system.
func openbaoNs(args ...string) *exec.Cmd {
	return kubectlForCluster(ClusterMgmt,
		append([]string{"-n", openbaoNamespace}, args...)...)
}

// bankVaultsNs builds a kubectl command scoped to bank-vaults-operator.
func bankVaultsNs(args ...string) *exec.Cmd {
	return kubectlForCluster(ClusterMgmt,
		append([]string{"-n", bankVaultsOperatorNs}, args...)...)
}

var _ = Describe("Journey: OpenBao Smoke (F1.10)", Label("journey"), func() {
	BeforeEach(func() {
		skipIfNotE2E()

		if !openbaoInstalled() {
			Skip("OpenBao not installed (baoconfigurations.bao.banzaicloud.io CRD absent) — " +
				"set openbaoEnabled=true in helmfile env values + sync `helmfile -l name=openbao sync`")
		}
	})

	It("OB-1: bank-vaults-operator pod is Running", func() {
		Eventually(func() string {
			cmd := bankVaultsNs("get", "pods",
				"-l", "app.kubernetes.io/name=bank-vaults",
				"-o", "jsonpath={.items[0].status.phase}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 2*time.Minute, 5*time.Second).Should(
			Equal("Running"),
			"bank-vaults-operator pod must be Running in %s", bankVaultsOperatorNs,
		)
	})

	It("OB-2: openbao pod(s) are Running in secrets-system", func() {
		Eventually(func() string {
			cmd := openbaoNs("get", "pods",
				"-l", "app.kubernetes.io/name=openbao",
				"-o", "jsonpath={.items[*].status.phase}")
			out, _ := cmd.CombinedOutput()
			// Single-pod standalone: "Running"
			// Multi-pod prod: "Running Running Running"
			phases := strings.Fields(strings.TrimSpace(string(out)))
			if len(phases) == 0 {
				return "no-pods"
			}
			for _, p := range phases {
				if p != "Running" {
					return p
				}
			}
			return "Running"
		}, 3*time.Minute, 5*time.Second).Should(
			Equal("Running"),
			"openbao pod(s) must all be Running in %s", openbaoNamespace,
		)
	})

	It("OB-3: openbao Service exposes port 8200", func() {
		cmd := openbaoNs("get", "svc", openbaoServiceName,
			"-o", "jsonpath={.spec.ports[?(@.name==\"api\")].port}")
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(),
			"kubectl get svc openbao must succeed: %s", string(out))
		Expect(strings.TrimSpace(string(out))).To(
			Equal("8200"),
			"Service openbao must expose port 8200 named 'api'",
		)
	})

	It("OB-4: cert-manager Certificate openbao-tls is Ready", func() {
		Eventually(func() string {
			cmd := openbaoNs("get", "certificate", openbaoCertificateName,
				"-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}")
			out, _ := cmd.CombinedOutput()
			return strings.TrimSpace(string(out))
		}, 2*time.Minute, 5*time.Second).Should(
			Equal("True"),
			"Certificate openbao-tls must become Ready (self-signed-issuer)",
		)
	})

	It("OB-5: NetworkPolicy openbao-allow-egress has port 443 rule (EC-01)", func() {
		// Critical: without HTTPS egress, AWS KMS unseal fails in prod
		cmd := openbaoNs("get", "networkpolicy", "openbao-allow-egress",
			"-o", "jsonpath={.spec.egress[*].ports[*].port}")
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		ports := strings.Fields(strings.TrimSpace(string(out)))
		Expect(ports).To(ContainElement("443"),
			"NetworkPolicy egress must allow port 443 (AWS KMS + DO Spaces); without it, "+
				"prod boot loops dia 1 (EC-01 P0)")
	})

	It("OB-6: NetworkPolicy openbao-allow-ingress includes loopruntime-system", func() {
		// Broker pattern Q6: loopruntime-api must reach OpenBao
		cmd := openbaoNs("get", "networkpolicy", "openbao-allow-ingress",
			"-o", "jsonpath={.spec.ingress[*].from[*].namespaceSelector.matchLabels}")
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(ContainSubstring("loopruntime-system"),
			"NetworkPolicy ingress must allow loopruntime-system namespace (broker pattern Q6)")
	})

	It("OB-7: ValidatingAdmissionPolicy openbao-no-devmode-in-prod exists (EC-04)", func() {
		cmd := kubectlForCluster(ClusterMgmt,
			"get", "validatingadmissionpolicy", "openbao-no-devmode-in-prod",
			"--no-headers", "-o", "name")
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(),
			"VAP openbao-no-devmode-in-prod must exist (EC-04 B33 pattern): %s", string(out))
	})

	It("OB-8: BaoConfiguration CR openbao exists in secrets-system", func() {
		cmd := openbaoNs("get", "baoconfiguration", openbaoReleaseName,
			"--no-headers", "-o", "name")
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(),
			"BaoConfiguration CR openbao must exist in secrets-system: %s", string(out))
	})

	It("OB-9: bao status returns Initialized=true + Sealed=false (dev-mode)", func() {
		if !baoCLIAvailable() {
			Skip("bao CLI not in PATH — skipping bao status check; install from openbao.org")
		}

		// Port-forward in background for the duration of the spec
		pfCmd := openbaoNs("port-forward", "svc/"+openbaoServiceName, "8200:8200")
		err := pfCmd.Start()
		Expect(err).ToNot(HaveOccurred(), "kubectl port-forward must start")
		defer func() {
			_ = pfCmd.Process.Kill()
			_ = pfCmd.Wait()
		}()

		// Wait for port-forward to be ready
		// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
		time.Sleep(3 * time.Second)

		Eventually(func() string {
			cmd := exec.Command("bao", "status", "-format=json")
			cmd.Env = append(cmd.Env,
				"VAULT_ADDR=http://127.0.0.1:8200",
				"VAULT_SKIP_VERIFY=true",
			)
			out, _ := cmd.CombinedOutput()
			text := string(out)
			// dev-mode: Initialized=true Sealed=false
			if strings.Contains(text, "\"initialized\":true") &&
				strings.Contains(text, "\"sealed\":false") {
				return "OK"
			}
			return strings.TrimSpace(text)
		}, 30*time.Second, 2*time.Second).Should(
			Equal("OK"),
			"bao status must report Initialized=true + Sealed=false in dev-mode standalone",
		)
	})

	It("OB-10: kv-v2 engine enabled at path secret/", func() {
		if !baoCLIAvailable() {
			Skip("bao CLI not in PATH — skipping engine check")
		}

		// Re-use port-forward pattern from OB-9
		pfCmd := openbaoNs("port-forward", "svc/"+openbaoServiceName, "8200:8200")
		err := pfCmd.Start()
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			_ = pfCmd.Process.Kill()
			_ = pfCmd.Wait()
		}()
		// intentional-sleep: poll-until-state-transition (M23-T3.2 follow-up: candidate for PollUntil replacement)
		time.Sleep(3 * time.Second)

		Eventually(func() string {
			cmd := exec.Command("bao", "secrets", "list", "-format=json")
			cmd.Env = append(cmd.Env,
				"VAULT_ADDR=http://127.0.0.1:8200",
				"VAULT_TOKEN=loopruntime-dev-vault-root", // dev-mode well-known
				"VAULT_SKIP_VERIFY=true",
			)
			out, _ := cmd.CombinedOutput()
			return string(out)
		}, 30*time.Second, 2*time.Second).Should(
			ContainSubstring("\"secret/\""),
			"kv-v2 engine must be enabled at path secret/ per F1.4 BaoConfig scope",
		)
	})
})
