// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Journey 03: Secrets Lifecycle
//
// Migrated from: infra/tests/dogfood/journeys/journey-03-secrets.sh
//
// Flow: deploy → secret set → redeploy → verify env → secret delete → redeploy → verify gone
// Fixture: secret-app (single app with secrets: [SECRET_PING])
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

var _ = Describe("Journey 03: Secrets Lifecycle", Ordered, Label("journey"), func() {
	const (
		project    = "secret-app"
		app        = "api"
		env        = "j-secrets"
		secretName = "SECRET_PING"
	)

	var (
		workDir     string
		host        string
		secretValue string
	)

	BeforeAll(func() {
		skipIfNotE2E()
		seedE2E()

		host = seed.TenantID + "-" + project + "-" + app + "-" + env + "." + e2eBaseDomain()
		secretValue = fmt.Sprintf("ginkgo-test-%d", time.Now().Unix())

		ensureProjectDeleted(project)

		workDir = copyFixture("secret-app")
		initGitRepo(workDir)

		// V9 L15: loopzero secret set requires project record in DB. With L8
		// env-per-journey isolation, each journey is now truly isolated, so
		// the project must be created within the journey itself BEFORE
		// attempting "sets secret before deploy". Pre-L8, journeys
		// implicitly shared the project record from earlier specs.
		runLoopZeroInDir(workDir, "project", "create", project, "--json")
	})

	AfterAll(func() {
		ensureProjectDeleted(project)
		if workDir != "" {
			os.RemoveAll(workDir)
		}
	})

	It("sets secret before deploy", func() {
		secretArg := fmt.Sprintf("%s=%s", secretName, secretValue)
		stdout, stderr, exitCode := runLoopZeroInDir(workDir, "secret", "set", secretArg,
			"--env", env, "--app", app, "--json")

		Expect(exitCode).To(Equal(0),
			"loopzero secret set should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"secret set stdout": stdout,
			"secret set stderr": stderr,
		})
	})

	It("deploys with secret", func() {
		deployAndWaitSuccess(workDir, env, deployTimeout)
	})

	It("app responds HTTP 200", func() {
		waitAppHealthyViaKubectl(project, app, env, "/health", httpProbeTimeout)
	})

	It("secret is visible in running app", func() {
		// ESO sync can take up to 30s.
		Eventually(func() string {
			_, body, _ := httpGetWithHost(host, "/secret-check")
			return body
		}, 60*time.Second, 5*time.Second).Should(
			ContainSubstring(secretValue),
			"app should see SECRET_PING=%s via /secret-check", secretValue)
	})

	It("secret list shows name but NEVER value", func() {
		stdout, _, exitCode := runLoopZeroInDir(workDir, "secret", "list",
			"--env", env, "--json")

		Expect(exitCode).To(Equal(0))
		Expect(stdout).To(ContainSubstring(secretName),
			"secret list should contain the secret name")
		Expect(stdout).NotTo(ContainSubstring(secretValue),
			"secret list MUST NOT contain the secret value")
	})

	It("deletes secret", func() {
		stdout, stderr, exitCode := runLoopZeroInDir(workDir, "secret", "delete", secretName,
			"--env", env, "--app", app, "--yes", "--json")

		Expect(exitCode).To(Equal(0),
			"loopzero secret delete should exit 0\nstdout: %s\nstderr: %s", stdout, stderr)

		assertNoForbiddenTermsInAll(map[string]string{
			"secret delete stdout": stdout,
			"secret delete stderr": stderr,
		})
	})

	It("secret gone from list after delete", func() {
		stdout, _, exitCode := runLoopZeroInDir(workDir, "secret", "list",
			"--env", env, "--json")

		Expect(exitCode).To(Equal(0))
		Expect(stdout).NotTo(ContainSubstring(secretName),
			"secret list should NOT contain deleted secret name")
	})

	It("redeploys after secret removal", func() {
		// Remove secrets declaration from loopzero.yaml to avoid MISSING_SECRETS.
		loopzeroYAML := workDir + "/loopzero.yaml"
		content, err := os.ReadFile(loopzeroYAML)
		Expect(err).NotTo(HaveOccurred())

		// Remove the secrets: line from loopzero.yaml.
		lines := strings.Split(string(content), "\n")
		var filtered []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "- SECRET_PING" || trimmed == "secrets:" {
				continue
			}
			filtered = append(filtered, line)
		}
		err = os.WriteFile(loopzeroYAML, []byte(strings.Join(filtered, "\n")), 0644)
		Expect(err).NotTo(HaveOccurred())

		// Commit the change.
		commitInDir(workDir, "remove secret from loopzero.yaml")

		deployAndWaitSuccess(workDir, env, deployTimeout)
	})

	It("secret no longer visible in app after removal", func() {
		Eventually(func() bool {
			_, body, err := httpGetWithHost(host, "/secret-check")
			if err != nil {
				return false
			}
			return strings.Contains(body, "unset") || !strings.Contains(body, secretValue)
		}, 60*time.Second, 5*time.Second).Should(BeTrue(),
			"app should no longer see SECRET_PING after secret deletion + redeploy")
	})

	It("audit records secret operations", func() {
		assertAuditEntry("secret.set", project)
	})

	It("cleanup: project delete + Prova de Morte", func() {
		ensureProjectDeleted(project)
		proveDead(seed.TenantID, project, env)
	})
})

// commitInDir stages all changes and creates a commit in the given directory.
func commitInDir(dir, message string) {
	for _, args := range [][]string{
		{"git", "add", "-A"},
		{"git", "commit", "-m", message},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(),
			"git %v failed: %s", args, string(output))
	}
}
