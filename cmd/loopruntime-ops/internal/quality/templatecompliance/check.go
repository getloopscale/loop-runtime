// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package templatecompliance verifies that a PR body has all Quality
// Gates Checklist items ticked, or has explicit `N/A justifications`
// for any unticked items. Replaces infra/scripts/quality/template-compliance.sh
// (~81 LOC) per Phase 5 T5.8 of plan
// 2026-05-21-bash-to-go-migration-plan.
//
// Origin: plan 2026-05-21-comprehensive-quality-gates v1.1 T5.1 + EC-5.
//
// Sections scanned (Markdown headings):
//   - `## Quality Gates Checklist` — required anchor; `- [ ]` boxes counted.
//   - `### N/A justifications` — body text qualifies unchecked boxes
//     as deliberate (allows merge with WARN).
//
// EC-5 escape valves: bot authors (dependabot/renovate/github-actions)
// and labels (`dependencies`/`automated`) bypass the gate.
package templatecompliance

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// Default bot/label allowlists (matches bash arrays).
var (
	DefaultBotAllowlist   = []string{"dependabot[bot]", "renovate[bot]", "github-actions[bot]"}
	DefaultLabelAllowlist = []string{"dependencies", "automated"}
)

// Heading anchors used to identify scanned sections.
const (
	ChecklistHeading        = "## Quality Gates Checklist"
	NAJustificationsHeading = "### N/A justifications"
)

// Config holds the gate inputs.
type Config struct {
	// PRBody is the markdown body of the pull request.
	PRBody string

	// PRAuthor is the GitHub login of the PR author.
	PRAuthor string

	// PRLabels is the raw CSV list of labels attached to the PR.
	PRLabels string

	// BotAllowlist overrides the default bot author list. Empty = use default.
	BotAllowlist []string

	// LabelAllowlist overrides the default label allow list. Empty = default.
	LabelAllowlist []string
}

// Outcome captures the gate result.
type Outcome struct {
	// Pass is true when (no unchecked boxes) OR (skip via EC-5) OR
	// (unchecked boxes but N/A justifications present → WARN+pass).
	Pass bool

	// Skipped is true when EC-5 escape valve fired.
	Skipped bool

	// SkipReason explains the EC-5 trigger (empty when not skipped).
	SkipReason string

	// UncheckedBoxes is the count of `- [ ]` lines in the checklist section.
	UncheckedBoxes int

	// HasNAJustifications is true when the N/A section has non-empty body.
	HasNAJustifications bool

	// MissingChecklist is true when the required heading is absent.
	MissingChecklist bool

	// EmptyBody is true when PRBody is blank.
	EmptyBody bool

	// Messages — diagnostic stream.
	Messages []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// IsBotAuthor returns true when author matches any allowlist entry.
func IsBotAuthor(author string, allowlist []string) bool {
	for _, bot := range allowlist {
		if bot == author {
			return true
		}
	}
	return false
}

// HasAllowedLabel returns the FIRST matching label name (empty when
// no PR label matches the allowlist). CSV-tolerant + whitespace-trimmed.
func HasAllowedLabel(labelsCSV string, allowlist []string) string {
	if labelsCSV == "" {
		return ""
	}
	for _, raw := range strings.Split(labelsCSV, ",") {
		lbl := strings.TrimSpace(raw)
		if lbl == "" {
			continue
		}
		for _, allowed := range allowlist {
			if lbl == allowed {
				return lbl
			}
		}
	}
	return ""
}

// CountUncheckedBoxes returns the number of `- [ ]` lines inside the
// Quality Gates Checklist section (until the next `## ` heading).
func CountUncheckedBoxes(body string) int {
	count := 0
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inSection := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == ChecklistHeading {
			inSection = true
			continue
		}
		// Any subsequent `## ` heading ends the section.
		if inSection && strings.HasPrefix(line, "## ") {
			inSection = false
		}
		if inSection && strings.HasPrefix(line, "- [ ]") {
			count++
		}
	}
	return count
}

