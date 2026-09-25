// Package openbaobootstrapincore forbids the `k3d:secrets:bootstrap-in-core`
// shortcut, which installs OpenBao into the EXISTING mgmt cluster instead of
// the separate `loopzero-secrets` cluster.
//
// # WHAT CHANGED, AND WHY IT MATTERS MORE THAN THE DIFF
//
// This gate used to assert that the shortcut CANONICAL-MIRRORED the upstream
// task: matching helmfile selectors, matching kubectl waits, a total context
// swap, a pre-flight citing `task setup`. Eighteen assertions on the SHAPE of
// something the platform's own topology forbids: installing OpenBao into the
// core cluster is a topology violation, not a shortcut (CLAUDE.md § Four Planes;
// docs/decisions.md § Open, 2026-09-09).
//
// A gate that verifies a violator is well-formed does worse than nothing: its
// green is read as approval. B-047 measured exactly that — the rule prohibited,
// the taskfile offered, and this gate certified the offer.
//
// ADR docs/decisions.md decided the
// rule wins, and named three consequences: remove the task, INVERT this gate,
// fix the two runbooks. This file is the second one.
//
// # WHAT IT ASSERTS NOW
//
// Two things, because forbidding one task name would be evadable by renaming:
//
//  1. No task target named *bootstrap-in-core* exists.
//  2. No task body pairs a SECRETS helmfile with a MGMT kube-context.
//
// WHAT IT DOES NOT CATCH — measured 2026-08-12 by an independent review that ran
// eight fixtures through Check. Do not read (2) as "whatever the target is
// called"; an earlier version of this comment did, and it was wrong:
//
//	kubectl config use-context k3d-loopzero-core   // context set by a PRIOR command
//	env: { KUBECONFIG: ... }                   // context via environment
//
// Both need dataflow across commands, which this gate does not do. The
// authoritative cover for them is `helmfile-context-parity`, which compares an
// invocation's context against the kubeContext the helmfile declares, across
// ALL taskfiles — it was unwired precisely because this task made it fail, and
// wiring it is part of the same slice that removed the task.
//
// Closed since that review: the `--kube-context=X` equals form, `{{.VAR}}`
// context variables (the repo's dominant style), YAML line continuations,
// four-space indentation, trailing comments on the target, and every file under
// taskfiles/ rather than k3d.yml alone.
//
// Hermetic — no kubectl, no helmfile. Pure parse of taskfiles/*.yml.
package openbaobootstrapincore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const taskfileGlob = "taskfiles/*.yml"

// mgmtContextVars are Taskfile variables that resolve to the core context.
//
// The identifier keeps `mgmt` ON PURPOSE: `MGMT_CONTEXT` is still an accepted
// variable name in the list below, and renaming the identifier without renaming what
// it matches would make the code say one thing and check another. The MESSAGES, which
// the operator reads, were updated to `core` — they named a cluster that has not
// existed since the B-001 rename.
// Measured: Taskfile.yml defines CORE_CONTEXT: k3d-loopzero-core, and
// taskfiles/infra.yml writes `--kube-context {{.CORE_CONTEXT}}` — the repo's
// dominant style. A literal-only scan would miss the way the repo actually
// writes this.
var mgmtContextVars = []string{"CORE_CONTEXT", "MGMT_CONTEXT"}

// mgmtContexts are the kube-contexts of the management cluster. A secrets
// helmfile pointed at one of these is the topology violation itself.
var mgmtContexts = []string{"k3d-loopzero-core", "loopzero-core"}

// secretsHelmfile marks a helmfile that installs the secrets plane.
const secretsHelmfile = "secrets.helmfile"

// Config controls the check.
type Config struct {
	RepoRoot string
	// TaskfilePath scans exactly this file instead of the glob (tests).
	TaskfilePath string
}

// Outcome is the summary.
type Outcome struct {
	// Violations are the offending lines, with file:line.
	Violations []string
	// LinesScanned proves the gate read something. A check that parsed nothing
	// and reported success is the `PASS — 0 routes` shape of B-040.
	LinesScanned int
}

// ErrForbiddenShortcut means the core-cluster OpenBao shortcut is present.
var ErrForbiddenShortcut = errors.New(
	"installing OpenBao into the core cluster is forbidden: it is a topology " +
		"violation, not a shortcut (docs/decisions.md § Open, 2026-09-09)")

