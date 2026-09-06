// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package helmfilecontextparity asserts that the cluster a helmfile invocation
// targets is the cluster that helmfile itself declares.
//
// # Why this gate exists at all
//
// The obvious design would be to let helmfile enforce this: declare
// `kubeContext` in the file, and let a divergent `--kube-context` be an error.
// It is not. Measured against helmfile 1.1.6 — the installed binary — with a
// synthetic chart:
//
//	sem kubeContext, sem flag  → helm runs with NO --kube-context (kubectl's
//	                             active context decides, silently)
//	com kubeContext, sem flag  → helm runs with the declared context      ✅
//	com kubeContext, flag ≠    → helm runs with THE FLAG's context        ❌
//
// The third row is the one that matters: the flag wins, with no error and no
// warning. So declaring `kubeContext` fixes the dangerous DEFAULT, and cannot
// produce the conflict. The conflict has to come from here.
//
// See docs/wiki/decisions and the slice-3 plan ADR D1.
//
// # What this gate proves, and what it does not
//
// It compares two DECLARATIONS of the same fact: the `--kube-context` written
// in taskfiles/*.yml and the `kubeContext:` written in the helmfile it targets.
// When both are literal, they must be equal.
//
// It CANNOT compare a context that is parameterised — `{{.CORE_CONTEXT}}` on
// the task side, `{{ requiredEnv ... }}` on the helmfile side. Those pairs are
// counted and reported separately, never folded into the pass, because
// `cell.helmfile` serves N cells and parameterisation there is the CORRECT
// design, not a defect.
//
// And it sees only invocations that go through a taskfile. An operator typing
// `helmfile -f … --kube-context <wrong>` by hand is outside its reach. That
// limit is stated rather than papered over.
//
// # The floor
//
// Enumerating zero comparable invocations is a typed ERROR, not a pass. This is
// the B-040 lesson: `route-auth-coverage` reported "PASS — 0 routes" while
// examining nothing. A gate that verified nothing must say so.
package helmfilecontextparity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ErrContextDiverges means a task targets a cluster its helmfile does not declare.
var ErrContextDiverges = errors.New("a helmfile invocation targets a cluster the helmfile itself does not declare")

// ErrContextNotDeclared means an invoked helmfile declares no kubeContext, so a
// bare `helmfile sync` against it would inherit kubectl's active context.
var ErrContextNotDeclared = errors.New("an invoked helmfile declares no kubeContext — a bare sync would land on kubectl's active context")

// ErrNothingCompared means the walk enumerated no comparable invocation. The
// gate examined nothing, so it cannot vouch for anything.
var ErrNothingCompared = errors.New("zero helmfile invocations were enumerated — the gate examined nothing, so it cannot vouch for anything")

// ErrRequiredEnvNotSet means a helmfile's context is `requiredEnv "X"` and the
// task invoking it never exports X — so the invocation dies at template parse.
var ErrRequiredEnvNotSet = errors.New("a helmfile demands an env var for its kubeContext that the invoking task does not export")

// requiredEnvRe extracts the variable name from `{{ requiredEnv "X" }}`.
var requiredEnvRe = regexp.MustCompile(`requiredEnv\s+"([A-Z0-9_]+)"`)

// Config controls the check.
type Config struct {
	RepoRoot string
	// TaskfileGlobs defaults to the root Taskfile.yml PLUS taskfiles/*.yml.
	//
	// The root file is not optional. Scanning only taskfiles/ left 12 invocations
	// invisible — including two that point the WORKLOAD helmfile at the BUILD
	// cluster, the exact cross-plane class this gate exists to name — and it is
	// how a change that broke `task setup` shipped past a green gate.
	TaskfileGlobs []string
}

// Divergence is one task/helmfile pair that disagrees on the target cluster.
type Divergence struct {
	Taskfile string
	Line     int
	Helmfile string
	Invoked  string
	Declared string
}

// Outcome is the summary. ComparedCount and SkippedParameterised are reported
// separately on purpose: a run that skipped everything is not a clean run.
type Outcome struct {
	ComparedCount        int
	SkippedParameterised int
	Divergences          []Divergence
	Undeclared           []string
	// MissingEnv lists invocations whose helmfile demands an env var that the
	// invoking command never sets. A Task var is not an env var — a distinction
	// nothing in the tree stated until it broke an install.
	MissingEnv []string
}

