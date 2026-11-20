// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package shellscript is the one vocabulary the shell-script gates share for
// classifying a script: which CLIs make a script effectful, what a Python
// heredoc looks like, how a row of the shell-script allowlist is read, and the
// disposition codes a script can be proposed for.
//
// scripts-audit, unhomed-logic, no-new-bash and sprint-syntax used to carry
// their own copies and had drifted: two different "effectful CLI" lists (one
// without k3d, the other without helmfile and terraform), two heredoc
// patterns, two allowlist-row patterns, and two disposition vocabularies naming
// the same outcome differently (MIGRATE-GINKGO vs "MIGRATE-ginkgo (LIVE)").
package shellscript

import (
	"regexp"
	"strings"
)

// EffectfulCLI matches an invocation of a CLI that acts on live infrastructure.
// A script that runs one is not pure logic: porting it to Go would only wrap the
// same call (a forbidden shell-wrap).
var EffectfulCLI = regexp.MustCompile(`\b(kubectl|k3d|helm|helmfile|cosign|argocd|doctl|pulumi|terraform)\b`)

// PythonHeredoc matches a Python program embedded as a heredoc — the script IS a
// Python program wearing a bash costume.
var PythonHeredoc = regexp.MustCompile(`(?m)(python3?\s+(-\s+)?<<|<<\s*['"]?(PY|PYEOF|PYTHON))`)

// Disposition is a proposed fate for a shell script.
type Disposition string

// The disposition vocabulary, one name per outcome.
const (
	KeepOperator            Disposition = "KEEP-OPERATOR"
	KeepOrchestrator        Disposition = "KEEP-ORCHESTRATOR"
	DelegateHelmfile        Disposition = "DELEGATE-HELMFILE"
	DelegateArgoCD          Disposition = "DELEGATE-ARGOCD"
	MigrateGoPureFn         Disposition = "MIGRATE-GO-PUREFN"
	MigrateGoSDK            Disposition = "MIGRATE-GO-SDK"
	MigrateGinkgo           Disposition = "MIGRATE-GINKGO"
	MigrateGenerator        Disposition = "MIGRATE-GENERATOR"
	MigrateLoopZeroOpsDrill Disposition = "MIGRATE-LOOPZERO-OPS-DRILL"
	AutoDie                 Disposition = "AUTO-DIE"
	DeleteDead              Disposition = "DELETE-DEAD"
	ReviewNeeded            Disposition = "REVIEW-NEEDED"
)

// allowlistPath is the first cell of an allowlist row: a .sh path, never a
// template placeholder such as `<script-path>.sh`.
var allowlistPath = regexp.MustCompile(`^[^\s|<>]+\.sh$`)

// AllowlistRow is one data row of the markdown shell-script allowlist.
type AllowlistRow struct {
	// Path is the script path in the first cell.
	Path string
	// Cells are every cell after the first, trimmed.
	Cells []string
}

// ParseAllowlistRow reads one markdown table row whose first cell is a .sh path.
// It returns false for any other line: prose, the header, the separator row, or
// a template row with placeholder syntax.
func ParseAllowlistRow(line string) (AllowlistRow, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return AllowlistRow{}, false
	}
	cells := strings.Split(strings.TrimPrefix(trimmed, "|"), "|")
	if len(cells) < 2 {
		return AllowlistRow{}, false
	}
	path := strings.TrimSpace(cells[0])
	if !allowlistPath.MatchString(path) {
		return AllowlistRow{}, false
	}
	row := AllowlistRow{Path: path}
	for _, c := range cells[1:] {
		row.Cells = append(row.Cells, strings.TrimSpace(c))
	}
	// A row closed by a trailing pipe yields one empty cell after it.
	if n := len(row.Cells); n > 0 && row.Cells[n-1] == "" && strings.HasSuffix(trimmed, "|") {
		row.Cells = row.Cells[:n-1]
	}
	return row, true
}
