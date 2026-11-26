// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// infra/staging/validate.sh is run against a GitHub served by the test, with a token
// command and a suite command that stand in for the Kratos seed and the dogfood suite.

const validatedSHA = "0123456789abcdef0123456789abcdef01234567"

type validateHarness struct {
	t         *testing.T
	github    *fakeGitHub
	server    *httptest.Server
	state     string
	tokenExit string
	planExit  string
	suiteExit string
	suiteOut  string
}

func newValidateHarness(t *testing.T) *validateHarness {
	t.Helper()
	for _, tool := range []string{"curl", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("infra/staging/validate.sh needs %s on PATH: %v", tool, err)
		}
	}
	h := &validateHarness{t: t, state: filepath.Join(t.TempDir(), "state"), tokenExit: "0", planExit: "0", suiteExit: "0"}
	h.github = &fakeGitHub{published: map[string]bool{}, committed: time.Now()}
	h.server = httptest.NewServer(h.github)
	t.Cleanup(h.server.Close)
	return h
}

func (h *validateHarness) script(name, body string) string {
	h.t.Helper()
	path := filepath.Join(h.t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body), 0o755); err != nil {
		h.t.Fatal(err)
	}
	return path
}

func (h *validateHarness) validate(args ...string) (string, error) {
	h.t.Helper()
	token := filepath.Join(h.t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("status-token"), 0o600); err != nil {
		h.t.Fatal(err)
	}
	// The fake token command writes the token where the suite is told to read it; the
	// fake suite records the variables it ran with.
	tokenCmd := h.script("token", fmt.Sprintf("echo seeded-token > \"$LOOPZERO_DOGFOOD_JWT_PATH\"\nexit %s\n", h.tokenExit))
	planCmd := h.script("plan", fmt.Sprintf("echo \"plan set for the tenant of $(cat \"$LOOPZERO_DOGFOOD_JWT_PATH\")\"\nexit %s\n", h.planExit))
	suiteCmd := h.script("suite", fmt.Sprintf("echo \"jwt=$(cat \"$LOOPZERO_DOGFOOD_JWT_PATH\") deep=$LOOPZERO_DOGFOOD_DEEP_HEALTH_URL cloud=$LOOPZERO_DOGFOOD_CLOUD_URL\"\nprintf '%%s' %q\nexit %s\n", h.suiteOut, h.suiteExit))
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(), "infra/staging/validate.sh")}, args...)...)
	cmd.Env = append(os.Environ(),
		"DEPLOY_GITHUB_API="+h.server.URL,
		"DEPLOY_STATE_DIR="+h.state,
		"DEPLOY_STATUS_TOKEN_FILE="+token,
		"VALIDATE_CHECKOUT="+h.t.TempDir(),
		"VALIDATE_TOKEN_CMD="+tokenCmd,
		"VALIDATE_PLAN_CMD="+planCmd,
		"VALIDATE_SUITE_CMD="+suiteCmd,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (h *validateHarness) log() string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.state, "logs", validatedSHA+".validate.log"))
	if err != nil {
		h.t.Fatalf("no validation log: %v", err)
	}
	return string(data)
}

func TestStagingValidateReportsSuccessWhenTheJourneysPass(t *testing.T) {
	h := newValidateHarness(t)

	out, err := h.validate(validatedSHA)

	if err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out)
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/validate=pending,staging/validate=success" {
		t.Errorf("statuses %s, want pending then success", got)
	}
	for _, s := range h.github.statuses {
		if s["sha"] != validatedSHA {
			t.Errorf("status posted on %s, want %s", s["sha"], validatedSHA)
		}
	}
}

func TestStagingValidatePointsTheSuiteAtStagingWithTheSeededToken(t *testing.T) {
	h := newValidateHarness(t)

	if out, err := h.validate(validatedSHA); err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out)
	}

	log := h.log()
	for _, want := range []string{
		"jwt=seeded-token",
		"deep=https://api.staging.getloopscale.com/api/v1/health/deep",
		"cloud=https://cloud.staging.getloopscale.com",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the suite did not run with %s; log:\n%s", want, log)
		}
	}
}

func TestStagingValidateReportsFailureWhenAJourneyFails(t *testing.T) {
	h := newValidateHarness(t)
	h.suiteExit = "1"
	h.suiteOut = "  [FAIL] J-18 Status + Health deep health probe\n"

	out, err := h.validate(validatedSHA)

	if err == nil {
		t.Fatalf("a failed suite exited 0\n%s", out)
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/validate=pending,staging/validate=failure" {
		t.Errorf("statuses %s, want pending then failure", got)
	}
	if last := h.github.statuses[len(h.github.statuses)-1]["description"]; !strings.Contains(last, "1 staging journey specs failed") {
		t.Errorf("the failure says %q, want the count of failed specs", last)
	}
	if !strings.Contains(h.log(), "[FAIL] J-18") {
		t.Errorf("the log does not keep the suite's output")
	}
}

