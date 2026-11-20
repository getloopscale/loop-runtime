// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package alertrunbooks implements the T5.8 gate: every PrometheusRule alert
// must carry a runbook_url (or legacy runbook:) annotation so Alertmanager can
// link operators to recovery docs.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check_alert_runbooks.sh (which embedded a python3
// heredoc). Pure-Go (file read + regexp) — no external CLI. (The "helm" in the
// rules-dir path is a directory name, not a helm invocation.)
package alertrunbooks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// DefaultRulesDir holds the PrometheusRule manifests, relative to repo root.
//
// Kept for compatibility: it is the historical directory and is still the first in the
// list. What gets scanned is `DefaultRulesDirs`.
const DefaultRulesDir = "infra/helm/monitoring/manifests"

// DefaultRulesDirs are ALL the directories where alerts actually live (B-164).
//
// Until 2026-08-19 the gate scanned only the first. Measured on that date: the tree has
// **233** alerts and it looked at **110** — 47%. And the two `admission-policies` ones are
// precisely where 22 of the 23 `runbook_url` annotations live, so the unscanned half held
// almost all the evidence this gate exists to check.
//
// `charts/` is not only what ArgoCD delivers: charts/loopzero-admission-policies is also the
// chart Helmfile installs on core and every cell. Its bootstrap copy under
// infra/helm/admission-policies was listed here too until it was deleted — scanning two
// identical copies scanned every admission alert twice.
var DefaultRulesDirs = []string{
	"infra/helm/monitoring/manifests",
	"charts/loopzero-monitoring/templates",
	"charts/loopzero-admission-policies/templates",
}

// runbookLookahead is the per-alert-block window scanned for a runbook
// annotation (matches the bash `blk[:1500]`).
const runbookLookahead = 1500

var (
	alertSplitRe = regexp.MustCompile(`\n\s*- alert:`)
	alertNameRe  = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)`)
)

// DefaultExceptionList are pre-T5.8-baseline alerts permitted without a runbook.
var DefaultExceptionList = []string{
	"PulumiOrphanStackDetected", "GiteaAuthLatencyHigh", "ArgoWorkflowQuotaExceeded",
	"HPAAtMaxReplicas", "PVCPendingSustained", "TenantReconcileBacklog",
	"BuildFailureRateHigh", "LoopZeroQueueSlow", "LoopZeroDeployFailureRate",
	"LoopZeroLockContention", "LoopZeroSyncSlow", "LoopZeroBuildQueueSlow",
	"LoopZeroTempoStorageHigh", "LoopZeroCollectorExportErrors",
}

// Config drives the gate.
type Config struct {
	Root          string
	RulesDir      string   // a single directory (compat); empty uses RulesDirs
	RulesDirs     []string // default DefaultRulesDirs (relative to Root)
	ExceptionList []string // default DefaultExceptionList
}

// Outcome captures the result.
type Outcome struct {
	Pass bool
	// Violations are SITES in violation, in the form "<dir>/<file>:<alert>".
	//
	// The directory has been part of the key since 2026-09-06 (B-067) and the reason is
	// measured: 9 of the alert files exist TWICE in the tree — one copy in `infra/helm/...`
	// and the mirrored copy in `charts/...` that ArgoCD delivers to tenants. With the key
	// being only the basename, those 9 pairs produced IDENTICAL lines: the report said
	// `policy-controller-alerts.yaml:LoopZeroPolicyControllerDown` twice without saying which
	// one to open, and fixing one copy left the other failing with the same line —
	// indistinguishable from the work that had just been done.
	Violations []string
	// ViolatingAlertNames holds the name of each alert in violation, in site order, so
	// that `DistinctAlerts` can separate "how many alerts" from "how many places".
	ViolatingAlertNames []string
	// DeadLinks are runbook annotations that POINT TO NOTHING and that the gate can
	// prove without the network: a path in this repository that does not exist on
	// disk, or a GitHub URL in an organisation different from the repo's canonical
	// identity.
	//
	// Measured on 2026-08-19 (B-161): of the 7 URLs in the tree, SIX led nowhere, and
	// all seven counted as satisfied. An alert that fires at 3am sending the operator
	// to a 404 is worse than an alert without a link — the link makes a promise.
	DeadLinks []DeadLink
	// UnverifiableLinks counts the external URLs. A hermetic gate cannot tell whether
	// `https://docs.getloopscale.com/runbooks/x` is alive; asserting either would be
	// inventing. Counting is honest, and the number is what makes someone check.
	UnverifiableLinks int
	// DirsScanned counts how many rules directories actually existed. Zero is a failure:
	// it is the difference between "no alert without a runbook" and "no alert looked at".
	DirsScanned int
	// LinksChecked counts ALL annotations examined, including the healthy ones.
	//
	// It exists because the population scan summed dead + unverifiable and treated zero
	// as "reached nothing" — when zero is also the result of **every link being good**.
	// Confusing the two is the exact shape this gate exists to fight, and it was in the
	// test that was supposed to prove it.
	LinksChecked int
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// DistinctAlerts counts ALERTS, not sites. The two numbers diverge exactly when an alert
// lives in the `infra/helm` copy and in the `charts` copy — and the difference matters
// because they are different claims: "37 alerts do not tell the operator what to do" is
// runbook debt; "46 places to edit" is the size of the fix. Reporting the second number
// with the word of the first inflates the debt by 24% (measured 2026-09-06: 46 sites,
// 37 alerts).
func (o *Outcome) DistinctAlerts() int {
	seen := make(map[string]struct{}, len(o.ViolatingAlertNames))
	for _, n := range o.ViolatingAlertNames {
		seen[n] = struct{}{}
	}
	return len(seen)
}

