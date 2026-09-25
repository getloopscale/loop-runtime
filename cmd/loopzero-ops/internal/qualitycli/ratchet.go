// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/deadlineratchet"
)

// ratchetLabels are the words a deadline-ratchet gate reports with.
type ratchetLabels struct {
	dormant   string // dormant-state message
	subject   string // e.g. "cosign keyless cutover"
	exception string // exception file path (for the unblock hint)
	adr       string // governing ADR filename
}

// ratchetSetup builds the Setup of a deadline-ratchet gate (cosign cutover,
// service-auth rotation): a blown deadline with no sponsor exception fails;
// a warning window or a present exception passes with a warning.
func ratchetSetup(name string, spec deadlineratchet.Spec, lbl ratchetLabels) func(*flag.FlagSet) Check {
	return func(fs *flag.FlagSet) Check {
		asOf := fs.String("as-of", "", "reference date YYYY-MM-DD (testing; default: today)")
		return func(inv Invocation) (Outcome, error) {
			cfg := deadlineratchet.Config{Root: inv.Root}
			if *asOf != "" {
				t, err := time.Parse("2006-01-02", *asOf)
				if err != nil {
					return Fail, usageError("invalid --as-of date %q (want YYYY-MM-DD)", *asOf)
				}
				cfg.AsOf = t
			}
			o, err := deadlineratchet.Check(spec, cfg)
			if err != nil {
				return Fail, err
			}
			if o.Dormant {
				fmt.Fprintf(inv.Stdout, "[%s] %s\n", name, lbl.dormant)
				return Pass, nil
			}
			fmt.Fprintf(inv.Stdout, "[%s] start = %s; days since = %d\n", name, o.Date, o.Days)
			emit := ratchetEmitter(inv, name)
			switch {
			case o.Blocked:
				emit("error", fmt.Sprintf("%s DEADLINE BLOWN at day %d (>%d days). No sponsor exception found. "+
					"Per ADR %s this auto-blocks. To unblock: execute it and update the marker, OR create "+
					"%s with an 'approved-by: <name>' line.", lbl.subject, o.Days, spec.DeadlineDay, lbl.adr, lbl.exception))
				return Fail, nil
			case o.ExceptionPermitted:
				emit("warning", fmt.Sprintf("%s deadline (%dd) BLOWN at day %d — sponsor exception present. Permitting.",
					lbl.subject, spec.DeadlineDay, o.Days))
			case o.Warn:
				emit("warning", fmt.Sprintf("%s slip-alert: day %d of %d (deadline approaching). T0+%dd window open.",
					lbl.subject, o.Days, spec.DeadlineDay, spec.WarnDay))
			default:
				fmt.Fprintf(inv.Stdout, "[%s] within budget (day %d of %d) — OK.\n", name, o.Days, spec.DeadlineDay)
			}
			return Pass, nil
		}
	}
}

// ratchetEmitter writes a GitHub workflow command under GitHub Actions, and a
// plain labelled line anywhere else.
func ratchetEmitter(inv Invocation, name string) func(level, msg string) {
	if os.Getenv("GITHUB_ACTIONS") != "" {
		return func(level, msg string) { fmt.Fprintf(inv.Stdout, "::%s::%s\n", level, msg) }
	}
	return func(level, msg string) {
		fmt.Fprintf(inv.Stderr, "[%s] %s: %s\n", name, strings.ToUpper(level), msg)
	}
}
