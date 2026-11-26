// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package openbaoinitauth

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Every declared policy must be carried by at least one role (B-098).
//
// A policy no role carries grants nothing, which is exactly why nobody notices it.
// The cost shows up when someone edits "the loop-runtime-api policy" and finds several
// similar names: B-072 patched `loop-runtime-api-transit-encrypt` instead of `loop-runtime-api`
// that way. The orphans measured on 2026-08-19 were earlier, WIDER versions of the
// current policy, so wiring one by mistake would grant too much, not too little.

var (
	reWritePolicy = regexp.MustCompile(`write_policy\s+"([^"]+)"`)
	reWriteRole   = regexp.MustCompile(`write_auth_role\s+((?:"[^"]*"\s*\\?\s*)+)`)
	reQuoted      = regexp.MustCompile(`"([^"]*)"`)
)

// policiesArgIndex is the index of the `policies` argument in the rendered
// helper's signature: mount, role, bound_sas, bound_ns, policies, ttl[, audiences].
const policiesArgIndex = 4

// orphanPolicyFailures returns one failure per problem: policies written that no
// role carries. It also fails when NO policy is found — a scan that reaches
// nothing reports green over nothing.
func orphanPolicyFailures(rendered string) []string {
	declared := map[string]bool{}
	for _, m := range reWritePolicy.FindAllStringSubmatch(rendered, -1) {
		declared[m[1]] = true
	}
	if len(declared) == 0 {
		return []string{"no policy found in the rendered chart — the scan reached nothing"}
	}

	carried := map[string]bool{}
	var malformed []string
	for _, m := range reWriteRole.FindAllStringSubmatch(rendered, -1) {
		args := reQuoted.FindAllStringSubmatch(m[1], -1)
		if len(args) <= policiesArgIndex {
			// The helper signature changed. Report it instead of reading the wrong
			// argument: a parser that misaligns silently marks live policies orphan.
			malformed = append(malformed, fmt.Sprintf(
				"write_auth_role with %d argument(s), expected at least %d — the helper signature changed",
				len(args), policiesArgIndex+1))
			continue
		}
		for _, p := range strings.Split(args[policiesArgIndex][1], ",") {
			if p = strings.TrimSpace(p); p != "" {
				carried[p] = true
			}
		}
	}
	if len(carried) == 0 {
		malformed = append(malformed, "no role carries any policy — the parser did not reach the bindings")
	}

	var orphans []string
	for p := range declared {
		if !carried[p] {
			orphans = append(orphans, p)
		}
	}
	sort.Strings(orphans)

	failures := malformed
	if len(orphans) > 0 {
		failures = append(failures, fmt.Sprintf(
			"policy declared that NO role carries: %s — inert today, but one more name among the "+
				"candidates whoever edits next must choose from, and B-072 already patched the wrong one",
			strings.Join(orphans, ", ")))
	}
	return failures
}
