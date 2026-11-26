// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// infra/staging/alarm.sh and infra/staging/rebuild.sh run against a GitHub issues API
// served by the test.

type fakeIssue struct {
	Number   int
	Title    string
	State    string
	Labels   []string
	Comments []string
}

type fakeIssues struct {
	mu     sync.Mutex
	issues []*fakeIssue
	// expiries maps a token to the github-authentication-token-expiration header GitHub
	// answers it with; a token missing from the map is rejected with 401.
	expiries map[string]string
}

func (f *fakeIssues) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	base := "/repos/" + deployRepo + "/issues"
	body, _ := io.ReadAll(r.Body)
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	rest := strings.TrimPrefix(r.URL.Path, base)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/rate_limit":
		expiry, ok := f.expiries[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			http.Error(w, `{"message": "Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("github-authentication-token-expiration", expiry)
		_, _ = w.Write([]byte("{}"))
	case r.Method == http.MethodGet && r.URL.Path == base:
		out := []map[string]any{}
		for _, issue := range f.issues {
			if issue.State == r.URL.Query().Get("state") && contains(issue.Labels, r.URL.Query().Get("labels")) {
				out = append(out, map[string]any{"number": issue.Number, "title": issue.Title})
			}
		}
		_ = json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodPost && r.URL.Path == base:
		issue := &fakeIssue{Number: len(f.issues) + 1, Title: payload["title"].(string), State: "open"}
		for _, label := range payload["labels"].([]any) {
			issue.Labels = append(issue.Labels, label.(string))
		}
		issue.Comments = append(issue.Comments, payload["body"].(string))
		f.issues = append(f.issues, issue)
		_ = json.NewEncoder(w).Encode(map[string]any{"number": issue.Number})
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/comments"):
		issue := f.find(strings.TrimSuffix(strings.TrimPrefix(rest, "/"), "/comments"))
		issue.Comments = append(issue.Comments, payload["body"].(string))
		_, _ = w.Write([]byte("{}"))
	case r.Method == http.MethodPatch:
		f.find(strings.TrimPrefix(rest, "/")).State = payload["state"].(string)
		_, _ = w.Write([]byte("{}"))
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusTeapot)
	}
}

func (f *fakeIssues) find(number string) *fakeIssue {
	n, _ := strconv.Atoi(number)
	return f.issues[n-1]
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func alarmEnv(t *testing.T, server *httptest.Server) []string {
	t.Helper()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	return append(os.Environ(), "DEPLOY_GITHUB_API="+server.URL, "DEPLOY_STATUS_TOKEN_FILE="+token)
}

func runAlarm(t *testing.T, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(repoRoot(), "infra/staging/alarm.sh")}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("alarm.sh %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestAnAlarmRaisedTwiceIsOneIssueWithTwoReports(t *testing.T) {
	issues := &fakeIssues{}
	server := httptest.NewServer(issues)
	defer server.Close()
	env := alarmEnv(t, server)

	runAlarm(t, env, "raise", "rebuild", "first failure")
	runAlarm(t, env, "raise", "rebuild", "second failure")

	if len(issues.issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues.issues))
	}
	issue := issues.issues[0]
	if issue.Title != "staging alarm: rebuild" || !contains(issue.Labels, "staging-alarm") || len(issue.Comments) != 2 {
		t.Errorf("got %+v", issue)
	}
}

func TestClearingAnAlarmClosesItsIssue(t *testing.T) {
	issues := &fakeIssues{}
	server := httptest.NewServer(issues)
	defer server.Close()
	env := alarmEnv(t, server)
	runAlarm(t, env, "raise", "rebuild", "failure")

	runAlarm(t, env, "clear", "rebuild", "recovered")

	if issues.issues[0].State != "closed" {
		t.Errorf("the alarm's issue is %s after clearing", issues.issues[0].State)
	}
}

func TestClearingAnAlarmThatIsNotRaisedOpensNothing(t *testing.T) {
	issues := &fakeIssues{}
	server := httptest.NewServer(issues)
	defer server.Close()

	runAlarm(t, alarmEnv(t, server), "clear", "rebuild", "fine")

	if len(issues.issues) != 0 {
		t.Errorf("clearing opened %d issues", len(issues.issues))
	}
}

func TestTheDiskAlarmIsRaisedAboveTheLimitOnly(t *testing.T) {
	issues := &fakeIssues{}
	server := httptest.NewServer(issues)
	defer server.Close()
	env := alarmEnv(t, server)

	runAlarm(t, append(env, "ALARM_DISK_LIMIT_PERCENT=100"), "check-disk")
	if len(issues.issues) != 0 {
		t.Fatalf("a disk under its limit raised an alarm")
	}
	runAlarm(t, append(env, "ALARM_DISK_LIMIT_PERCENT=0"), "check-disk")

	if len(issues.issues) != 1 || issues.issues[0].Title != "staging alarm: disk" {
		t.Fatalf("a disk over its limit raised %+v", issues.issues)
	}
}

func rebuild(t *testing.T, exitCode string) (*fakeIssues, string, error) {
	t.Helper()
	h := newDeployHarness(t)
	issues := &fakeIssues{}
	server := httptest.NewServer(issues)
	t.Cleanup(server.Close)
	if err := os.MkdirAll(h.state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.state, "handled"), []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	up := filepath.Join(t.TempDir(), "up")
	if err := os.WriteFile(up, []byte("#!/usr/bin/env bash\necho rebuilding\nexit "+exitCode+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra/staging/rebuild.sh"))
	cmd.Env = append(alarmEnv(t, server),
		"DEPLOY_REMOTE="+h.remote,
		"DEPLOY_STATE_DIR="+h.state,
		"REBUILD_UP_CMD="+up,
		"REBUILD_ALARM_CMD=bash "+filepath.Join(repoRoot(), "infra/staging/alarm.sh"),
	)
	out, err := cmd.CombinedOutput()
	t.Logf("rebuild.sh:\n%s", out)
	return issues, h.state, err
}

func TestAFailedRebuildRaisesTheRebuildAlarm(t *testing.T) {
	issues, _, err := rebuild(t, "4")

	if err == nil {
		t.Fatal("a failed rebuild exited 0")
	}
	if len(issues.issues) != 1 || issues.issues[0].Title != "staging alarm: rebuild" {
		t.Fatalf("got %+v", issues.issues)
	}
	if !strings.Contains(issues.issues[0].Comments[0], "rebuilding") {
		t.Errorf("the alarm does not carry the rebuild's log: %q", issues.issues[0].Comments[0])
	}
}

func TestAGoodRebuildMakesTheDeployHandleTheHeadAgain(t *testing.T) {
	issues, state, err := rebuild(t, "0")

	if err != nil {
		t.Fatalf("a good rebuild failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(state, "handled")); !os.IsNotExist(statErr) {
		t.Errorf("the deploy's handled commit survived the rebuild: %v", statErr)
	}
	if len(issues.issues) != 0 {
		t.Errorf("a good rebuild opened %+v", issues.issues)
	}
}

// tokenFiles writes the two tokens check-tokens watches, the status token as a plain file
// and the GHCR pull token inside its {"username", "password"} file, and points the alarm
// at them.
func tokenFiles(t *testing.T, server *httptest.Server, statusToken, pullToken string) []string {
	t.Helper()
	dir := t.TempDir()
	status := filepath.Join(dir, "github-status-token")
	pull := filepath.Join(dir, "ghcr-pull.json")
	if err := os.WriteFile(status, []byte(statusToken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pull, []byte(`{"username": "someone", "password": "`+pullToken+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return append(os.Environ(), "DEPLOY_GITHUB_API="+server.URL, "DEPLOY_STATUS_TOKEN_FILE="+status,
		"ALARM_TOKEN_FILES="+status+" "+pull)
}

func expiryIn(days int) string {
	return time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format("2006-01-02 15:04:05 UTC")
}

func TestATokenCloseToItsExpiryRaisesTheTokenAlarm(t *testing.T) {
	issues := &fakeIssues{expiries: map[string]string{"status-secret": expiryIn(60), "pull-secret": expiryIn(5)}}
	server := httptest.NewServer(issues)
	defer server.Close()

	runAlarm(t, tokenFiles(t, server, "status-secret", "pull-secret"), "check-tokens")

	if len(issues.issues) != 1 || issues.issues[0].Title != "staging alarm: token-expiry" {
		t.Fatalf("a token 5 days from expiry raised %+v", issues.issues)
	}
	report := issues.issues[0].Comments[0]
	if !strings.Contains(report, "ghcr-pull.json") || strings.Contains(report, "github-status-token") {
		t.Errorf("the alarm does not name only the expiring token's file: %q", report)
	}
	if strings.Contains(report, "pull-secret") {
		t.Errorf("the alarm leaks the token value: %q", report)
	}
}

func TestATokenGitHubRejectsRaisesTheTokenAlarm(t *testing.T) {
	issues := &fakeIssues{expiries: map[string]string{"status-secret": expiryIn(60)}}
	server := httptest.NewServer(issues)
	defer server.Close()

	runAlarm(t, tokenFiles(t, server, "status-secret", "revoked-secret"), "check-tokens")

	if len(issues.issues) != 1 || !strings.Contains(issues.issues[0].Comments[0], "rejected") {
		t.Fatalf("a rejected token raised %+v", issues.issues)
	}
}

func TestTokensFarFromExpiryClearTheTokenAlarm(t *testing.T) {
	issues := &fakeIssues{expiries: map[string]string{"status-secret": expiryIn(60), "pull-secret": expiryIn(5)}}
	server := httptest.NewServer(issues)
	defer server.Close()
	runAlarm(t, tokenFiles(t, server, "status-secret", "pull-secret"), "check-tokens")

	issues.expiries["pull-secret"] = expiryIn(90)
	runAlarm(t, tokenFiles(t, server, "status-secret", "pull-secret"), "check-tokens")

	if len(issues.issues) != 1 || issues.issues[0].State != "closed" {
		t.Fatalf("renewed tokens left %+v", issues.issues)
	}
}

func TestTheTokenCheckIsInstalledAndRunsEveryDay(t *testing.T) {
	read := func(path string) string {
		data, err := os.ReadFile(filepath.Join(repoRoot(), path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if service := read("infra/staging/systemd/loop-runtime-staging-tokens.service"); !strings.Contains(service, "ExecStart=/usr/local/lib/loop-runtime-staging/alarm.sh check-tokens") {
		t.Errorf("loop-runtime-staging-tokens.service does not run alarm.sh check-tokens:\n%s", service)
	}
	if timer := read("infra/staging/systemd/loop-runtime-staging-tokens.timer"); !strings.Contains(timer, "OnCalendar=daily") || !strings.Contains(timer, "Persistent=true") {
		t.Errorf("loop-runtime-staging-tokens.timer does not run once a day, catching up after downtime:\n%s", timer)
	}
	task := read("taskfiles/staging.yml")
	if !strings.Contains(task, "infra/staging/systemd/loop-runtime-staging-tokens.timer") || !strings.Contains(task, "loop-runtime-staging-disk.timer loop-runtime-staging-tokens.timer") {
		t.Errorf("staging:nightly does not install and enable loop-runtime-staging-tokens.timer")
	}
}
