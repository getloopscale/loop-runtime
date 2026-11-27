// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// `max_prepared_transactions` is declared, and declared as 0.
//
// Nothing in this ecosystem uses two-phase commit. Measured 2026-08-14 across all
// eight repositories: zero `PREPARE TRANSACTION`, zero `COMMIT PREPARED`. The 14
// grep hits were comments about two-phase upload/rollout designs and `sha512-…2Pc…`
// hashes in lockfiles.
//
// The value still has to be written down. A prepared transaction that nobody commits
// or rolls back holds its locks and its snapshot INDEFINITELY — it survives the
// client disconnecting, and it survives a restart, because that is the entire point
// of the mechanism. Against a tenant-migration or shard-cutover protocol, that makes
// the maximum transaction duration unbounded, and no quiesce window can be proven to
// close. PostgreSQL already defaults this to 0, so today the ecosystem is safe by
// inheritance — and an inherited default is exactly what this test refuses to rely
// on. A future operator raising it for one integration would silently remove the
// bound, with nothing in review to catch it.
//
// Declaring 0 explicitly also makes the decision reversible on purpose rather than by
// accident: whoever needs 2PC has to change this line, in a diff, with a reason.
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// valuesFiles are every Postgres values file that configures a real server.
func valuesFiles(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "infra", "helm", "postgresql")
	out := map[string]string{}
	for _, name := range []string{"values.yaml", "values.production.yaml"} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		out[name] = string(b)
	}
	return out
}

// declared matches `max_prepared_transactions = 0` allowing incidental whitespace.
var declared = regexp.MustCompile(`(?m)^\s*max_prepared_transactions\s*=\s*0\s*$`)

// anyValue matches the setting at any value, so a non-zero one fails loudly with
// the value quoted rather than reading as "not declared".
var anyValue = regexp.MustCompile(`(?m)^\s*max_prepared_transactions\s*=\s*(\S+)\s*$`)

func TestMaxPreparedTransactionsIsDeclaredZero(t *testing.T) {
	for name, body := range valuesFiles(t) {
		if declared.MatchString(body) {
			continue
		}
		if m := anyValue.FindStringSubmatch(body); m != nil {
			t.Errorf("%s declares max_prepared_transactions = %s, not 0.\n"+
				"Two-phase commit makes the maximum transaction duration unbounded: a prepared "+
				"transaction holds locks and its snapshot until someone commits or rolls it back, "+
				"across disconnects and restarts. No tenant-migration quiesce window closes while "+
				"that is possible. If 2PC is genuinely needed, that is a decision to argue for on "+
				"its own, not a value to raise in passing.", name, m[1])
			continue
		}
		t.Errorf("%s does not declare max_prepared_transactions.\n"+
			"PostgreSQL defaults it to 0, so the ecosystem is currently safe by inheritance — "+
			"which is what this test exists to stop relying on. Add `max_prepared_transactions = 0` "+
			"to primary.extendedConfiguration so the bound is a written decision, not luck.", name)
	}
}

// TestNoTwoPhaseCommitInTree keeps the premise honest. The declaration above is
// justified by nothing using 2PC; if that stops being true, the justification has to
// be revisited rather than the setting silently contradicting the code.
func TestNoTwoPhaseCommitInTree(t *testing.T) {
	root := repoRoot(t)
	stmt := regexp.MustCompile(`(?i)\b(PREPARE\s+TRANSACTION|COMMIT\s+PREPARED|ROLLBACK\s+PREPARED)\b`)

	var hits []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			if info != nil && info.IsDir() {
				base := info.Name()
				if base == "node_modules" || base == "vendor" || base == ".git" || base == "dist" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go", ".sql", ".ts":
		default:
			return nil
		}
		// This test names the statements it looks for, so it would match itself.
		if strings.HasSuffix(path, "postgres_prepared_transactions_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if stmt.Match(b) {
			rel, _ := filepath.Rel(root, path)
			hits = append(hits, rel)
		}
		return nil
	})

	if len(hits) > 0 {
		t.Errorf("two-phase commit appeared in: %s\n"+
			"The `max_prepared_transactions = 0` declaration rests on nothing using 2PC. "+
			"Either this usage goes, or that declaration needs a new argument.",
			strings.Join(hits, ", "))
	}
}
