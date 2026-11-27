// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenancy

import (
	"sort"

	"github.com/getloopscale/loop-runtime/api/internal/ports"

	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// resolveEnvEntry picks the Tenant CR environment entry that answers a lookup
// for (envName, project), returning its index, or -1 when nothing matches.
//
// Issue #139. The previous rule was strict tuple equality
// (`entry.Name == envName && entry.Project == project`). The CLI's
// `loopscale env inspect <name>` has no `--project` flag, so it always asks with
// project="". Before the #124 backfill that resolved the legacy entry, which
// was the only entry a tenant had. After the backfill the entry carries a real
// project, strict equality can never match, and a live environment reports
// "not found" — while `env list` (a different predicate) still shows it.
//
// The resolution order below is not arbitrary; each step exists to keep a
// property the previous behaviour had:
//
//  1. project != ""  → exact tuple only. An explicit request must never fall
//     back to another project (that would be a cross-project answer).
//  2. project == "" and a legacy entry (Project "") exists for the name → it
//     wins. ADR D2 makes the shared entry canonical while it exists.
//  3. project == "" and exactly ONE project has the name → that one. This is
//     the #139 fix and the overwhelmingly common shape (single-project tenant).
//  4. project == "" and SEVERAL projects have the name → ErrAmbiguousEnvironment.
//     See the type's doc for why guessing is worse than failing.
//
// The returned `ambiguous` slice is the sorted candidate project list; it is
// non-empty only alongside ErrAmbiguousEnvironment, and exists so callers can
// build an actionable message without re-deriving it.
func resolveEnvEntry(es []apiv1.TenantEnvironmentEntry, envName, project string) (idx int, ambiguous []string, err error) {
	// (1) Explicit project — exact tuple, unchanged M2 semantics.
	if project != "" {
		for i := range es {
			if es[i].Name == envName && es[i].Project == project {
				return i, nil, nil
			}
		}
		return -1, nil, nil
	}

	// Project-less lookup: collect every entry carrying this name.
	var candidates []int
	for i := range es {
		if es[i].Name == envName {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return -1, nil, nil
	}

	// (2) Legacy shared entry wins while it exists (ADR D2).
	for _, i := range candidates {
		if es[i].Project == "" {
			return i, nil, nil
		}
	}

	// (3) Unique project — the #139 case.
	if len(candidates) == 1 {
		return candidates[0], nil, nil
	}

	// (4) Several projects — refuse to guess.
	projects := make([]string, 0, len(candidates))
	for _, i := range candidates {
		projects = append(projects, es[i].Project)
	}
	sort.Strings(projects)
	return -1, projects, &ports.ErrAmbiguousEnvironment{EnvName: envName, Projects: projects}
}
