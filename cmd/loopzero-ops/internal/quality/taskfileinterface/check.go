// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package taskfileinterface implements the Taskfile public-interface gate
// from MEET-2026-05-03-062 (DX engineer condition #5): public Taskfile
// commands MUST resolve identically across refactors.
//
// Plan T5.3 of docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces scripts/check-taskfile-interface.sh. The bash version shelled out
// to `task --list-all` and parsed its output (requiring the `task` binary).
// This pure-Go version parses the Taskfile YAML files DIRECTLY — no `task`
// binary dependency, no shell-out. Per D5 + Anti-Pattern #8: a genuine
// pure-logic migration (eliminates external dependency, not shell-wrap).
package taskfileinterface

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ExpectedCommands is the public Taskfile contract. These commands MUST
// resolve. Referenced by CI workflows, onboarding docs, CLAUDE.md.
// Adding = strengthening the contract; removing = breaking change.
var ExpectedCommands = []string{
	"setup",
	"doctor",
	"test",
	"k3d:heal",
	"dev:up",
	"db:migrate",
	"db:reset",
	"validate:matrix",
	"parity:preflight",

	// `cli:link` and `cli:build` were removed here on 2026-07-30 (issue #254).
	// They ran `cd cli && npm …`, but `cli/` left this repository at M2 of the
	// monorepo decomposition, so they had been failing for anyone who invoked
	// them. Building the CLI is the responsibility of the repo that owns it
	// (`loopzero-cli`); it is no longer part of THIS Taskfile's public contract.
	//
	// Per the comment above, removing an entry is a breaking change to the
	// contract — deliberate here, because the contract was asserting a
	// capability this repository stopped having two milestones ago. Keeping
	// the entries would have forced the tasks back into existence to satisfy
	// a promise nobody could keep.
}

// Config tells Check where the repo root is.
type Config struct {
	RepoRoot string
}

// Outcome captures the gate result.
type Outcome struct {
	Pass    bool
	Missing []string
}

// taskfileDoc is the minimal YAML shape we parse: the `tasks:` map keys are
// task names; `includes:` maps include-name → include spec.
type taskfileDoc struct {
	Tasks    map[string]yaml.Node      `yaml:"tasks"`
	Includes map[string]includeSpec    `yaml:"includes"`
}

// includeSpec is the include directive. `taskfile:` may be a string (path) or
// (rarely) a map; we only handle the common string form. flatten is recorded
// for completeness but all LoopZero includes flatten into root namespace.
type includeSpec struct {
	Taskfile string `yaml:"taskfile"`
	Flatten  bool   `yaml:"flatten"`
}

// Check parses the repo's Taskfile.yml + flattened includes and verifies every
// ExpectedCommand resolves.
func Check(cfg Config) (*Outcome, error) {
	names, err := CollectTaskNames(cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	outcome := &Outcome{Pass: true}
	for _, cmd := range ExpectedCommands {
		if !names[cmd] {
			outcome.Pass = false
			outcome.Missing = append(outcome.Missing, cmd)
		}
	}
	return outcome, nil
}

// CollectTaskNames parses Taskfile.yml at root + every flattened include and
// returns the set of all defined task names. Missing/malformed includes are
// skipped (best-effort) so the gate degrades gracefully rather than crashing
// on an unrelated include error.
func CollectTaskNames(repoRoot string) (map[string]bool, error) {
	rootPath := filepath.Join(repoRoot, "Taskfile.yml")
	rootDoc, err := parseTaskfile(rootPath)
	if err != nil {
		return nil, fmt.Errorf("parse root Taskfile.yml: %w", err)
	}

	names := map[string]bool{}
	for name := range rootDoc.Tasks {
		names[name] = true
	}

	// Parse each include (flattened into root namespace).
	for _, inc := range rootDoc.Includes {
		if inc.Taskfile == "" {
			continue
		}
		incPath := filepath.Join(repoRoot, filepath.Clean(inc.Taskfile))
		incDoc, err := parseTaskfile(incPath)
		if err != nil {
			// Distinguish missing FILE (optional include — skip) from a parse
			// ERROR on existing content (broken Taskfile — surface loud). A
			// duplicate task key, for example, is a real defect Task v3 would
			// silently shadow; the gate must NOT swallow it.
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("include %q parse error (broken Taskfile): %w", inc.Taskfile, err)
		}
		for name := range incDoc.Tasks {
			names[name] = true
		}
	}

	return names, nil
}

// parseTaskfile reads and unmarshals a single Taskfile YAML.
func parseTaskfile(path string) (*taskfileDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc taskfileDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return &doc, nil
}