func TestStagingValidateReportsFailureWhenTheIdentityCannotBeSeeded(t *testing.T) {
	h := newValidateHarness(t)
	h.tokenExit = "1"

	out, err := h.validate(validatedSHA)

	if err == nil {
		t.Fatalf("a failed seed exited 0\n%s", out)
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/validate=pending,staging/validate=failure" {
		t.Errorf("statuses %s, want pending then failure", got)
	}
}

// A tenant starts on Starter, whose 50 builds a month the journeys use up in a day of
// deploys. From then on every build was refused with HTTP 402 and the validation of a
// healthy staging failed, so the run puts its tenant on a larger plan first.
func TestStagingValidateSetsThePlanOfItsTenantBeforeTheJourneys(t *testing.T) {
	h := newValidateHarness(t)

	out, err := h.validate(validatedSHA)

	if err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out)
	}
	log := h.log()
	plan, suite := strings.Index(log, "plan set for the tenant of seeded-token"), strings.Index(log, "jwt=seeded-token")
	if plan < 0 || suite < 0 || plan > suite {
		t.Errorf("the plan step must run with the seeded token and before the suite; log:\n%s", log)
	}
}

func TestStagingValidateReportsFailureWhenThePlanCannotBeSet(t *testing.T) {
	h := newValidateHarness(t)
	h.planExit = "1"

	out, err := h.validate(validatedSHA)

	if err == nil {
		t.Fatalf("a failed plan step exited 0\n%s", out)
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/validate=pending,staging/validate=failure" {
		t.Errorf("statuses %s, want pending then failure", got)
	}
	if strings.Contains(h.log(), "jwt=seeded-token") {
		t.Errorf("the suite ran on a tenant whose plan was not set; log:\n%s", h.log())
	}
}

func TestStagingValidateKeepsTheStatusDescriptionWithinGitHubsLimit(t *testing.T) {
	h := newValidateHarness(t)
	h.suiteExit = "1"
	h.suiteOut = strings.Repeat("  [FAIL] a journey\n", 1000)

	_, _ = h.validate(validatedSHA)

	for _, s := range h.github.statuses {
		if len(s["description"]) > 140 {
			t.Errorf("description of %d characters: GitHub refuses more than 140", len(s["description"]))
		}
	}
}

func TestStagingValidateRefusesAnArgumentThatIsNotACommit(t *testing.T) {
	h := newValidateHarness(t)

	out, err := h.validate("develop")

	if err == nil || !strings.Contains(out, "usage") {
		t.Fatalf("got %v: %s", err, out)
	}
	if statuses := h.github.contexts(); len(statuses) != 0 {
		t.Errorf("posted %v for no commit", statuses)
	}
}

func TestStagingValidateFailsAJourneyFilterThatSelectsNothing(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra/staging/validate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "-ginkgo.fail-on-empty") {
		t.Errorf("the suite runs without -ginkgo.fail-on-empty: a filter that selects no spec would pass")
	}
}

func TestStagingDeployUnitLeavesTimeForTheValidation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra/staging/systemd/loopzero-staging-deploy.service"))
	if err != nil {
		t.Fatal(err)
	}
	// A platform sync takes about ten minutes and the suite may take up to its 30 minute
	// go test timeout, so the unit allows an hour.
	if !strings.Contains(string(data), "TimeoutStartSec=60min") {
		t.Errorf("loopzero-staging-deploy.service does not allow 60 minutes for a deploy and its validation")
	}
}

// go test prints a passing package's output only with -v, so without it a green validate
// log holds no Ginkgo summary, and nothing on the host says how many journeys ran.
func TestTheValidateSuiteLogsItsSummaryWhenItPasses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra/staging/validate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "run_suite() {")
	end := strings.Index(text[start:], "\n}")
	if start < 0 || end < 0 {
		t.Fatal("no run_suite in infra/staging/validate.sh")
	}
	body := text[start : start+end]
	if !regexp.MustCompile(`go test (\S+ )*-v( |$)`).MatchString(body) {
		t.Errorf("run_suite runs go test without -v:\n%s", body)
	}
}

