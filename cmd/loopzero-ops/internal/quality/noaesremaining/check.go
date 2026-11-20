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
// Every non-test `*.go` file under each entry of scanRoots is scanned:
// `api/`, `pkg/` and `operators/api/`, which is where the modules that
// compile into an api binary live inside this repository. See scanRoots for
// the `go list -deps` measurement behind that set, and for the one module it
// cannot reach. The package doc used to claim one directory — and before
// that, "secret_wrapper.go plus any *.go file in api/ that imports
// crypto/aes" — while the implementation read one hardcoded path,
// `api/internal/db/store/secret_wrapper.go`, and nothing else. The
// difference was not academic. Measured on 2026-09-05 (B-162), against the
// binary of the day:
//
//   - a tree with the canonical file absent → PASS, because a missing file
//     was read as "T10.3 completed"; the gate could not tell a finished
//     migration from a wrong `-root`;
//   - a full AES-256-GCM implementation moved one file sideways, into
//     `api/internal/db/store/secret_wrapper_aes.go` → PASS.
//
// And measured on 2026-09-06 (B-195), against the binary of that day: the
// same implementation moved one DIRECTORY over, into `pkg/vault/`, → PASS
// again, because the walk stopped at `api/`. Same evasion, same ADR, one
// `mv` further out.
//
// `docs/decisions.md` names that
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
	"fmt"
	"go/scanner"
	"go/token"
	"path/filepath"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/linescan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
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

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Hit is a single violation location.
type Hit struct {
	File       string
	Line       int // 0 when the finding is about the scan itself, not a line
	Reason     string
	Suggestion string
}

// scanRoots are the directory trees walked for AES markers: the in-repo homes
// of every module that compiles into an api binary.
//
// MEASURED, NOT ASSUMED. `go list -deps ./cmd ./cmd/controller`, run in the api
// module on 2026-09-06, returns 943 packages. The first-party ones belong to
// four modules, and only three of those live under this repository root:
//
//	github.com/getloopscale/loopzero/api            85 pkgs  ->  api/
//	github.com/getloopscale/loopzero/pkg            10 pkgs  ->  pkg/
//	github.com/getloopscale/loopzero/operators/api   1 pkg   ->  operators/api/
//	github.com/getloopscale/contracts       3 pkgs  ->  outside this repo, see GAP
//
// The root used to be `api` alone, and the reason first written for it was
// false: "everything the api module compiles into a running binary lives under
// here". The gated file itself imports pkg/errors. The list above is what a
// measurement says in place of that sentence.
//
// WHAT THE NARROW ROOT COST, measured on the tree before this widening: a
// complete AES-256-GCM implementation -- write path, `gcm.Seal` and all --
// moved into `pkg/vault/legacy_aes.go`, with `secret_wrapper.go` delegating to
// it, builds clean (`go build ./internal/db/store/` and `go build ./vault/`,
// both exit 0) and drew `PASS (ENFORCED)`, `Scanned: 374 of 374`. That is the
// evasion the 2026-09-04 ADR names and rejects ("relocating the code would pass
// the implementation while defeating the stated intent"), closed one file
// sideways and left open one directory over. Walking the modules that actually
// link is that rejection made mechanical at the boundary that matters.
//
// GRANULARITY IS THE MODULE DIRECTORY, NOT THE PACKAGE LIST, and it over-scans
// on purpose: pkg/ holds 65 non-test .go files while only 10 of its packages
// reach an api binary. Pinning the exact package list would be tighter and
// would rot in silence the first time an import changed -- and a stale scope in
// a security gate reads, from the outside, exactly like coverage. A directory
// does not rot. The over-scan is priced: 374 -> 451 non-test files (+77), and
// zero new findings -- the same three hits in secret_wrapper.go before and
// after, none from the two added roots, because no file under pkg/ or
// operators/api/ names AES at all. (Repo-wide the one other non-test match is
// aesMarkers in this file; cmd/loopzero-ops is not a scan root and does not link
// into an api binary, so it is out of scope rather than excused.) If some pkg/
// package that never links into api ever needs AES for a legitimate reason,
// -allowlist is the declared way to say so.
//
// GAP, NAMED RATHER THAN PAPERED OVER: loopzero-contracts is a separate repository
// (`use ../loopzero-contracts` in go.work), so three packages of the api binary sit
// outside any path a `-root`-relative walk can reach. This gate does not cover
// them and cannot; a gate in that repository would have to. The widening below
// changes nothing about that, and writing it down costs less than the next
// reader inferring from "we scan what links" that it was handled.
var scanRoots = []string{"api", "pkg", "operators/api"}

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
		foundBefore := out.FilesFound
		keep := func(rel string) bool {
			return strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go")
		}
		// An absent root is not an error here: the zero-found floor below is
		// what turns "nothing was read" into a violation, and it does so with a
		// message that says which root was empty.
		_, walkErr := linescan.Walk(cfg.RepoRoot, []string{rootRel}, keep, func(rel string, data []byte) error {
			out.FilesFound++
			if allowSet[filepath.Clean(rel)] {
				return nil
			}
			out.FilesScanned++
			hits, scanErr := scanFile(data, rel)
			if scanErr != nil {
				// A file counted as found and then not read would let the one
				// file holding the AES path vouch for itself; fail closed.
				return fmt.Errorf("%s: %w", rel, scanErr)
			}
			out.Hits = append(out.Hits, hits...)
			return nil
		})
		if walkErr != nil {
			return Outcome{}, fmt.Errorf("noaesremaining: walking %s: %w", rootRel, walkErr)
		}
		if out.FilesFound == foundBefore {
			// PER ROOT, and that is the point of putting it here rather than
			// after the loop. The floor exists so a mistyped -root or a moved
			// tree cannot read as a clean bill of health. Summed over three
			// roots it would only catch the case where ALL of them are empty:
			// api/ present and pkg/ relocated would keep the total above zero,
			// the gate would report PASS, and its stated scope would once again
			// exceed the tree it actually read -- which is the exact defect
			// B-195 exists to fix. Widening the roots without also splitting
			// this floor would have rebuilt that defect inside its own repair.
			out.Hits = append(out.Hits, Hit{
				File: rootRel,
				Reason: fmt.Sprintf(
					"scanned 0 Go files under %s — a scan that read nothing there cannot show T10.3 is done",
					rootRel),
				Suggestion: fmt.Sprintf(
					"check -root: it must point at the repository root, the one holding %s — "+
						"or, if -root is right, %s moved and scanRoots must follow it",
					canonicalStoreFile, rootRel),
			})
		}
	}

	// Found-but-unread, inverted: the gate read nothing and would otherwise
	// report the cleanest possible result. Deleting the AES path and pointing
	// the gate at the wrong tree are indistinguishable from the outside, and
	// only one of them is the desired state. The check now lives inside the
	// walk loop above, once per root, for the reason written there.

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

