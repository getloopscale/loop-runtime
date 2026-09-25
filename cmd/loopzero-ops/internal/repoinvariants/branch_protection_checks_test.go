// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repoinvariants

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
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
