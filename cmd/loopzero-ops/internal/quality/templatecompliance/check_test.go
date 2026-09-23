// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package templatecompliance

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// IsBotAuthor
// =============================================================================

func TestIsBotAuthor_PresentInAllowlist(t *testing.T) {
	assert.True(t, IsBotAuthor("dependabot[bot]", DefaultBotAllowlist))
	assert.True(t, IsBotAuthor("renovate[bot]", DefaultBotAllowlist))
	assert.True(t, IsBotAuthor("github-actions[bot]", DefaultBotAllowlist))
}

func TestIsBotAuthor_NotPresent(t *testing.T) {
	assert.False(t, IsBotAuthor("alice", DefaultBotAllowlist))
	assert.False(t, IsBotAuthor("", DefaultBotAllowlist))
	assert.False(t, IsBotAuthor("dependabot", DefaultBotAllowlist), "exact match required")
}

// =============================================================================
// HasAllowedLabel
// =============================================================================

func TestHasAllowedLabel_PresentExact(t *testing.T) {
	assert.Equal(t, "dependencies",
		HasAllowedLabel("bug,dependencies,enhancement", DefaultLabelAllowlist))
	assert.Equal(t, "automated",
		HasAllowedLabel("automated", DefaultLabelAllowlist))
}

func TestHasAllowedLabel_WhitespaceTolerant(t *testing.T) {
	assert.Equal(t, "dependencies",
		HasAllowedLabel("  dependencies  ,bug", DefaultLabelAllowlist))
}

func TestHasAllowedLabel_NotPresent(t *testing.T) {
	assert.Equal(t, "", HasAllowedLabel("", DefaultLabelAllowlist))
	assert.Equal(t, "", HasAllowedLabel("bug,enhancement", DefaultLabelAllowlist))
}

// =============================================================================
// HasChecklistSection
// =============================================================================

func TestHasChecklistSection_Present(t *testing.T) {
	body := "## Overview\nLorem.\n\n## Quality Gates Checklist\n- [x] All tests pass\n"
	assert.True(t, HasChecklistSection(body))
}

func TestHasChecklistSection_Absent(t *testing.T) {
	body := "## Overview\nLorem.\n"
	assert.False(t, HasChecklistSection(body))
}

// =============================================================================
// CountUncheckedBoxes
// =============================================================================

func TestCountUncheckedBoxes_AllChecked_Zero(t *testing.T) {
	body := "## Quality Gates Checklist\n- [x] Test 1\n- [x] Test 2\n"
	assert.Equal(t, 0, CountUncheckedBoxes(body))
}

func TestCountUncheckedBoxes_Mixed(t *testing.T) {
	body := "" +
		"## Quality Gates Checklist\n" +
		"- [x] Done item\n" +
		"- [ ] Pending 1\n" +
		"- [ ] Pending 2\n"
	assert.Equal(t, 2, CountUncheckedBoxes(body))
}

func TestCountUncheckedBoxes_OnlyCountsInsideSection(t *testing.T) {
	body := "" +
		"- [ ] Not in section (above)\n" +
		"## Quality Gates Checklist\n" +
		"- [ ] Inside section\n" +
		"## Next Section\n" +
		"- [ ] Not in section (below)\n"
	assert.Equal(t, 1, CountUncheckedBoxes(body))
}

func TestCountUncheckedBoxes_NoSection_Zero(t *testing.T) {
	body := "## Other\n- [ ] Random\n"
	assert.Equal(t, 0, CountUncheckedBoxes(body))
}

// =============================================================================
// HasNonEmptyNAJustifications
// =============================================================================

func TestHasNonEmptyNAJustifications_WithContent(t *testing.T) {
	body := "" +
		"### N/A justifications\n" +
		"Test X is not applicable because feature is dark-launched.\n"
	assert.True(t, HasNonEmptyNAJustifications(body))
}

func TestHasNonEmptyNAJustifications_OnlyComment(t *testing.T) {
	body := "" +
		"### N/A justifications\n" +
		"<!-- list any N/A justifications here -->\n"
	assert.False(t, HasNonEmptyNAJustifications(body), "HTML comment doesn't count")
}

func TestHasNonEmptyNAJustifications_BlankBody(t *testing.T) {
	body := "" +
		"### N/A justifications\n" +
		"\n" +
		"\n"
	assert.False(t, HasNonEmptyNAJustifications(body))
}

func TestHasNonEmptyNAJustifications_StopsAtNextH2(t *testing.T) {
	body := "" +
		"### N/A justifications\n" +
		"\n" +
		"## Other Section\n" +
		"Real content but not in N/A section.\n"
	assert.False(t, HasNonEmptyNAJustifications(body), "must stop at next ## heading")
}

func TestHasNonEmptyNAJustifications_NoSection(t *testing.T) {
	assert.False(t, HasNonEmptyNAJustifications("## Overview\nNothing here.\n"))
}

func TestHasNonEmptyNAJustifications_OnlyAlnumCounts(t *testing.T) {
	// Just symbols → no alnum → does NOT qualify.
	body := "### N/A justifications\n!!!\n---\n"
	assert.False(t, HasNonEmptyNAJustifications(body))
}

