// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// infra/staging/restore-drill.sh runs the PostgreSQL restore drill against staging and
// says how it went in two places: the staging/restore-drill status of the commit staging
// runs, and the "restore-drill" alarm. The nightly rebuild runs it on the environment it
// has just built, so a backup that cannot be recovered is known the next morning.

type postedStatus struct{ SHA, State, Context, Description string }

type fakeStatuses struct {
	issues *fakeIssues
	mu     sync.Mutex
	posted []postedStatus
}

func (f *fakeStatuses) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prefix := "/repos/" + deployRepo + "/statuses/"
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, prefix) {
		f.issues.ServeHTTP(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var s postedStatus
	_ = json.Unmarshal(body, &s)
	s.SHA = strings.TrimPrefix(r.URL.Path, prefix)
	f.mu.Lock()
	f.posted = append(f.posted, s)
	f.mu.Unlock()
	_, _ = w.Write([]byte("{}"))
}

func stub(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stub")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func restoreDrill(t *testing.T, drill string) (*fakeStatuses, error) {
	t.Helper()
	github := &fakeStatuses{issues: &fakeIssues{}}
	server := httptest.NewServer(github)
	t.Cleanup(server.Close)
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra/staging/restore-drill.sh"), strings.Repeat("a", 40))
	cmd.Env = append(alarmEnv(t, server),
		"DEPLOY_STATE_DIR="+t.TempDir(),
		"RESTORE_DRILL_CMD="+stub(t, drill),
		"RESTORE_DRILL_ALARM_CMD=bash "+filepath.Join(repoRoot(), "infra/staging/alarm.sh"),
	)
	out, err := cmd.CombinedOutput()
	t.Logf("restore-drill.sh:\n%s", out)
	return github, err
}

func lastStatus(t *testing.T, github *fakeStatuses) postedStatus {
	t.Helper()
	if len(github.posted) == 0 {
		t.Fatal("no commit status was posted")
	}
	return github.posted[len(github.posted)-1]
}

func TestAGoodRestoreDrillReportsSuccessOnTheCommit(t *testing.T) {
	github, err := restoreDrill(t, "echo recovered\nexit 0")

	if err != nil {
		t.Fatalf("a good drill failed: %v", err)
	}
	got := lastStatus(t, github)
	if got.State != "success" || got.Context != "staging/restore-drill" || got.SHA != strings.Repeat("a", 40) {
		t.Errorf("status = %+v", got)
	}
	if len(github.issues.issues) != 0 {
		t.Errorf("a good drill raised %+v", github.issues.issues)
	}
}

func TestAFailedRestoreDrillReportsFailureAndRaisesItsAlarm(t *testing.T) {
	github, err := restoreDrill(t, "echo 'recovery timed out'\nexit 1")

	if err == nil {
		t.Fatal("a failed drill exited 0")
	}
	if got := lastStatus(t, github); got.State != "failure" || got.Context != "staging/restore-drill" {
		t.Errorf("status = %+v", got)
	}
	issues := github.issues.issues
	if len(issues) != 1 || issues[0].Title != "staging alarm: restore-drill" {
		t.Fatalf("got %+v", issues)
	}
	if !strings.Contains(issues[0].Comments[0], "recovery timed out") {
		t.Errorf("the alarm does not carry the drill's output: %q", issues[0].Comments[0])
	}
}

func rebuildWithDrill(t *testing.T, upExit, drillExit string) (ran string, err error) {
	t.Helper()
	h := newDeployHarness(t)
	server := httptest.NewServer(&fakeIssues{})
	t.Cleanup(server.Close)
	record := filepath.Join(t.TempDir(), "drilled")
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra/staging/rebuild.sh"))
	cmd.Env = append(alarmEnv(t, server),
		"DEPLOY_REMOTE="+h.remote,
		"DEPLOY_STATE_DIR="+h.state,
		"REBUILD_UP_CMD="+stub(t, "exit "+upExit),
		"REBUILD_ALARM_CMD=bash "+filepath.Join(repoRoot(), "infra/staging/alarm.sh"),
		"REBUILD_RESTORE_DRILL_CMD="+stub(t, `echo "$1" > `+record+"\nexit "+drillExit),
	)
	out, err := cmd.CombinedOutput()
	t.Logf("rebuild.sh:\n%s", out)
	raw, _ := os.ReadFile(record)
	return strings.TrimSpace(string(raw)), err
}

func TestAGoodRebuildRunsTheRestoreDrillOnTheRebuiltCommit(t *testing.T) {
	ran, err := rebuildWithDrill(t, "0", "0")

	if err != nil {
		t.Fatalf("a good rebuild with a good drill failed: %v", err)
	}
	if len(ran) != 40 {
		t.Errorf("the drill ran with %q, want the rebuilt commit", ran)
	}
}

func TestAFailedRestoreDrillFailsTheRebuildRun(t *testing.T) {
	_, err := rebuildWithDrill(t, "0", "1")

	if err == nil {
		t.Error("the rebuild run exited 0 although the restore drill failed")
	}
}

func TestAFailedRebuildRunsNoRestoreDrill(t *testing.T) {
	ran, _ := rebuildWithDrill(t, "4", "0")

	if ran != "" {
		t.Errorf("the drill ran on an environment that was not rebuilt (%q)", ran)
	}
}