// DeadLink is a runbook annotation proven useless without leaving the machine.
type DeadLink struct {
	File   string
	Alert  string
	URL    string
	Reason string
}

// Check scans PrometheusRule YAMLs for alerts missing a runbook annotation.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	dirs := cfg.RulesDirs
	if len(dirs) == 0 {
		if cfg.RulesDir != "" {
			dirs = []string{cfg.RulesDir}
		} else {
			dirs = DefaultRulesDirs
		}
	}
	exc := cfg.ExceptionList
	if exc == nil {
		exc = DefaultExceptionList
	}
	excepted := make(map[string]struct{}, len(exc))
	for _, e := range exc {
		excepted[e] = struct{}{}
	}

	out := &Outcome{Pass: true}
	for _, rulesDir := range dirs {
		dir := filepath.Join(root, rulesDir)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			// An absent directory is tolerated — test trees hold only one of them —
			// but NOT silently: `DirsScanned` counts how many existed, and zero fails.
			continue
		}
		if err != nil {
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s: unreadable rules directory, not audited: %v", rulesDir, err))
			continue
		}
		out.DirsScanned++
		scanDirectory(root, rulesDir, dir, entries, excepted, out)
	}
	if out.DirsScanned == 0 {
		out.Pass = false
		out.Violations = append(out.Violations,
			"NO rules directory found — a scan that reaches nothing is not green")
	}
	if len(out.Violations) > 0 || len(out.DeadLinks) > 0 {
		out.Pass = false
	}
	return out
}

func scanDirectory(root, relDir, dir string, entries []os.DirEntry, excepted map[string]struct{}, out *Outcome) {
	for _, e := range entries {
		// The historical filter required "prometheusrule" in the name — inherited from the
		// bash script this gate replaced. Measured on 2026-08-19 (B-164): the
		// `admission-policies` alerts live in `*-alerts.yaml` (`sigstore-storage-alerts.yaml`,
		// `tenant-namespace-alerts.yaml`, …), so widening the directory scope was not
		// enough: the NAME filter kept discarding 37 alerts and 22 of the 23
		// `runbook_url` annotations in the tree.
		//
		// Accept both forms. What decides is the file declaring an alert, and both names
		// are live conventions in the repository.
		name := e.Name()
		relevant := strings.Contains(name, "prometheusrule") || strings.Contains(name, "alert")
		if e.IsDir() || !relevant || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			// An alert file nobody could read was never audited; it is not clean.
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s/%s: unreadable, runbook links not checked: %v", relDir, e.Name(), rerr))
			continue
		}
		for _, name := range missingRunbookAlerts(string(data), excepted) {
			out.Violations = append(out.Violations, relDir+"/"+e.Name()+":"+name)
			out.ViolatingAlertNames = append(out.ViolatingAlertNames, name)
		}
		checkLinks(root, e.Name(), string(data), out)
	}
}

// canonicalRepo is this repository's NAME, taken from the `go.mod` module path
// (`github.com/getloopscale/loopzero/...`) rather than from the git remote, which varies
// by checkout and by ssh alias.
//
// The comparison is by REPOSITORY, not by organisation: the same repository is
// reachable under more than one organisation, so an organisation mismatch proves
// nothing about whether the file exists. A version of this gate that treated a
// different organisation as a dead link "by construction" held for a few hours on
// 2026-08-19 and would have produced false positives (B-164).
//
// How that error got in, because the shape repeats: the truncated URLs were read,
// the file names they SEEMED to have were guessed, and the guesses were probed and
// returned Not Found. The real names were different and resolve. Probing what was
// assumed instead of what the file says is evidence fabrication with an extra step.
const canonicalRepo = "loopzero"

