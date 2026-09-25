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
//	no kubeContext, no flag      → helm runs with NO --kube-context (kubectl's
//	                               active context decides, silently)
//	kubeContext, no flag         → helm runs with the declared context      ✅
//	kubeContext, different flag  → helm runs with THE FLAG's context        ❌
//
// The third row is the one that matters: the flag wins, with no error and no
// warning. So declaring `kubeContext` fixes the dangerous DEFAULT, and cannot
// produce the conflict. The conflict has to come from here.
//
// The table above is the measurement the design rests on; the slice-3 plan that
// first recorded it (ADR D1) is in git history.
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

// defaultGlobs is the ONE declaration of the scanned set. The independent
// counter in `TestCheck_RealTree` reads it instead of repeating the list: two
// lists that must agree end up disagreeing, and the reconciliation then
// compares two different trees and fails for a reason unrelated to the parser
// (B-078).
var defaultGlobs = []string{
	"Taskfile.yml",
	filepath.Join("taskfiles", "*.yml"),
	// SCRIPT invokers are left OUT, for a reason measured on 2026-08-18
	// (B-078), not assumed:
	//
	//  1. They would buy no comparison. The four script invocations
	//     (`vault-bootstrap-dev.sh:267`, `operator-cutover-execute.sh:142,:204`,
	//     `enable-vault-backup.sh:205`) parameterise BOTH the context
	//     (`$CTX`, `$SECRETS_CONTEXT`, `$OPENBAO_CONTEXT`) AND the path
	//     (`-f "$HELMFILE_PATH"`, `-f "${REPO_ROOT}/..."`). All of them would
	//     land in "out of static reach".
	//
	//  2. And the parser does not pair them reliably. It needs `-f` and
	//     `--kube-context` in the SAME window, and a shell invocation breaks
	//     across continuations: with the scripts included the tree carried 37
	//     `--kube-context` occurrences and the scan paired 28 — 9 fell away in
	//     silence, which is precisely the defect the reconciliation in
	//     `TestCheck_RealTree` exists to prevent.
	//
	// Half-scanning yields a denominator that is neither one nor the other, and
	// an unstable denominator is worse than a declared scope. The scope is
	// declared here and the fraction appears in `Summary()`.
}

// invocation is one targeted helmfile call: the line of its `helmfile` token
// (0-based) and the two operands read from its window.
type invocation struct {
	line     int
	helmfile string
	context  string
}

// Check walks every taskfile, pairs each helmfile invocation with the file it
// targets, and compares the two declarations of the target cluster.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	globs := cfg.TaskfileGlobs
	if len(globs) == 0 {
		globs = defaultGlobs
	}
	taskfiles, err := taskfilesIn(cfg.RepoRoot, globs)
	if err != nil {
		return out, err
	}

	c := classifier{root: cfg.RepoRoot, declared: map[string]string{}, undeclared: map[string]bool{}, out: &out}
	for _, tf := range taskfiles {
		raw, err := os.ReadFile(tf)
		if err != nil {
			return out, fmt.Errorf("reading %s: %w", tf, err)
		}
		rel, _ := filepath.Rel(cfg.RepoRoot, tf)
		lines := strings.Split(string(raw), "\n")
		for _, inv := range parseInvocations(lines) {
			c.classify(rel, lines, inv)
		}
	}
	sort.Strings(out.Undeclared)
	return out, verdict(out)
}

// taskfilesIn expands the globs under root, sorted.
func taskfilesIn(root string, globs []string) ([]string, error) {
	var taskfiles []string
	for _, g := range globs {
		found, err := filepath.Glob(filepath.Join(root, g))
		if err != nil {
			return nil, fmt.Errorf("globbing %s: %w", g, err)
		}
		taskfiles = append(taskfiles, found...)
	}
	sort.Strings(taskfiles)
	return taskfiles, nil
}

