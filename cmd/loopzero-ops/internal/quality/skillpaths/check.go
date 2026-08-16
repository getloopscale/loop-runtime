// Package skillpaths enforces that every infra/scripts/ path referenced in
// any .claude/skills/*/SKILL.md AND every `task <name>` invocation cited in
// those SKILL.md files actually resolves.
//
// Plan 2026-05-21-deep-review-remediation T0.1 + EC D5.
// Background: drill-test/SKILL.md previously had 6 broken paths; this gate
// prevents regression across all skills.
//
// Migrated from infra/tests/skills/skill-paths_test.sh (M-Migrate2
// continuation 2026-07-07). Hermetic — pure text walk on markdown + taskfiles.
package skillpaths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	skillsDir        = ".claude/skills"
	rootTaskfile     = "Taskfile.yml"
	taskfilesDirName = "taskfiles"
)

// flattenedSubfiles are the taskfile includes that use `flatten: true` in
// Taskfile.yml — their task names appear at root namespace (e.g. `task setup`).
var flattenedSubfiles = []string{"test", "helm", "infra", "dev", "db", "k3d", "security", "observability"}

// namespacedSubfiles map filename → namespace prefix for includes WITHOUT
// `flatten: true` in Taskfile.yml.
var namespacedSubfiles = map[string]string{
	"drill.yml":   "drill:",
	"quality.yml": "quality:",
}

// skipSkillsForTaskInvocationCheck are skills that are generic (not LoopZero-
// customized) — their `task X` refs are not real LoopZero task refs.
var skipSkillsForTaskInvocationCheck = map[string]bool{
	"task-automation": true,
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome carries the assertion failures + counters.
type Outcome struct {
	CheckedSkills int
	CheckedPaths  int
	CheckedTasks  int
	Failures      []string
}

// ErrViolations means at least one skill reference resolves to a missing file
// / non-existent task.
var ErrViolations = errors.New("SKILL.md file references at least one missing infra/scripts path OR non-existent task")

var (
	scriptRefRE    = regexp.MustCompile(`infra/scripts/[a-zA-Z0-9_./-]+\.sh`)
	drillAbbrevRE  = regexp.MustCompile(`drill/[a-zA-Z0-9_-]+\.sh`)
	backtickTaskRE = regexp.MustCompile("`task ([a-z][a-z0-9_:-]+)`")
	taskNameLineRE = regexp.MustCompile(`^  ([a-z][a-z0-9:_-]*):[[:space:]]*(#.*)?$`)
	dogfoodGHTokRE = regexp.MustCompile(`GH_TOKEN.*\.env|\.env.*GH_TOKEN`)
	toPlanTeardown = regexp.MustCompile(`\btask teardown\b`)
)

// Check runs the full audit and returns Outcome + err.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	tasks, err := collectAllTaskNames(root)
	if err != nil {
		return Outcome{}, fmt.Errorf("collect task names: %w", err)
	}

	skillsRoot := filepath.Join(root, skillsDir)
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return Outcome{}, fmt.Errorf("read skills dir %s: %w", skillsDir, err)
	}

	out := Outcome{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillName := e.Name()
		skillMD := filepath.Join(skillsRoot, skillName, "SKILL.md")
		bodyBytes, err := os.ReadFile(skillMD)
		if err != nil {
			continue // no SKILL.md → skip
		}
		body := string(bodyBytes)
		out.CheckedSkills++

		// 2a: full infra/scripts/... paths
		for _, path := range uniqMatches(scriptRefRE, body) {
			if strings.Contains(path, "*") {
				continue
			}
			out.CheckedPaths++
			if _, err := os.Stat(filepath.Join(root, path)); err != nil {
				out.Failures = append(out.Failures, fmt.Sprintf("%s: missing path %s", skillName, path))
			}
		}

		// 2a-bis: abbreviated drill/ script refs (must resolve under infra/scripts/)
		for _, drref := range uniqMatches(drillAbbrevRE, body) {
			if strings.Contains(drref, "*") {
				continue
			}
			out.CheckedPaths++
			if _, err := os.Stat(filepath.Join(root, "infra", "scripts", drref)); err != nil {
				out.Failures = append(out.Failures, fmt.Sprintf("%s: missing drill/ script (abbreviated form) infra/scripts/%s", skillName, drref))
			}
		}

		// 2b: task invocations (backticked only)
		if !skipSkillsForTaskInvocationCheck[skillName] {
			for _, task := range uniqTaskInvocations(body) {
				out.CheckedTasks++
				if _, present := tasks[task]; !present {
					out.Failures = append(out.Failures, fmt.Sprintf("%s: references non-existent task '%s'", skillName, task))
				}
			}
		}

		// 2c: per-skill anti-patterns
		if skillName == "dogfood" && dogfoodGHTokRE.MatchString(body) {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: SKILL.md still references GH_TOKEN flow via .env (T0.2 — replace with ~/.loopzero/local-dogfood-token.txt)", skillName))
		}
		if skillName == "to-plan" && toPlanTeardown.MatchString(body) {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: SKILL.md still references non-existent 'task teardown' (T0.3 — replace with task clean:all)", skillName))
		}
	}

	if len(out.Failures) > 0 {
		return out, ErrViolations
	}
	return out, nil
}

// collectAllTaskNames walks Taskfile.yml + the include set (flattened + namespaced)
// and returns the sorted set of canonical task names.
func collectAllTaskNames(root string) (map[string]struct{}, error) {
	set := map[string]struct{}{}
	add := func(names []string, prefix string) {
		for _, n := range names {
			set[prefix+n] = struct{}{}
		}
	}
	rootTasks, err := extractTasksFromFile(filepath.Join(root, rootTaskfile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rootTaskfile, err)
	}
	add(rootTasks, "")
	for _, sub := range flattenedSubfiles {
		names, err := extractTasksFromFile(filepath.Join(root, taskfilesDirName, sub+".yml"))
		if err != nil {
			// Missing sub-file is not fatal (project may not have all subfiles).
			continue
		}
		add(names, "")
	}
	for filename, prefix := range namespacedSubfiles {
		names, err := extractTasksFromFile(filepath.Join(root, taskfilesDirName, filename))
		if err != nil {
			continue
		}
		add(names, prefix)
	}
	return set, nil
}

// extractTasksFromFile ports the bash awk task-name extractor.
func extractTasksFromFile(path string) ([]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
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
		// A top-level key (not indented) ends the tasks: block.
		if len(line) > 0 && line[0] >= 'a' && line[0] <= 'z' {
			inTasks = false
			continue
		}
		if m := taskNameLineRE.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	return names, nil
}

// uniqMatches returns the sorted unique regex matches over body.
func uniqMatches(re *regexp.Regexp, body string) []string {
	all := re.FindAllString(body, -1)
	set := map[string]struct{}{}
	for _, s := range all {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// uniqTaskInvocations captures the sorted unique `task X` invocations inside backticks.
func uniqTaskInvocations(body string) []string {
	all := backtickTaskRE.FindAllStringSubmatch(body, -1)
	set := map[string]struct{}{}
	for _, m := range all {
		if len(m) == 2 {
			set[m[1]] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
