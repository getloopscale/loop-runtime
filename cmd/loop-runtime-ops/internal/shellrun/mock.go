// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package shellrun

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// MockRunner is a Runner impl for tests. Pre-program responses keyed
// by command (e.g., "pulumi up --stack drill") and capture invocation
// history for assertions.
//
// Usage:
//
//	mock := shellrun.NewMock()
//	mock.SetResponse("pulumi up --stack drill", &shellrun.Result{
//	    Stdout:   "Resources: 13 created",
//	    ExitCode: 0,
//	})
//	result, err := mock.Run(ctx, "pulumi", []string{"up", "--stack", "drill"})
//	require.NoError(t, err)
//	require.Equal(t, 1, mock.CallCount("pulumi up --stack drill"))
type MockRunner struct {
	mu        sync.Mutex
	responses map[string]*Result
	errors    map[string]error
	calls     map[string]int
	history   []MockInvocation
	// Default response when no SetResponse matches. nil = panic on
	// unexpected call (fail-fast in tests).
	defaultResponse *Result
}

// MockInvocation records a single Run() call for history assertions.
type MockInvocation struct {
	Name string
	Args []string
}

// NewMock constructs a MockRunner with no responses set. Unmatched
// calls will panic (fail-fast). Set a default via SetDefaultResponse
// for permissive mode.
func NewMock() *MockRunner {
	return &MockRunner{
		responses: make(map[string]*Result),
		errors:    make(map[string]error),
		calls:     make(map[string]int),
	}
}

// SetResponse pre-programs a response for a specific command line.
// Key format matches Result.Command ("name arg1 arg2 ...").
func (m *MockRunner) SetResponse(command string, result *Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[command] = result
}

// SetError pre-programs an error to return for a specific command.
// The Result is still returned (with its ExitCode); this models the
// (Result, error) pair pattern of DefaultRunner.
func (m *MockRunner) SetError(command string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[command] = err
}

// SetDefaultResponse sets the fallback Result for unmatched calls.
// Without this, unmatched calls panic.
func (m *MockRunner) SetDefaultResponse(result *Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultResponse = result
}

// Run implements Runner.Run. Looks up the response by command, records
// the call, returns the configured Result + error.
func (m *MockRunner) Run(ctx context.Context, name string, args []string, opts ...Option) (*Result, error) {
	command := name
	if len(args) > 0 {
		command = name + " " + strings.Join(args, " ")
	}

	m.mu.Lock()
	m.calls[command]++
	m.history = append(m.history, MockInvocation{Name: name, Args: append([]string{}, args...)})
	resp, hasResp := m.responses[command]
	err := m.errors[command]
	def := m.defaultResponse
	m.mu.Unlock()

	if !hasResp {
		if def != nil {
			respCopy := *def
			respCopy.Command = command
			return &respCopy, nil
		}
		panic(fmt.Sprintf("shellrun.MockRunner: unexpected call %q (no response set, no default)", command))
	}

	// Make a copy so tests can mutate Stdout/Stderr without affecting
	// pre-programmed response.
	respCopy := *resp
	respCopy.Command = command
	return &respCopy, err
}

// CallCount returns the number of times the given command was invoked.
// 0 = never called.
func (m *MockRunner) CallCount(command string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[command]
}

// History returns all invocations in chronological order.
func (m *MockRunner) History() []MockInvocation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MockInvocation{}, m.history...)
}

// Reset clears history + call counts (responses + errors preserved).
func (m *MockRunner) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = make(map[string]int)
	m.history = nil
}

// Static check that MockRunner satisfies Runner.
var _ Runner = (*MockRunner)(nil)
