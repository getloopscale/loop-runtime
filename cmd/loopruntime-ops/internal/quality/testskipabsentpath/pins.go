// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// parsePins reads the declaration file. A missing file is NOT an empty pin:
// the pin is committed, so its absence means it was deleted or the path
// drifted, and reporting a clean run over a pin nobody read is the exact
// failure this gate exists to prevent (the same reasoning B-006 applied to the
// frozen gate-set oracle).
func parsePins(path string) ([]Pin, error) {
	// #nosec G304 -- path is the pin file the gate is configured to read.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading the pinned declaration %s: %w -- the pin is committed, so its "+
			"absence is a broken setup, not an empty list of tracked skips", path, err)
	}
	defer func() { _ = f.Close() }()

	var pins []Pin
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		var kept []string
		for _, fld := range strings.Split(line, "\t") {
			if strings.TrimSpace(fld) != "" {
				kept = append(kept, strings.TrimSpace(fld))
			}
		}
		if len(kept) < 4 {
			return nil, fmt.Errorf("%s:%d: cannot parse %q -- expected four TAB-separated fields: "+
				"<test file>\t<func>\t<guarded path>\t<why, naming what covers this instead>", path, n, line)
		}
		p := Pin{
			File:   filepath.ToSlash(kept[0]),
			Func:   kept[1],
			Path:   filepath.ToSlash(filepath.Clean(kept[2])),
			Reason: strings.Join(kept[3:], " "),
			Line:   n,
		}
		if prev, dup := seen[p.Key()]; dup {
			return nil, fmt.Errorf("%s:%d: %s is declared twice (first at line %d) -- one skip, one answer",
				path, n, p.Key(), prev)
		}
		seen[p.Key()] = n
		pins = append(pins, p)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return pins, nil
}
