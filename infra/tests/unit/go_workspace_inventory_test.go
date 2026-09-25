// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// B-170: every module on disk must be listed in `go.work`.
//
// Mechanism this pins. A `go.work` at the repo root puts every directory
// beneath it into workspace mode — the toolchain walks up and finds it, so
// there is no "outside the workspace" subdirectory. In workspace mode the
// `./...` pattern resolves only against modules the workspace lists in `use`
// (plus their selected dependencies). A directory that carries its own
// `go.mod` but is absent from `use` is, to the workspace, not part of the
// build: the prefix `.` matches no workspace module and the toolchain refuses
// with
//
//	pattern ./...: directory prefix . does not contain modules listed in
//	go.work or their selected dependencies
//
// That is what `tools/gen-service-auth-ed25519` hit — go.work listed 8 of the
// 9 modules (fixed in 0f8a2e5f9). The identical message at the repo root has
// the identical cause and is NOT a defect: the root carries no `go.mod` and is
// not in `use`, so it contains modules without being one, and `./...` there is
// meaningless by construction. Nothing at the root needs fixing; the runner
// already iterates the `use` list instead.
//
// Why a guard and not just the one-line go.work fix. The workspace runner
// derives what to test FROM `use`. A module missing from that list is not
// reported as failing — it is silently never run, and the suite reports PASS
// over tests it never executed. This test makes the inventory gap say so
// itself instead of being absorbed as a green run.
//
// Scope: modules INSIDE this repository. `use` entries that leave the repo
// (`../loopzero-contracts`) belong to a sibling repo with its own gates and are
// deliberately not audited from here.
//
// KNOWN TENSION, recorded rather than silently settled. Two taskfile comments
// (`taskfiles/quality.yml` "tools/ esta FORA do go.work por desenho",
// `taskfiles/test.yml` "Fora do go.work por desenho") say tools/ modules are
// deliberately OUTSIDE the workspace and therefore invoked with `GOWORK=off`.
// Since 0f8a2e5f9 (B-170) `./tools/gen-service-auth-ed25519` IS in go.work, so
// those comments no longer describe the file — they are corrected in the same
// commit as this test. This guard pins the state as it is, because leaving the
// module out is precisely what made `go test ./...` fail there (B-170). It is
// not a verdict that tools/ must live in the workspace forever: if that is
// revisited, this test fails first and the decision gets made deliberately
// instead of re-arriving as a mystery error. Measured: the module declares no
// requires at all, so workspace membership changes nothing about how its
// dependencies resolve, and `GOWORK=off go test` still passes either way.
//
// CACHING — this guard needs `-count=1` to mean anything. Go's test cache keys
// on THIS module's inputs; go.work is not one of them. Measured 2026-09-05 with
// an isolated GOCACHE: prime the suite green, delete a module line from go.work,
// re-run `go test ./unit/...` — it reports "ok (cached)" and never observes the
// change. The same hole hides T-10's subject: deleting
// api/internal/placement/placementhttp/admin_pin_cell.go outright still yields a cached
// "ok". The repo already learned this for cross-module fixture reads
// (taskfiles/quality.yml, "-count=1: these read fixtures from ANOTHER module"),
// and the infra/tests unit invocations now carry the same flag.
//
// Deliberately NOT asserted: the reverse direction (a `use` entry naming a
// module that does not exist). Measured 2026-09-05 by adding
// `./tools/does-not-exist` to the block: the toolchain refuses to load the
// workspace at all —
//
//	go: cannot load module ../../tools/does-not-exist listed in go.work file:
//	open ../../tools/does-not-exist/go.mod: no such file or directory
//
// — so no test in any module gets to run, and a guard for that state could
// never execute to report it. Go already fails first, and with a clearer
// message than this test could produce. An assertion that cannot fire is not
// coverage; it is the same blind spot this file exists to close.

package unit

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goWorkUseEntries returns the raw `use` entries declared in go.work, covering
// both the block form and the single-line form.
func goWorkUseEntries(t *testing.T, goWorkPath string) []string {
	t.Helper()

	data, err := os.ReadFile(goWorkPath)
	if err != nil {
		t.Fatalf("go.work unreadable at %s: %v — the guard cannot see its subject, "+
			"so it is reporting nothing about workspace inventory", goWorkPath, err)
	}

	var entries []string
	inBlock := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "use ("):
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			entries = append(entries, strings.Trim(line, `"`))
		case strings.HasPrefix(line, "use "):
			entries = append(entries, strings.Trim(strings.TrimPrefix(line, "use "), `"`))
		}
	}
	return entries
}

// TestGoWorkListsEveryModuleInRepo pins ONE direction: every in-repo `go.mod`
// appears in `go.work`. The reverse — every declaration resolving to a real
// module — is deliberately absent, and the header of this file explains why:
// it was written, measured to be unreachable (the toolchain refuses to load
// the workspace before any test runs, with a better message than this file
// could produce), and deleted rather than shipped as an assertion that cannot
// fire. This comment said "both ways" until an audit read the body.
func TestGoWorkListsEveryModuleInRepo(t *testing.T) {
	root := repoRoot()

	goWorkPath := filepath.Join(root, "go.work")
	if _, err := os.Stat(goWorkPath); err != nil {
		t.Fatalf("go.work absent at %s: %v — this repo is a Go workspace and the "+
			"module runner derives its test list from it; without the file the "+
			"guard has no subject to check", goWorkPath, err)
	}

	// Declared, normalised to repo-relative slash paths. Sibling entries ("..")
	// are another repo's gate, not ours.
	declared := map[string]bool{}
	var siblings []string
	for _, entry := range goWorkUseEntries(t, goWorkPath) {
		if strings.HasPrefix(entry, "..") {
			siblings = append(siblings, entry)
			continue
		}
		declared[strings.TrimSuffix(strings.TrimPrefix(entry, "./"), "/")] = true
	}

	if len(declared) == 0 {
		t.Fatalf("parsed zero in-repo `use` entries from %s (siblings seen: %v) — "+
			"the parser matched nothing, so a green result here would prove the "+
			"opposite of what it looks like", goWorkPath, siblings)
	}

	// On disk.
	onDisk := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip, never fail the walk silently elsewhere
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil // a root module would make ./... valid at the root; not this repo
		}
		onDisk[rel] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s for go.mod failed: %v — inventory unmeasured", root, err)
	}

	if len(onDisk) == 0 {
		t.Fatalf("found zero go.mod files under %s — the walk matched nothing, so "+
			"this test would pass without having compared anything", root)
	}

	for module := range onDisk {
		if !declared[module] {
			t.Errorf("module %s/go.mod exists on disk but is NOT listed in go.work `use`.\n"+
				"Consequence: `go test ./...` inside it fails with \"directory prefix . "+
				"does not contain modules listed in go.work\", and the workspace test "+
				"runner — which derives its module list from `use` — never runs it at "+
				"all, reporting PASS over tests it did not execute.\n"+
				"Fix: add `./%s` to the `use` block in go.work.", module, module)
		}
	}
}