// HasChecklistSection returns true when the required heading anchor
// is present anywhere in body.
func HasChecklistSection(body string) bool {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if scanner.Text() == ChecklistHeading {
			return true
		}
	}
	return false
}

// HasNonEmptyNAJustifications returns true when a `### N/A justifications`
// section exists AND has at least one line of non-template, non-comment
// content before the next `## ` heading.
func HasNonEmptyNAJustifications(body string) bool {
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inSection := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == NAJustificationsHeading {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			return false
		}
		if !inSection {
			continue
		}
		// Skip empty + HTML comments + sub-headings (e.g., another `### `).
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			continue
		}
		// Must contain at least one alnum to qualify as "real content".
		if containsAlnum(trimmed) {
			return true
		}
	}
	return false
}

// containsAlnum returns true when s has at least one ASCII alnum char.
func containsAlnum(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}

// resolveAllowlists returns the effective allowlists honoring config
// overrides, falling back to package defaults.
func resolveAllowlists(cfg Config) (bots, labels []string) {
	bots = cfg.BotAllowlist
	if len(bots) == 0 {
		bots = DefaultBotAllowlist
	}
	labels = cfg.LabelAllowlist
	if len(labels) == 0 {
		labels = DefaultLabelAllowlist
	}
	return
}

// Run executes the gate.
func Run(cfg Config) (*Outcome, error) {
	bots, labels := resolveAllowlists(cfg)
	out := &Outcome{}

	// EC-5 — bot author.
	if IsBotAuthor(cfg.PRAuthor, bots) {
		out.Pass = true
		out.Skipped = true
		out.SkipReason = "bot author " + cfg.PRAuthor
		out.Messages = append(out.Messages,
			"[template-compliance] EC-5: bot author "+cfg.PRAuthor+" — SKIP gate")
		return out, nil
	}

	// EC-5 — allowed label.
	if lbl := HasAllowedLabel(cfg.PRLabels, labels); lbl != "" {
		out.Pass = true
		out.Skipped = true
		out.SkipReason = "label '" + lbl + "'"
		out.Messages = append(out.Messages,
			"[template-compliance] EC-5: label '"+lbl+"' present — SKIP gate")
		return out, nil
	}

	// Empty body → FAIL.
	if cfg.PRBody == "" {
		out.EmptyBody = true
		out.Pass = false
		out.Messages = append(out.Messages, "[template-compliance] FAIL: empty PR body")
		return out, nil
	}

	if !HasChecklistSection(cfg.PRBody) {
		out.MissingChecklist = true
		out.Pass = false
		out.Messages = append(out.Messages,
			"[template-compliance] FAIL: PR body missing 'Quality Gates Checklist' section",
			"[template-compliance] use .github/PULL_REQUEST_TEMPLATE.md as starting point")
		return out, nil
	}

	out.UncheckedBoxes = CountUncheckedBoxes(cfg.PRBody)
	if out.UncheckedBoxes == 0 {
		out.Pass = true
		out.Messages = append(out.Messages, "[template-compliance] PASS — all checkboxes ticked")
		return out, nil
	}

	// Unchecked boxes present → require N/A justifications for WARN+pass.
	out.HasNAJustifications = HasNonEmptyNAJustifications(cfg.PRBody)
	if out.HasNAJustifications {
		out.Pass = true
		out.Messages = append(out.Messages, fmt.Sprintf(
			"[template-compliance] WARN: %d unchecked box(es) with N/A justifications — manual review required",
			out.UncheckedBoxes))
		return out, nil
	}

	out.Pass = false
	out.Messages = append(out.Messages,
		fmt.Sprintf("[template-compliance] FAIL: %d unchecked box(es) in Quality Gates Checklist",
			out.UncheckedBoxes),
		"[template-compliance] check the boxes OR add explanations under 'N/A justifications'",
	)
	return out, nil
}
