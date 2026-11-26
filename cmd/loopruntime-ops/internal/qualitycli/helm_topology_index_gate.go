// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/helmtopology"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "helm-topology-index",
		Summary: "Index of which chart runs on which cluster, derived from the helmfiles (never a hand-written second copy).",
		Setup:   setupHelmTopologyIndex,
	})
}

// setupHelmTopologyIndex implements the `helm-topology-index` gate (plan helm-topology-index).
//
// Derives "which cluster does this chart run on?" from the four helmfiles — the executable
// source of truth — instead of encoding it a second time as a folder tree. Two representations
// of the same fact drift; the helmfile cannot, because it is what installs.
func setupHelmTopologyIndex(fs *flag.FlagSet) Check {
	doc := fs.String("doc", "", "generated index path (default docs/architecture.md)")
	write := fs.Bool("write", false, "regenerate the index instead of verifying it")
	return func(inv Invocation) (verdict.Result, error) {
		mode := helmtopology.ModeCheck
		if *write {
			mode = helmtopology.ModeWrite
		}
		rep, err := helmtopology.Check(inv.Root, mode, *doc)
		if err != nil {
			return nil, err
		}
		if *write {
			inv.Stdout.Printf("helm-topology-index: wrote index with %d entries\n", len(rep.Entries))
			return rep, nil
		}
		if rep.Verdict() == verdict.Pass {
			inv.Stdout.Printf("helm-topology-index: PASS — %d charts indexed, in sync with the helmfiles\n",
				len(rep.Entries))
			return rep, nil
		}
		inv.Stderr.Printf("helm-topology-index: FAIL — %s\n", rep.Message)
		return rep, nil
	}
}
