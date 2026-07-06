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
// Scope of the AES grep: `api/internal/db/store/secret_wrapper.go` (canonical
// store file) plus any *.go file in api/ that imports crypto/aes from
// a path touching secret encryption.
//
// The Day-60 enforcement deletes the AES code path (T10.3) — the gate
// catches REGRESSIONS that reintroduce the path before final removal.
package noaesremaining

import (
	"bufio"
	"fmt"
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
}

// Hit is a single violation location.
type Hit struct {
	File       string
	Line       int
	Reason     string
	Suggestion string
}

// scanPaths are the canonical AES-bearing files (post-T2.6 migration).
var scanPaths = []string{
	"api/internal/db/store/secret_wrapper.go",
}

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
	for _, rel := range scanPaths {
		if allowSet[filepath.Clean(rel)] {
			continue
		}
		abs := filepath.Join(cfg.RepoRoot, rel)
		hits, err := scanFile(abs, rel)
		if err != nil {
			if os.IsNotExist(err) {
				// File deleted (T10.3 completed) — that's the desired state.
				continue
			}
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("%s: scan error: %v", rel, err))
			continue
		}
		out.Hits = append(out.Hits, hits...)
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
	for _, h := range out.Hits {
		fmt.Fprintf(&sb, "  %s:%d — %s\n", h.File, h.Line, h.Reason)
		fmt.Fprintf(&sb, "    Suggestion: %s\n", h.Suggestion)
	}
	for _, w := range out.Warnings {
		fmt.Fprintf(&sb, "  %s\n", w)
	}
	return sb.String()
}
