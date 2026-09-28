// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// clustercredential-create.sh is the one constructor of a cell's ClusterCredential +
// kubeconfig Secret. The local and drill bootstrap libraries used to build their own
// heredoc copies, and the copies disagreed: the drill copy labelled with
// loopzero.dev/cell-id + loopzero.dev/drill instead of loopzero.io/cell (which the multi-cell
// runbook selects on) and set no spec.tls. These tests drive the constructor in
// DRY_RUN mode — it renders instead of applying — so they need no cluster.

const ccCreate = "../../helmfile/scripts/lib/clustercredential-create.sh"

func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	body := "apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: " + server + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return p
}

func renderCC(t *testing.T, env []string, args ...string) (map[string]map[string]any, string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{ccCreate}, args...)...)
	cmd.Env = append(os.Environ(), append([]string{"DRY_RUN=true"}, env...)...)
	out, err := cmd.Output()
	docs := map[string]map[string]any{}
	dec := yaml.NewDecoder(strings.NewReader(string(out)))
	for {
		var d map[string]any
		if decErr := dec.Decode(&d); decErr != nil {
			break
		}
		if d != nil {
			docs[d["kind"].(string)] = d
		}
	}
	return docs, string(out), err
}

func field(t *testing.T, doc map[string]any, path ...string) any {
	t.Helper()
	var cur any = doc
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %q is not a mapping", path, p)
		}
		cur = m[p]
	}
	return cur
}

func TestClusterCredentialCreateRendersTheCanonicalLabelSet(t *testing.T) {
	// Arrange
	kc := writeKubeconfig(t, "https://10.0.0.5:6443")

	// Act
	docs, out, err := renderCC(t, []string{
		"INGRESS_DOMAIN=loopzero.local", "TLS_INSECURE=true", "MANAGED_BY=bootstrap-cell-cc",
	}, "cell-0", "cell-0", kc)

	// Assert
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	cc, ok := docs["ClusterCredential"]
	if !ok {
		t.Fatalf("no ClusterCredential rendered:\n%s", out)
	}
	labels := field(t, cc, "metadata", "labels").(map[string]any)
	want := map[string]string{
		"loopzero.io/cell":                 "cell-0",
		"loopzero.io/ingress-domain":       "loopzero.local",
		"app.kubernetes.io/managed-by": "bootstrap-cell-cc",
	}
	for k, v := range want {
		if labels[k] != v {
			t.Errorf("label %s = %v, want %s", k, labels[k], v)
		}
	}
	if field(t, cc, "spec", "tls", "insecure") != true {
		t.Errorf("spec.tls.insecure = %v, want true", field(t, cc, "spec", "tls", "insecure"))
	}
	if field(t, cc, "spec", "apiServer") != "https://10.0.0.5:6443" {
		t.Errorf("spec.apiServer = %v, want the kubeconfig server", field(t, cc, "spec", "apiServer"))
	}
	if _, ok := docs["Secret"]; !ok {
		t.Errorf("no kubeconfig Secret rendered")
	}
}

func TestClusterCredentialCreateHonoursAnExplicitAPIServer(t *testing.T) {
	// Arrange — drill reads the endpoint from a stack output, not from the kubeconfig.
	kc := writeKubeconfig(t, "https://internal:6443")

	// Act
	docs, out, err := renderCC(t, []string{"API_SERVER=https://abc.k8s.ondigitalocean.com"},
		"cell-1", "cell-1", kc)

	// Assert
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	if got := field(t, docs["ClusterCredential"], "spec", "apiServer"); got != "https://abc.k8s.ondigitalocean.com" {
		t.Errorf("spec.apiServer = %v, want the explicit API_SERVER", got)
	}
}

func TestClusterCredentialCreateRejectsAnInvalidTLSInsecureValue(t *testing.T) {
	// Arrange
	kc := writeKubeconfig(t, "https://10.0.0.5:6443")

	// Act
	_, out, err := renderCC(t, []string{"TLS_INSECURE=yes"}, "cell-0", "cell-0", kc)

	// Assert — a usage error (exit 1) naming the value, not "fail: command not found" (127).
	var exitErr *exec.ExitError
	if err == nil {
		t.Fatalf("TLS_INSECURE=yes was accepted:\n%s", out)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		exitErr = ee
	}
	if exitErr == nil || exitErr.ExitCode() != 1 {
		t.Fatalf("exit = %v, want usage error 1", err)
	}
	if !strings.Contains(string(exitErr.Stderr), "TLS_INSECURE must be 'true' or 'false'") {
		t.Errorf("stderr does not name the bad value: %s", exitErr.Stderr)
	}
}

func TestDrillCellBootstrapLabelsTheCredentialWithTheCanonicalCellLabel(t *testing.T) {
	// Arrange — the drill library, fed a stack output fixture, in dry-run mode.
	for _, tool := range []string{"jq", "npx", "kubectl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is a prerequisite of the drill bootstrap and is not on PATH", tool)
		}
	}
	mgmt := writeKubeconfig(t, "https://mgmt:6443")
	fixture := `{"runtimeClusterEndpoint":"https://abc.k8s.ondigitalocean.com",` +
		`"runtimeClusterKubeconfig":"apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: https://abc.k8s.ondigitalocean.com\n"}`
	script := `source ../../scripts/lib/bootstrap-cell-cc-drill-common.sh && bootstrap_cell_cc_drill cell-0 runtimeCluster --dry-run`
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "DRILL_KUBECONFIG="+mgmt, "MOCK_PULUMI_JSON="+fixture)

	// Act
	out, err := cmd.Output()

	// Assert
	if err != nil {
		t.Fatalf("drill dry run failed: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{"loopzero.io/cell: cell-0", "app.kubernetes.io/managed-by: bootstrap-cell-cc-drill",
		"apiServer: https://abc.k8s.ondigitalocean.com", "kind: ClusterCredential"} {
		if !strings.Contains(s, want) {
			t.Errorf("drill render lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "loopzero.dev/") {
		t.Errorf("drill render still carries a loopzero.dev/ label:\n%s", s)
	}
}
