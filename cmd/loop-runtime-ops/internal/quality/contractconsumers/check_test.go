// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package contractconsumers

import (
	"os"
	"os/exec"
	"path/filepath"
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
		`const c = p.apps_count;`,   // longer name, should not match
		`return project.apps_spec;`, // longer name
		`x.appsSpec = []`,           // CamelCase variant
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
	if a1 >= a5 || a5 >= b10 {
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

// gitTree builds a throwaway repository so the diff path can be exercised for
// real. The gate shells out to `git diff`; a fake would test the fake.
func gitTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "gate@test"},
		{"config", "user.name", "gate"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	return root
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

// Moving a file is not dropping its fields.
//
// `git diff` without rename detection renders a `git mv` as a whole-file
// deletion plus a whole-file addition, so EVERY json tag in the moved file
// appears on a removed line. The gate then hunts consumers for tags that were
// never dropped, and blocks the push.
//
// Measured on the routes split (B-080): moving repositories.go into its own
// subpackage — a pure rename, byte-identical struct — reported `json:"repo"`
// as dropped and named 9 "consumers". Thirteen more families are queued behind
// that move, so this fires on each of them.
func TestCollectDroppedJSONTags_PureRenameDropsNothing(t *testing.T) {
	root := gitTree(t)
	dir := filepath.Join(root, "api", "internal", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package routes\n\ntype Repo struct {\n\tRepo string `json:\"repo\"`\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "repositories.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")

	sub := filepath.Join(dir, "repositories")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "mv", "api/internal/routes/repositories.go", "api/internal/routes/repositories/repositories.go")
	git(t, root, "commit", "-qm", "move")

	tags, err := collectDroppedJSONTags(root, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("a pure rename drops no field — the struct is byte-identical and the tag still exists at HEAD; got dropped=%v", tags)
	}
}

// The other half: a REAL drop must still be caught. Without this, the fix above
// could be "never report anything", which is the failure mode a gate that
// stopped working looks like from the outside.
func TestCollectDroppedJSONTags_RealDropIsStillCaught(t *testing.T) {
	root := gitTree(t)
	dir := filepath.Join(root, "api", "internal", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "repositories.go")
	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype Repo struct {\n\tRepo string `json:\"repo\"`\n\tGone string `json:\"gone\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")

	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype Repo struct {\n\tRepo string `json:\"repo\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-aqm", "drop gone")

	tags, err := collectDroppedJSONTags(root, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(tags) != 1 || tags[0] != "gone" {
		t.Errorf("a field deleted from the struct is exactly what this gate exists to catch; got %v", tags)
	}
}

// And a rename that ALSO drops a field reports the drop and nothing else.
// This is the case that separates "detect renames" from "ignore renamed files":
// the second would be a hole big enough to move a contract through.
func TestCollectDroppedJSONTags_RenameWithARealDropReportsOnlyTheDrop(t *testing.T) {
	root := gitTree(t)
	dir := filepath.Join(root, "api", "internal", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repositories.go"),
		[]byte("package routes\n\ntype Repo struct {\n\tRepo string `json:\"repo\"`\n\tGone string `json:\"gone\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")

	sub := filepath.Join(dir, "repositories")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "mv", "api/internal/routes/repositories.go", "api/internal/routes/repositories/repositories.go")
	if err := os.WriteFile(filepath.Join(sub, "repositories.go"),
		[]byte("package repositories\n\ntype Repo struct {\n\tRepo string `json:\"repo\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "move and drop")

	tags, err := collectDroppedJSONTags(root, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(tags) != 1 || tags[0] != "gone" {
		t.Errorf("the rename must be seen through, not skipped: `gone` really left the contract, `repo` did not; got %v", tags)
	}
}

// A removed LINE is not a removed TAG.
//
// The gate reads json tags off the "-" side of the diff and never asks whether
// the tag still exists. So any edit that rewrites the line — a comment, a
// rename of the Go field, or gofmt realigning the trailing comment — reports
// the field as dropped.
//
// Measured on the routes split (B-080), and by the most ordinary cause there
// is: `gofmt -w` collapsed the padding before a trailing comment, changing
//
//   - Repo string `json:"repo"`            // "owner/repo"
//   - Repo string `json:"repo"` // "owner/repo"
//
// The gate declared `repo` dropped and blocked the push, naming nine
// "consumers" — none of which read the tag. Whitespace cannot break a wire
// contract, and a gate that says it can teaches people to push past it.
func TestCollectDroppedJSONTags_ReformattingTheLineDropsNothing(t *testing.T) {
	root := gitTree(t)
	dir := filepath.Join(root, "api", "internal", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "repositories.go")
	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype Repo struct {\n\tRepo string `json:\"repo\"`            // owner/repo\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")

	// The exact gofmt change: padding collapsed, tag untouched.
	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype Repo struct {\n\tRepo string `json:\"repo\"` // owner/repo\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-aqm", "gofmt")

	tags, err := collectDroppedJSONTags(root, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("gofmt cannot break a wire contract — the tag is still in the struct at HEAD; got dropped=%v", tags)
	}
}

// Renaming the Go field while keeping the tag is the same shape: the wire
// contract is what the tag says, and it did not move.
func TestCollectDroppedJSONTags_RenamingTheGoFieldKeepsTheTag(t *testing.T) {
	root := gitTree(t)
	dir := filepath.Join(root, "api", "internal", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "repositories.go")
	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype Repo struct {\n\tRepo string `json:\"repo\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")
	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype Repo struct {\n\tFullName string `json:\"repo\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-aqm", "rename field")

	tags, err := collectDroppedJSONTags(root, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("the wire contract is the tag, and it is unchanged; got dropped=%v", tags)
	}
}

// A tag declared by TWO structs, leaving one of them, IS a drop — and it was
// invisible while the check only asked whether the tag existed in some struct:
// the survivor answered "still exists". Measured in `repositories.go`, which
// declared `json:"repo"` twice (B-100).
//
// This test demanded the OPPOSITE until 2026-08-19 — it documented the hole, with
// the instruction to rewrite it, not delete it, once the hole closed. That is
// what is happening here.
//
// The discriminator is the COUNT of declarations, not the struct name. Scoping by
// name would close the hole by creating a new false positive — renaming the
// struct, or moving it to another package, does not change the wire contract and
// would start being blamed as a break. See `structsDeclaringTagAt`.
func TestCollectDroppedJSONTags_OneOfTwoDeclarationsLeavingIsADrop(t *testing.T) {
	root := gitTree(t)
	dir := filepath.Join(root, "api", "internal", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "repositories.go")
	two := "package routes\n\ntype A struct {\n\tRepo string `json:\"repo\"`\n}\n\ntype B struct {\n\tRepo string `json:\"repo\"`\n}\n"
	if err := os.WriteFile(path, []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")

	one := "package routes\n\ntype A struct {\n\tRepo string `json:\"repo\"`\n}\n\ntype B struct {\n\tRepo string `json:\"repository\"`\n}\n"
	if err := os.WriteFile(path, []byte(one), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-aqm", "rename one of them")

	tags, err := collectDroppedJSONTags(root, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(tags) != 1 || tags[0] != "repo" {
		t.Fatalf("one of the two `repo` declarations left and the drop was not reported: %v.\n\n"+
			"This was the B-100 hole — the surviving struct answered \"still exists\" for the one that left.", tags)
	}
}

// And the drop the gate exists for is still caught: the LAST declaration of a
// tag leaving the tracked set. Without this the fix would be indistinguishable
// from switching the gate off.
func TestCollectDroppedJSONTags_LastDeclarationLeavingIsCaught(t *testing.T) {
	root := gitTree(t)
	dir := filepath.Join(root, "api", "internal", "routes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "repositories.go")
	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype A struct {\n\tRepo string `json:\"repo\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")
	if err := os.WriteFile(path,
		[]byte("package routes\n\ntype A struct {\n\tRepo string `json:\"repository\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-aqm", "rename the only one")

	tags, err := collectDroppedJSONTags(root, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(tags) != 1 || tags[0] != "repo" {
		t.Errorf("the last declaration leaving IS the wire break this gate exists for; got %v", tags)
	}
}