// helmfileTokenRe finds the start of an invocation.
var helmfileTokenRe = regexp.MustCompile(`\bhelmfile\b`)

// fileFlagRe and contextFlagRe read the two operands out of the window.
var fileFlagRe = regexp.MustCompile(`(?:^|\s)(?:-f|--file)[\s=]+"?([^\s"\\]+)"?`)
var contextFlagRe = regexp.MustCompile(`--kube-context[\s=]+"?([^\s"\\]+)"?`)

// declaredContextRe reads `kubeContext:` out of a helmfile. It is deliberately
// line-oriented rather than a YAML parse: the files are Go templates, so they
// are not valid YAML until rendered, and rendering needs an environment the
// gate must not require.
var declaredContextRe = regexp.MustCompile(`(?m)^\s*kubeContext:\s*"?(.+?)"?\s*$`)

// parameterised reports whether a value is a template expression rather than a
// literal cluster name. Both `{{.CORE_CONTEXT}}` (Task) and `{{ requiredEnv … }}`
// (helmfile) resolve only at run time, so no static comparison is possible.
func parameterised(v string) bool {
	return strings.Contains(v, "{{") || strings.Contains(v, "$")
}

// invocationWindow is how many lines after the `helmfile` token may still carry
// its operands. Real invocations in this repo wrap across up to ~8 lines, with
// and without a trailing backslash.
const invocationWindow = 10

// Check walks every taskfile, pairs each helmfile invocation with the file it
// targets, and compares the two declarations of the target cluster.
// globsPadrao e' a UNICA declaracao do conjunto varrido. O contador
// independente de `TestCheck_RealTree` le' daqui em vez de repetir a lista:
// duas listas que precisam concordar acabam discordando, e quando isso
// acontece a reconciliacao passa a comparar duas arvores diferentes e falha
// por uma razao que nada tem a ver com o parser — que e' o proprio aviso
// escrito no comentario daquele contador (B-078).
var globsPadrao = []string{
	"Taskfile.yml",
	filepath.Join("taskfiles", "*.yml"),
	// Os invocadores em SCRIPT ficam DE FORA, e a razao foi medida em 2026-08-18
	// (B-078), nao suposta:
	//
	//  1. Nao comprariam comparacao nenhuma. As quatro invocacoes em script
	//     (`vault-bootstrap-dev.sh:267`, `operator-cutover-execute.sh:142,:204`,
	//     `enable-vault-backup.sh:205`) parametrizam TANTO o contexto
	//     (`$CTX`, `$SECRETS_CONTEXT`, `$OPENBAO_CONTEXT`) QUANTO o caminho
	//     (`-f "$HELMFILE_PATH"`, `-f "${REPO_ROOT}/..."`). Todas cairiam em
	//     "fora de alcance estatico".
	//
	//  2. E o parser nao as pareia de forma confiavel. Ele exige `-f` e
	//     `--kube-context` na MESMA janela, e invocacao em shell quebra em
	//     continuacoes: com os scripts incluidos, a arvore carregava 37
	//     ocorrencias de `--kube-context` e a varredura pareava 28 — 9 caiam em
	//     silencio, que e' precisamente o defeito que a reconciliacao de
	//     `TestCheck_RealTree` existe para impedir.
	//
	// Meio-varrer produz um denominador que nao e' nem 24 nem 28, e um
	// denominador instavel e' pior que um escopo declarado. O escopo esta'
	// declarado aqui e a fracao aparece no `Summary()`.
}

