// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Command gen-dots regenerates DOT files for every state machine.
//
// Plan T5.2 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/gen-state-machine-dots.sh with idiomatic Go via
// `//go:generate`. Output is deterministic (Visualize sorts states +
// transitions) — drift between code and committed DOT signals unintended
// transition change in PR review.
//
// Usage:
//
//	gen-dots --check <dir>   # exit 1 if any DOT is out-of-date
//	gen-dots --write <dir>   # regenerate all DOTs
//
// Invoked via `go generate ./statemachine/...` from pkg/.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	sm "github.com/getloopscale/loopzero/pkg/statemachine"
)

// machines is the canonical state-machine registry. Add new entries here
// when introducing a new machine; gen-dots emits one DOT per entry.
func machines() map[string]*sm.Machine {
	return map[string]*sm.Machine{
		"build":              sm.NewBuildMachine(),
		"release":            sm.NewReleaseMachine(),
		"tenant":             sm.NewTenantMachine(),
		"domain":             sm.NewDomainMachine(),
		"environment":        sm.NewEnvironmentMachine(),
		"platform-bootstrap": sm.NewPlatformBootstrapMachine(),
		"promotion":          sm.NewPromotionMachine(),
		"rollback":           sm.NewRollbackMachine(),
		"application":        sm.NewApplicationMachine(),
		"exposed-route":      sm.NewExposedRouteMachine(),
		"preview":            sm.NewPreviewMachine(),
	}
}

func main() {
	check := flag.Bool("check", false, "exit 1 if any DOT file is out-of-date")
	write := flag.Bool("write", false, "regenerate all DOT files (default mode)")
	outDir := flag.String("out", "docs/state-machines", "output directory for DOT files")
	flag.Parse()

	if *check && *write {
		fmt.Fprintln(os.Stderr, "--check and --write are mutually exclusive")
		os.Exit(2)
	}
	if !*check && !*write {
		// Default to write (preserves previous shell-script default behavior
		// when invoked without mode flag from `go generate`).
		*write = true
	}

	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", *outDir, err)
		os.Exit(3)
	}

	switch {
	case *write:
		if err := writeAll(*outDir); err != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", err)
			os.Exit(3)
		}
		fmt.Printf("Wrote %d DOT files to %s\n", len(machines()), *outDir)
	case *check:
		drift, err := checkDrift(*outDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "check: %v\n", err)
			os.Exit(3)
		}
		if len(drift) > 0 {
			fmt.Fprintln(os.Stderr, "ERROR: state machine DOT files out-of-date:")
			for _, name := range drift {
				fmt.Fprintf(os.Stderr, "  - %s.dot\n", name)
			}
			fmt.Fprintln(os.Stderr, "Run: cd pkg && go generate ./statemachine/...")
			os.Exit(1)
		}
		fmt.Println("OK: state machine DOT files are up-to-date.")
	}
}

// writeAll regenerates every DOT file. Idempotent: matching content unchanged.
func writeAll(outDir string) error {
	for name, m := range machines() {
		path := filepath.Join(outDir, name+".dot")
		if err := os.WriteFile(path, []byte(m.Visualize()), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// checkDrift compares committed DOT files against current Visualize() output.
// Returns list of machine names whose files differ. Order is deterministic
// (lexicographic) so CI diff output is stable.
func checkDrift(outDir string) ([]string, error) {
	var drift []string
	for name, m := range machines() {
		path := filepath.Join(outDir, name+".dot")
		actual, err := os.ReadFile(path) //nolint:gosec // G304 false positive: path = fixed outDir flag + name from the compile-time machines() registry, never external input
		if err != nil {
			drift = append(drift, name)
			continue
		}
		expected := []byte(m.Visualize())
		if !bytes.Equal(actual, expected) {
			drift = append(drift, name)
		}
	}
	sort.Strings(drift) // machines() is a map: range order is random
	return drift, nil
}
