// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/events"
)

// Plan J-17 Layer 2 (2026-06-06) — pipeline emits structured logs
// intended for user view.
//
// Why: the dashboard's deploy detail "Logs" tab used to show every
// stdout/stderr line from every container, including Argo executor
// banners (level=INFO msg="Starting Workflow Executor" goVersion=...
// argo=true version=v4.0.3) and other infrastructure noise. That's
// (a) UX-noise users don't understand and (b) version disclosure that
// helps attackers target CVEs against our exact Argo/Go versions.
//
// How: each pipeline step wraps its main script with `::loopzero::`
// prefixed markers that Promtail extracts into a `loopzero_phase` label
// (see promtail relabel config). The API's default LogQL filter
// `{loopzero_phase=~".+"}` then shows only labeled lines; debug mode
// (`?mode=debug`, admin-only) shows raw output.
//
// Marker format (mirrors Mocha/GitHub Actions style for grep-friendly
// patterns, no JSON parsing needed in bash):
//
//	::loopzero:: phase=<group> step=<name> level=<info|warn|error> msg="..."
//
// Phase is derived via events.PhaseGroupForStep — same mapping the
// frontend BuildTimelineLive uses to group steps into Clone / Install /
// Build / Sign / Deliver / Other. Keeping the mapping in api/internal/events as
// the single source of truth avoids drift between pipeline and UI.

// pipefailRE casa qualquer forma de set -<flags>o pipefail, nao so a literal do shellgen.
//
// B-153 (revisao): o saneador substituia a string exata "set -euo pipefail". Mas scripts.go
// escreve "set -uo pipefail" a mao, sem o e, e essa forma escapava. A imagem do Node e
// Debian, cujo /bin/sh e dash, e set e builtin especial: a opcao ilegal MATA a concha na
// primeira linha com codigo 2. Enquanto o status morria no cano do tee isso ficava
// invisivel; ao propagar o status, todo build Node passaria a reprovar no security-scan —
// trocando "nenhum passo consegue falhar" por "nenhum build Node consegue passar".
var pipefailRE = regexp.MustCompile(`set -([a-zA-Z]*)o pipefail`)

// expandPipefail rewrites one match into the POSIX-safe equivalent, preserving the other
// flags. The pipefail attempt stays guarded because dash simply does not have it.
func expandPipefail(match string) string {
	m := pipefailRE.FindStringSubmatch(match)
	if m == nil {
		return match
	}
	var b strings.Builder
	for _, f := range m[1] {
		fmt.Fprintf(&b, "set -%c\n", f)
	}
	b.WriteString("(set -o pipefail) 2>/dev/null || true")
	return b.String()
}