func Check(cfg Config) (Outcome, error) {
	var out Outcome

	globs := cfg.TaskfileGlobs
	if len(globs) == 0 {
		globs = globsPadrao

	}
	var taskfiles []string
	for _, g := range globs {
		found, err := filepath.Glob(filepath.Join(cfg.RepoRoot, g))
		if err != nil {
			return out, fmt.Errorf("globbing %s: %w", g, err)
		}
		taskfiles = append(taskfiles, found...)
	}
	sort.Strings(taskfiles)

	declaredCache := map[string]string{}
	undeclaredSet := map[string]bool{}

	for _, tf := range taskfiles {
		raw, err := os.ReadFile(tf)
		if err != nil {
			return out, fmt.Errorf("reading %s: %w", tf, err)
		}
		rel, _ := filepath.Rel(cfg.RepoRoot, tf)
		lines := strings.Split(string(raw), "\n")

		// Uma invocacao pode casar o token `helmfile` em MAIS DE UMA linha — o
		// proprio caminho o contem (`-f infra/helmfile/...`), e em script a
		// invocacao quebra em varias linhas com `\`. Sem esta desduplicacao a
		// mesma ocorrencia de `--kube-context` era pareada uma vez por linha que
		// casasse, e a varredura reportava 36 pareamentos para 24 ocorrencias
		// reais. Quem pegou foi a reconciliacao contra a contagem independente em
		// `TestCheck_RealTree`, que existe exatamente para isso (B-078).
		contextoJaPareado := map[int]bool{}

		for i, line := range lines {
			if !helmfileTokenRe.MatchString(line) {
				continue
			}
			janela := windowFrom(lines, i)
			ctxLinha := -1
			for k, l := range janela {
				if contextFlagRe.MatchString(l) {
					ctxLinha = i + k
					break
				}
			}
			if ctxLinha >= 0 && contextoJaPareado[ctxLinha] {
				continue
			}
			window := joinNonComment(janela)

			fm := fileFlagRe.FindStringSubmatch(window)
			cm := contextFlagRe.FindStringSubmatch(window)
			if fm == nil || cm == nil {
				continue // not a targeted invocation (e.g. a bare `helmfile sync` in a doc block)
			}
			hfRel, invoked := fm[1], cm[1]

			// O CAMINHO tambem pode ser parametrizado, e nao so' o contexto — e' o
			// caso comum nos invocadores em script (`-f "$HELMFILE_PATH"`,
			// `-f "${REPO_ROOT}/infra/..."`). Sem esta guarda o gate tentava ler o
			// literal como arquivo, falhava, e classificava como "helmfile que NAO
			// declara contexto" — uma acusacao grave inventada por um caminho que
			// ele simplesmente nao consegue resolver. Fora de alcance estatico e'
			// a classificacao honesta (B-078).
			if ctxLinha >= 0 {
				contextoJaPareado[ctxLinha] = true
			}

			if parameterised(hfRel) {
				out.SkippedParameterised++
				continue
			}

			declared, seen := declaredCache[hfRel]
			if !seen {
				declared = readDeclaredContext(cfg.RepoRoot, hfRel)
				declaredCache[hfRel] = declared
			}
			if declared == "" {
				if !undeclaredSet[hfRel] {
					undeclaredSet[hfRel] = true
					out.Undeclared = append(out.Undeclared, hfRel)
				}
				continue
			}
			if m := requiredEnvRe.FindStringSubmatch(declared); m != nil {
				if !strings.Contains(envAssignmentWindow(lines, i), m[1]+"=") {
					out.MissingEnv = append(out.MissingEnv,
						fmt.Sprintf("%s:%d — %s exige %s, e a invocação não a exporta", rel, i+1, hfRel, m[1]))
				}
			}
			if parameterised(invoked) || parameterised(declared) {
				out.SkippedParameterised++
				continue
			}
			out.ComparedCount++
			if invoked != declared {
				out.Divergences = append(out.Divergences, Divergence{
					Taskfile: rel, Line: i + 1, Helmfile: hfRel,
					Invoked: invoked, Declared: declared,
				})
			}
		}
	}

	sort.Strings(out.Undeclared)

	// The floor comes AFTER the findings: when every invocation diverges, the
	// counts alone cannot distinguish "worst possible result" from "measured
	// nothing", and reporting the instrumentation error would bury the finding.
	//
	// Divergence is reported before absence because it names a concrete wrong
	// cluster, while an undeclared helmfile is a latent one. Summary() carries
	// both counts either way, so neither is hidden by the choice of error.
	if len(out.Divergences) > 0 {
		return out, ErrContextDiverges
	}
	if len(out.Undeclared) > 0 {
		return out, ErrContextNotDeclared
	}
	if len(out.MissingEnv) > 0 {
		return out, ErrRequiredEnvNotSet
	}
	// The floor measures what the doc-comment promises: zero COMPARISONS. Folding
	// the parameterised count in here let a tree whose every context is a template
	// report PASS having compared nothing — `route-auth-coverage: PASS — 0 routes`
	// with a different subject. The parameterised count is reported, never
	// counted as coverage.
	if out.ComparedCount == 0 {
		return out, ErrNothingCompared
	}
	return out, nil
}

