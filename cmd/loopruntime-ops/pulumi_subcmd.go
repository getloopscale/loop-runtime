// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/pulumi/archive"
)

const pulumiHelp = `loopruntime-ops pulumi — Pulumi operational tooling

USAGE:
  loopruntime-ops pulumi <command> [flags]

COMMANDS:
  archive   Export Pulumi stack state to encrypted S3 archive.
            Flags:
              --root DIR        repo root (default: ".")
              --dry-run         report intended exports, no S3 write
              --archive-only    skip pre-flight bucket checks (quarterly mode)
              --bucket NAME     override archive bucket
              --accept-plaintext-hits
                                upload archives the plaintext-secret scan flagged (after review)
`

// runPulumi dispatches the `loopruntime-ops pulumi <command>` subcommands.
func runPulumi(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, pulumiHelp)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(pulumiHelp)
		return 0
	case "archive":
		return runPulumiArchive(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown pulumi command: %s\n\n%s", args[0], pulumiHelp)
		return 1
	}
}

// runPulumiArchive replaces pulumi-archive.sh: exports Pulumi stack state to
// encrypted S3. Shells out to pulumi/aws via internal/shellrun (testable via mock).
func runPulumiArchive(args []string) int {
	fs := flag.NewFlagSet("pulumi archive", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	dryRun := fs.Bool("dry-run", false, "report intended exports without writing to S3")
	archiveOnly := fs.Bool("archive-only", false, "skip pre-flight bucket checks (quarterly mode)")
	bucket := fs.String("bucket", "", "override archive bucket")
	acceptHits := fs.Bool("accept-plaintext-hits", false, "upload archives whose plaintext-secret scan found candidates, after review")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := archive.Run(archive.Config{
		Root:                *root,
		DryRun:              *dryRun,
		ArchiveOnly:         *archiveOnly,
		ArchiveBucket:       *bucket,
		AcceptPlaintextHits: *acceptHits,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pulumi archive: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	if !outcome.Pass {
		return 1
	}
	return 0
}
