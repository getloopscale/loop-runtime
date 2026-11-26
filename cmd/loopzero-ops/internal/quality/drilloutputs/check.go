// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package drilloutputs implements the EC-02 gate (plan 2026-05-16 T1.1.bis):
// grep-audit the repo for LEGACY consumers of the pulumi-drill outputs schema.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check-drill-outputs-consumers.sh + its hermetic test
// (D6 AUTO-DIE). Pure-Go (filepath walk + regexp string-match) — no external
// CLI. The `pulumi`/`kubectl` strings in the patterns are search targets, NOT
// executions, so this is a genuine MIGRATE-GO-PUREFN (not a shell-wrap).
//
// Background: T1.1 migrated drill outputs from singular (v0) / object-literal
// (v1) to flat named exports (v2). A consumer left on v0/v1 gets an undefined
// kubeconfig → kubectl falls back to the default context, which MAY be prod
// after a prior drill teardown. This gate fails CI if any legacy pattern
// survives outside the pulumi-drill source itself.
package drilloutputs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// legacyPatterns are the v0/v1 + phantom-output regexes. The original bash used
// two PCRE negative-lookaheads (`cluster\b(?!Kubeconfig|Map|Endpoint)` and
// `\.outputs\.cluster\b(?!Map)`); both are REDUNDANT with the preceding `\b`
// (a word boundary after "cluster" already excludes clusterKubeconfig/Map/
// Endpoint), so the RE2-compatible forms below drop the lookaheads with no
// behaviour change.
var legacyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`pulumi stack output cluster\b`),                               // v0 bare singular
	regexp.MustCompile(`pulumi stack output clusterKubeconfig`),                       // v0 singular kubeconfig
	regexp.MustCompile(`\bstack[._]output\.cluster\b`),                                // JSON .cluster (singular)
	regexp.MustCompile(`\bDRILL_CLUSTER_KUBECONFIG\b`),                                // legacy env var
	regexp.MustCompile(`\.outputs\.(mgmt|build|runtime|runtimeCluster2?)Cluster2?\.`), // v1 nested
	regexp.MustCompile(`\.outputs\.cluster\b`),                                        // v0 .outputs.cluster
	regexp.MustCompile(`\.outputs\.bucket\b`),                                         // v0 .outputs.bucket
	regexp.MustCompile(`\.outputs\.pg\.`),                                             // v1 .outputs.pg.X
	regexp.MustCompile(`\.outputs\.buckets\.`),                                        // v1 .outputs.buckets.X
	regexp.MustCompile(`\.outputs\.zone\.`),                                           // v1 .outputs.zone.X
	regexp.MustCompile(`\.outputs\.vpc\.`),                                            // v1 .outputs.vpc.X
	regexp.MustCompile(`"\s*\+\s*\.(mgmt|build|runtime)Cluster2?\.`),                  // jq concat `" + .mgmtCluster.X`
	regexp.MustCompile(`"\s*\+\s*\.pg\.(host|port|uri|database)`),                     // jq concat `" + .pg.host`
	regexp.MustCompile(`pulumi stack output apiUrl`),                                  // phantom output (no schema has it)
}

// DefaultExcludeDirs are pruned anywhere in the tree.
var DefaultExcludeDirs = []string{
	"node_modules", "dist", "build", ".git", ".terraform", "vendor",
	".claude",            // docs may quote legacy patterns deliberately
	"infra/pulumi-drill", // the source itself — patterns are legitimate here
}

// DefaultScanExts are the file extensions scanned.
var DefaultScanExts = []string{".sh", ".yml", ".yaml", ".go", ".ts", ".tsx", ".js"}

// selfPkgFragment is the gate's own package path — skipped because check.go
// contains every pattern as a string literal (would self-match).
const selfPkgFragment = "cmd/loopzero-ops/internal/quality/drilloutputs/"

// Config drives the gate.
type Config struct {
	Root        string
	ExcludeDirs []string // default DefaultExcludeDirs
	ScanExts    []string // default DefaultScanExts
}

// Outcome captures the result.
type Outcome struct {
	Pass     bool
	Findings []string // "file:line: <trimmed content>"
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check scans cfg.Root for legacy drill-outputs consumer patterns.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	excl := cfg.ExcludeDirs
	if excl == nil {
		excl = DefaultExcludeDirs
	}
	exts := cfg.ScanExts
	if exts == nil {
		exts = DefaultScanExts
	}
	extSet := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		extSet[e] = struct{}{}
	}

	out := &Outcome{Pass: true}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, path)
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			for _, x := range excl {
				// Match a dir component (e.g. "node_modules") or a path prefix
				// (e.g. "infra/pulumi-drill").
				if d.Name() == x || relSlash == x || strings.HasPrefix(relSlash, x+"/") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if _, ok := extSet[filepath.Ext(path)]; !ok {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), selfPkgFragment) {
			return nil // skip the gate's own source
		}
		return scanFile(path, out)
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out.Findings)
	if len(out.Findings) > 0 {
		out.Pass = false
	}
	return out, nil
}

func scanFile(path string, out *Outcome) error {
	f, err := os.Open(path) // #nosec G304 -- path comes from walking the repo root
	if err != nil {
		return fmt.Errorf("%s: unreadable, so it was never scanned: %w", path, err)
	}
	defer f.Close()
	lineNum := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		lineNum++
		for _, re := range legacyPatterns {
			if re.MatchString(line) {
				out.Findings = append(out.Findings,
					fmt.Sprintf("%s:%d: %s", path, lineNum, strings.TrimSpace(line)))
				break // one finding per line is enough
			}
		}
	}
	return sc.Err()
}
