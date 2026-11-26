// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// infra/staging/deploy.sh is run against a GitHub and a registry served by the test, a
// local git repository standing in for getloopscale/loop-runtime, and a platform command
// that records the values it was given instead of running helmfile.

const deployRepo = "getloopscale/loop-runtime"

type fakeGitHub struct {
	mu        sync.Mutex
	runs      []map[string]any
	published map[string]bool // "<image>:<tag>"
	statuses  []map[string]string
	committed time.Time
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/repos/"+deployRepo+"/statuses/"):
		body, _ := io.ReadAll(r.Body)
		var status map[string]string
		_ = json.Unmarshal(body, &status)
		status["sha"] = strings.TrimPrefix(r.URL.Path, "/repos/"+deployRepo+"/statuses/")
		f.statuses = append(f.statuses, status)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("{}"))
	case r.URL.Path == "/repos/"+deployRepo+"/actions/runs":
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": f.runs})
	case strings.HasPrefix(r.URL.Path, "/repos/"+deployRepo+"/commits/"):
		_ = json.NewEncoder(w).Encode(map[string]any{"commit": map[string]any{"committer": map[string]any{"date": f.committed.UTC().Format(time.RFC3339)}}})
	case r.URL.Path == "/token":
		_, _ = w.Write([]byte(`{"token":"anonymous"}`))
	case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/getloopscale/"):
		rest := strings.TrimPrefix(r.URL.Path, "/v2/getloopscale/")
		image, tag, _ := strings.Cut(rest, "/manifests/")
		if f.published[image+":"+tag] {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusTeapot)
	}
}

func (f *fakeGitHub) contexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, s := range f.statuses {
		out = append(out, s["context"]+"="+s["state"])
	}
	return out
}

type deployHarness struct {
	t        *testing.T
	github   *fakeGitHub
	server   *httptest.Server
	remote   string
	state    string
	calls    string // file the fake platform appends the state values of every call to
	exitCode string
	// validated is the file the fake validation appends the commit of every call to, and
	// validateExit the code it exits with.
	validated    string
	validateExit string
}

const stagingPins = `platformImages:
  registry: "ghcr.io/getloopscale/"
  tags:
    loopruntime-api: "develop-1111111"
    loopruntime-minio: "develop-1111111"
    loopruntime-cloud: "develop-ccccccc"
`

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newDeployHarness(t *testing.T) *deployHarness {
	t.Helper()
	for _, tool := range []string{"git", "curl", "jq", "yq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("infra/staging/deploy.sh needs %s on PATH (yq is mikefarah/yq v4): %v", tool, err)
		}
	}
	root := t.TempDir()
	remote := filepath.Join(root, "loopruntime")
	if err := os.MkdirAll(filepath.Join(remote, "infra/helmfile/environments"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, remote, "git", "init", "--quiet", "--initial-branch=develop")
	h := &deployHarness{t: t, remote: remote, state: filepath.Join(root, "state"), calls: filepath.Join(root, "calls"), exitCode: "0",
		validated: filepath.Join(root, "validated"), validateExit: "0"}
	h.commit(stagingPins)
	h.github = &fakeGitHub{published: map[string]bool{}, committed: time.Now()}
	h.server = httptest.NewServer(h.github)
	t.Cleanup(h.server.Close)
	return h
}

// commit adds a commit with the given staging environment and returns its SHA.
func (h *deployHarness) commit(environment string) string {
	h.t.Helper()
	path := filepath.Join(h.remote, "infra/helmfile/environments/staging.yaml")
	if err := os.WriteFile(path, []byte(environment), 0o644); err != nil {
		h.t.Fatal(err)
	}
	run(h.t, h.remote, "git", "add", "-A")
	run(h.t, h.remote, "git", "commit", "--quiet", "--allow-empty", "-m", "change")
	return run(h.t, h.remote, "git", "rev-parse", "HEAD")
}

func (h *deployHarness) artifactsRun(status, conclusion string) {
	h.artifactsRunAttempt(status, conclusion, 1)
}

