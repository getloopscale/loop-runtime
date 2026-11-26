// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package capacity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCapacityGiteaPushLatency validates the cell-budget.md assumption:
//
//	"Gitea disk I/O — push latency under 100 repos has never been measured."
//
// The test creates repos in tiers (10, 50, 100), performs concurrent git
// pushes at each tier, and measures wall-clock push latency. This detects
// whether Gitea disk I/O degrades superlinearly with repo count.
//
// Run:
//
//	LOOP_RUNTIME_CAPACITY=1 \
//	LOOP_RUNTIME_GITEA_URL=http://localhost:13000 \
//	LOOP_RUNTIME_GITEA_USER=gitea_admin \
//	LOOP_RUNTIME_GITEA_PASS=... \
//	go test ./infra/tests/performance/capacity/ -run TestCapacityGiteaPush -v -timeout 30m \
//	  -args -workers=10
func TestCapacityGiteaPushLatency(t *testing.T) {
	SkipUnlessCapacityEnabled(t)

	giteaURL := os.Getenv("LOOP_RUNTIME_GITEA_URL")
	user := os.Getenv("LOOP_RUNTIME_GITEA_USER")
	pass := os.Getenv("LOOP_RUNTIME_GITEA_PASS")
	if giteaURL == "" || user == "" || pass == "" {
		t.Fatal("LOOP_RUNTIME_GITEA_URL, LOOP_RUNTIME_GITEA_USER, LOOP_RUNTIME_GITEA_PASS required")
	}

	repoCountsStr := os.Getenv("LOOP_RUNTIME_GITEA_REPO_COUNTS")
	if repoCountsStr == "" {
		repoCountsStr = "10,50,100"
	}
	repoCounts := parseIntList(repoCountsStr)
	if len(repoCounts) == 0 {
		t.Fatal("LOOP_RUNTIME_GITEA_REPO_COUNTS must be a comma-separated list of integers")
	}

	// Gap 5: Repos with real history (not just auto_init's single commit).
	// Real tenant repos have 50-1000+ commits. Without history, push latency
	// misses git's pack/delta computation overhead on the server side.
	seedCommits := 50
	if s := os.Getenv("LOOP_RUNTIME_GITEA_SEED_COMMITS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			seedCommits = n
		}
	}

	cfg := NewConfig("gitea-push")
	t.Logf("config: workers=%d tiers=%v seed_commits=%d", cfg.Workers, repoCounts, seedCommits)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Second}

	// Cleanup function: delete all repos matching the bench prefix.
	defer func() {
		t.Log("cleaning up cap-bench repos...")
		cleanupGiteaRepos(t, client, giteaURL, user, pass)
	}()

	// Track tier results for cross-tier degradation analysis.
	type tierResult struct {
		RepoCount int
		Result    Result
	}
	var tierResults []tierResult

	// Cumulative repo count across tiers.
	totalReposCreated := 0

	for _, targetCount := range repoCounts {
		t.Logf("--- tier: %d repos ---", targetCount)

		// Create repos up to this tier's count (incremental).
		reposNeeded := targetCount - totalReposCreated
		if reposNeeded > 0 {
			created := createGiteaRepos(t, ctx, client, giteaURL, user, pass, totalReposCreated, reposNeeded)
			totalReposCreated += created
			t.Logf("  created %d repos (total: %d)", created, totalReposCreated)

			// Gap 5: Seed repos with commit history to simulate real repos.
			if seedCommits > 0 {
				t.Logf("  seeding %d repos with %d commits each...", created, seedCommits)
				for i := totalReposCreated - created; i < totalReposCreated; i++ {
					repoName := fmt.Sprintf("cap-bench-repo-%04d", i)
					if err := seedRepoWithHistory(ctx, giteaURL, user, pass, repoName, seedCommits); err != nil {
						t.Logf("  warning: seed %s: %v", repoName, err)
					}
				}
			}

			// Let Gitea settle after repo creation + seeding.
			// intentional-sleep: benchmark warmup / measurement window
			time.Sleep(5 * time.Second)
		}

		// Measure push latency: push to a subset of repos concurrently.
		pushCount := cfg.Workers * 3
		if pushCount > totalReposCreated {
			pushCount = totalReposCreated
		}

		lat := NewLatencies(pushCount + 100)
		var total, errors int64
		var wg sync.WaitGroup
		sem := make(chan struct{}, cfg.Workers)

		for i := 0; i < pushCount; i++ {
			repoIdx := i % totalReposCreated
			repoName := fmt.Sprintf("cap-bench-repo-%04d", repoIdx)

			wg.Add(1)
			sem <- struct{}{}
			go func(repo string, idx int) {
				defer wg.Done()
				defer func() { <-sem }()

				d, err := gitPushToRepo(ctx, giteaURL, user, pass, repo, idx)
				lat.Record(d)
				atomic.AddInt64(&total, 1)
				if err != nil {
					atomic.AddInt64(&errors, 1)
				}
			}(repoName, i)
		}
		wg.Wait()

		result := Result{
			Subsystem:    fmt.Sprintf("gitea-push-%d-repos", targetCount),
			StartedAt:    time.Now(),
			Duration:     0, // individual push latency, not test duration
			RequestedRPS: 0,
			Workers:      cfg.Workers,
			TotalReqs:    total,
			Errors:       errors,
			ErrorRate:    float64(errors) / float64(max(total, 1)),
			P50:          lat.Percentile(0.50),
			P95:          lat.Percentile(0.95),
			P99:          lat.Percentile(0.99),
			Notes:        fmt.Sprintf("repos=%d, pushes=%d", totalReposCreated, pushCount),
		}

		t.Logf("  pushes:     %d (errors: %d, rate: %.2f%%)", result.TotalReqs, result.Errors, result.ErrorRate*100)
		t.Logf("  p50/95/99:  %v / %v / %v", result.P50, result.P95, result.P99)

		// Write per-tier result.
		tierFile := strings.Replace(cfg.OutFile, "gitea-push", fmt.Sprintf("gitea-push-%d", targetCount), 1)
		if err := WriteResult(result, tierFile); err != nil {
			t.Errorf("write result: %v", err)
		} else {
			t.Logf("  result:     %s", tierFile)
		}

		tierResults = append(tierResults, tierResult{RepoCount: targetCount, Result: result})

		// Per-tier assertions (aligned with cell-budget dashboard yellow thresholds).
		switch {
		case targetCount <= 10:
			if result.P95 > 1*time.Second {
				t.Errorf("p95 > 1s at %d repos — Gitea I/O issue at small scale", targetCount)
			}
		case targetCount <= 50:
			if result.P95 > 1500*time.Millisecond {
				t.Errorf("p95 > 1.5s at %d repos — Gitea I/O degrading", targetCount)
			}
		default:
			if result.P95 > 2*time.Second {
				t.Errorf("p95 > 2s at %d repos — exceeds cell-budget yellow threshold", targetCount)
			}
		}
	}

	// Cross-tier degradation check.
	if len(tierResults) >= 2 {
		first := tierResults[0].Result.P95
		last := tierResults[len(tierResults)-1].Result.P95
		if first > 0 && last > 3*first {
			t.Errorf("push latency degrades superlinearly: p95 at %d repos (%v) > 3x p95 at %d repos (%v)",
				tierResults[len(tierResults)-1].RepoCount, last,
				tierResults[0].RepoCount, first)
		}
	}
}