// wrapWithLoopZeroMarkers wraps a bash script with start/success/error
// markers that Promtail extracts into loopzero_phase + loopzero_step + loopzero_level
// labels.
//
// The wrapper preserves the original script's exit code semantics AND
// captures its combined stdout/stderr to a temp log so that:
//   - Start marker prints unconditionally.
//   - Original script runs through a `tee` pipe — every line goes both
//     to the container stdout (where Promtail still picks it up for
//     debug mode) AND to a temp file so the trap can replay context.
//   - On success (exit 0): a "completed in Ns" marker prints (with the
//     measured duration so the LogsTab can show step timings).
//   - On failure (exit != 0): a "failed (exit N, ran Ns)" header marker
//     prints, then the LAST 50 LINES of the script's combined output
//     are replayed as `level=error` markers. Each replayed line is
//     truncated to 400 chars and has `"` + `\` escaped so it survives
//     the marker regex parser (Layer 4). The user finally sees WHY the
//     build failed in the LogsTab without needing operator debug mode.
//
// O status do passo NAO depende de pipefail. Ele e capturado dentro do bloco, antes do
// cano, e o invólucro termina com esse valor.
//
// Este paragrafo dizia o contrario ate 2026-08-18: que pipefail era o que fazia o status
// atravessar o cano para o tee, e que a nao-propagacao em dash puro era "rare in our
// stack". As duas afirmacoes eram falsas, e da forma mais cara possivel — a nao-propagacao
// era 100% dos passos, porque a tentativa de pipefail estava entre parenteses e morria com
// o subshell. Medido ao vivo em B-153: cinco passos de falha-fechada reportaram Succeeded.
//
// A tentativa protegida continua, agora pelo que de fato vale: onde a concha suporta
// pipefail, um comando falho no meio de um cano DENTRO do script do usuario e detectado.
func wrapWithLoopZeroMarkers(stepName, script string) string {
	phase := events.PhaseGroupForStep(stepName)
	display := humanizeStepName(stepName)

	// Canonical 2026-06-16 (dash-compat shim): shellgen.SetE emits
	// `set -euo pipefail` which is NOT POSIX — Debian's /bin/sh (dash)
	// errors with exit 2 ("illegal option") on `set -o pipefail`. The
	// canonical solution would be to make shellgen emit POSIX-compatible
	// strict-mode (`set -e` + `set -u` + graceful pipefail). Since the
	// shellgen package is outside the current scope, transform the inner
	// script here to swap the offending `set -euo pipefail` line for the
	// dash-compatible 3-line equivalent. Alpine ash + bash continue to
	// work because they accept all three forms.
	script = pipefailRE.ReplaceAllStringFunc(script, expandPipefail)

	return fmt.Sprintf(`echo '::loopzero:: phase=%[1]s step=%[2]s level=info msg="%[3]s starting"'
__loopzero_start=$(date +%%s)
__loopzero_log=$(mktemp 2>/dev/null || echo /tmp/loopzero-step.$$.log)
__loopzero_rc_file=$(mktemp 2>/dev/null || echo /tmp/loopzero-step.$$.rc)
: > "$__loopzero_log"
__loopzero_on_exit() {
  __loopzero_rc=$?
  __loopzero_dur=$(( $(date +%%s) - __loopzero_start ))
  if [ "$__loopzero_rc" -eq 0 ]; then
    printf '::loopzero:: phase=%[1]s step=%[2]s level=info msg="%[3]s completed in %%ds"\n' "$__loopzero_dur"
  else
    printf '::loopzero:: phase=%[1]s step=%[2]s level=error msg="%[3]s failed (exit %%d after %%ds) — last output:"\n' "$__loopzero_rc" "$__loopzero_dur"
    tail -50 "$__loopzero_log" 2>/dev/null | while IFS= read -r __loopzero_line; do
      [ -z "$__loopzero_line" ] && continue
      __loopzero_esc=$(printf '%%s' "$__loopzero_line" | cut -c1-400 | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
      printf '::loopzero:: phase=%[1]s step=%[2]s level=error msg="%%s"\n' "$__loopzero_esc"
    done
  fi
  rm -f "$__loopzero_log" 2>/dev/null || true
}
trap __loopzero_on_exit EXIT
# O status do passo e capturado DENTRO do bloco, antes do pipe.
#
# B-153: aqui o script corria como um pipeline para o tee, e havia um set -o pipefail
# dentro de parenteses. Duas coisas erradas ao mesmo tempo: o status de um pipeline POSIX
# e o do ULTIMO comando (o tee, que sempre sucede), e a opcao pipefail ligada num subshell
# e descartada junto com ele. Medido ao vivo: TODO passo construido por buildContainer
# saia 0 fizesse o que fizesse — os cinco passos de falha-fechada do runtime nao suportado
# reportaram Succeeded, e o build seguiu sem Dockerfile ate morrer com uma mensagem que
# nao nomeava nada disso.
#
# A forma abaixo e POSIX (ash/dash/bash): o script roda num subshell, o status dele e
# gravado por um echo que corre no bloco externo — nao abortado, porque o set -e do script
# fica confinado ao subshell — e o shell termina com esse status.
{
  (
%[4]s
  )
  echo $? > "$__loopzero_rc_file"
} 2>&1 | tee -a "$__loopzero_log"
__loopzero_status=$(cat "$__loopzero_rc_file" 2>/dev/null || true)
# Um arquivo VAZIO faz o cat sair 0, entao o fallback de erro nao cobre esse caso — e o
# shell recusa um exit sem numero com 2. Fecha do mesmo jeito, mas com um valor que nao e o
# que este codigo declara, e que manda quem depura procurar um passo inexistente.
case "$__loopzero_status" in ''|*[!0-9]*) __loopzero_status=1 ;; esac
rm -f "$__loopzero_rc_file" 2>/dev/null || true
exit "$__loopzero_status"
`, phase, stepName, display, script)
}

// humanizeStepName converts an internal step name into a sentence-style
// display name. "install-deps-node" → "Install deps", "build-apps-go"
// → "Build apps", "sign-image-api" → "Sign image". The language /
// app suffix is dropped because the dashboard already groups by step
// and the language is implicit in the app context.
func humanizeStepName(name string) string {
	// Strip known language/runtime suffix tokens that add noise. The
	// "-user-dockerfile" suffix is meaningful (user-supplied Dockerfile
	// path) and intentionally NOT stripped.
	for _, suf := range []string{"-node", "-go", "-python", "-ruby", "-java", "-php", "-rust", "-deno", "-bun"} {
		name = strings.TrimSuffix(name, suf)
	}
	parts := strings.Split(name, "-")
	if len(parts) == 0 || parts[0] == "" {
		return name
	}
	parts[0] = strings.ToUpper(parts[0][:1]) + parts[0][1:]
	return strings.Join(parts, " ")
}