// artifactsRunAttempt is the commit's artifacts run at its attempt-th attempt: a re-run
// keeps the run's id and raises run_attempt, as GitHub reports it.
func (h *deployHarness) artifactsRunAttempt(status, conclusion string, attempt int) {
	h.github.mu.Lock()
	defer h.github.mu.Unlock()
	h.github.runs = []map[string]any{{"id": 7, "run_attempt": attempt, "name": "artifacts", "status": status, "conclusion": conclusion, "created_at": time.Now().UTC().Format(time.RFC3339)}}
}

func (h *deployHarness) deploy() (string, error) {
	h.t.Helper()
	token := filepath.Join(h.t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("status-token"), 0o600); err != nil {
		h.t.Fatal(err)
	}
	platform := filepath.Join(h.t.TempDir(), "platform")
	script := fmt.Sprintf("#!/usr/bin/env bash\ncat \"$STAGING_STATE_VALUES\" >> %q\necho >> %q\nexit %s\n", h.calls, h.calls, h.exitCode)
	if err := os.WriteFile(platform, []byte(script), 0o755); err != nil {
		h.t.Fatal(err)
	}
	validate := filepath.Join(h.t.TempDir(), "validate")
	validateScript := fmt.Sprintf("#!/usr/bin/env bash\necho \"$1\" >> %q\nexit %s\n", h.validated, h.validateExit)
	if err := os.WriteFile(validate, []byte(validateScript), 0o755); err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra/staging/deploy.sh"))
	cmd.Env = append(os.Environ(),
		"DEPLOY_VALIDATE_CMD="+validate,
		"DEPLOY_REMOTE="+h.remote,
		"DEPLOY_GITHUB_API="+h.server.URL,
		"DEPLOY_REGISTRY="+h.server.URL,
		"DEPLOY_STATE_DIR="+h.state,
		"DEPLOY_STATUS_TOKEN_FILE="+token,
		"DEPLOY_PLATFORM_CMD="+platform,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// deployedTags is the platformImages.tags of every platform call, oldest first.
func (h *deployHarness) deployedTags() []map[string]string {
	h.t.Helper()
	data, err := os.ReadFile(h.calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	var calls []map[string]string
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for decoder.More() {
		var values struct {
			PlatformImages struct {
				Tags map[string]string `json:"tags"`
			} `json:"platformImages"`
		}
		if err := decoder.Decode(&values); err != nil {
			h.t.Fatalf("the platform got state values that are not JSON: %v\n%s", err, data)
		}
		calls = append(calls, values.PlatformImages.Tags)
	}
	return calls
}

func TestStagingDeployPinsTheImagesTheCommitPublishedAndKeepsTheOthers(t *testing.T) {
	h := newDeployHarness(t)
	sha := h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "success")
	h.github.published["loopruntime-api:develop-"+sha[:7]] = true

	out, err := h.deploy()

	if err != nil {
		t.Fatalf("deploy failed: %v\n%s", err, out)
	}
	calls := h.deployedTags()
	if len(calls) != 1 {
		t.Fatalf("the platform ran %d times, want 1\n%s", len(calls), out)
	}
	want := map[string]string{"loopruntime-api": "develop-" + sha[:7], "loopruntime-minio": "develop-1111111", "loopruntime-cloud": "develop-ccccccc"}
	for image, tag := range want {
		if calls[0][image] != tag {
			t.Errorf("%s deployed as %q, want %q", image, calls[0][image], tag)
		}
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/deploy=pending,staging/deploy=success" {
		t.Errorf("statuses %s, want pending then success", got)
	}
}

// validatedCommits is the commit of every validation the deploys ran, oldest first.
func (h *deployHarness) validatedCommits() []string {
	h.t.Helper()
	data, err := os.ReadFile(h.validated)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func TestStagingDeployValidatesTheCommitItDeployed(t *testing.T) {
	h := newDeployHarness(t)
	sha := h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "success")

	if out, err := h.deploy(); err != nil {
		t.Fatalf("deploy failed: %v\n%s", err, out)
	}

	if got := h.validatedCommits(); len(got) != 1 || got[0] != sha {
		t.Errorf("validated %v, want the deployed commit %s once", got, sha)
	}
}

func TestStagingDeployDoesNotValidateACommitThatFailedToDeploy(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "success")
	h.exitCode = "3"

	_, _ = h.deploy()

	if got := h.validatedCommits(); len(got) != 0 {
		t.Errorf("validated %v after a failed platform run", got)
	}
}

func TestStagingDeployFailsWhenTheValidationFails(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "success")
	h.validateExit = "1"

	out, err := h.deploy()

	if err == nil {
		t.Fatalf("a failed validation exited 0\n%s", out)
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/deploy=pending,staging/deploy=success" {
		t.Errorf("statuses %s: the deploy itself succeeded, the validation reports its own", got)
	}
	// The commit stays handled: a failed validation is not retried every five minutes.
	if out, err := h.deploy(); err != nil {
		t.Fatalf("second run failed: %v\n%s", err, out)
	}
	if got := h.validatedCommits(); len(got) != 1 {
		t.Errorf("validated %d times, want once", len(got))
	}
}

