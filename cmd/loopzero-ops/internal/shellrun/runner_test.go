// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package shellrun

import (
	"context"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunner_RunSuccess: happy-path exit 0 with stdout capture.
func TestRunner_RunSuccess(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	result, err := r.Run(ctx, "echo", []string{"hello", "world"})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello world\n", result.Stdout)
	assert.Empty(t, result.Stderr)
	assert.False(t, result.Truncated)
	assert.True(t, result.IsSuccess())
	assert.Equal(t, "echo hello world", result.Command)
}

// TestRunner_RunFailure_NonZeroExit: command exits non-zero, error returned + ExitCode populated.
func TestRunner_RunFailure_NonZeroExit(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	result, err := r.Run(ctx, "sh", []string{"-c", "exit 2"})

	require.Error(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, result.ExitCode)
	assert.False(t, result.IsSuccess())
}

// TestRunner_RunCanceled_KillsGrandchildrenViaPGID: EC-4 fix — Setpgid + negative PID
// signaling kills the entire process group, not just the direct child.
func TestRunner_RunCanceled_KillsGrandchildrenViaPGID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Process groups not implemented on Windows")
	}
	r := NewDefault()
	r.GracePeriod = 500 * time.Millisecond

	// Spawn shell that forks a grandchild that traps SIGTERM (refuses
	// to die without explicit group kill).
	// The child is the shell; the grandchild is the inner sleep.
	// If we only kill the direct child, the grandchild orphan-survives
	// the parent's exit and continues running until SIGKILL group.
	script := `sh -c 'trap "" TERM; sleep 30 & wait' &
sleep 60`

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = r.Run(ctx, "sh", []string{"-c", script})
	elapsed := time.Since(start)

	// Must complete in < (timeout 200ms + grace 500ms + slack 500ms = 1.2s).
	// Without PGID kill, would hang for sleep 60 = 60s.
	assert.Less(t, elapsed, 2*time.Second, "PGID kill should propagate to grandchildren")
}

// TestRunner_WithCwd_ChangesWorkingDir: WithCwd sets process working dir.
func TestRunner_WithCwd_ChangesWorkingDir(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	result, err := r.Run(ctx, "pwd", nil, WithCwd("/tmp"))

	require.NoError(t, err)
	// macOS may symlink-resolve /tmp → /private/tmp; check suffix.
	assert.True(t, strings.HasSuffix(strings.TrimSpace(result.Stdout), "/tmp"),
		"cwd should end in /tmp, got %q", result.Stdout)
}

// TestRunner_LargeStdout_Truncates: output > MaxBufferSize sets Truncated=true.
func TestRunner_LargeStdout_Truncates(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	// Generate ~11 MiB of output (exceeds 10 MiB cap).
	bytesToGen := MaxBufferSize + 1024*1024
	result, err := r.Run(ctx, "sh", []string{"-c", "yes a | head -c " + itoa(bytesToGen)})

	require.NoError(t, err)
	assert.True(t, result.Truncated, "should mark Truncated when output > MaxBufferSize")
	assert.LessOrEqual(t, len(result.Stdout), MaxBufferSize)
}

// TestRunner_NonexistentBinary_StartError: command not found returns error.
func TestRunner_NonexistentBinary_StartError(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	result, err := r.Run(ctx, "/nonexistent/binary/path/xyz", nil)

	require.Error(t, err)
	require.NotNil(t, result)
	assert.NotEqual(t, 0, result.ExitCode, "should not report success on start failure")
}

// TestRunner_StderrCapture: stderr captured separately from stdout.
func TestRunner_StderrCapture(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	result, err := r.Run(ctx, "sh", []string{"-c", "echo to-stdout; echo to-stderr >&2"})

	require.NoError(t, err)
	assert.Equal(t, "to-stdout\n", result.Stdout)
	assert.Equal(t, "to-stderr\n", result.Stderr)
}

// TestRunner_DurationCaptured: Result.Duration reflects elapsed time.
func TestRunner_DurationCaptured(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	result, err := r.Run(ctx, "sleep", []string{"0.1"})

	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.Duration, 100*time.Millisecond)
	assert.Less(t, result.Duration, 2*time.Second)
}

// TestRunner_ConcurrentRuns_ParallelSafe: DefaultRunner is safe for parallel use.
func TestRunner_ConcurrentRuns_ParallelSafe(t *testing.T) {
	r := NewDefault()
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 10)
	results := make([]*Result, 10)

	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = r.Run(ctx, "echo", []string{"concurrent"})
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		require.NoError(t, e, "goroutine %d failed", i)
		assert.Equal(t, "concurrent\n", results[i].Stdout)
	}
}

// TestRunner_IsSignalKilled: ExitCode < 0 means signal-killed.
func TestRunner_IsSignalKilled(t *testing.T) {
	r := &Result{ExitCode: -9}
	assert.True(t, r.IsSignalKilled())

	r = &Result{ExitCode: 0}
	assert.False(t, r.IsSignalKilled())

	r = &Result{ExitCode: 1}
	assert.False(t, r.IsSignalKilled())
}

