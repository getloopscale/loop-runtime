// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package config

import (
	"regexp"
	"strings"
	"testing"
)

// ciSentinel is the immutable placeholder `charts/loop-runtime-api/values-bootstrap.yaml`
// ships as its default tag, decided under issue #232. It is schema-valid (the
// chart's own `values.schema.json` requires minLength 1), it renders, and if it
// ever reaches a cluster the pod fails with `ImagePullBackOff` naming it — which
// says exactly what went wrong: somebody forgot the override.
const ciSentinel = "SET-BY-CI"

var (
	// Anchored on the VALUE. The previous form matched `^\s*tag:\s*…` against
	// whole-file text, which cannot be unit-tested one shape at a time and
	// silently accepted a match anywhere in the file.
	tagSemverRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[a-z0-9.-]+)?$`)
	tagSHARE    = regexp.MustCompile(`^(sha-)?[a-f0-9]{8,40}$`)
	tagBranchRE = regexp.MustCompile(`^(chg|feat|fix|hotfix|achado)[a-z0-9-]*$`) // english-only: `achado` is a prefix real historical branch tags used, so detection must keep it
)

// imageTagVerdict decides whether a chart's default image tag is admissible
// under ADR-0020 D1, as amended by issue #232.
//
// It returns the reason on refusal because a gate that only says "no" teaches
// the reader to widen it. The reason names WHICH rule caught the value, so the
// next person can argue with that rule instead of deleting the check.
//
// Order matters: branch names and `latest` are refused BEFORE the accepting
// shapes are tried, so a value that is both plausible and banned is reported by
// the rule that bans it rather than by a generic "no shape matched".
func imageTagVerdict(tag string) (bool, string) {
	switch {
	case tag == "":
		return false, "empty tag — the chart's own `values.schema.json` requires minLength 1, " +
			"and a default the chart rejects breaks `helm template` for every tool " +
			"that renders with pure defaults (issue #232)"
	case tagBranchRE.MatchString(tag):
		return false, "points at a branch name, which is mutable — banned by ADR-0020 D1"
	case tag == "latest":
		return false, "`latest` was evaluated and rejected in issue #232: it gives the quietest " +
			"possible failure — a non-deterministic deploy, or a VAP rejection " +
			"pointing at admission instead of at the override that was missing. The " +
			"decided form is the sentinel `" + ciSentinel + "`"
	case tag == ciSentinel:
		return true, ""
	case tagSemverRE.MatchString(tag), tagSHARE.MatchString(tag):
		return true, ""
	}
	return false, "does not match any accepted form (semver, sha-*, or the sentinel `" +
		ciSentinel + "`) — ADR-0020 D1 + issue #232"
}

// TestImageTagVerdict is the table that makes the tag contract of ADR-0020 D1
// reviewable — and that corrects it.
//
// # What the old assertion said, and why it was wrong
//
// `TestB31ImageTagsPinned` accepted `semver | sha-* | latest` and rejected
// branch names. The real value in `charts/loop-runtime-api/values-bootstrap.yaml` is the
// sentinel `SET-BY-CI`, chosen under issue #232 with the reasoning written into
// the file itself (`values.yaml:17-33`): three candidates were weighed, and
// `latest` was rejected BY NAME because it produced the quietest failure — a
// non-deterministic deploy, or a VAP rejection pointing at admission instead of
// at the override that was forgotten.
//
// So the assertion permitted what the decision rejected, and rejected what the
// decision chose. It froze a superseded reading of ADR-0020 D1 — and nothing
// caught it, because the package it lives in was wired into no task target
// (B-092). An un-run test does not merely fail to protect: it preserves an
// expired contract, and would block the correct design on the day it was wired.
//
// # Why a table instead of a wider regex
//
// A regex inside an `if` is an assertion with no cases: it cannot be reviewed,
// only widened until it passes. Each row below states one accepted or refused
// shape WITH its reason, so the next change to this contract is a line someone
// can argue with.
func TestImageTagVerdict(t *testing.T) {
	cases := []struct {
		name       string
		tag        string
		wantOK     bool
		wantReason string // expected substring when refused
	}{
		{name: "plain semver", tag: "v1.2.3", wantOK: true},
		{name: "semver with pre-release", tag: "v1.2.3-rc.1", wantOK: true},
		{name: "prefixed sha", tag: "sha-a1b2c3d4", wantOK: true},
		{name: "bare 40-char sha", tag: "a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0", wantOK: true},
		{
			// The row that corrects the contract. See values.yaml:17-33 + issue #232.
			name: "the SET-BY-CI sentinel is the form decided in issue #232",
			tag:  "SET-BY-CI", wantOK: true,
		},
		{
			// The other half of the correction: what used to be accepted is now refused.
			name: "latest is refused for the reason recorded in #232, not by taste",
			tag:  "latest", wantOK: false, wantReason: "#232",
		},
		{name: "feat branch name", tag: "feat-multi-aud", wantOK: false, wantReason: "branch"},
		{name: "hotfix branch name", tag: "hotfix-abc", wantOK: false, wantReason: "branch"},
		{
			// The chart's own schema requires minLength 1; an empty tag breaks
			// `helm template` for every tool that renders with defaults.
			name: "empty tag is refused — the chart schema rejects it",
			tag:  "", wantOK: false, wantReason: "empty",
		},
		{name: "arbitrary text is none of the accepted forms", tag: "multi-aud-fix", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := imageTagVerdict(tc.tag)
			if ok != tc.wantOK {
				t.Fatalf("imageTagVerdict(%q) = %v (%q), want %v", tc.tag, ok, reason, tc.wantOK)
			}
			if ok {
				return
			}
			if reason == "" {
				t.Fatal("refusal without a reason: whoever reads the failure needs to know which rule caught it")
			}
			if tc.wantReason != "" && !strings.Contains(reason, tc.wantReason) {
				t.Errorf("refusal reason = %q, want it to contain %q", reason, tc.wantReason)
			}
		})
	}
}
