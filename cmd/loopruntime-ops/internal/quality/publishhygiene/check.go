// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package publishhygiene refuses a versioned file that carries a real host address
// or a workstation path.
//
// # Why here, and not only in loopruntime-rag
//
// Of the **20** sibling repositories measured on 2026-08-17, **one** had this lock —
// `loopruntime-rag/scripts/check-publish-hygiene.sh` — and that is why the leak surfaced
// there: it failed `develop` over a line that came from the kit. In the other 19 the
// same leak aged in silence.
//
// This repository publishes OCI charts and images, so it is one of the places where
// publication is real. And **published history is public RETROACTIVELY** — unlike
// almost every other technical debt, this one does not undo.
//
// # The criterion is HOME PATH, and loopruntime-rag's IP half was NOT carried over
//
// **Path**: only a REAL user home. `/home/{runner,node,dev,…}` are CI or synthetic and
// pass — that boundary is what separates this lock from one that fails the security
// test itself.
//
// The IP half was left out, and the reason is measurement, not laziness. Run against
// this tree it produced **104 findings, almost all false**:
//
//	`13.3.2.1-1`, `15.5-10`   VERSION strings matching the four-octet format
//	`1.2.3.4`                 the canonical placeholder of the DNS/CF tests
//	`173.245.48.0/20`         Cloudflare CIDR ranges, which the code carries on
//	                          purpose to recognise a trusted proxy
//	`paulo@165.227.121.20`    the live target DECLARED in `rules/live-target.txt`,
//	                          which `cycle-discover.md` requires for live-test mode
//
// Closing them one by one would be an arms race in which every exception weakens the
// signal, and the last line is not even a leak: it is a contract. The IP half stays
// where it was calibrated — in `loopruntime-rag`, whose profile is different. Promising IP
// coverage here would be the same overstatement this ecosystem measured in three items
// this session.
//
// # And what it does NOT scan, which loopruntime-rag does
//
// The audit trail. `docs/README.md` declares that `docs/wiki/`, `plans/completed/`,
// `operations/baselines/`, `meetings/records/`, `operations/findings/` and `reviews/`
// **must never be edited to match the present** — rewriting a dated record destroys
// the only evidence of what was true then.
//
// That is not convenience: measured in this repository, **738 of the 796** home-path
// occurrences are there, and **495 in one pair of JUnit artifacts**. Scanning everything
// would produce a lock that can only go green by breaking another repository rule — and a
// lock that cannot go green gets switched off.
//
// The honest consequence is that this gate protects what is **written from now on**,
// not what was already recorded. Saying so is the alternative to a green that
// overstates itself.
package publishhygiene

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// ErrLeak means a versioned live file carries a host address or workstation path.
var ErrLeak = errors.New("a versioned file carries a workstation path — published history is public retroactively")

// ErrNothingScanned means the scan reached no file.
//
// Absence is a FAILURE, never a skip: a scan that reached nothing reporting green is
// the `PASS — 0 routes` shape this repository has already paid for three times.
var ErrNothingScanned = errors.New("no file scanned — finding nothing is not the same as finding nothing wrong")

var (
	// homeRE finds a user home path; syntheticHomeRE removes CI, container and
	// fixture users.
	homeRE = regexp.MustCompile(`/(home|Users)/[a-z][a-z0-9_-]*/`)
	// `k6` was added by measurement: it is the user INSIDE the k6 image
	// (`mountPath: /home/k6/scripts`), not anybody's workstation.
	syntheticHomeRE = regexp.MustCompile(`/(home|Users)/(runner|node|dev|user|ubuntu|app|vscode|test|x|k6)/`)
	// urlHomeRE discards a `/home/` that is a URL SEGMENT, not a disk path —
	// `https://example.com/home/product` matched the rule and leaks nothing.
	// Measured in `landscape.yml`, which catalogues hundreds of external projects.
	urlHomeRE = regexp.MustCompile(`https?://[^\s"'` + "`" + `]*/home/`)
)

// auditTrailPrefixes are the dated records `docs/README.md` forbids editing.
var auditTrailPrefixes = []string{
	"docs/wiki/",
	"docs/plans/completed/",
	"docs/operations/baselines/",
	"docs/operations/evidence/",
	"docs/operations/findings/",
	"docs/meetings/",
	"docs/reviews/",
	"docs/knowledge-base/",
	"docs/scripts-audit/",
	"docs/handoff/",
	".claude/knowledge-base/",
	".claude/agents/",
	".claude/.compaction-snapshots/",
	// `.claude/records/` is the canonical cycle-artifact tree (`rules/records-location.md`):
	// alignment briefs, plans, edge-case reviews, deps-audit reports, implementation logs,
	// mini-reviews, maintenance-runs, and cycle events. Every one of these is DATED evidence
	// that the docs/README convention explicitly forbids editing to match the present —
	// which is exactly what a workstation-path scrub of an already-written brief would do.
	// A brief's "Measured on <date> in <workstation path> at <sha>" line is the measurement,
	// not code, and rewriting it retroactively destroys the only evidence of what was true
	// when the measurement was taken.
	".claude/records/",
	"archive/",
	"CHANGELOG.md",
	"BACKLOG.md",
}

