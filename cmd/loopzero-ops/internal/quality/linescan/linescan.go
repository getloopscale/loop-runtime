// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package linescan is the file-tree half the forbidden-token gates share: walk
// a set of repository directories, hand each kept file's bytes to the gate, and
// split content into numbered lines.
//
// It exists because the same loop was written once per gate, and the copies
// disagreed on the parts that decide whether a gate can be trusted: one skipped
// a file it could not read, one ignored the scanner error that a line longer
// than its buffer raises (which ends the scan early and reports the rest of the
// file clean), one discarded the error from filepath.Rel. Here each of those is
// an error returned to the gate. What counts as a violation stays in each gate.
package linescan

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// maxLine is the longest line Lines accepts. Past it, bufio.Scanner stops with
// ErrTooLong; Lines returns that error rather than a truncated file.
const maxLine = 4 * 1024 * 1024

// Lines splits data on newlines, without a trailing empty element, numbering
// lines the way line-oriented tools do (index i is line i+1).
func Lines(data []byte) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading line %d: %w", len(lines)+1, err)
	}
	return lines, nil
}

// Walked reports what a walk reached, so a gate can tell a clean scan from a
// scan over nothing.
type Walked struct {
	// Dirs counts the requested roots that exist.
	Dirs int
	// Missing lists the requested roots that do not exist.
	Missing []string
	// Files counts the files handed to the visitor.
	Files int
}

// Visit receives a kept file's repository-relative, slash-separated path and
// its content.
type Visit func(rel string, data []byte) error

// Walk visits every regular file under root/dirs for which keep(rel) is true.
// A root that does not exist is recorded in Missing; every other walk, read or
// visit error stops the walk and is returned.
func Walk(root string, dirs []string, keep func(rel string) bool, visit Visit) (Walked, error) {
	return WalkPruned(root, dirs, nil, keep, visit)
}

// WalkPruned is Walk that does not descend into a directory whose base name is
// in prune.
func WalkPruned(root string, dirs []string, prune map[string]bool, keep func(rel string) bool, visit Visit) (Walked, error) {
	var w Walked
	for _, dir := range dirs {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); errors.Is(err, fs.ErrNotExist) {
			w.Missing = append(w.Missing, dir)
			continue
		} else if err != nil {
			return w, fmt.Errorf("stat %s: %w", dir, err)
		}
		w.Dirs++
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if path != base && prune[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				// WalkDir does not descend into a symlinked directory; reading the
				// link as a file would fail. A dangling link is still read, and fails.
				if st, err := os.Stat(path); err == nil && st.IsDir() {
					return nil
				}
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return fmt.Errorf("locating %s under %s: %w", path, root, err)
			}
			rel = filepath.ToSlash(rel)
			if !keep(rel) {
				return nil
			}
			data, err := os.ReadFile(path) // #nosec G304 -- path comes from walking the repository
			if err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			w.Files++
			return visit(rel, data)
		})
		if err != nil {
			return w, fmt.Errorf("scan %s: %w", dir, err)
		}
	}
	return w, nil
}
