// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// drill_skill_teardown (lib/drill-skill-common.sh) wrote "**Teardown:** CLEAN"
// and returned 0 after pulumi destroy had left resources behind: it looked for
// the validator at infra/scripts/drill-teardown-validator.sh, a path that no
// longer exists, and skipped it without saying so.

// runSkillTeardown sources the lib and runs drill_skill_teardown with pulumi
// reporting three residual resources after every destroy. doctlListFails makes
// the validator's cloud listing fail.
func runSkillTeardown(t *testing.T, doctlListFails bool) (int, string, string) {
	t.Helper()
	stub := doctlStub(t, doctlListFails, "")
	for name, body := range map[string]string{
		"npx": "#!/bin/sh\ncase \"$*\" in\n" +
			"  'pulumi destroy'*) exit 1 ;;\n" +
			"  'pulumi stack'*) echo 'Current stack resources (3):' ;;\n" +
			"esac\nexit 0\n",
		"kubectl": "#!/bin/sh\nexit 1\n",
	} {
		if err := os.WriteFile(filepath.Join(stub, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "pulumi-drill"), 0o755); err != nil {
		t.Fatal(err)
	}
	audit := filepath.Join(work, "audit.md")
	if err := os.WriteFile(audit, []byte("# drill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := filepath.Abs(filepath.Join("..", "..", "scripts", "lib", "drill-skill-common.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", `source "$1"; drill_skill_teardown`, "teardown", lib)
	cmd.Dir = work
	cmd.Env = append(os.Environ(),
		"PATH="+stub+string(os.PathListSeparator)+"/usr/bin:/bin",
		"_DRILL_SKILL_DOTENV_LOADED=1",
		"DIGITALOCEAN_TOKEN=fixture-token",
		"DRILL_TEARDOWN_LOCK_FILE="+filepath.Join(work, "teardown.lock"),
		"DRILL_AUDIT_DIR="+work,
		"DRILL_SKILL_AUDIT_DIR="+filepath.Join(work, "run-1"),
		"DRILL_SKILL_AUDIT_MD="+audit,
		"PULUMI_DRILL_DIR="+filepath.Join(work, "pulumi-drill"),
		"EVIDENCE_DIR="+filepath.Join(work, "evidence"),
		"AUDIT_DOC_DIR="+filepath.Join(work, "audit-docs"),
		"KUBECONFIG=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	md, _ := os.ReadFile(audit)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out), string(md)
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), string(out), string(md)
	default:
		t.Fatalf("run: %v", err)
		return -1, "", ""
	}
}

func TestSkillTeardown_ResidualsTheValidatorCannotCheckAreNotClean(t *testing.T) {
	code, out, md := runSkillTeardown(t, true)

	if code == 0 {
		t.Fatalf("teardown returned 0 with residuals nobody verified\n%s", out)
	}
	if strings.Contains(md, "**Teardown:** CLEAN") {
		t.Fatalf("audit says CLEAN:\n%s", md)
	}
}

// With pulumi state out of sync but the cloud verified empty, the teardown is
// clean — and the record says why.
func TestSkillTeardown_IsCleanWhenTheValidatorFindsNothing(t *testing.T) {
	code, out, md := runSkillTeardown(t, false)

	if code != 0 || !strings.Contains(md, "**Teardown:** CLEAN") {
		t.Fatalf("exit = %d, audit:\n%s\noutput:\n%s", code, md, out)
	}
	if !strings.Contains(out, "teardown validator PASS") {
		t.Fatalf("the validator did not run:\n%s", out)
	}
}
