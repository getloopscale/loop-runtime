// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package clibundlesize implements the T9.3 gate: fail CI if the compiled CLI
// bundle (cli/dist/*.js) grows beyond a documented byte cap.
//
// Plan T5.3 of docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/check_cli_bundle_size.sh. Pure-Go (filepath walk +
// size sum) — replaces bash find/awk. No external CLI.
//
// T9.3 (lazy-load OTel) was deferred per KISS+YAGNI; this gate is the
// regression guard — if a PR bloats dist/ past the cap, the build fails.
package clibundlesize

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaxBytes is the 4 MiB cap (baseline ~1 MiB, ~3 MiB headroom).
const DefaultMaxBytes int64 = 4 * 1024 * 1024

// Config drives the gate.
type Config struct {
	Root     string // repo root (default ".")
	DistDir  string // dist dir relative to root (default "cli/dist")
	MaxBytes int64  // default DefaultMaxBytes
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Skipped    bool // dist dir not present
	TotalBytes int64
	MaxBytes   int64
}

// Check sums cli/dist/*.js bytes and compares against the cap. Per-entry stat
// errors are tolerated (skipped), mirroring the bash gate's `find -printf`
// behaviour, so Check has no failure mode of its own.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	distRel := cfg.DistDir
	if distRel == "" {
		distRel = "cli/dist"
	}
	maxBytes := cfg.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	dist := filepath.Join(root, distRel)

	info, err := os.Stat(dist)
	if err != nil || !info.IsDir() {
		// dist not built yet (or not a dir) — skip (bash: PASS when absent).
		return &Outcome{Pass: true, Skipped: true, MaxBytes: maxBytes}
	}

	var total int64
	_ = filepath.WalkDir(dist, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil // tolerate per-entry errors; skip dirs + non-.js
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})

	return &Outcome{
		Pass:       total <= maxBytes,
		TotalBytes: total,
		MaxBytes:   maxBytes,
	}
}