// parseInvocations finds every targeted helmfile invocation in a taskfile.
//
// One invocation can match the `helmfile` token on MORE THAN ONE line — the
// path itself contains it (`-f infra/helmfile/...`), and a shell invocation
// wraps across lines with `\`. Without de-duplicating by the line that carries
// `--kube-context`, the same occurrence was paired once per matching line and
// the scan reported 36 pairings for 24 real occurrences. The reconciliation
// against the independent count in `TestCheck_RealTree` caught it (B-078).
func parseInvocations(lines []string) []invocation {
	var found []invocation
	pairedContextLines := map[int]bool{}
	for i, line := range lines {
		if !helmfileTokenRe.MatchString(line) {
			continue
		}
		window := windowFrom(lines, i)
		contextLine := -1
		for k, l := range window {
			if contextFlagRe.MatchString(l) {
				contextLine = i + k
				break
			}
		}
		if contextLine >= 0 && pairedContextLines[contextLine] {
			continue
		}
		joined := joinNonComment(window)
		fm := fileFlagRe.FindStringSubmatch(joined)
		cm := contextFlagRe.FindStringSubmatch(joined)
		if fm == nil || cm == nil {
			continue // not a targeted invocation (e.g. a bare `helmfile sync` in a doc block)
		}
		if contextLine >= 0 {
			pairedContextLines[contextLine] = true
		}
		found = append(found, invocation{line: i, helmfile: fm[1], context: cm[1]})
	}
	return found
}

// classifier sorts invocations into the outcome, reading each targeted
// helmfile's declared context once.
type classifier struct {
	root       string
	declared   map[string]string
	undeclared map[string]bool
	out        *Outcome
}

func (c classifier) classify(rel string, lines []string, inv invocation) {
	// The PATH can be parameterised too, not only the context — the common case
	// in script invokers (`-f "$HELMFILE_PATH"`, `-f "${REPO_ROOT}/infra/..."`).
	// Without this guard the gate tried to read the literal as a file, failed,
	// and classified it as a "helmfile that declares NO context" — a serious
	// accusation invented by a path it simply cannot resolve. Out of static
	// reach is the honest classification (B-078).
	if parameterised(inv.helmfile) {
		c.out.SkippedParameterised++
		return
	}
	declared, seen := c.declared[inv.helmfile]
	if !seen {
		declared = readDeclaredContext(c.root, inv.helmfile)
		c.declared[inv.helmfile] = declared
	}
	if declared == "" {
		if !c.undeclared[inv.helmfile] {
			c.undeclared[inv.helmfile] = true
			c.out.Undeclared = append(c.out.Undeclared, inv.helmfile)
		}
		return
	}
	if m := requiredEnvRe.FindStringSubmatch(declared); m != nil {
		if !strings.Contains(envAssignmentWindow(lines, inv.line), m[1]+"=") {
			c.out.MissingEnv = append(c.out.MissingEnv,
				fmt.Sprintf("%s:%d — %s requires %s, and the invocation does not export it", rel, inv.line+1, inv.helmfile, m[1]))
		}
	}
	if parameterised(inv.context) || parameterised(declared) {
		c.out.SkippedParameterised++
		return
	}
	c.out.ComparedCount++
	if inv.context != declared {
		c.out.Divergences = append(c.out.Divergences, Divergence{
			Taskfile: rel, Line: inv.line + 1, Helmfile: inv.helmfile,
			Invoked: inv.context, Declared: declared,
		})
	}
}

// verdict turns the outcome into the gate's error.
//
// The floor comes AFTER the findings: when every invocation diverges, the
// counts alone cannot distinguish "worst possible result" from "measured
// nothing", and reporting the instrumentation error would bury the finding.
//
// Divergence is reported before absence because it names a concrete wrong
// cluster, while an undeclared helmfile is a latent one. Summary() carries
// both counts either way, so neither is hidden by the choice of error.
func verdict(out Outcome) error {
	switch {
	case len(out.Divergences) > 0:
		return ErrContextDiverges
	case len(out.Undeclared) > 0:
		return ErrContextNotDeclared
	case len(out.MissingEnv) > 0:
		return ErrRequiredEnvNotSet
	case out.ComparedCount == 0:
		// The floor measures what the doc-comment promises: zero COMPARISONS.
		// Folding the parameterised count in here let a tree whose every
		// context is a template report PASS having compared nothing —
		// `route-auth-coverage: PASS — 0 routes` with a different subject.
		return ErrNothingCompared
	}
	return nil
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
