// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package noaesremaining is the Go home for the no-aes-remaining quality
// gate (T10.2 of plan 2026-06-02-openbao-adoption v1.3).
//
// Calendar-gated: only enforced after T0+60d (where T0 = openbao-adoption
// v1.3 day-0 cutover). Before T+60d, the gate emits a WARN with countdown
// (operator visibility) but does NOT fail CI. After T+60d, it FAILs if
// any AES code path remains in the secret store (vault:v1: prefix is the
// only acceptable on-the-wire format).
//
// # Scope
//
// Every non-test `*.go` file under `api/` is scanned. The package doc used
// to claim exactly this — "secret_wrapper.go plus any *.go file in api/
// that imports crypto/aes" — while the implementation read one hardcoded
// path, `api/internal/db/store/secret_wrapper.go`, and nothing else. The
// difference was not academic. Measured on 2026-09-05 (B-162), against the
// binary of the day:
//
//   - a tree with the canonical file absent → PASS, because a missing file
//     was read as "T10.3 completed"; the gate could not tell a finished
//     migration from a wrong `-root`;
//   - a full AES-256-GCM implementation moved one file sideways, into
//     `api/internal/db/store/secret_wrapper_aes.go` → PASS.
//
// `docs/wiki/decisions/2026-09-04-t103-aes-write-path-removal.md` names that
// second move and rejects it on judgment ("relocating the code would pass
// the implementation while defeating the stated intent"). Walking the tree
// is that rejection made mechanical.
//
// Test files are deliberately excluded. `_test.go` never compiles into a
// running binary, so no production key path can hide there, and the legacy
// AES fixtures are what B-162 DoD bullet 3 leans on — the read path stays
// alive so pre-T10.3 rows stay readable, and it can only be tested by
// building a legacy blob from a fixture. Flagging those files would demand
// deleting the tests that prove the read path works.
//
// # Zero files FOUND is a violation, not a pass -- and `found`, not `scanned`, is the line. An allowlisted file was found; the operator excused it on purpose, and a declared exemption is not the blindness this floor is for
//
// A scan that found nothing proves nothing. A scan that found files and read none of them proves nothing either, but it says so: see the all-excused line below, added 2026-09-06 after an audit read this paragraph as promising a floor on `scanned` that the code does not have. If the walk finds no candidate
// file at all, the gate reports a violation instead of PASS, because the
// alternative is that a mistyped `-root` or a moved tree reads as a clean
// bill of health. Same shape as the `runbook-parity` floor (B-164): a
// missing directory stays tolerated, zero found does not.
//
// The Day-60 enforcement deletes the AES code path (T10.3) — the gate
// catches REGRESSIONS that reintroduce the path before final removal.
package noaesremaining

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CutoverDay is the openbao-adoption v1.3 T+0 reference (UTC).
// Operator updates this constant when the actual cutover happens.
// Until then, the value is the plan publish date.
var CutoverDay = time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

// EnforcementDelay is the calendar gate (T+60d per plan T10.2).
const EnforcementDelay = 60 * 24 * time.Hour

// Config tells Check which repo to scan + allows operator override.
type Config struct {
	RepoRoot       string
	AllowlistPaths []string
	// NowOverride lets tests pin Now() for deterministic gate behavior.
	NowOverride time.Time
}

// Outcome is the gate result.
type Outcome struct {
	Pass            bool
	Hits            []Hit
	Warnings        []string
	Enforced        bool      // false → gate emits WARN only (pre-T+60d)
	EnforcementTime time.Time // T0 + 60d UTC
	Now             time.Time // resolved Now() for trace
	FilesFound      int       // candidate non-test .go files under scanRoots
	FilesScanned    int       // of those, the ones not allowlisted away
}

// Hit is a single violation location.
type Hit struct {
	File       string
	Line       int // 0 when the finding is about the scan itself, not a line
	Reason     string
	Suggestion string
}

// scanRoots are the directory trees walked for AES markers.
//
// ONE ROOT, AND THE REASON FIRST WRITTEN FOR IT WAS FALSE. It read "everything the
// api module compiles into a running binary lives under here". Measured 2026-09-06:
// the gated file itself imports pkg/errors (secret_wrapper.go:28), and four non-test
// api files import pkg/vault -- a separate go.work module at the repo root. The
// widening from one hardcoded path to a walk is right; that justification for its
// SHAPE was not, and a right answer through a false premise is what exonerates the
// wrong scan root next time.
//
// What the narrow root costs is measured, not guessed: a complete AES-256-GCM
// implementation -- write path included -- moved into pkg/vault/ compiles, is
// reachable from api, and PASSES this gate. That is the same evasion the 2026-09-04
// ADR names and rejects, closed one file sideways and open one directory over.
// Tracked as B-195. There is no crypto/aes outside api/ today, so nothing is hiding
// now; what is missing is the mechanism, which is what
// makes a tree walk the right shape: the invariant is about the code that
// runs, not about one filename.
var scanRoots = []string{"api"}

// canonicalStoreFile is the file T10.3 is actually about. It is not the
// scan's boundary any more — it is kept only so the suggestion text can
// point somewhere useful.
const canonicalStoreFile = "api/internal/db/store/secret_wrapper.go"

// aesMarkers are heuristics flagging a remaining AES code path.
var aesMarkers = []string{
	"aes.NewCipher",
	"cipher.NewGCM",
	"crypto/aes",
}

