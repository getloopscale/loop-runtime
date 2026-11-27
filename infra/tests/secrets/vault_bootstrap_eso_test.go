// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"
)

// vault-bootstrap-eso.sh (Runbook P1 of plan 2026-06-02-loop-runtime-api-vault-integration)
// applies the ESO reader ServiceAccount, copies the OpenBao CA from the secrets
// cluster into a ConfigMap on core, applies the ClusterSecretStore and waits for it.
// Each step has its own exit code, and the CA copy is the step that fails in a way
// the operator cannot see: an empty or non-PEM CA produces a store that never
// becomes Ready.

const esoBootstrapScript = openbaoScripts + "/vault-bootstrap-eso.sh"

// caFixture is PEM-shaped so the script's header check passes; its body is not a
// certificate and does not pretend to be one.
const caFixture = "-----BEGIN CERTIFICATE-----\nfixture-not-a-real-certificate\n-----END CERTIFICATE-----\n"

func init() {
	registerFakes("vault-bootstrap-eso", map[string]fakeTool{"kubectl": esoKubectl})
}

func esoKubectl(args []string) int {
	switch {
	case argsMatch(args, "version --request-timeout"):
		return 0
	case argsMatch(args, "get secret", "openbao-tls", "-o jsonpath"):
		if code := envExit("FAKE_GET_SECRET_EXIT"); code != 0 {
			fmt.Fprintln(os.Stderr, `Error from server (NotFound): secrets "openbao-tls" not found`)
			return code
		}
		ca := os.Getenv("FAKE_CA_BASE64")
		if ca == "" {
			ca = base64.StdEncoding.EncodeToString([]byte(caFixture))
		}
		fmt.Println(ca)
		return 0
	case argsMatch(args, "create namespace"):
		return 0
	case argsMatch(args, "create configmap", "--dry-run=client"):
		drainStdin() // the script pipes the CA in through /dev/stdin
		fmt.Println("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: loop-runtime-secrets-ca")
		return 0
	case argsMatch(args, "apply -f -"):
		drainStdin()
		return envExit("FAKE_APPLY_EXIT")
	case argsMatch(args, "apply -f"):
		return envExit("FAKE_APPLY_EXIT")
	case argsMatch(args, "wait clustersecretstore"):
		return envExit("FAKE_WAIT_EXIT")
	default:
		return 0
	}
}

// esoManifests writes the two manifests the script requires and returns their directory.
func esoManifests(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, "eso-vault-reader-sa.yaml",
		"apiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: eso-vault-reader\n  namespace: loop-runtime-system\n")
	writeFixture(t, dir, "loop-runtime-secrets-secrets.yaml",
		"apiVersion: external-secrets.io/v1beta1\nkind: ClusterSecretStore\nmetadata:\n  name: loop-runtime-secrets-secrets\n")
	return dir
}

func runESOBootstrap(t *testing.T, manifestDir string, env []string, args ...string) scriptRun {
	t.Helper()
	return runScript(t, esoBootstrapScript, "vault-bootstrap-eso", env, append([]string{
		"--mgmt-context", "k3d-mock-mgmt",
		"--vault-context", "k3d-mock-vault",
		"--manifest-dir", manifestDir,
	}, args...)...)
}

func TestVaultBootstrapESORejectsANonIntegerWaitTimeout(t *testing.T) {
	r := runESOBootstrap(t, esoManifests(t), nil, "--wait-timeout", "abc")
	expectExit(t, r, 1, "wait-timeout must be")
}

func TestVaultBootstrapESORefusesAMissingManifest(t *testing.T) {
	r := runESOBootstrap(t, t.TempDir(), nil)
	expectExit(t, r, 1, "manifest not found")
}

func TestVaultBootstrapESODryRunNamesAllFourSteps(t *testing.T) {
	r := runESOBootstrap(t, esoManifests(t), []string{"SKIP_PREFLIGHT=1"}, "--dry-run")
	expectExit(t, r, 0, "STEP 1/4", "STEP 2/4", "STEP 3/4", "STEP 4/4", "DRY-RUN COMPLETE")
}

func TestVaultBootstrapESOExitsTwoWhenTheCASecretIsMissing(t *testing.T) {
	r := runESOBootstrap(t, esoManifests(t), []string{"SKIP_PREFLIGHT=0", "FAKE_GET_SECRET_EXIT=1"},
		"--wait-timeout", "5")
	expectExit(t, r, 2, "CA cert empty")
}

func TestVaultBootstrapESOExitsTwoWhenTheCAIsNotPEM(t *testing.T) {
	notPEM := base64.StdEncoding.EncodeToString([]byte("invalid certificate content\n"))
	r := runESOBootstrap(t, esoManifests(t), []string{"SKIP_PREFLIGHT=0", "FAKE_CA_BASE64=" + notPEM},
		"--wait-timeout", "5")
	expectExit(t, r, 2, "PEM header")
}

func TestVaultBootstrapESOCompletesOnTheHappyPath(t *testing.T) {
	r := runESOBootstrap(t, esoManifests(t), []string{"SKIP_PREFLIGHT=0"}, "--wait-timeout", "5")
	expectExit(t, r, 0, "ESO ClusterSecretStore bootstrapped")
}

// A store that never becomes Ready has its own exit code: the manifests applied,
// and what failed is the store reaching OpenBao.
func TestVaultBootstrapESOExitsThreeWhenTheStoreNeverBecomesReady(t *testing.T) {
	r := runESOBootstrap(t, esoManifests(t), []string{"SKIP_PREFLIGHT=0", "FAKE_WAIT_EXIT=1"},
		"--wait-timeout", "5")
	expectExit(t, r, 3, "did NOT reach Ready")
}

func TestVaultBootstrapESOExitsTwoWhenTheManifestApplyFails(t *testing.T) {
	r := runESOBootstrap(t, esoManifests(t), []string{"SKIP_PREFLIGHT=0", "FAKE_APPLY_EXIT=1"},
		"--wait-timeout", "5")
	expectExit(t, r, 2, "kubectl apply")
}
