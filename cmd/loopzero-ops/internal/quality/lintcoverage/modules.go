// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrNoModules means the sweep found no Go module.
//
// Absence is a FAILURE, never "no modules, all fine": a sweep that reached nothing and
// reports green is the shape this repository already paid for in `B-040`, `B-124` and `B-143`.
var ErrNoModules = errors.New("no Go module found — finding nothing is not the same as finding nothing wrong")

// skipDirs are subtrees that contain third-party `go.mod` files.
//
// A closed, short list on purpose. What it does NOT do is list our own modules — it was
// exactly a hand-written list of modules that left five out of `task lint` without anyone
// noticing, because the gate stayed green over what it did not look at.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"testdata":     true,
}

// DiscoverModules lists every Go module in the tree, relative to root.
//
// Derived from DISK, never declared: a new module enters on its own, which is the property that
// was missing. The ordering is stable so that the output and the baseline are deterministic —
// a baseline whose order varies produces a diff nobody can review.
func DiscoverModules(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable directory is not a module; total absence fails below
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() != "go.mod" {
			return nil
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(p))
		if rerr != nil {
			return nil //nolint:nilerr // a path outside the root is not our module
		}
		if rel == "." {
			return nil // this repository has no go.mod at the root (see CLAUDE.md § Multi-module)
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, ErrNoModules
	}
	return out, nil
}

// ModuleDir resolves a module's absolute directory.
func ModuleDir(root, mod string) string {
	return filepath.Join(root, filepath.FromSlash(mod))
}
