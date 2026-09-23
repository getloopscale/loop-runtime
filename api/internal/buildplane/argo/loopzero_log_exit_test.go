// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"os/exec"
	"strings"
	"testing"

	forge "github.com/getloopscale/loopzero-forge"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
)

// runInContainerShell executes the wrapped script with `sh`, which is what the build
// container runs (`Command: []string{"sh", "-c"}`). Asserting in bash would not measure
// the shell that actually runs in production.
func runInContainerShell(t *testing.T, script string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			return ee.ExitCode()
		}
		t.Fatalf("execução falhou por outro motivo: %v", err)
	}
	return 0
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// The step's exit status MUST survive the log-marker wrapper.
//
// Measured 2026-08-18 (B-153): the wrapper ran the script inside `{ ... } | tee`, whose
// POSIX status is tee's — always 0. Every step built by buildContainer reported success
// no matter what it did, which made the fail-closed path for an unsupported runtime inert
// and turned a legible error into `Failed to build manifests with custom domains`.
func TestWrapper_PropagatesNonZeroExit(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("generate-dockerfile", `echo "boom" >&2
exit 1`)

	if rc := runInContainerShell(t, wrapped); rc == 0 {
		t.Fatal("um passo que sai 1 reportou 0 — o status morreu no pipeline do tee")
	}
}

// The exact code matters: 2 is not 1, and a wrapper that normalizes every failure to 1
// loses the distinction the caller relied on.
func TestWrapper_PreservesTheExactCode(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("build-apps", "exit 3")

	if rc := runInContainerShell(t, wrapped); rc != 3 {
		t.Fatalf("esperava código 3 preservado, veio %d", rc)
	}
}

// A step that succeeds must still succeed — the fix must not invert the failure.
func TestWrapper_KeepsSuccessSuccessful(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("clone-repo", "echo ok")

	if rc := runInContainerShell(t, wrapped); rc != 0 {
		t.Fatalf("passo bem-sucedido reprovou com %d", rc)
	}
}

// The markers are the reason the wrapper exists; propagating the status must not cost them.
func TestWrapper_StillEmitsMarkersOnFailure(t *testing.T) {
	wrapped := wrapWithLoopZeroMarkers("install-deps", `echo "detalhe do erro" >&2
exit 1`)

	out, _ := exec.Command("sh", "-c", wrapped).CombinedOutput()
	s := string(out)
	if !strings.Contains(s, "level=error") {
		t.Fatalf("marcador de erro ausente na saída:\n%s", s)
	}
	// Exigir o texto DENTRO de uma linha de marcador. A revisao provou que a assercao
	// anterior passava mesmo com o replay desabilitado: a string chegava pelo stderr cru
	// via tee, nao pelo marcador que o usuario realmente le no painel.
	if !strings.Contains(s, `level=error msg="detalhe do erro"`) {
		t.Fatalf("a última saída do passo deve chegar como marcador, não só no fluxo cru:\n%s", s)
	}
}

// The population: every step the strategies build for an unsupported runtime is a
// fail-closed step, and all five were inert. rules/testing.md § 4.2 — a synthetic case
// proves the wrapper propagates; this proves the real construction path does.
func TestUnsupportedRuntimeStepsActuallyFail(t *testing.T) {
	s := &unsupportedStrategy{runtime: "unsupported"}
	steps := map[string]*forge.Container{
		"validate-workspace":  s.ValidateStep(nil),
		"install-deps":        s.InstallStep(nil),
		"build-apps":          s.BuildStep(nil),
		"security-scan":       s.ScanStep(nil),
		"generate-dockerfile": s.DockerfileStep(nil, 1),
	}
	for name, c := range steps {
		if len(c.Args) == 0 {
			t.Fatalf("%s: container sem script", name)
		}
		if rc := runInContainerShell(t, c.Args[len(c.Args)-1]); rc == 0 {
			t.Errorf("%s: passo de falha-fechada reportou sucesso", name)
		}
	}
}

