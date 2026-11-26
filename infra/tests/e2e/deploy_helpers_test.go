// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Deploy-flow helpers shared by journey_*.go specs.
//
// Extracted on 2026-05-25 from deploy_happy_path_test.go + concurrent_deploy_test.go
// when those Describe blocks were absorbed into Journey 14 (concurrency)
// and removed (happy path duplicated by Journey 01: Solo Deploy).
//
// IMPORTANT: do NOT add Describe/It blocks here. This file holds shared
// helpers only — keeps the journey_*.go files thin and the helper
// surface explicit.
package e2e

import (
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"
)

// deployResult captures the output of a single `loopruntime deploy` invocation.
// Used by Journey 14 concurrent-deploy specs.
type deployResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// initGitRepo initializes a git repo in the given directory, adds all
// files, and creates an initial commit. Returns the commit SHA.
func initGitRepo(dir string) string {
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "e2e@test.local"},
		{"git", "config", "user.name", "E2E Test"},
		{"git", "add", "-A"},
		{"git", "commit", "-m", "initial commit"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "git command %v failed: %s", args, string(output))
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "git rev-parse failed: %s", string(output))

	return strings.TrimSpace(string(output))
}

// runLoopRuntimeDeploy executes `loopruntime deploy` in the given directory with optional
// additional arguments. Returns stdout, stderr, and the exit code.
// Auth is via configStore (XDG_CONFIG_HOME from BeforeSuite), not env.
func runLoopRuntimeDeploy(dir string, args ...string) (stdout, stderr string, exitCode int) {
	return execLoopRuntime(loopruntimeInvocation{dir: dir}, append([]string{"deploy"}, args...)...)
}
