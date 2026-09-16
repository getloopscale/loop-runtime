// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docRelPath is the declaration of the four periodic jobs that lost their
// scheduler when the loopzero-ci-workflows chart was retired (B-019, B-147).
const docRelPath = "docs/operations/retired-periodic-jobs.md"

// historicalBrokenTaskName is the one task reference in that document which is
// EXPECTED not to resolve: it is the command the retired chart declared for
// coverageReport, and recording that it never resolved is the point of the row
// it appears in. Every other `task X` in the document must be real.
const historicalBrokenTaskName = "quality:coverage:report"

// realCoverageReportTaskName is the target that actually exists.
const realCoverageReportTaskName = "quality:test:coverage:report"

// Task-name resolution below mirrors the include layout of Taskfile.yml. It is
// duplicated rather than imported because the authority for it lives in
// cmd/loopzero-ops/internal/quality/skillpaths, which is an internal package of a
// different module and cannot be reached from infra/tests.
var (
	flattenedSubfiles = []string{
		"test", "helm", "infra", "dev", "db", "k3d", "security", "observability",
	}
	namespacedSubfiles = map[string]string{
		"drill.yml":   "drill:",
		"quality.yml": "quality:",
	}
	backtickTaskRE = regexp.MustCompile("`task ([a-z][a-z0-9_:-]+)`")
	taskNameLineRE = regexp.MustCompile(`^  ([a-z][a-z0-9:_-]*):[[:space:]]*(#.*)?$`)
)

// repoRootFromTests resolves the repository root relative to infra/tests/scripts/.
func repoRootFromTests(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Taskfile.yml")); err != nil {
		t.Fatalf("repo root %s has no Taskfile.yml: %v", root, err)
	}
	return root
}

// tasksInFile extracts the task names declared under a taskfile's `tasks:` key.
func tasksInFile(path string) []string {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var names []string
	inTasks := false
	for _, line := range strings.Split(string(body), "\n") {
		if !inTasks {
			if strings.HasPrefix(line, "tasks:") {
				inTasks = true
			}
			continue
		}
		if len(line) > 0 && line[0] >= 'a' && line[0] <= 'z' {
			inTasks = false
			continue
		}
		if m := taskNameLineRE.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

// allTaskNames returns every task name invocable as `task <name>`.
func allTaskNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	for _, n := range tasksInFile(filepath.Join(root, "Taskfile.yml")) {
		set[n] = true
	}
	for _, sub := range flattenedSubfiles {
		for _, n := range tasksInFile(filepath.Join(root, "taskfiles", sub+".yml")) {
			set[n] = true
		}
	}
	for file, prefix := range namespacedSubfiles {
		for _, n := range tasksInFile(filepath.Join(root, "taskfiles", file)) {
			set[prefix+n] = true
		}
	}
	if len(set) == 0 {
		t.Fatal("extracted zero task names — the extractor is broken, not the document")
	}
	return set
}

// TestRetiredPeriodicJobsDoc_TaskRefsResolve is the mechanical half of B-147's
// "what has no executor is declared as such, and does not vanish silently".
//
// The declaration is only worth reading if the commands in it are real. The
// coverageReport job proves the cost of the alternative: the retired chart
// scheduled `task quality:coverage:report` for months, and no such target ever
// existed, so the job would have failed even with a working runner image.
func TestRetiredPeriodicJobsDoc_TaskRefsResolve(t *testing.T) {
	root := repoRootFromTests(t)
	// The document went with the 2026-09-11 consolidation; what this asserts is what it
	// SAYS about task names, which history holds exactly.
	body := []byte(readDocument(t, "19b8c6eaa~1", docRelPath))
	known := allTaskNames(t, root)

	seen := map[string]bool{}
	var unresolved []string
	for _, m := range backtickTaskRE.FindAllStringSubmatch(string(body), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if name == historicalBrokenTaskName {
			continue
		}
		if !known[name] {
			unresolved = append(unresolved, name)
		}
	}
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		t.Fatalf("%s names task target(s) that do not resolve: %s\n"+
			"Either the target was renamed and the document was not updated, or the "+
			"document records a command that cannot run.", docRelPath, strings.Join(unresolved, ", "))
	}
	if !seen[historicalBrokenTaskName] {
		t.Fatalf("%s no longer mentions %q. That name is the measured defect the "+
			"document exists to record; if it was removed on purpose, remove the "+
			"exception in this test too.", docRelPath, historicalBrokenTaskName)
	}
}

// TestCoverageReportTaskName_MeasuredNames pins the measurement the document
// rests on, so the exception above can never quietly become a lie: the name the
// retired chart scheduled is absent, and the real one is present.
func TestCoverageReportTaskName_MeasuredNames(t *testing.T) {
	root := repoRootFromTests(t)
	known := allTaskNames(t, root)

	if known[historicalBrokenTaskName] {
		t.Fatalf("%q now resolves. The document records it as a target that never "+
			"existed; update %s and drop the exception in this test.",
			historicalBrokenTaskName, docRelPath)
	}
	if !known[realCoverageReportTaskName] {
		t.Fatalf("%q does not resolve. The document names it as the real coverage "+
			"report target; if it was renamed, update %s.",
			realCoverageReportTaskName, docRelPath)
	}
}