// =============================================================================
// Run — EC-5 escape valves
// =============================================================================

func TestRun_BotAuthor_SkipPass(t *testing.T) {
	out, err := Run(Config{PRAuthor: "dependabot[bot]"})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.True(t, out.Skipped)
	assert.Contains(t, out.SkipReason, "dependabot[bot]")
	assert.Contains(t, strings.Join(out.Messages, "\n"), "EC-5: bot author")
}

func TestRun_AllowedLabel_SkipPass(t *testing.T) {
	out, err := Run(Config{
		PRAuthor: "alice",
		PRLabels: "bug, dependencies",
	})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.True(t, out.Skipped)
	assert.Contains(t, out.SkipReason, "dependencies")
}

func TestRun_BotAuthorTakesPriorityOverLabel(t *testing.T) {
	out, err := Run(Config{
		PRAuthor: "renovate[bot]",
		PRLabels: "not-an-allowed-label",
	})
	require.NoError(t, err)
	assert.True(t, out.Skipped)
	assert.Contains(t, out.SkipReason, "renovate[bot]")
}

func TestRun_CustomBotAllowlist(t *testing.T) {
	out, err := Run(Config{
		PRAuthor:     "ci-bot",
		BotAllowlist: []string{"ci-bot"},
	})
	require.NoError(t, err)
	assert.True(t, out.Skipped)
}

func TestRun_CustomLabelAllowlist(t *testing.T) {
	out, err := Run(Config{
		PRAuthor:       "alice",
		PRLabels:       "release-train",
		LabelAllowlist: []string{"release-train"},
	})
	require.NoError(t, err)
	assert.True(t, out.Skipped)
}

// =============================================================================
// Run — validation paths
// =============================================================================

func TestRun_EmptyBody_Fail(t *testing.T) {
	out, err := Run(Config{PRAuthor: "alice"})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	assert.True(t, out.EmptyBody)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "empty PR body")
}

func TestRun_MissingChecklist_Fail(t *testing.T) {
	out, err := Run(Config{
		PRAuthor: "alice",
		PRBody:   "## Overview\nLorem ipsum.\n",
	})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	assert.True(t, out.MissingChecklist)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "missing 'Quality Gates Checklist' section")
	assert.Contains(t, joined, "PULL_REQUEST_TEMPLATE.md")
}

// =============================================================================
// Run — happy paths
// =============================================================================

func TestRun_AllChecked_Pass(t *testing.T) {
	body := "" +
		"## Quality Gates Checklist\n" +
		"- [x] Tests pass\n" +
		"- [x] Lint clean\n"
	out, err := Run(Config{PRAuthor: "alice", PRBody: body})
	require.NoError(t, err)
	assert.True(t, out.Pass)
	assert.Equal(t, 0, out.UncheckedBoxes)
	assert.Contains(t, strings.Join(out.Messages, "\n"), "PASS — all checkboxes ticked")
}

func TestRun_UncheckedWithNAJustifications_WarnPass(t *testing.T) {
	body := "" +
		"## Quality Gates Checklist\n" +
		"- [x] Tests pass\n" +
		"- [ ] Coverage diff\n" +
		"\n" +
		"### N/A justifications\n" +
		"Coverage diff N/A: docs-only PR.\n"
	out, err := Run(Config{PRAuthor: "alice", PRBody: body})
	require.NoError(t, err)
	assert.True(t, out.Pass, "WARN path: unchecked but N/A justified")
	assert.Equal(t, 1, out.UncheckedBoxes)
	assert.True(t, out.HasNAJustifications)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "WARN: 1 unchecked box(es) with N/A justifications")
}

func TestRun_UncheckedNoJustifications_Fail(t *testing.T) {
	body := "" +
		"## Quality Gates Checklist\n" +
		"- [ ] Tests pass\n" +
		"- [ ] Lint clean\n"
	out, err := Run(Config{PRAuthor: "alice", PRBody: body})
	require.NoError(t, err)
	assert.False(t, out.Pass)
	assert.Equal(t, 2, out.UncheckedBoxes)
	assert.False(t, out.HasNAJustifications)
	joined := strings.Join(out.Messages, "\n")
	assert.Contains(t, joined, "FAIL: 2 unchecked box(es)")
	assert.Contains(t, joined, "check the boxes OR add explanations")
}

func TestRun_UncheckedWithOnlyCommentNA_Fail(t *testing.T) {
	body := "" +
		"## Quality Gates Checklist\n" +
		"- [ ] Tests pass\n" +
		"\n" +
		"### N/A justifications\n" +
		"<!-- describe any N/A items here -->\n"
	out, err := Run(Config{PRAuthor: "alice", PRBody: body})
	require.NoError(t, err)
	assert.False(t, out.Pass, "only comment in N/A → does NOT qualify")
	assert.Equal(t, 1, out.UncheckedBoxes)
	assert.False(t, out.HasNAJustifications)
}