// createGiteaRepos creates repos via Gitea API, starting from startIdx.
func createGiteaRepos(t *testing.T, ctx context.Context, client *http.Client, giteaURL, user, pass string, startIdx, count int) int {
	t.Helper()
	created := 0
	for i := 0; i < count; i++ {
		repoName := fmt.Sprintf("cap-bench-repo-%04d", startIdx+i)
		body := fmt.Sprintf(`{"name":%q,"auto_init":true,"default_branch":"main"}`, repoName)
		req, err := http.NewRequestWithContext(ctx, "POST", giteaURL+"/api/v1/user/repos", strings.NewReader(body))
		if err != nil {
			t.Logf("  warning: request error for %s: %v", repoName, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(user, pass)

		resp, err := client.Do(req)
		if err != nil {
			t.Logf("  warning: create %s: %v", repoName, err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 201 || resp.StatusCode == 409 {
			created++
		} else {
			t.Logf("  warning: create %s: HTTP %d", repoName, resp.StatusCode)
		}
	}
	return created
}

// gitPushToRepo clones a repo, creates a commit, pushes, and returns the
// wall-clock duration of the push operation. The push latency is what
// matters for measuring Gitea disk I/O under load.
func gitPushToRepo(ctx context.Context, giteaURL, user, pass, repoName string, pushIdx int) (time.Duration, error) {
	tmpDir, err := os.MkdirTemp("", "cap-push-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir)

	cloneURL := fmt.Sprintf("%s/%s/%s.git", giteaURL, user, repoName)
	// Embed credentials in URL for git operations.
	cloneURL = strings.Replace(cloneURL, "://", fmt.Sprintf("://%s:%s@", user, pass), 1)

	// Clone.
	cloneCmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", cloneURL, tmpDir)
	cloneCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("clone %s: %w: %s", repoName, err, string(out))
	}

	// Create a commit.
	content := fmt.Sprintf("bench push %d at %s\n", pushIdx, time.Now().Format(time.RFC3339Nano))
	filePath := filepath.Join(tmpDir, fmt.Sprintf("bench-%d.txt", pushIdx))
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return 0, err
	}

	for _, args := range [][]string{
		{"git", "config", "user.email", "bench@loop-runtime.test"},
		{"git", "config", "user.name", "Capacity Bench"},
		{"git", "add", "-A"},
		{"git", "commit", "-q", "-m", fmt.Sprintf("bench push %d", pushIdx)},
	} {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return 0, fmt.Errorf("git %s: %w: %s", args[1], err, string(out))
		}
	}

	// Push — THIS is what we measure.
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = tmpDir
	pushCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	start := time.Now()
	out, err := pushCmd.CombinedOutput()
	pushDuration := time.Since(start)

	if err != nil {
		return pushDuration, fmt.Errorf("push %s: %w: %s", repoName, err, string(out))
	}
	return pushDuration, nil
}

// cleanupGiteaRepos deletes all repos with the "cap-bench-repo-" prefix.
func cleanupGiteaRepos(t *testing.T, client *http.Client, giteaURL, user, pass string) {
	t.Helper()

	// List user repos and delete matching ones.
	page := 1
	deleted := 0
	for {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/user/repos?page=%d&limit=50", giteaURL, page), nil)
		req.SetBasicAuth(user, pass)

		resp, err := client.Do(req)
		if err != nil {
			t.Logf("cleanup list error: %v", err)
			return
		}

		var repos []struct {
			Name string `json:"name"`
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err := json.Unmarshal(body, &repos); err != nil || len(repos) == 0 {
			break
		}

		for _, repo := range repos {
			if !strings.HasPrefix(repo.Name, "cap-bench-repo-") {
				continue
			}
			delReq, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/repos/%s/%s", giteaURL, user, repo.Name), nil)
			delReq.SetBasicAuth(user, pass)
			delResp, err := client.Do(delReq)
			if err == nil {
				delResp.Body.Close()
				deleted++
			}
		}
		page++
	}
	t.Logf("deleted %d cap-bench repos", deleted)
}

// parseIntList parses a comma-separated list of integers.
func parseIntList(s string) []int {
	parts := strings.Split(s, ",")
	var result []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n, err := strconv.Atoi(p)
		if err == nil && n > 0 {
			result = append(result, n)
		}
	}
	return result
}

// seedRepoWithHistory clones a repo, pushes N commits with varying content,
// then cleans up the local clone. This populates the repo with realistic
// git history so that subsequent push latency measurements include git's
// pack/delta computation overhead (Gap 5).
func seedRepoWithHistory(ctx context.Context, giteaURL, user, pass, repoName string, commits int) error {
	tmpDir, err := os.MkdirTemp("", "cap-seed-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	cloneURL := fmt.Sprintf("%s/%s/%s.git", giteaURL, user, repoName)
	cloneURL = strings.Replace(cloneURL, "://", fmt.Sprintf("://%s:%s@", user, pass), 1)

	cloneCmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", cloneURL, tmpDir)
	cloneCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone: %w: %s", err, string(out))
	}

	// Configure git identity.
	for _, args := range [][]string{
		{"git", "config", "user.email", "seed@loop-runtime.test"},
		{"git", "config", "user.name", "Seed"},
	} {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = tmpDir
		cmd.CombinedOutput()
	}

	// Create N commits with varying file sizes to build realistic pack data.
	for i := 0; i < commits; i++ {
		// Vary content size (100B to 2KB) to create interesting deltas.
		content := fmt.Sprintf("commit %d at %s\n%s\n", i, time.Now().Format(time.RFC3339Nano),
			strings.Repeat(fmt.Sprintf("line-%04d-data-padding\n", i), 10+(i%100)))

		fileName := fmt.Sprintf("src/file-%03d.txt", i%20) // Rotate across 20 files
		filePath := filepath.Join(tmpDir, fileName)
		os.MkdirAll(filepath.Dir(filePath), 0755)
		os.WriteFile(filePath, []byte(content), 0644)

		for _, args := range [][]string{
			{"git", "add", "-A"},
			{"git", "commit", "-q", "-m", fmt.Sprintf("seed commit %d", i)},
		} {
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			cmd.Dir = tmpDir
			if _, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("commit %d: %w", i, err)
			}
		}
	}

	// Push all seed commits at once.
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = tmpDir
	pushCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("push seed: %w: %s", err, string(out))
	}

	return nil
}