var runbookURLRe = regexp.MustCompile(`runbook_url:\s*"([^"]+)"`)
var blobRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/blob/[^/]+/(.+)$`)

// checkLinks proves, WITHOUT the network, which runbook annotations lead nowhere.
//
// It asserts only what can be asserted hermetically:
//   - a GitHub blob URL into THIS repository (any organisation) -> the path is
//     checked on disk; a missing path is a dead link
//   - a GitHub blob URL into ANOTHER repository -> unverifiable without the
//     network: counted in UnverifiableLinks, never reported as dead
//   - any other URL -> unverifiable, and COUNTED
func checkLinks(root, file, content string, out *Outcome) {
	blocks := alertSplitRe.Split(content, -1)
	for _, blk := range blocks[1:] {
		m := alertNameRe.FindStringSubmatch(blk)
		if m == nil {
			continue
		}
		alert := m[1]
		body := blk
		if len(body) > runbookLookahead {
			body = body[:runbookLookahead]
		}
		u := runbookURLRe.FindStringSubmatch(body)
		if u == nil {
			continue
		}
		out.LinksChecked++
		url := u[1]
		b := blobRe.FindStringSubmatch(url)
		if b == nil {
			out.UnverifiableLinks++
			continue
		}
		repo, path := b[2], b[3]
		// The anchor (and any query) are NOT part of the on-disk path. `blobRe` captures
		// everything after the branch in a single group, so without this cut `Stat` asks
		// for `runbook-security-alerts.md#loopzerounsignedreleasepushed` — a name no file has —
		// and a runbook that exists is accused of being dead.
		//
		// Measured on 2026-09-04 (B-067): the EIGHT dead links this gate reported in the
		// tree were the eight false ones, and four of them were precisely the security
		// runbooks delivered by bullet 2 of the item itself. Since a non-empty `DeadLinks`
		// forces `Pass = false`, the gate failed over no defect at all.
		//
		// An empty path after the cut is still dead: `Stat` on the root would answer
		// "exists" for a `runbook_url` that points at no file at all.
		if i := strings.IndexAny(path, "#?"); i >= 0 {
			path = path[:i]
		}
		if path == "" {
			out.DeadLinks = append(out.DeadLinks, DeadLink{
				File: file, Alert: alert, URL: url,
				Reason: "runbook_url has no file path",
			})
			continue
		}
		if repo != canonicalRepo {
			// Another repository: the path does not exist in this tree by definition, and
			// asserting it does not exist in THAT one would require the network. Counted,
			// not accused.
			out.UnverifiableLinks++
			continue
		}
		// The path comes from the CONTENT of a YAML file — untrusted input. Without
		// handling, a `runbook_url` with `../../..` would make this `Stat` ask the disk for
		// a file OUTSIDE the tree, and one that existed would answer "runbook ok". gosec
		// flagged it (G703) and was right.
		//
		// `Clean("/"+path)` resolves the `..` against the ROOT and not against `root`, so
		// the result always starts with `/` and always lands inside `root` after the Join —
		// the traversal becomes an ordinary relative path that simply does not exist.
		//
		// I also wrote a containment check here and REMOVED it: after the `Clean` it had no
		// way to fire. An unreachable guard is dead code that reads as protection, and this
		// is the wrong repository to leave one of those in. What proves the neutralisation
		// is `TestTraversalDoesNotReachFileOutsideRoot`, which puts a REAL file outside and
		// requires that it not be accepted.
		target := filepath.Join(root, filepath.Clean("/"+path))
		if _, err := os.Stat(target); err != nil {
			out.DeadLinks = append(out.DeadLinks, DeadLink{
				File: file, Alert: alert, URL: url,
				Reason: path + " does not exist in this repository",
			})
		}
	}
}

// missingRunbookAlerts returns alert names in content lacking a runbook
// annotation and not in the exception set.
func missingRunbookAlerts(content string, excepted map[string]struct{}) []string {
	var missing []string
	blocks := alertSplitRe.Split(content, -1)
	for _, blk := range blocks[1:] { // [0] is the pre-first-alert preamble
		m := alertNameRe.FindStringSubmatch(blk)
		if m == nil {
			continue
		}
		name := m[1]
		body := blk
		if len(body) > runbookLookahead {
			body = body[:runbookLookahead]
		}
		if strings.Contains(body, "runbook_url") || strings.Contains(body, "runbook:") {
			continue
		}
		if _, ok := excepted[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}