func TestStagingDeployKeepsTheConsolePinsFromGit(t *testing.T) {
	h := newDeployHarness(t)
	sha := h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "success")
	h.github.published["loopruntime-cloud:develop-"+sha[:7]] = true

	if out, err := h.deploy(); err != nil {
		t.Fatalf("deploy failed: %v\n%s", err, out)
	}

	if got := h.deployedTags()[0]["loopruntime-cloud"]; got != "develop-ccccccc" {
		t.Errorf("loopruntime-cloud deployed as %q: the console's images keep the pin in git", got)
	}
}

// looptrace is built by getloopscale/looptrace, so no commit of this repository publishes
// it: the only way its image moves is the pin in git. A deploy that kept the previous
// deploy's tag for it would never roll a new one out.
func TestStagingDeployFollowsTheGitPinOfAnImageAnotherRepositoryBuilds(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "    looptrace: \"develop-aaaaaaa\"\n")
	h.artifactsRun("completed", "success")
	if out, err := h.deploy(); err != nil {
		t.Fatalf("first deploy failed: %v\n%s", err, out)
	}
	h.commit(stagingPins + "    looptrace: \"develop-bbbbbbb\"\n")

	if out, err := h.deploy(); err != nil {
		t.Fatalf("second deploy failed: %v\n%s", err, out)
	}

	calls := h.deployedTags()
	if len(calls) != 2 {
		t.Fatalf("the platform ran %d times, want 2", len(calls))
	}
	if got := calls[1]["looptrace"]; got != "develop-bbbbbbb" {
		t.Errorf("looptrace deployed as %q after its pin moved to develop-bbbbbbb", got)
	}
}

func TestStagingDeployKeepsThePreviousDeploysTagForAnImageTheCommitDidNotBuild(t *testing.T) {
	h := newDeployHarness(t)
	first := h.commit(stagingPins + "# first\n")
	h.artifactsRun("completed", "success")
	h.github.published["loopruntime-api:develop-"+first[:7]] = true
	if out, err := h.deploy(); err != nil {
		t.Fatalf("first deploy failed: %v\n%s", err, out)
	}
	second := h.commit(stagingPins + "# second\n")
	h.github.published["loopruntime-minio:develop-"+second[:7]] = true

	if out, err := h.deploy(); err != nil {
		t.Fatalf("second deploy failed: %v\n%s", err, out)
	}

	calls := h.deployedTags()
	if len(calls) != 2 {
		t.Fatalf("the platform ran %d times, want 2", len(calls))
	}
	if calls[1]["loopruntime-api"] != "develop-"+first[:7] || calls[1]["loopruntime-minio"] != "develop-"+second[:7] {
		t.Errorf("second deploy pinned %v", calls[1])
	}
}