// yamlBoundaryRe marks where one YAML construct ends and the next begins: a new
// list item (`- foo`) or a new mapping key (`cmds:`, `desc:`).
var yamlBoundaryRe = regexp.MustCompile(`^\s*(-\s|[\w.-]+:\s*(\||>|$))`)

// windowFrom returns the lines that may still carry the operands of the
// invocation starting at i, stopping at the first YAML boundary.
//
// The bound matters. A window that simply runs forward N lines walks out of one
// block into the next: measured on the real tree, taskfiles/k3d.yml:255 — prose
// inside a `desc:` documenting manual operator steps — was reported as a
// divergence because the window reached past `cmds:` and borrowed the operands
// of the real invocation at :262. The finding underneath was genuine; the count
// (26 pairings against 16 --kube-context lines) and that line number were not.
func windowFrom(lines []string, i int) []string {
	end := i + invocationWindow
	if end > len(lines) {
		end = len(lines)
	}
	for j := i + 1; j < end; j++ {
		if yamlBoundaryRe.MatchString(lines[j]) {
			return lines[i:j]
		}
	}
	return lines[i:end]
}

// envAssignmentWindow is the forward window PLUS a short look back.
//
// The two searches are deliberately separate. Operands must come from the
// forward window only: extending it backwards made a `- helmfile …` line
// consume the PREVIOUS command's `-f`, so the second of two consecutive
// invocations was paired with the first one's helmfile. But in a folded `>-`
// block the env assignment legitimately sits on the line ABOVE the token —
// taskfiles/infra.yml:264-265 is exactly that shape, and a forward-only search
// reported the repository's own fix as a missing variable.
func envAssignmentWindow(lines []string, i int) string {
	start := i - envLookback
	if start < 0 {
		start = 0
	}
	return joinNonComment(lines[start:i]) + " " + joinNonComment(windowFrom(lines, i))
}

// envLookback is how many lines above the token may carry the assignment. Two
// covers the folded-block shape without reaching into a neighbouring command.
const envLookback = 2

// joinNonComment flattens a window into one string, dropping comment lines.
//
// A comment is not an invocation. Without this, prose describing a command —
// `taskfiles/infra.yml:301` carries "# `helm upgrade -f values.yaml` would 404"
// — is swallowed into a neighbouring window and reported as a helmfile that
// declares no context. That false positive was measured, not imagined.
func joinNonComment(lines []string) string {
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, " ")
}

// readDeclaredContext returns the kubeContext a helmfile declares, or "" when
// it declares none or cannot be read. An unreadable file is indistinguishable
// from an undeclared one on purpose: both mean the gate could not confirm the
// target, and both must surface as Undeclared rather than as a silent skip.
func readDeclaredContext(root, rel string) string {
	// The path comes from parsing a taskfile, so nothing guarantees it stays
	// inside the repository — `-f ../../etc/hosts` parses just as well as a real
	// helmfile. Containment is checked rather than assumed: a path that escapes
	// is treated as unreadable, which surfaces it as Undeclared (a finding the
	// operator sees) instead of reading a file outside the tree under audit.
	// Both sides are resolved to absolute BEFORE comparing. Comparing a cleaned
	// relative pair silently rejects everything: with root "." — which is how the
	// CLI invokes the gate — Join(".", "infra/x") cleans to "infra/x", which does
	// not start with "./", so every real helmfile read as an escape and the gate
	// reported "0 compared, 4 undeclared". Measured, not imagined.
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return ""
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return ""
	}
	raw, readErr := os.ReadFile(abs)
	if readErr != nil {
		return ""
	}
	m := declaredContextRe.FindSubmatch(raw)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

// Summary renders the one-line gate verdict.
func (o Outcome) Summary() string {
	total := o.ComparedCount + o.SkippedParameterised
	return fmt.Sprintf("%d of %d invocation(s) compared, %d parameterised (out of static reach), "+
		"%d divergence(s), %d helmfile(s) declaring no context, %d missing env",
		o.ComparedCount, total, o.SkippedParameterised,
		len(o.Divergences), len(o.Undeclared), len(o.MissingEnv))
}
