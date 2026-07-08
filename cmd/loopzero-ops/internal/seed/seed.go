// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package seed hosts the Go replacements for the canonical post-Cleanroom
// shell seeds (M15 — operator-glue-go-migration). Each seed implements the
// Seed interface and follows the canonical pipeline (blueprint ADR D2):
// read → guard(no-op) → precondition → CreateOrUpdate/patch → verify.
//
// The Verdict vocabulary is the parity contract with the bash seeds:
//   PASS — the seed converged the cluster (or was already converged)
//   SKIP — a sponsor input was absent/placeholder (SKIP-graceful semantics)
//   FAIL — a typed error the operator must fix (bad mode, malformed input, k8s error)
package seed

import "context"

// Verdict is the tri-state outcome of a seed Run/Verify. It mirrors the
// PASS/SKIP/FAIL vocabulary the bash seeds print, so parity is a verdict
// comparison, not a string diff.
type Verdict int

const (
	// PASS — the seed applied (or the cluster was already converged).
	PASS Verdict = iota
	// SKIP — a required sponsor input was absent or a placeholder; not an error.
	SKIP
	// FAIL — a typed error the operator must resolve.
	FAIL
)

func (v Verdict) String() string {
	switch v {
	case PASS:
		return "PASS"
	case SKIP:
		return "SKIP"
	case FAIL:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// ExitCode maps a verdict to the loopzero-ops exit-code contract (CLAUDE.md Rule 4):
// PASS/SKIP are success (0); FAIL is a failure of the build/exec class (2).
func (v Verdict) ExitCode() int {
	if v == FAIL {
		return 2
	}
	return 0
}

// Result is a verdict plus a human-readable reason. Reason is REQUIRED for
// SKIP and FAIL so the operator (and the parity comparison) knows why.
type Result struct {
	Verdict Verdict
	Reason  string
}

// Deps carries the runtime dependencies a seed needs. It is populated once by
// the composition root (seed_subcmd.go) — never constructed inside seed logic
// (architecture.md § Dependency Inversion). DryRun makes Run print intended
// actions without mutating the cluster.
type Deps struct {
	Ctx    context.Context
	Kube   KubeClient
	Creds  *Creds
	DryRun bool
}

// Seed is one migrated canonical seed. Name is the CLI token (e.g. "github-app").
// Run applies the seed; Verify probes the post-condition (the --verify path).
type Seed interface {
	Name() string
	Run(Deps) Result
	Verify(Deps) Result
}

// Registry holds the seeds in deterministic insertion order (the canonical
// chain order). Iteration is stable — never map-random — so `seed all` runs
// the chain in the documented dependency order.
type Registry struct {
	names  []string
	byName map[string]Seed
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Seed)}
}

// Register adds a seed. It panics on a duplicate name — that is a programming
// error (two seeds claiming the same CLI token), not a runtime condition.
func (r *Registry) Register(s Seed) {
	if _, dup := r.byName[s.Name()]; dup {
		panic("seed: duplicate registration for " + s.Name())
	}
	r.byName[s.Name()] = s
	r.names = append(r.names, s.Name())
}

// Names returns the registered seed names in insertion order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.names))
	copy(out, r.names)
	return out
}

// Lookup returns the seed for name, or (nil, false) if unregistered.
func (r *Registry) Lookup(name string) (Seed, bool) {
	s, ok := r.byName[name]
	return s, ok
}