func TestStagingDeployDoesNotDeployACommitWhoseImagesFailedToBuild(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "failure")

	if out, err := h.deploy(); err != nil {
		t.Fatalf("deploy failed: %v\n%s", err, out)
	}

	if calls := h.deployedTags(); len(calls) != 0 {
		t.Errorf("the platform ran for a commit whose artifacts failed: %v", calls)
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/deploy=failure" {
		t.Errorf("statuses %s, want one failure", got)
	}
}

// A failed artifacts run is often a transient registry error, fixed by re-running the
// failed job. The re-run must reach staging; the failure itself is reported once.
func TestStagingDeployDeploysACommitWhoseFailedImageBuildWasRerun(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "# change\n")
	h.artifactsRunAttempt("completed", "failure", 1)
	for range 2 {
		if out, err := h.deploy(); err != nil {
			t.Fatalf("deploy failed: %v\n%s", err, out)
		}
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/deploy=failure" {
		t.Fatalf("statuses %s after two runs over one failed build, want one failure", got)
	}

	h.artifactsRunAttempt("completed", "success", 2)
	if out, err := h.deploy(); err != nil {
		t.Fatalf("deploy after the re-run failed: %v\n%s", err, out)
	}
	if calls := h.deployedTags(); len(calls) != 1 {
		t.Errorf("the platform ran %d times after the build was re-run, want 1", len(calls))
	}
}

func TestStagingDeployWaitsWhileTheImagesAreBuilding(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "# change\n")
	h.artifactsRun("in_progress", "")

	if out, err := h.deploy(); err != nil {
		t.Fatalf("deploy failed: %v\n%s", err, out)
	}

	if calls := h.deployedTags(); len(calls) != 0 {
		t.Errorf("the platform ran while the artifacts run was in progress")
	}
	if statuses := h.github.contexts(); len(statuses) != 0 {
		t.Errorf("a waiting deploy posted %v", statuses)
	}
}

func TestStagingDeployHandlesACommitOnce(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "success")

	for range 2 {
		if out, err := h.deploy(); err != nil {
			t.Fatalf("deploy failed: %v\n%s", err, out)
		}
	}

	if calls := h.deployedTags(); len(calls) != 1 {
		t.Errorf("the platform ran %d times for one commit, want 1", len(calls))
	}
}

func TestStagingDeployReportsAFailedPlatformRun(t *testing.T) {
	h := newDeployHarness(t)
	h.commit(stagingPins + "# change\n")
	h.artifactsRun("completed", "success")
	h.exitCode = "3"

	out, err := h.deploy()

	if err == nil {
		t.Fatalf("a failed platform run exited 0\n%s", out)
	}
	if !strings.Contains(out, "exited 3") {
		t.Errorf("the error does not name the exit code: %s", out)
	}
	if got := strings.Join(h.github.contexts(), ","); got != "staging/deploy=pending,staging/deploy=failure" {
		t.Errorf("statuses %s, want pending then failure", got)
	}
}

func TestStagingDeployRefusesToRunWithoutTheStatusToken(t *testing.T) {
	h := newDeployHarness(t)
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra/staging/deploy.sh"))
	cmd.Env = append(os.Environ(), "DEPLOY_REMOTE="+h.remote, "DEPLOY_STATE_DIR="+h.state, "DEPLOY_STATUS_TOKEN_FILE="+filepath.Join(h.state, "missing"))

	out, err := cmd.CombinedOutput()

	if err == nil || !strings.Contains(string(out), "Commit statuses") {
		t.Fatalf("got %v: %s", err, out)
	}
}

func TestStagingPlatformLayersTheDeploysStateValuesOverEveryHelmfile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, helmfile := range []string{"core", "build", "cell"} {
		line := "infra/helmfile/" + helmfile + ".helmfile.yaml.gotmpl -e staging ${STAGING_STATE_VALUES:+--state-values-file \"$STAGING_STATE_VALUES\"} sync"
		if !strings.Contains(text, line) {
			t.Errorf("the %s sync does not take STAGING_STATE_VALUES", helmfile)
		}
	}
}
