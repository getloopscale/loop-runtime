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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/taskfileinterface"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

const skillsDir = ".claude/skills"

// skipSkillsForTaskInvocationCheck are skills that are generic (not LoopRuntime-
// customized) — their `task X` refs are not real LoopRuntime task refs.
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
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
	// Unverified is why the check could verify nothing on this machine
	// (wrapping one of its Err* reasons), or nil.
	Unverified error
}

// Verdict is Fail when the check found a violation, NotVerified when it
// could verify nothing on this machine, and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Violation != nil:
		return verdict.Fail
	case o.Unverified != nil:
		return verdict.NotVerified
	}
	return verdict.Pass
}

// ErrSkillsRootMissing means .claude/skills is absent. That directory is the
// installed kit, which .gitignore excludes, so a fresh clone never has it: the
// gate verified nothing, and says so instead of reporting a count of zero.
var ErrSkillsRootMissing = errors.New("the skills root " + skillsDir + " is absent — it is the installed, gitignored kit, so nothing was verified on this machine")

// ErrViolations means at least one skill reference resolves to a missing file
// / non-existent task.
var ErrViolations = errors.New("SKILL.md file references at least one missing infra/scripts path OR non-existent task")

var (
	scriptRefRE    = regexp.MustCompile(`infra/scripts/[a-zA-Z0-9_./-]+\.sh`)
	drillAbbrevRE  = regexp.MustCompile(`drill/[a-zA-Z0-9_-]+\.sh`)
	backtickTaskRE = regexp.MustCompile("`task ([a-z][a-z0-9_:-]+)`")
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

	tasks, err := taskfileinterface.CollectTaskNames(root)
	if err != nil {
		return Outcome{}, fmt.Errorf("collect task names: %w", err)
	}

	skillsRoot := filepath.Join(root, skillsDir)
	entries, err := os.ReadDir(skillsRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return Outcome{Unverified: ErrSkillsRootMissing}, nil
	}
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
		if errors.Is(err, fs.ErrNotExist) {
			continue // a directory with no SKILL.md is not a skill
		}
		if err != nil {
			return out, fmt.Errorf("read %s: %w", skillMD, err)
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
				if !tasks[task] {
					out.Failures = append(out.Failures, fmt.Sprintf("%s: references non-existent task '%s'", skillName, task))
				}
			}
		}

		// 2c: per-skill anti-patterns
		if skillName == "dogfood" && dogfoodGHTokRE.MatchString(body) {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: SKILL.md still references GH_TOKEN flow via .env (T0.2 — replace with ~/.loop-runtime/local-dogfood-token.txt)", skillName))
		}
		if skillName == "to-plan" && toPlanTeardown.MatchString(body) {
			out.Failures = append(out.Failures, fmt.Sprintf("%s: SKILL.md still references non-existent 'task teardown' (T0.3 — replace with task clean:all)", skillName))
		}
	}

	if len(out.Failures) > 0 {
		out.Violation = ErrViolations
		return out, nil
	}
	return out, nil
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
