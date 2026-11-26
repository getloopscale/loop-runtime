// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T1.1 of plan dev-public-env-config-completeness (umbrella).
//
// TestRuntimeEnvCompleteness asserts the install-config-contract surface
// (∪ of CONFIG.md + SECRETS.md keys across the 7 env/secret charts) is
// set-equal to the checked-in runtime manifest fixture derived from the
// env master table — and that every SECRET runtime var is materialized
// by a producer (ADR D4 / EC-1). Hermetic: reads only checked-in files
// (no kubectl, no helm, no cross-repo .md live-read — D2).
//
// Sub-tests close the 5 edge cases from edge-case-plan absorbed v1.1:
//   - T1: completeness  — 0 req=Y orphan + 0 phantom (matrix lock)
//   - T2: anti-vacuity  — len(runtimeVars) >= MIN (parser-drop guard) [EC-2]
//   - T3: fixture sync  — manifest row count matches the producers / master-table floor [EC-5]
//   - T4: SECRET-materialization — every SECRET runtime var has a producer row [EC-1/D4]
//   - T5: camelAPI/UPPER_SNAKE no false flag — viteApiUrl ↔ VITE_API_URL [EC-4]
//
// Reuses helpers from sibling tests (same package `charts`): mdTableKeys,
// camelAPI, fileExists.
package charts

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// minRuntimeVars is the anti-vacuity floor (EC-2). The current master
// table has 200 distinct (var,chart-section) runtime rows. We choose a
// floor of 150 — leaves slack for legitimate consolidation but FAILs
// loud if the parser silently drops > 25% of rows.
const minRuntimeVars = 150

// minSecretProducers is the floor on producer rows (EC-5 companion).
// The fixture authored from the master table covers 39 distinct SECRET
// runtime vars. Floor 30 leaves slack for legitimate consolidation while
// catching parser-drops of producers (orphan-FAIL canary).
const minSecretProducers = 30

// fixturesDir resolves loop-runtime/infra/tests/fixtures relative to this test
// file (`infra/tests/charts/`). Same convention as chartsRoot.
func fixturesDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs(filepath.Join("..", "fixtures"))
	if err != nil {
		t.Fatalf("resolve fixtures dir: %v", err)
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatalf("fixtures dir missing at %s: %v", d, err)
	}
	return d
}

// manifestRow is one parsed row of runtime-env-manifest.txt.
type manifestRow struct {
	Var     string
	Plane   string // CONFIG | SECRET
	Req     string // Y | when-X | N | — | Y(...) variants
	Section string // chart section, e.g. loop-console, loop-runtime-api, loop-runtime-{app,...}-operator
}

// parseManifest reads runtime-env-manifest.txt. Skips comments + blank
// lines. Returns rows in file order. Hermetic: no I/O outside the file.
func parseManifest(t *testing.T) []manifestRow {
	t.Helper()
	p := filepath.Join(fixturesDir(t), "runtime-env-manifest.txt")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open %s: %v", p, err)
	}
	defer f.Close()
	var rows []manifestRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			t.Fatalf("malformed manifest row %q (want VAR|plane|req|section)", line)
		}
		rows = append(rows, manifestRow{
			Var: parts[0], Plane: parts[1], Req: parts[2], Section: parts[3],
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", p, err)
	}
	return rows
}

// parseKnownGaps reads env-completeness-known-gaps.txt. Returns two sets
// keyed by VAR for `orphan` and `phantom` kinds. The presence of a VAR in
// the matching set DOWNGRADES the test verdict to WARN for that VAR; new
// orphans/phantoms NOT in the allowlist FAIL. Each allowlist row carries
// an explicit justification — empty justifications are themselves an
// authorship FAIL (handled at parse time below).
func parseKnownGaps(t *testing.T) (orphans, phantoms map[string]bool) {
	t.Helper()
	orphans = map[string]bool{}
	phantoms = map[string]bool{}
	p := filepath.Join(fixturesDir(t), "env-completeness-known-gaps.txt")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open %s: %v", p, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			t.Fatalf("malformed known-gaps row %q (want KIND|VAR|justification)", line)
		}
		if strings.TrimSpace(parts[2]) == "" {
			t.Fatalf("known-gaps row %q has empty justification — refusing silent debt", line)
		}
		switch parts[0] {
		case "orphan":
			orphans[parts[1]] = true
		case "phantom":
			phantoms[parts[1]] = true
		default:
			t.Fatalf("known-gaps row %q has invalid KIND %q (want orphan|phantom)", line, parts[0])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", p, err)
	}
	return orphans, phantoms
}