// TestRunner_CappedBufferTruncates_Unit: cappedBuffer drops bytes after cap.
func TestRunner_CappedBufferTruncates_Unit(t *testing.T) {
	cb := &cappedBuffer{cap: 10}

	n, err := cb.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.False(t, cb.truncated)
	assert.Equal(t, "hello", cb.String())

	n, err = cb.Write([]byte("world!!!extra"))
	require.NoError(t, err)
	assert.Equal(t, 13, n, "should report all bytes accepted even when truncating")
	assert.True(t, cb.truncated)
	assert.Equal(t, "helloworld", cb.String(), "should keep only first cap bytes")
}

// TestMockRunner_BasicResponseLookup: MockRunner returns programmed response.
func TestMockRunner_BasicResponseLookup(t *testing.T) {
	m := NewMock()
	m.SetResponse("pulumi up --stack drill", &Result{
		Stdout:   "Resources: 13 created",
		ExitCode: 0,
	})

	result, err := m.Run(context.Background(), "pulumi", []string{"up", "--stack", "drill"})

	require.NoError(t, err)
	assert.Equal(t, "Resources: 13 created", result.Stdout)
	assert.Equal(t, 1, m.CallCount("pulumi up --stack drill"))
}

// TestMockRunner_UnmatchedCallPanics: unmatched call without default panics.
func TestMockRunner_UnmatchedCallPanics(t *testing.T) {
	m := NewMock()
	assert.Panics(t, func() {
		_, _ = m.Run(context.Background(), "unknown", nil)
	})
}

// TestMockRunner_DefaultResponseUsed: unmatched call uses default when set.
func TestMockRunner_DefaultResponseUsed(t *testing.T) {
	m := NewMock()
	m.SetDefaultResponse(&Result{Stdout: "default", ExitCode: 0})

	result, err := m.Run(context.Background(), "any", []string{"command"})

	require.NoError(t, err)
	assert.Equal(t, "default", result.Stdout)
	assert.Equal(t, "any command", result.Command)
}

// TestMockRunner_HistoryCapturesInvocations: History returns chronological calls.
func TestMockRunner_HistoryCapturesInvocations(t *testing.T) {
	m := NewMock()
	m.SetDefaultResponse(&Result{ExitCode: 0})

	_, _ = m.Run(context.Background(), "first", []string{"a"})
	_, _ = m.Run(context.Background(), "second", []string{"b", "c"})

	hist := m.History()
	require.Len(t, hist, 2)
	assert.Equal(t, "first", hist[0].Name)
	assert.Equal(t, []string{"a"}, hist[0].Args)
	assert.Equal(t, "second", hist[1].Name)
	assert.Equal(t, []string{"b", "c"}, hist[1].Args)
}

// TestMockRunner_ErrorReturned: SetError makes Run return the configured error.
func TestMockRunner_ErrorReturned(t *testing.T) {
	m := NewMock()
	m.SetResponse("failing cmd", &Result{ExitCode: 2})
	m.SetError("failing cmd", errSentinel)

	result, err := m.Run(context.Background(), "failing", []string{"cmd"})

	assert.ErrorIs(t, err, errSentinel)
	assert.Equal(t, 2, result.ExitCode)
}

// TestMockRunner_Reset_ClearsHistoryButPreservesResponses: Reset() drops
// invocation history and call counts but retains pre-programmed responses.
func TestMockRunner_Reset_ClearsHistoryButPreservesResponses(t *testing.T) {
	m := NewMock()
	m.SetResponse("cmd a", &Result{ExitCode: 0})
	_, _ = m.Run(context.Background(), "cmd", []string{"a"})
	require.Equal(t, 1, m.CallCount("cmd a"))
	require.Len(t, m.History(), 1)

	m.Reset()
	assert.Equal(t, 0, m.CallCount("cmd a"))
	assert.Empty(t, m.History())

	// Responses still work.
	_, err := m.Run(context.Background(), "cmd", []string{"a"})
	require.NoError(t, err)
}

// TestRunner_KillProcessGroup_ProcessNotStarted: defensive path when Process is nil.
func TestRunner_KillProcessGroup_ProcessNotStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Process group semantics differ on Windows")
	}
	// Build a fresh Cmd without Start() — Process is nil.
	r := NewDefault()
	_ = r // not used; we're testing the helper directly.
	cmd := mustNewUnstartedCmd(t)
	err := killProcessGroup(cmd, false)
	assert.Error(t, err, "should fail when Process is nil")
}

var errSentinel = sentinelError("test")

type sentinelError string

func (s sentinelError) Error() string { return string(s) }

// helper: integer to string without strconv (keep test file self-contained).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 11)
	for n > 0 {
		buf = append(buf, '0'+byte(n%10))
		n /= 10
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// mustNewUnstartedCmd builds an exec.Cmd without calling Start() so that
// Process is nil — used to test killProcessGroup defensive paths.
func mustNewUnstartedCmd(t *testing.T) *exec.Cmd {
	t.Helper()
	return exec.Command("/bin/true")
}

// Avoid unused-import warnings during stage builds.
var _ = io.Discard