// The real population (rules/testing.md § 4.2). The unsupported-runtime steps above are the
// SAME script five times — a synthetic case repeated, not a population. These are the steps
// the shipping strategies actually build.
//
// This is the assertion that would have caught it: scripts.go writes `set -uo pipefail` by
// hand while the sanitizer only replaced the literal `set -euo pipefail`, so that form
// reached dash (node:22.12.0 is Debian) and killed the shell on line 1 with `Illegal option
// -o pipefail`. The tee hid it; propagating the status would have failed every Node build.
//
// The check is textual on purpose. Executing these scripts would run real installs and
// audits — slow, networked and non-deterministic — to observe a defect that is entirely
// visible in the emitted text.
func TestRealStrategySteps_EmitNoUnguardedPipefail(t *testing.T) {
	apps := []deploycontracts.AppSpec{{Name: "api", Type: "server", Port: 8000, Start: "python main.py", Runtime: "python"}}
	for _, tc := range []struct {
		name string
		s    LanguageStrategy
	}{
		{"node", &NodeStrategy{}},
		{"python", &PythonStrategy{}},
	} {
		steps := map[string]*forge.Container{
			"validate-workspace":  tc.s.ValidateStep(apps),
			"install-deps":        tc.s.InstallStep(apps),
			"build-apps":          tc.s.BuildStep(apps),
			"security-scan":       tc.s.ScanStep(apps),
			"generate-dockerfile": tc.s.DockerfileStep(apps, 1),
		}
		for step, c := range steps {
			if len(c.Args) == 0 {
				t.Fatalf("%s/%s: container sem script", tc.name, step)
			}
			for _, line := range strings.Split(c.Args[len(c.Args)-1], "\n") {
				trimmed := strings.TrimSpace(line)
				// Comentario nao executa. Sem isto o proprio comentario do invólucro,
				// que descreve o defeito, dispara o detector — a mesma armadilha que o
				// gate runbook-parity ja pagou nesta base.
				if strings.HasPrefix(trimmed, "#") {
					continue
				}
				if !strings.Contains(trimmed, "o pipefail") {
					continue
				}
				// A unica forma tolerada e a tentativa protegida em subshell, que o dash
				// simplesmente ignora em vez de morrer.
				if strings.HasPrefix(trimmed, "(set -o pipefail)") {
					continue
				}
				t.Errorf("%s/%s: %q chega cru na concha do container — o dash mata a concha aqui",
					tc.name, step, trimmed)
			}
		}
	}
}

// The guarded form must survive sanitisation, and the other flags with it — dropping `-u`
// while removing pipefail would silently disable a protection the script asked for.
func TestSanitizer_PreservesOtherFlags(t *testing.T) {
	out := wrapWithLoopZeroMarkers("build-apps", "set -uo pipefail\necho ok")

	if !strings.Contains(out, "set -u") {
		t.Fatalf("a opcao -u foi perdida na sanitizacao:\n%s", out)
	}
	if !strings.Contains(out, "(set -o pipefail) 2>/dev/null || true") {
		t.Fatalf("a tentativa protegida de pipefail sumiu:\n%s", out)
	}
}

// The recorded status must be validated before it becomes the shell's exit code.
//
// Review 2026-08-18: `cat` of an EMPTY file exits 0, so the `|| echo 1` fallback never
// fired and the wrapper ran `exit ""` — which the shell rejects with 2. Fail-closed either
// way, but not the value the code claimed, and someone debugging a 2 would hunt for a step
// that returned 2.
//
// Asserted on the emitted text rather than by execution: the state only arises when a
// signal kills the shell mid-flight, and contriving that here would test the harness, not
// the guard. What is verifiable is that the guard is present and normalises to 1.
func TestWrapper_GuardsAgainstAStatusThatIsNotANumber(t *testing.T) {
	out := wrapWithLoopZeroMarkers("build-apps", "echo ok")

	if !strings.Contains(out, "*[!0-9]*") {
		t.Fatalf("sem guarda para status vazio ou nao-numerico:\n%s", out)
	}
	if !strings.Contains(out, "__loopzero_status=1") {
		t.Fatalf("a guarda deve normalizar para o fechamento declarado (1):\n%s", out)
	}
}

// The path of the status file and the status itself must not share a name. They did, and
// it worked only because the trap happens to run last.
func TestWrapper_StatusFileAndStatusAreDistinctNames(t *testing.T) {
	out := wrapWithLoopZeroMarkers("build-apps", "echo ok")

	if strings.Contains(out, `__loopzero_rc=$(mktemp`) && strings.Contains(out, "__loopzero_rc=$?") {
		t.Fatal("__loopzero_rc designa o caminho do arquivo E o status — um exit antecipado quebra o outro em silêncio")
	}
}
