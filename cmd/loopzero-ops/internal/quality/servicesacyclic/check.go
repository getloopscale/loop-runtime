// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package servicesacyclic fences the bounded contexts of api/internal/services.
//
// It is a registry over subpackagefence, the engine it shares with
// routesacyclic. The inter-context import graph must stay the acyclic DAG the M9
// split produced, deny-by-default:
//
//	build     → (no sibling)
//	platform  → (no sibling)
//
// The shared types every context needs live in api/internal/deploycontracts,
// OUTSIDE this package, which is why no context needs a sibling to reach them.
//
// release, scaling, preview, domain and placement left for their feature
// directories when api/internal was organized by feature (2026-09-25);
// feature-fence governs them there.
//
// Any edge not in the allow-list fails: a reverse edge would re-create a cycle, a new cross edge erodes the split, and a new context that is
// not registered forces a conscious decision instead of silently widening the
// graph. The registry itself is checked for cycles before the tree is read.
//
// `billing` is deliberately absent: its code moved to loopzero-cloud, and a context
// registered for code that does not exist would pre-approve the decision this
// gate exists to force for whoever recreates the directory.
package servicesacyclic

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
)

const (
	servicesRoot    = "api/internal/services"
	servicesPkgPath = "github.com/getloopscale/loopzero/api/internal/services"

	// sinkRoot is the shared contracts package every context depends on. It
	// sits outside servicesRoot, so the subpackage fence never reads it, and
	// it may import no context: every context imports it, so one edge back
	// would close a cycle through it.
	sinkRoot = "api/internal/deploycontracts"
)

// allowed is the deny-by-default registry: allowed[A] is the set of contexts A
// may import.
var allowed = map[string]map[string]bool{
	"build":    {},
	"platform": {},
}

// Config controls the gate.
type Config struct {
	RepoRoot string
}

func fence() subpackagefence.Fence {
	return subpackagefence.Fence{Dir: servicesRoot, PkgPath: servicesPkgPath, Allowed: allowed}
}

// Check scans api/internal/services against the registry, then checks that the
// deploycontracts sink imports no context.
func Check(cfg Config) (subpackagefence.Outcome, error) {
	out, err := subpackagefence.Check(cfg.RepoRoot, fence())
	return out, errors.Join(err, checkSink(cfg.RepoRoot))
}

// checkSink reports every import of a services context from the sink, test
// files included: a test that imports a context still couples the two trees.
// An absent sink is not this gate's finding — domain-sink-purity fails closed
// on it — and a missing sink imports nothing.
func checkSink(repoRoot string) error {
	dir := filepath.Join(repoRoot, sinkRoot)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	var edges []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imp := range file.Imports {
			target, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return fmt.Errorf("%s: import path %s: %w", path, imp.Path.Value, err)
			}
			if target == servicesPkgPath || strings.HasPrefix(target, servicesPkgPath+"/") {
				pos := fset.Position(imp.Pos())
				edges = append(edges, fmt.Sprintf("deploycontracts → %s (%s:%d)",
					strings.TrimPrefix(strings.TrimPrefix(target, servicesPkgPath), "/"), pos.Filename, pos.Line))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(edges) == 0 {
		return nil
	}
	sort.Strings(edges)
	return fmt.Errorf("%s: %w: the shared sink must import no services context: %s",
		sinkRoot, subpackagefence.ErrForbiddenEdge, strings.Join(edges, "; "))
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "An inter-context import edge violates the frozen acyclic DAG of " +
		"api/internal/services (M9 Step 6). Either (a) the shared symbol belongs " +
		"in api/internal/deploycontracts — move it there; or (b) the edge is a " +
		"genuine new dependency that must be reviewed and added to the allow-list " +
		"in servicesacyclic/check.go, which refuses any addition that creates a " +
		"cycle. Reverse edges are forbidden — they " +
		"re-introduce the cycle the split removed. An unregistered context needs " +
		"the same decision before it may exist."
}