// parseSecretProducers reads secret-producers.txt. Returns set of VAR
// names that HAVE a producer row. A SECRET runtime var absent from this
// set is an orphan FAIL (D4 / EC-1).
func parseSecretProducers(t *testing.T) map[string]bool {
	t.Helper()
	p := filepath.Join(fixturesDir(t), "secret-producers.txt")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open %s: %v", p, err)
	}
	defer f.Close()
	have := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			t.Fatalf("malformed producer row %q (want VAR|kind|id)", line)
		}
		have[parts[0]] = true
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", p, err)
	}
	return have
}

// contractSurface returns the union of UPPER_SNAKE keys documented in
// each of the 7 install-config-contract charts' CONFIG.md + SECRETS.md.
// Keys originating from a dashboard-style camelCase CONFIG.md (e.g.
// viteApiUrl) are normalized via camelAPI inversion to UPPER_SNAKE so
// they compare correctly against the master-table runtime keys (EC-4).
func contractSurface(t *testing.T) map[string]bool {
	t.Helper()
	root := filepath.Join(fixturesDir(t), "..", "..", "..", "charts")
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolve charts root: %v", err)
	}
	// The 7 install-config-contract charts (CONFIG.md and/or SECRETS.md
	// shipped). loop-runtime-build-cwts ships only SECRETS.md (build-time
	// secrets); loop-console-dashboard ships only CONFIG.md (camelCase keys);
	// loop-console ships only SECRETS.md (CONFIG comes from cloud-cfg
	// runtime).
	charts := []string{
		"loop-runtime-api",
		"loop-runtime-app-operator",
		"loop-runtime-build-cwts",
		"loop-console",
		"loop-console-dashboard",
		"loop-runtime-platform-operator",
		"loop-runtime-preview-operator",
		"loop-runtime-tenant-operator",
	}
	surface := map[string]bool{}
	for _, c := range charts {
		cfg := filepath.Join(root, c, "CONFIG.md")
		sec := filepath.Join(root, c, "SECRETS.md")
		if fileExists(cfg) {
			for _, k := range mdTableKeys(t, cfg) {
				surface[normalizeKey(k)] = true
			}
		}
		if fileExists(sec) {
			for _, k := range mdTableKeys(t, sec) {
				surface[normalizeKey(k)] = true
			}
		}
	}
	return surface
}

