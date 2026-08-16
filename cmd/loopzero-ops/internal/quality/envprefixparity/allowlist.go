// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package envprefixparity

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultAllowlistRelPath is where the dated exemptions live, next to the gate
// that reads them — the same shape docsrefs and code-quality already use.
const DefaultAllowlistRelPath = "cmd/loopzero-ops/internal/quality/envprefixparity/allowlist.txt"

// Exemption covers ONE prefix on the SET axis, until a date.
//
// It deliberately cannot cover the case axis, and cannot cover "the set axis"
// wholesale: it names a prefix. A sixth diverging prefix fails the gate even
// while GOOGLE_ is exempt, which is what keeps the exemption from becoming a
// hole the size of the rule.
type Exemption struct {
	Prefix string
	Sunset time.Time
	Reason string
	Line   int
}

// parseAllowlist reads the exemption file. A missing file is not an error: a
// repository with no known gap should not be forced to carry an empty one.
func parseAllowlist(path string) ([]Exemption, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Exemption
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("%s:%d: malformed entry, want `prefix | sunset | reason`", path, n)
		}
		sunset, perr := time.Parse("2006-01-02", strings.TrimSpace(parts[1]))
		if perr != nil {
			return nil, fmt.Errorf("%s:%d: bad sunset %q: %w", path, n, strings.TrimSpace(parts[1]), perr)
		}
		out = append(out, Exemption{
			Prefix: strings.TrimSpace(parts[0]),
			Sunset: sunset,
			Reason: strings.TrimSpace(parts[2]),
			Line:   n,
		})
	}
	return out, sc.Err()
}

// activeExemptions returns the prefixes still covered at `now`.
//
// An expired entry is dropped rather than reported as covering: the whole point
// of a date is that the gap comes back into view when it passes. Callers see the
// divergence re-fire, which is the intended alarm.
func activeExemptions(exs []Exemption, now time.Time) map[string]bool {
	active := map[string]bool{}
	for _, e := range exs {
		// Sunset is INCLUSIVE — the entry expires the day AFTER.
		if !now.After(e.Sunset.AddDate(0, 0, 1)) {
			active[e.Prefix] = true
		}
	}
	return active
}

// loadExemptions resolves the allowlist for a repo root.
func loadExemptions(repoRoot, override string, now time.Time) (map[string]bool, []Exemption, error) {
	path := override
	if path == "" {
		path = filepath.Join(repoRoot, DefaultAllowlistRelPath)
	}
	exs, err := parseAllowlist(path)
	if err != nil {
		return nil, nil, err
	}
	return activeExemptions(exs, now), exs, nil
}