// ErrNothingScanned means the taskfile could not be read or was empty, so
// nothing was verified.
var ErrNothingScanned = errors.New("taskfiles/k3d.yml produced no lines to scan; the boundary was not verified")

// targetRe accepts any indentation and a trailing comment — an earlier version
// required exactly two spaces and end-of-line, so `  x-in-core: # shortcut` and a
// four-space target both slipped through.
var targetRe = regexp.MustCompile(`^\s+([a-zA-Z0-9:_.-]*bootstrap-in-core[a-zA-Z0-9:_.-]*):\s*(#.*)?$`)

// ctxRe matches both `--kube-context X` and `--kube-context=X`.
var ctxRe = regexp.MustCompile(`--kube-context[= ]+(\S+)`)

// Check reports every way the forbidden shortcut appears.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	files, err := taskfiles(root, cfg.TaskfilePath)
	if err != nil {
		return Outcome{}, err
	}

	var out Outcome
	for _, rel := range files {
		body, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			return out, fmt.Errorf("%w: %w", ErrNothingScanned, rerr)
		}
		lines := joinContinuations(strings.Split(string(body), "\n"))
		out.LinesScanned += len(lines)
		out.Violations = append(out.Violations, scan(rel, lines)...)
	}
	if out.LinesScanned <= 1 {
		return out, ErrNothingScanned
	}
	if len(out.Violations) > 0 {
		return out, ErrForbiddenShortcut
	}
	return out, nil
}

// taskfiles resolves the files to scan. An explicit TaskfilePath wins; the glob
// is the default because a forbidden target in taskfiles/infra.yml is invocable
// exactly like one in k3d.yml — the root Taskfile flattens them.
func taskfiles(root, explicit string) ([]string, error) {
	if explicit != "" {
		return []string{explicit}, nil
	}
	abs, err := filepath.Glob(filepath.Join(root, taskfileGlob))
	if err != nil {
		return nil, err
	}
	if len(abs) == 0 {
		return nil, fmt.Errorf("%w: no file matched %s", ErrNothingScanned, taskfileGlob)
	}
	rels := make([]string, 0, len(abs))
	for _, a := range abs {
		r, rerr := filepath.Rel(root, a)
		if rerr != nil {
			return nil, rerr
		}
		rels = append(rels, r)
	}
	sort.Strings(rels)
	return rels, nil
}

// joinContinuations folds `\`-continued shell lines into one logical line, so a
// helmfile and its --kube-context split across two physical lines are seen
// together. Line numbers stay those of the FIRST physical line.
func joinContinuations(raw []string) []logicalLine {
	var out []logicalLine
	for i := 0; i < len(raw); i++ {
		text, start := raw[i], i+1
		for strings.HasSuffix(strings.TrimRight(text, " \t"), "\\") && i+1 < len(raw) {
			text = strings.TrimSuffix(strings.TrimRight(text, " \t"), "\\") + " " + strings.TrimSpace(raw[i+1])
			i++
		}
		out = append(out, logicalLine{Text: text, Line: start})
	}
	return out
}

type logicalLine struct {
	Text string
	Line int
}

// scan finds both violation shapes in one file.
func scan(rel string, lines []logicalLine) []string {
	var vs []string
	for _, l := range lines {
		if m := targetRe.FindStringSubmatch(l.Text); m != nil {
			vs = append(vs, fmt.Sprintf(
				"%s:%d — task target %q installs the secrets plane into the core cluster", rel, l.Line, m[1]))
			continue
		}
		if !strings.Contains(l.Text, secretsHelmfile) {
			continue
		}
		for _, m := range ctxRe.FindAllStringSubmatch(l.Text, -1) {
			if !isMgmtContext(m[1]) {
				continue
			}
			vs = append(vs, fmt.Sprintf(
				"%s:%d — a secrets helmfile is pointed at the core context %q", rel, l.Line, m[1]))
			break
		}
	}
	return vs
}

// isMgmtContext accepts the literal contexts AND the Taskfile variables that
// resolve to them.
func isMgmtContext(v string) bool {
	for _, c := range mgmtContexts {
		if v == c {
			return true
		}
	}
	for _, name := range mgmtContextVars {
		if strings.Contains(v, "{{") && strings.Contains(v, name) {
			return true
		}
	}
	return false
}