// normalizeKey accepts UPPER_SNAKE OR camelCase keys and returns the
// canonical UPPER_SNAKE form so EC-4 false flags (viteApiUrl ↔
// VITE_API_URL) are avoided. UPPER_SNAKE is returned as-is.
func normalizeKey(k string) string {
	if strings.IndexByte(k, '_') >= 0 || isAllUpper(k) {
		return k
	}
	// camelCase → UPPER_SNAKE
	var b strings.Builder
	for i, r := range k {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r - 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isAllUpper(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

// TestRuntimeEnvCompleteness composes all 5 sub-tests. Per-sub-test
// failures Errorf (continue) so the full report shows every gap in one
// run; Fatalf is used only for unrecoverable parse errors.
func TestRuntimeEnvCompleteness(t *testing.T) {
	rows := parseManifest(t)

	// Sub-test T2 — anti-vacuity floor (EC-2). Run FIRST so a broken
	// parser FAILs loud before anything else.
	t.Run("AntiVacuityFloor", func(t *testing.T) {
		if len(rows) < minRuntimeVars {
			t.Fatalf("EC-2 floor breached: parsed %d runtime rows < min %d — parser silently dropped master-table rows", len(rows), minRuntimeVars)
		}
		t.Logf("EC-2 floor OK: %d rows >= %d", len(rows), minRuntimeVars)
	})

	producers := parseSecretProducers(t)

	// Sub-test T3 — fixture-sync companion floor (EC-5). The producer
	// fixture must cover the unique SECRET runtime vars; a producer
	// drop is itself an orphan FAIL.
	t.Run("ProducerFloor", func(t *testing.T) {
		if len(producers) < minSecretProducers {
			t.Fatalf("EC-5 producer floor breached: %d producer rows < min %d — secret-producers.txt drifted from master table", len(producers), minSecretProducers)
		}
		t.Logf("EC-5 producer floor OK: %d rows >= %d", len(producers), minSecretProducers)
	})

	surface := contractSurface(t)

	knownOrphans, knownPhantoms := parseKnownGaps(t)

	// Sub-test T1 — completeness: 0 NEW req=Y orphan + 0 NEW phantom.
	// orphan = manifest VAR NOT in contract surface; phantom = surface
	// key NOT in any manifest VAR. req=Y orphans FAIL; req=N|when-X|—
	// soft orphans WARN (Q1 of plan). Orphans/phantoms in
	// env-completeness-known-gaps.txt are DOWNGRADED to WARN with
	// rationale (the allowlist gate refuses silent debt).
	t.Run("Completeness", func(t *testing.T) {
		manifestSet := map[string]bool{}
		var newHardOrphans, newSoftOrphans, allowedHardOrphans []string
		seen := map[string]bool{}
		for _, r := range rows {
			manifestSet[r.Var] = true
			if surface[r.Var] {
				continue
			}
			if seen[r.Var] {
				continue
			}
			seen[r.Var] = true
			isHard := strings.HasPrefix(r.Req, "Y")
			switch {
			case knownOrphans[r.Var]:
				if isHard {
					allowedHardOrphans = append(allowedHardOrphans, r.Var)
				} else {
					// soft + allowlisted — log as WARN
					newSoftOrphans = append(newSoftOrphans, r.Var)
				}
			case isHard:
				newHardOrphans = append(newHardOrphans, r.Var)
			default:
				newSoftOrphans = append(newSoftOrphans, r.Var)
			}
		}
		var newPhantoms, allowedPhantoms []string
		for k := range surface {
			if manifestSet[k] {
				continue
			}
			if knownPhantoms[k] {
				allowedPhantoms = append(allowedPhantoms, k)
			} else {
				newPhantoms = append(newPhantoms, k)
			}
		}
		sort.Strings(newHardOrphans)
		sort.Strings(newSoftOrphans)
		sort.Strings(allowedHardOrphans)
		sort.Strings(newPhantoms)
		sort.Strings(allowedPhantoms)
		if len(newSoftOrphans) > 0 {
			t.Logf("WARN soft orphans (req!=Y or allowlisted soft): %v", newSoftOrphans)
		}
		if len(allowedHardOrphans) > 0 {
			t.Logf("WARN allowlisted HARD orphans (see env-completeness-known-gaps.txt): %v", allowedHardOrphans)
		}
		if len(allowedPhantoms) > 0 {
			t.Logf("WARN allowlisted PHANTOMS (see env-completeness-known-gaps.txt): %v", allowedPhantoms)
		}
		if len(newHardOrphans) > 0 {
			t.Errorf("NEW HARD orphans (req=Y, manifest-but-not-in-contract; NOT in allowlist): %v", newHardOrphans)
		}
		if len(newPhantoms) > 0 {
			t.Errorf("NEW PHANTOMS (in contract surface but NOT in runtime manifest; NOT in allowlist): %v", newPhantoms)
		}
	})

	// Sub-test T4 — SECRET materialization (EC-1 / D4). Every distinct
	// SECRET runtime VAR must have a producer row.
	t.Run("SecretMaterialization", func(t *testing.T) {
		seenSecret := map[string]bool{}
		var unproduced []string
		for _, r := range rows {
			if r.Plane != "SECRET" {
				continue
			}
			if seenSecret[r.Var] {
				continue
			}
			seenSecret[r.Var] = true
			if !producers[r.Var] {
				unproduced = append(unproduced, r.Var)
			}
		}
		sort.Strings(unproduced)
		if len(unproduced) > 0 {
			t.Errorf("EC-1/D4 unproduced SECRET runtime vars (no row in secret-producers.txt): %v", unproduced)
		}
	})

	// Sub-test T5 — camelCase ↔ UPPER_SNAKE no false flag (EC-4).
	// Constructed pair: VITE_API_URL master-table form should match
	// viteApiUrl dashboard form via normalizeKey. The dashboard's
	// CONFIG.md ships keys in camelCase by design.
	t.Run("CamelUpperSnakeNoFalseFlag", func(t *testing.T) {
		got := normalizeKey("viteApiUrl")
		want := "VITE_API_URL"
		if got != want {
			t.Errorf("EC-4 normalize fail: viteApiUrl → %q (want %q)", got, want)
		}
		// Idempotence: UPPER_SNAKE passes through.
		if g := normalizeKey("VITE_API_URL"); g != want {
			t.Errorf("EC-4 normalize idempotence fail: VITE_API_URL → %q (want %q)", g, want)
		}
	})
}
