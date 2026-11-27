// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package platformcrnamespace refuses a versioned manifest that applies a platform CR
// (`runtime.getloopscale.com` Tenant or Environment) outside `loop-runtime-system`.
//
// B-095. Two facts make the placement load-bearing, and neither is obvious from reading the manifest:
//
//  1. The tenant-operator's cache is restricted to `loop-runtime-system`
//     (`operators/cmd/tenant-operator/main.go`). A CR applied in another namespace is INVISIBLE
//     to the controller — no error, nothing in the log, it simply never reconciles.
//  2. The admission policy `loop-runtime-tenant-namespace-policy` matches CREATE and UPDATE and validates
//     `namespace == "loop-runtime-system"`. Removing a finalizer is an UPDATE: a misplaced CR, with the
//     policy on, becomes IMPOSSIBLE TO DELETE.
//
// The first makes the drift go unnoticed; the second makes it expensive later. That is why the
// check is a gate and not a review item.
package platformcrnamespace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// ErrOutsideNamespace means a manifest applies a platform CR outside `loop-runtime-system`.
var ErrOutsideNamespace = errors.New("platform CR applied outside loop-runtime-system")

// ErrNothingScanned means the scan reached no manifest at all — green over nothing.
var ErrNothingScanned = errors.New("no platform CR manifest reached")

// CanonicalNamespace is the only namespace where Tenant and Environment may live.
const CanonicalNamespace = "loop-runtime-system"

// DefaultDirs are the trees scanned when Config.Dirs is empty.
var DefaultDirs = []string{"infra", "charts", "cmd"}

// Config parameterises the scan.
type Config struct {
	RepoRoot string
	Dirs     []string
}

// Occurrence is a manifest block that declares a platform CR.
type Occurrence struct {
	File      string
	Line      int
	Kind      string
	Namespace string
}

// Output reports what the scan saw. `Checked` exists to tell "nothing wrong" apart from
// "reached nothing" — without it, zero violations and zero coverage are the same green.
type Output struct {
	Checked    []Occurrence
	Violations []Occurrence
	Files      int
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Output) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

var (
	reKind           = regexp.MustCompile(`^\s*kind:\s*(Tenant|Environment)\s*$`)
	reNamespace      = regexp.MustCompile(`^\s*namespace:\s*["']?([A-Za-z0-9_.${}:-]+)["']?\s*$`)
	reAPILoopRuntime = regexp.MustCompile(`^\s*apiVersion:\s*runtime\.getloopscale\.com/`)
)

// Check scans the trees and returns the violations.
func Check(cfg Config) (Output, error) {
	var out Output
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = DefaultDirs
	}

	for _, d := range dirs {
		root := filepath.Join(cfg.RepoRoot, filepath.FromSlash(d))
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err //nolint:wrapcheck // traversal error propagates raw on purpose
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".yaml", ".yml", ".sh":
			default:
				return nil
			}
			// #nosec G304,G122 -- `p` comes from WalkDir over the trees declared in Config.Dirs,
			// under the repository itself; there is no external input on this path.
			b, rerr := os.ReadFile(filepath.Clean(p))
			if rerr != nil {
				return nil
			}
			rel, _ := filepath.Rel(cfg.RepoRoot, p)
			out.Files++
			out.Checked = append(out.Checked, occurrencesIn(string(b), filepath.ToSlash(rel))...)
			return nil
		})
		if err != nil {
			return out, fmt.Errorf("scanning %s: %w", d, err)
		}
	}

	if len(out.Checked) == 0 {
		return out, fmt.Errorf("%w: %d file(s) read", ErrNothingScanned, out.Files)
	}

	for _, o := range out.Checked {
		if !namespaceAccepted(o.Namespace) {
			out.Violations = append(out.Violations, o)
		}
	}
	sort.Slice(out.Violations, func(i, j int) bool {
		if out.Violations[i].File != out.Violations[j].File {
			return out.Violations[i].File < out.Violations[j].File
		}
		return out.Violations[i].Line < out.Violations[j].Line
	})

	if len(out.Violations) > 0 {
		var b strings.Builder
		for _, v := range out.Violations {
			fmt.Fprintf(&b, "\n  %s:%d — %s in %q", v.File, v.Line, v.Kind, v.Namespace)
		}
		out.Violation = fmt.Errorf("%w (expected %q):%s", ErrOutsideNamespace, CanonicalNamespace, b.String())
		return out, nil
	}
	return out, nil
}

// namespaceAccepted accepts the canonical namespace and also a shell variable — its value is
// decided at runtime, and hard-coding a judgement here about what it holds would be guessing. The
// seeds that use one declare `loop-runtime-system` as the default, and that is verifiable in the script itself.
func namespaceAccepted(ns string) bool {
	if ns == CanonicalNamespace {
		return true
	}
	return strings.HasPrefix(ns, "$")
}

// occurrencesIn finds `kind: Tenant|Environment` blocks under `apiVersion: runtime.getloopscale.com/` and the
// namespace that goes with them. Deliberately textual: the targets include heredocs inside shell
// scripts, which no YAML parser reads without running the shell first.
func occurrencesIn(content, file string) []Occurrence {
	lines := strings.Split(content, "\n")
	var out []Occurrence
	for i, l := range lines {
		m := reKind.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		// The anchor is the `apiVersion: runtime.getloopscale.com/` immediately above — without it, `kind: Environment`
		// would match any same-named CRD from another group.
		if i == 0 || !reAPILoopRuntime.MatchString(lines[i-1]) {
			continue
		}
		ns := ""
		for j := i + 1; j < len(lines) && j <= i+8; j++ {
			if nm := reNamespace.FindStringSubmatch(lines[j]); nm != nil {
				ns = nm[1]
				break
			}
		}
		out = append(out, Occurrence{File: file, Line: i + 1, Kind: m[1], Namespace: ns})
	}
	return out
}