// maskGoComments blanks the CONTENT of Go comments, preserving every byte offset
// and every newline so line numbers do not move.
//
// It fails toward MORE detection: a file the scanner cannot read cleanly is
// returned unchanged, so a malformed file is still matched in full. A masker that
// silently swallowed a marker would be the worse failure for a gate whose subject
// is that the marker is gone.
func maskGoComments(src []byte) []byte {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, scanner.ScanComments)

	out := append([]byte(nil), src...)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		start := file.Offset(pos)
		end := start + len(lit)
		if start < 0 || end > len(out) {
			continue
		}
		for i := start; i < end; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	if s.ErrorCount > 0 {
		return src
	}
	return out
}

func scanFile(src []byte, relPath string) ([]Hit, error) {
	var hits []Hit

	// COMMENTS ARE MASKED WITH GO'S OWN SCANNER, not tracked line by line.
	//
	// The line-based version set inComment on any line holding `/*` and then
	// `continue`d on any line holding `*/` -- so a line carrying BOTH discarded
	// itself, live code included. An audit measured the consequence on
	// 2026-09-06: a complete AES-256-GCM implementation whose three marker lines
	// each ended in `/* ... */` compiled, was gofmt-clean, and returned PASS
	// inside the very scan root this item had just widened. A one-token bypass in
	// a gate whose whole subject is that AES is gone.
	//
	// STRINGS ARE NOT MASKED, and that is deliberate: the primary marker lives in
	// one. `import "crypto/aes"` is an import path, which is a string literal, so
	// masking strings would hide exactly what this gate is looking for.
	//
	// The technique is already in this repository -- skip-link masks comments and
	// literals with go/scanner for the same reason. It was not carried across
	// when this gate was written.
	lines, err := linescan.Lines(maskGoComments(src))
	if err != nil {
		return nil, err
	}
	for i, line := range lines {
		lineNum := i + 1

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