// Check scans for remaining AES code paths.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		return Outcome{}, fmt.Errorf("noaesremaining: empty RepoRoot")
	}
	now := cfg.NowOverride
	if now.IsZero() {
		now = time.Now().UTC()
	}
	enforcementTime := CutoverDay.Add(EnforcementDelay)
	enforced := !now.Before(enforcementTime)

	allowSet := make(map[string]bool)
	for _, p := range cfg.AllowlistPaths {
		allowSet[filepath.Clean(p)] = true
	}

	out := Outcome{
		Enforced:        enforced,
		EnforcementTime: enforcementTime,
		Now:             now,
	}

	for _, rootRel := range scanRoots {
		absRoot := filepath.Join(cfg.RepoRoot, rootRel)
		walkErr := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				// An absent tree is not an error here: the zero-found floor
				// below is what turns "nothing was read" into a violation,
				// and it does so with a message that says which root was empty.
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(cfg.RepoRoot, p)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			out.FilesFound++
			if allowSet[filepath.Clean(rel)] {
				return nil
			}
			out.FilesScanned++
			hits, scanErr := scanFile(p, rel)
			if scanErr != nil {
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("%s: scan error: %v", rel, scanErr))
				return nil
			}
			out.Hits = append(out.Hits, hits...)
			return nil
		})
		if walkErr != nil {
			return Outcome{}, fmt.Errorf("noaesremaining: walking %s: %w", rootRel, walkErr)
		}
	}

	if out.FilesFound == 0 {
		// Found-but-unread, inverted: the gate read nothing and would
		// otherwise report the cleanest possible result. Deleting the AES
		// path and pointing the gate at the wrong tree are indistinguishable
		// from the outside, and only one of them is the desired state.
		out.Hits = append(out.Hits, Hit{
			File: strings.Join(scanRoots, ", "),
			Reason: fmt.Sprintf(
				"scanned 0 Go files under %s — a scan that read nothing cannot show T10.3 is done",
				strings.Join(scanRoots, ", ")),
			Suggestion: fmt.Sprintf(
				"check -root: it must point at the repository root, the one holding %s",
				canonicalStoreFile),
		})
	}

	if enforced {
		out.Pass = len(out.Hits) == 0
	} else {
		// Pre-enforcement: emit countdown WARN but pass CI.
		daysLeft := int(enforcementTime.Sub(now).Hours() / 24)
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("no-aes-remaining: WARN-only mode — enforcement begins %s (in %d days). Remaining hits: %d",
				enforcementTime.Format("2006-01-02"), daysLeft, len(out.Hits)))
		out.Pass = true
	}
	return out, nil
}

func scanFile(absPath, relPath string) ([]Hit, error) {
	// #nosec G304 -- absPath comes from filepath.WalkDir of cfg.RepoRoot/api.
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hits []Hit
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	lineNum := 0
	inComment := false
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Block-comment tracking (/* ... */).
		if strings.Contains(line, "/*") {
			inComment = true
		}
		if strings.Contains(line, "*/") {
			inComment = false
			continue
		}
		if inComment {
			continue
		}
		// Line comments — skip if marker is in the // part.
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		for _, m := range aesMarkers {
			if strings.Contains(line, m) {
				hits = append(hits, Hit{
					File:       relPath,
					Line:       lineNum,
					Reason:     fmt.Sprintf("AES code path remains (marker: %s); T10.3 demands deletion at T+60d", m),
					Suggestion: "remove the AES branch — only Transit (vault:v1:) prefix should remain in encryptForStore/decryptFromStore dispatcher",
				})
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// FormatOutcome renders the outcome for CLI output.
func FormatOutcome(out Outcome) string {
	// Every candidate excused: the walk found files and read none of them. The
	// operator's allowlist did that on purpose and the decision stands -- but a
	// plain PASS over it cannot be told apart from a scan that happened, which is
	// what an audit called this on 2026-09-06. The floor stays on FOUND (a declared
	// exemption is not blindness); what changes is that the exemption is audible.
	var excused string
	if out.FilesFound > 0 && out.FilesScanned == 0 {
		excused = fmt.Sprintf("no-aes-remaining: ALL EXCUSED — %d file(s) found, 0 read: "+
			"every candidate is on the allowlist, so this run checked nothing\n", out.FilesFound)
	}
	var sb strings.Builder
	mode := "ENFORCED"
	if !out.Enforced {
		mode = "WARN-only (pre-cutover)"
	}
	if out.Pass {
		fmt.Fprintf(&sb, "no-aes-remaining: PASS (%s)\n", mode)
	} else {
		fmt.Fprintf(&sb, "no-aes-remaining: FAIL (%s)\n", mode)
	}
	fmt.Fprintf(&sb, "  Enforcement starts: %s\n", out.EnforcementTime.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&sb, "  Now:                %s\n", out.Now.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&sb, "  Scanned:            %d of %d non-test .go files under %s\n",
		out.FilesScanned, out.FilesFound, strings.Join(scanRoots, ", "))
	for _, h := range out.Hits {
		if h.Line > 0 {
			fmt.Fprintf(&sb, "  %s:%d — %s\n", h.File, h.Line, h.Reason)
		} else {
			fmt.Fprintf(&sb, "  %s — %s\n", h.File, h.Reason)
		}
		fmt.Fprintf(&sb, "    Suggestion: %s\n", h.Suggestion)
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(&sb, "  %s\n", w)
	}
	return excused + sb.String()
}
