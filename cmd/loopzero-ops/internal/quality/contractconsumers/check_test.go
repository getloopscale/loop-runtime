// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contractconsumers

import (
	"regexp"
	"strings"
	"testing"
)

// TestJSONFieldRegex_ExtractsTag — regex correctly captures the JSON tag
// from a struct field line, before any comma options like ,omitempty.
func TestJSONFieldRegex_ExtractsTag(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		want  string
		match bool
	}{
		{
			name:  "simple_tag",
			line:  `	AppsCount int64 ` + "`json:\"apps_count\"`",
			want:  "apps_count",
			match: true,
		},
		{
			name:  "tag_with_omitempty",
			line:  `	LastDeployAt time.Time ` + "`json:\"last_deploy,omitempty\"`",
			want:  "last_deploy",
			match: true,
		},
		{
			name:  "removed_line_in_diff",
			line:  `-	Apps []BuildApp ` + "`json:\"apps\"`",
			want:  "apps",
			match: true,
		},
		{
			name:  "explicit_skip_dash",
			line:  `	Internal string ` + "`json:\"-\"`",
			want:  "-",
			match: true,
		},
		{
			name:  "no_json_tag",
			line:  `	PlainField int`,
			match: false,
		},
		{
			name:  "comment_with_json_word",
			line:  `	// json:"fake" — this is a comment, not a tag`,
			want:  "fake",
			match: true, // regex matches; caller filters via context — see TestCollectDroppedJSONTags_SkipsComments
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches := jsonFieldRegex.FindStringSubmatch(tc.line)
			if tc.match {
				if len(matches) < 2 {
					t.Fatalf("expected match on %q, got none", tc.line)
				}
				if got := strings.TrimSpace(matches[1]); got != tc.want {
					t.Fatalf("expected tag %q, got %q", tc.want, got)
				}
			} else {
				if len(matches) >= 2 {
					t.Fatalf("expected no match on %q, got %q", tc.line, matches[1])
				}
			}
		})
	}
}

// TestGrepConsumersForTag_PatternIsWordBoundary — the access pattern
// `.<tag>\b` must NOT match longer field names that have the dropped tag
// as a prefix (e.g., dropping `apps` must not flag `apps_count` access).
func TestGrepConsumersForTag_PatternIsWordBoundary(t *testing.T) {
	tag := "apps"
	pattern := `\.` + regexp.QuoteMeta(tag) + `\b`
	re := regexp.MustCompile(pattern)

	mustMatch := []string{
		`const c = p.apps;`,
		`return build.apps.length;`,
		`b.apps == nil`,
	}
	mustNotMatch := []string{
		`const c = p.apps_count;`,    // longer name, should not match
		`return project.apps_spec;`,  // longer name
		`x.appsSpec = []`,             // CamelCase variant
	}
	for _, line := range mustMatch {
		if !re.MatchString(line) {
			t.Errorf("expected match on %q with pattern %q", line, pattern)
		}
	}
	for _, line := range mustNotMatch {
		if re.MatchString(line) {
			t.Errorf("expected NO match on %q with pattern %q (substring trap)", line, pattern)
		}
	}
}

// TestActionableFix_NonEmpty — sanity check that the FAIL output recommendation
// renders some content (defensive: should never be empty since callers
// surface it to the user as the remediation hint).
func TestActionableFix_NonEmpty(t *testing.T) {
	fix := ActionableFix()
	if fix == "" {
		t.Fatal("ActionableFix() returned empty string")
	}
	if !strings.Contains(fix, "Audit") {
		t.Errorf("ActionableFix() does not contain 'Audit' — recommendation surface broken: %q", fix)
	}
}

// TestFormatFindings_StableOrder — findings are sorted by (DroppedTag,
// ConsumerFile, ConsumerLine) for deterministic CI output.
func TestFormatFindings_StableOrder(t *testing.T) {
	findings := []Finding{
		{DroppedTag: "apps", ConsumerFile: "b.tsx", ConsumerLine: 10, ConsumerSnippet: "b"},
		{DroppedTag: "apps", ConsumerFile: "a.tsx", ConsumerLine: 5, ConsumerSnippet: "a5"},
		{DroppedTag: "last_deploy", ConsumerFile: "c.tsx", ConsumerLine: 1, ConsumerSnippet: "c"},
		{DroppedTag: "apps", ConsumerFile: "a.tsx", ConsumerLine: 1, ConsumerSnippet: "a1"},
	}
	out := FormatFindings(findings)
	// Verify "apps" group comes before "last_deploy"
	appsIdx := strings.Index(out, `Dropped JSON tag: "apps"`)
	lastIdx := strings.Index(out, `Dropped JSON tag: "last_deploy"`)
	if appsIdx == -1 || lastIdx == -1 {
		t.Fatalf("expected both group headers in output, got:\n%s", out)
	}
	if appsIdx > lastIdx {
		t.Errorf("expected 'apps' group before 'last_deploy', got order:\n%s", out)
	}
	// Within "apps": a.tsx:1 before a.tsx:5 before b.tsx:10
	a1 := strings.Index(out, "a.tsx:1 ")
	a5 := strings.Index(out, "a.tsx:5 ")
	b10 := strings.Index(out, "b.tsx:10 ")
	if !(a1 < a5 && a5 < b10) {
		t.Errorf("expected within-group order a.tsx:1 < a.tsx:5 < b.tsx:10, got:\n%s", out)
	}
}

// TestFormatFindings_EmptyInput — empty findings → empty output (no header).
func TestFormatFindings_EmptyInput(t *testing.T) {
	out := FormatFindings(nil)
	if out != "" {
		t.Errorf("expected empty output for nil findings, got %q", out)
	}
	out = FormatFindings([]Finding{})
	if out != "" {
		t.Errorf("expected empty output for empty findings, got %q", out)
	}
}

// TestConfig_DefaultsApplied — empty Config fields use canonical defaults.
// This is a smoke test against the Check() default-application path; the
// real git operations are exercised by the integration bash test (which is
// hermetic via test repo fixtures).
func TestConfig_DefaultsApplied(t *testing.T) {
	// Check on a non-existent root should NOT panic — git command failure
	// surfaces via Outcome.CommandError, not a Go panic.
	out, err := Check(Config{RepoRoot: "/nonexistent-9b8c7d6e"})
	if err != nil {
		t.Fatalf("Check returned err (should surface via Outcome.CommandError): %v", err)
	}
	if out == nil {
		t.Fatal("Check returned nil outcome on non-existent root")
	}
	if out.CommandError == "" {
		t.Error("expected CommandError on non-existent root, got empty")
	}
}