// validateScript is infra/staging/validate.sh, and runSuiteBody its run_suite function.
func validateScript(t *testing.T) (script, runSuiteBody string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra/staging/validate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script = string(data)
	start := strings.Index(script, "run_suite() {")
	if start < 0 {
		t.Fatal("no run_suite in infra/staging/validate.sh")
	}
	end := strings.Index(script[start:], "\n}")
	if end < 0 {
		t.Fatal("run_suite in infra/staging/validate.sh has no end")
	}
	return script, script[start : start+end]
}

// The journeys used to read lists and health only: none deployed an app, so staging
// went green while secrets, scaling, runtime logs and metrics were broken (#86). The
// app lifecycle journey deploys one, and it runs only if JOURNEYS selects its label and
// a spec carries that label.
func TestStagingValidateSelectsTheAppLifecycleJourney(t *testing.T) {
	script, _ := validateScript(t)
	journeys := regexp.MustCompile(`(?m)^JOURNEYS="\$\{VALIDATE_JOURNEYS:-([^}]*)\}"`).FindStringSubmatch(script)
	if journeys == nil {
		t.Fatal("infra/staging/validate.sh declares no default JOURNEYS")
	}
	selected := false
	for _, label := range strings.Split(journeys[1], "||") {
		if strings.TrimSpace(label) == "app-lifecycle" {
			selected = true
		}
	}
	if !selected {
		t.Errorf("JOURNEYS is %q: it does not select app-lifecycle, so no journey deploys an app", journeys[1])
	}

	spec, err := os.ReadFile(filepath.Join(repoRoot(), "infra/tests/livetest/dogfood/journey_app_lifecycle_live_test.go"))
	if err != nil {
		t.Fatalf("no spec for the app lifecycle journey: %v", err)
	}
	if !regexp.MustCompile(`Label\([^)]*"app-lifecycle"`).Match(spec) {
		t.Error("journey_app_lifecycle_live_test.go carries no app-lifecycle label: the filter would select nothing of it")
	}
	if !strings.HasPrefix(string(spec), "// SPDX") || !strings.Contains(string(spec), "//go:build live") {
		t.Error("journey_app_lifecycle_live_test.go is not gated by the live build tag")
	}
	if regexp.MustCompile(`\bSkip\(`).Match(spec) {
		t.Error("the app lifecycle journey calls Skip: a step that cannot run must fail, not pass as skipped")
	}
}

// The journey reads the app's Rollout from the cell that runs it, so the suite needs
// kubectl's environment and the names of the cell contexts.
func TestStagingValidateGivesTheSuiteTheCells(t *testing.T) {
	_, body := validateScript(t)
	if !regexp.MustCompile(`(?m)^\s*kube_env\b`).MatchString(body) {
		t.Errorf("run_suite does not call kube_env, so the suite's kubectl cannot reach the cells:\n%s", body)
	}
	if !regexp.MustCompile(`LOOPZERO_DOGFOOD_CELL_CONTEXTS=`).MatchString(body) {
		t.Errorf("run_suite does not name the cell contexts in LOOPZERO_DOGFOOD_CELL_CONTEXTS:\n%s", body)
	}
}

// A build and two rollouts through canary steps take minutes. The suite's go test
// timeout has to hold the journey, and the deploy unit has to hold the suite.
func TestStagingValidateLeavesTimeForTheAppLifecycleJourney(t *testing.T) {
	_, body := validateScript(t)
	timeout := regexp.MustCompile(`-timeout (\d+)m`).FindStringSubmatch(body)
	if timeout == nil {
		t.Fatalf("run_suite sets no go test -timeout:\n%s", body)
	}
	spec, err := os.ReadFile(filepath.Join(repoRoot(), "infra/tests/livetest/dogfood/journey_app_lifecycle_live_test.go"))
	if err != nil {
		t.Fatalf("no spec for the app lifecycle journey: %v", err)
	}
	budget := regexp.MustCompile(`journeyBudget\s*=\s*(\d+) \* time\.Minute`).FindSubmatch(spec)
	if budget == nil {
		t.Fatal("journey_app_lifecycle_live_test.go declares no journeyBudget in minutes")
	}
	var suiteMinutes, journeyMinutes int
	fmt.Sscanf(timeout[1], "%d", &suiteMinutes)
	fmt.Sscanf(string(budget[1]), "%d", &journeyMinutes)
	if journeyMinutes+3 > suiteMinutes {
		t.Errorf("the journey may take %d minutes and go test stops the suite at %d: it would be killed without saying which step stopped", journeyMinutes, suiteMinutes)
	}
}
