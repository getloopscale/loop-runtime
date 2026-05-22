// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/loopzero/loopzero/pkg/quality/nonewbash"
	"github.com/loopzero/loopzero/pkg/quality/scriptsaudit"
)

const qualityHelp = `loopzero-ops quality — quality gate orchestrator

USAGE:
  loopzero-ops quality <gate> [flags]

GATES:
  scripts-audit   Walk repo, classify .sh files, emit CSV to stdout
                  Flags:
                    --root DIR     repo root (default: ".")
                    --output FILE  write to file instead of stdout

  no-new-bash     Fail if PR introduces new .sh outside allowlist (D3 gate).
                  Requires fetch-depth: 0 in CI checkout.
                  Flags:
                    --root DIR        repo root (default: ".")
                    --allowlist PATH  allowlist MD (default: ".claude/rules/shell-scripts-allowlist.md")
                    --base-ref REF    base git ref (default: "origin/main")
                    --head-ref REF    head git ref (default: "HEAD")
`

func runQuality(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, qualityHelp)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(qualityHelp)
		return 0
	case "scripts-audit":
		return runScriptsAudit(args[1:])
	case "no-new-bash":
		return runNoNewBash(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown gate: %s\n\n%s", args[0], qualityHelp)
		return 1
	}
}

func runNoNewBash(args []string) int {
	fs := flag.NewFlagSet("no-new-bash", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", ".claude/rules/shell-scripts-allowlist.md", "allowlist markdown file")
	baseRef := fs.String("base-ref", "origin/main", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	absRoot, err := os.Getwd()
	if err == nil && *root == "." {
		// Resolve "." to absolute path for clearer error messages
		*root = absRoot
	}

	outcome, err := nonewbash.Check(nonewbash.Config{
		RepoRoot:      *root,
		AllowlistPath: *allowlist,
		BaseRef:       *baseRef,
		HeadRef:       *headRef,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-new-bash: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "no-new-bash: FAIL — new bash scripts detected outside allowlist:")
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "To fix: add the script to %s with justificativa per D4 criteria,\n", *allowlist)
		fmt.Fprintln(os.Stderr, "OR migrate the function to: helmfile (D2), pkg/quality/X Go gate (D5),")
		fmt.Fprintln(os.Stderr, "Ginkgo (T4), go:generate (T5.2). See docs/plans/2026-05-22-shell-script-strategic-plan.md")
		return 1
	}
	fmt.Println("no-new-bash: PASS — no new bash scripts outside allowlist")
	return 0
}

func runScriptsAudit(args []string) int {
	fs := flag.NewFlagSet("scripts-audit", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	output := fs.String("output", "", "write CSV to file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	scripts, err := scriptsaudit.ScanRepo(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scripts-audit: %v\n", err)
		return 5
	}

	var w *os.File
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scripts-audit: create %s: %v\n", *output, err)
			return 5
		}
		defer f.Close()
		w = f
	} else {
		w = os.Stdout
	}

	if err := scriptsaudit.EmitCSV(scripts, w); err != nil {
		fmt.Fprintf(os.Stderr, "scripts-audit: emit: %v\n", err)
		return 5
	}
	if *output != "" {
		// Visibility for operator running interactively
		fmt.Fprintf(os.Stderr, "scripts-audit: %d scripts inventoried to %s\n", len(scripts), *output)
	}
	return 0
}