// Exemption is a live file allowed to carry the address, with a reason and a sunset.
type Exemption struct {
	Path   string
	Reason string
	Sunset string // YYYY-MM-DD
}

// DefaultExemptions carries the debt this gate cannot resolve by editing text.
var DefaultExemptions = []Exemption{
	{
		Path:   "infra/local/Caddyfile",
		Sunset: "2026-11-17",
		Reason: "The three `root *` are DEPLOYMENT paths that resolve on the droplet and serve " +
			"app-dev today. Parameterising with `{$VAR:default}` keeps the literal in the file and " +
			"closes nothing; parameterising without a default breaks the edge if the variable is not " +
			"set where Caddy starts. The durable fix is moving the dashboard out of one person's home — " +
			"an environment migration, with its own risk, not a hygiene edit. Measured " +
			"2026-08-17 in B-130; registered as its own item.",
	},
}

// Config drives the check.
type Config struct {
	RepoRoot   string
	Exemptions []Exemption
}

// Finding is one leak.
type Finding struct {
	File string
	Line int
	Text string
	Kind string // "home" — the IP half was not carried over; see the package doc
}

// ErrUnverified means a versioned file could not be read, so nothing can be said
// about what it would publish.
var ErrUnverified = errors.New("a versioned file could not be read — its content is unverified, not clean")

// Outcome reports the scan.
type Outcome struct {
	Scanned  int
	Findings []Finding
	Exempt   int
	// Binary counts versioned files skipped as binary (they hold a NUL byte).
	Binary int
	// Unverified lists versioned files that could not be read, with the reason.
	Unverified []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// Check scans every versioned live file.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	exempt := map[string]bool{}
	for _, e := range cfg.exemptions() {
		if strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.Sunset) == "" {
			out.Violation = fmt.Errorf("%w: exemption for %q has no reason or no sunset", ErrLeak, e.Path)
			return out, nil
		}
		exempt[e.Path] = true
	}

	files, err := versionedFiles(root)
	if err != nil {
		return out, err
	}
	for _, f := range files {
		rel := f.path
		if isAuditTrail(rel) {
			continue
		}
		if exempt[rel] {
			out.Exempt++
			continue
		}
		data, ferr := f.publishedContent(root)
		if ferr != nil {
			out.Unverified = append(out.Unverified, fmt.Sprintf("%s: %v", rel, ferr))
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			out.Binary++
			continue
		}
		out.Scanned++
		out.Findings = append(out.Findings, scanText(string(data), rel)...)
	}
	if len(out.Findings) > 0 {
		out.Violation = ErrLeak
		return out, nil
	}
	if len(out.Unverified) > 0 {
		return out, fmt.Errorf("%w: %s", ErrUnverified, strings.Join(out.Unverified, "; "))
	}
	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}
	return out, nil
}

func (c Config) exemptions() []Exemption {
	if c.Exemptions == nil {
		return DefaultExemptions
	}
	return c.Exemptions
}

// isAuditTrail reports whether a path is a dated record that must not be rewritten.
func isAuditTrail(rel string) bool {
	for _, p := range auditTrailPrefixes {
		if rel == p || strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// symlinkMode is the git index mode of a versioned symbolic link.
const symlinkMode = "120000"

// versionedFile is one entry of the git index.
type versionedFile struct {
	path    string
	symlink bool
}

// publishedContent is what git publishes for the entry. For a symlink that is
// the target text, never the target's content: a link to a directory is a
// readable one-line blob, and the directory's own files are versioned — and
// scanned — under their own paths.
func (f versionedFile) publishedContent(root string) ([]byte, error) {
	full := filepath.Clean(filepath.Join(root, f.path))
	if f.symlink {
		target, err := os.Readlink(full)
		if err != nil {
			return nil, err
		}
		return []byte(target), nil
	}
	return os.ReadFile(full)
}

// versionedFiles lists what git tracks — what will be published, not the
// working dir — with the index mode that says how each entry is published.
func versionedFiles(root string) ([]versionedFile, error) {
	cmd := exec.Command("git", "ls-files", "-s", "-z")
	cmd.Dir = root
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w", root, err)
	}
	var out []versionedFile
	for _, entry := range strings.Split(string(body), "\x00") {
		if entry == "" {
			continue
		}
		// "<mode> <object> <stage>\t<path>"
		meta, path, ok := strings.Cut(entry, "\t")
		if !ok {
			return nil, fmt.Errorf("git ls-files -s in %s: unexpected entry %q", root, entry)
		}
		mode, _, _ := strings.Cut(meta, " ")
		out = append(out, versionedFile{path: path, symlink: mode == symlinkMode})
	}
	return out, nil
}

// scanText reports every workstation home path in content, line by line. The
// whole file is read first, so no line is too long to be scanned.
func scanText(content, rel string) []Finding {
	var out []Finding
	for i, line := range strings.Split(content, "\n") {
		if homeRE.MatchString(line) && !syntheticHomeRE.MatchString(line) && !urlHomeRE.MatchString(line) {
			out = append(out, Finding{File: rel, Line: i + 1, Text: strings.TrimSpace(line), Kind: "home"})
		}
	}
	return out
}
