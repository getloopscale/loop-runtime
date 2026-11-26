// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repoinvariants

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
	"gopkg.in/yaml.v3"
)

// .github/branch-protection-config.yml is what infra/scripts/ci/branch-protection-update.sh
// applies to the remote. It required 13 status checks from workflows deleted on
// 2026-05-23 ("Quality Gates - Coverage Report", "Helm Charts / Render-time tests",
// "drill-hermetic-tests", ...). Applied, it would have blocked every merge waiting for
// checks nothing can produce any more. A required check is only meaningful if some
// workflow job reports under that exact name, so this test holds the file to the jobs
// that exist.

type branchProtectionConfig struct {
	Branches map[string]struct {
		RequiredStatusChecks []string `yaml:"required_status_checks"`
	} `yaml:"branches"`
}

type workflowJobs struct {
	Jobs map[string]struct {
		Name string `yaml:"name"`
	} `yaml:"jobs"`
}

// workflowCheckNames returns the names GitHub reports job checks under: the job's
// `name:` when set, the job key otherwise.
func workflowCheckNames(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(qtestutil.MustRepoRoot(), ".github/workflows/*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	names := map[string]bool{}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var wf workflowJobs
		if err := yaml.Unmarshal(body, &wf); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for key, job := range wf.Jobs {
			if job.Name != "" {
				names[job.Name] = true
			} else {
				names[key] = true
			}
		}
	}
	return names
}

func TestBranchProtectionRequiresOnlyChecksAWorkflowReports(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(qtestutil.MustRepoRoot(), ".github/branch-protection-config.yml"))
	if err != nil {
		t.Fatalf("read branch-protection-config.yml: %v", err)
	}
	var cfg branchProtectionConfig
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse branch-protection-config.yml: %v", err)
	}
	if len(cfg.Branches) == 0 {
		t.Fatal("branch-protection-config.yml declares no branch")
	}
	jobs := workflowCheckNames(t)
	for branch, rules := range cfg.Branches {
		if len(rules.RequiredStatusChecks) == 0 {
			t.Errorf("branch %s requires no status check: protection that requires nothing is not protection", branch)
		}
		for _, check := range rules.RequiredStatusChecks {
			if !jobs[check] {
				t.Errorf("branch %s requires status check %q, which no job in .github/workflows reports — "+
					"applied, it blocks every merge on a check that can never arrive", branch, check)
			}
		}
	}
}

type workflowWithTriggers struct {
	On   map[string]yaml.Node `yaml:"on"`
	Jobs map[string]struct {
		Name string `yaml:"name"`
	} `yaml:"jobs"`
}

// pathFilteredPullRequestChecks returns, per check name, the workflow file whose
// `pull_request` trigger carries a `paths` or `paths-ignore` filter.
func pathFilteredPullRequestChecks(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(qtestutil.MustRepoRoot(), ".github/workflows/*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	filtered := map[string]string{}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var wf workflowWithTriggers
		if err := yaml.Unmarshal(body, &wf); err != nil {
			// `on:` written as a string or a list has no filter to read.
			continue
		}
		trigger, ok := wf.On["pull_request"]
		if !ok {
			continue
		}
		var filters map[string]yaml.Node
		if err := trigger.Decode(&filters); err != nil {
			continue
		}
		_, hasPaths := filters["paths"]
		_, hasPathsIgnore := filters["paths-ignore"]
		if !hasPaths && !hasPathsIgnore {
			continue
		}
		for key, job := range wf.Jobs {
			name := job.Name
			if name == "" {
				name = key
			}
			filtered[name] = filepath.Base(f)
		}
	}
	return filtered
}

// A workflow skipped by a path filter reports no status at all, and a required
// check with no status blocks the merge. A pull request that changed only
// documentation could never be merged into develop for that reason.
func TestRequiredChecksAreReportedOnEveryPullRequest(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(qtestutil.MustRepoRoot(), ".github/branch-protection-config.yml"))
	if err != nil {
		t.Fatalf("read branch-protection-config.yml: %v", err)
	}
	var cfg branchProtectionConfig
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("parse branch-protection-config.yml: %v", err)
	}
	filtered := pathFilteredPullRequestChecks(t)
	for branch, rules := range cfg.Branches {
		for _, check := range rules.RequiredStatusChecks {
			if file, ok := filtered[check]; ok {
				t.Errorf("branch %s requires %q, and %s skips pull requests by path: "+
					"a pull request the filter excludes never gets the check and cannot merge", branch, check, file)
			}
		}
	}
}
