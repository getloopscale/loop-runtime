// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deadlineratchet"
)

// M9 Step 7: shared helpers for the quality gate funcs, decomposed out of the
// former quality_subcmd.go god file so the dispatcher (runQuality) imports ZERO
// gate packages. firstNonEmpty is a flag>env fallback; runRatchet is the shared
// CLI engine for the deadline-ratchet gates (cosign-cutover, service-auth-rotation)
// referenced from quality_gates_{supplychain,authz}.go
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism § Consequences).

// firstNonEmpty returns the first non-empty string (flag value > env fallback).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

type ratchetLabels struct {
	dormant   string // dormant-state message
	subject   string // e.g. "cosign keyless cutover"
	exception string // exception file path (for the unblock hint)
	adr       string // governing ADR filename
}

// runRatchet is the shared CLI handler for deadline-ratchet gates (cosign
// cutover §6.B.4, service-auth rotation §6.B.5). Exit: 0 OK/warn/exception,
// 1 usage/parse error, 2 blocked.
func runRatchet(name string, spec deadlineratchet.Spec, args []string, lbl ratchetLabels) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	asOfStr := fs.String("as-of", "", "reference date YYYY-MM-DD (testing; default: today)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg := deadlineratchet.Config{Root: *root}
	if *asOfStr != "" {
		t, err := time.Parse("2006-01-02", *asOfStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "::error::invalid --as-of date: %s\n", *asOfStr)
			return 1
		}
		cfg.AsOf = t
	}
	o, err := deadlineratchet.Check(spec, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error::%v\n", err)
		return 1
	}
	if o.Dormant {
		fmt.Printf("[%s] %s\n", name, lbl.dormant)
		return 0
	}
	fmt.Printf("[%s] start = %s; days since = %d\n", name, o.Date, o.Days)
	gha := os.Getenv("GITHUB_ACTIONS") != ""
	emit := func(level, msg string) {
		if gha {
			fmt.Printf("::%s::%s\n", level, msg)
		} else {
			fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", name, strings.ToUpper(level), msg)
		}
	}
	switch {
	case o.Blocked:
		emit("error", fmt.Sprintf("%s DEADLINE BLOWN at day %d (>%d days). No sponsor exception found. "+
			"Per ADR %s this auto-blocks. To unblock: execute it and update the marker, OR create "+
			"%s with an 'approved-by: <name>' line.", lbl.subject, o.Days, spec.DeadlineDay, lbl.adr, lbl.exception))
		return 2
	case o.ExceptionPermitted:
		emit("warning", fmt.Sprintf("%s deadline (%dd) BLOWN at day %d — sponsor exception present. Permitting.",
			lbl.subject, spec.DeadlineDay, o.Days))
		return 0
	case o.Warn:
		emit("warning", fmt.Sprintf("%s slip-alert: day %d of %d (deadline approaching). T0+%dd window open.",
			lbl.subject, o.Days, spec.DeadlineDay, spec.WarnDay))
		return 0
	default:
		fmt.Printf("[%s] within budget (day %d of %d) — OK.\n", name, o.Days, spec.DeadlineDay)
		return 0
	}
}

// resolveDotRoot expands a "." repo-root flag to the absolute cwd so gate error
// messages show a concrete path; no-op for an explicit root. Extracted from the
// ~70 gate handlers that repeated this idiom (M27 DRY: dry-gate-cli-scaffold).
func resolveDotRoot(root *string) {
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
}
