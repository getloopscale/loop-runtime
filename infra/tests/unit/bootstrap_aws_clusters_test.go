// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// infra/helmfile/scripts/bootstrap-aws-clusters.sh post-core runs against a fake kubectl,
// a fake go toolchain and recording copies of the lib/ scripts it calls.

// bootstrapRun runs the post-core phase in a copy of the scripts tree and returns, one per
// line, every lib/ script call it made ("<script> <args...>").
func bootstrapRun(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	scripts := filepath.Join(root, "infra", "helmfile", "scripts")
	lib := filepath.Join(scripts, "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "loopzero-ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "helmfile", "scripts", "bootstrap-aws-clusters.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(scripts, "bootstrap-aws-clusters.sh"), string(src))

	calls := filepath.Join(root, "calls.log")
	recorder := "#!/usr/bin/env bash\necho \"$(basename \"$0\") $*\" >>" + calls + "\n"
	for _, name := range []string{"argocd-register.sh", "clustercredential-create.sh"} {
		writeExec(t, filepath.Join(lib, name), recorder)
	}
	// scoped-kubeconfig.sh <cluster> <admin> <out>: writes the kubeconfig it is asked for.
	writeExec(t, filepath.Join(lib, "scoped-kubeconfig.sh"), "#!/usr/bin/env bash\necho scoped >\"$3\"\n")

	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(bin, "kubectl"), "#!/usr/bin/env bash\ncat >/dev/null 2>&1 || true\necho apiVersion: v1\n")
	// go build -o <bin> .: leaves a loopzero-ops that does nothing.
	writeExec(t, filepath.Join(bin, "go"), "#!/usr/bin/env bash\nwhile [[ $# -gt 0 ]]; do if [[ $1 == -o ]]; then printf '#!/usr/bin/env bash\\n' >\"$2\"; chmod +x \"$2\"; fi; shift; done\n")

	cmd := exec.Command("bash", filepath.Join(scripts, "bootstrap-aws-clusters.sh"), "post-core")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "CELL_CONTEXTS=cell-0 cell-1")
	cmd.Stdin = strings.NewReader("")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap-aws-clusters.sh post-core: %v\n%s", err, out)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// Each cell is registered in Argo CD by the clustercredential controller from its
// ClusterCredential. A second registration by argocd-register.sh gives Argo CD two clusters
// with the same name, and no Application can resolve its destination.
func TestEveryCellIsRegisteredInArgoCDOnlyThroughItsClusterCredential(t *testing.T) {
	calls := bootstrapRun(t)
	for _, cell := range []string{"cell-0", "cell-1"} {
		var credential, register int
		for _, c := range calls {
			f := strings.Fields(c)
			if len(f) < 2 || f[1] != cell {
				continue
			}
			switch f[0] {
			case "clustercredential-create.sh":
				credential++
			case "argocd-register.sh":
				register++
			}
		}
		if credential != 1 || register != 0 {
			t.Errorf("%s: %d ClusterCredential and %d argocd-register.sh calls, want 1 and 0; calls:\n%s",
				cell, credential, register, strings.Join(calls, "\n"))
		}
	}
}

// The build cluster has no ClusterCredential, so argocd-register.sh stays its only registration.
func TestTheBuildClusterIsRegisteredInArgoCDByTheBootstrap(t *testing.T) {
	calls := bootstrapRun(t)
	for _, c := range calls {
		if strings.HasPrefix(c, "argocd-register.sh build ") {
			return
		}
	}
	t.Errorf("no argocd-register.sh call for build; calls:\n%s", strings.Join(calls, "\n"))
}

// writeExec writes an executable file.
func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The broker-tokens phase seeds one secret-fetch broker token per cell (`loopzero-ops
// seed broker`) through the contexts this script names after the cell ids, not through
// the k3d ones the seed defaults to.
func TestBrokerTokensAreSeededThroughContextsNamedAfterTheCells(t *testing.T) {
	root := t.TempDir()
	scripts := filepath.Join(root, "infra", "helmfile", "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "loopzero-ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "helmfile", "scripts", "bootstrap-aws-clusters.sh"))
	if err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(scripts, "bootstrap-aws-clusters.sh"), string(src))
	calls := filepath.Join(root, "calls.log")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// go build -o <bin> .: leaves a loopzero-ops that records its arguments and whether
	// LOOPZERO_CELL_CONTEXT_PREFIX was set, and to what.
	opsRecorder := `#!/usr/bin/env bash
echo "loopzero-ops prefix=${LOOPZERO_CELL_CONTEXT_PREFIX-unset} core=${CORE_CONTEXT-unset} $*" >>` + calls + `
`
	opsFile := filepath.Join(root, "ops-recorder")
	if err := os.WriteFile(opsFile, []byte(opsRecorder), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(bin, "go"), "#!/usr/bin/env bash\nwhile [[ $# -gt 0 ]]; do if [[ $1 == -o ]]; then cp "+opsFile+" \"$2\"; chmod +x \"$2\"; fi; shift; done\n")

	cmd := exec.Command("bash", filepath.Join(scripts, "bootstrap-aws-clusters.sh"), "broker-tokens")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap-aws-clusters.sh broker-tokens: %v\n%s", err, out)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "loopzero-ops prefix= core=core seed broker" {
		t.Errorf("broker-tokens ran %q, want `seed broker` with LOOPZERO_CELL_CONTEXT_PREFIX set empty and CORE_CONTEXT=core", got)
	}
}
