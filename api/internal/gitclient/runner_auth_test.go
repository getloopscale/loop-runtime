// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGit puts a fake `git` first on PATH that records its arguments and the
// git configuration it received through the environment.
func stubGit(t *testing.T) (argsFile, envFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	envFile = filepath.Join(dir, "env")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\nenv | grep '^GIT_CONFIG_' > " + envFile + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile, envFile
}

func TestRunWithAuthKeepsTheTokenOffTheCommandLine(t *testing.T) {
	// Arrange
	argsFile, envFile := stubGit(t)
	r := NewGitRunner("gt_SECRET_TOKEN")

	// Act
	if err := r.RunWithAuth(context.Background(), t.TempDir(), "push", "origin", "main"); err != nil {
		t.Fatalf("RunWithAuth: %v", err)
	}

	// Assert: argv is readable by every process in the pod via /proc; the
	// token must travel in the environment instead.
	args, _ := os.ReadFile(argsFile)
	if strings.Contains(string(args), "gt_SECRET_TOKEN") {
		t.Errorf("token on the git command line: %s", args)
	}
	env, _ := os.ReadFile(envFile)
	for _, want := range []string{"GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: token gt_SECRET_TOKEN"} {
		if !strings.Contains(string(env), want) {
			t.Errorf("git environment lacks %q:\n%s", want, env)
		}
	}
}
