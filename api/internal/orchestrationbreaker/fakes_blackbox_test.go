// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package orchestrationbreaker_test

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// fakeWorkflowSubmitter records submissions; err fails every call.
type fakeWorkflowSubmitter struct {
	submitted  []deploycontracts.WorkflowSpec
	workflowID string
	err        error
	onSubmit   func() // optional callback for ordering tests (REVIEW-BLOCKER-1)
}

func (f *fakeWorkflowSubmitter) Submit(ctx context.Context, spec deploycontracts.WorkflowSpec) (string, error) {
	if f.onSubmit != nil {
		f.onSubmit()
	}
	if f.err != nil {
		return "", f.err
	}
	f.submitted = append(f.submitted, spec)
	if f.workflowID == "" {
		return "wf-fake-001", nil
	}
	return f.workflowID, nil
}

func (f *fakeWorkflowSubmitter) Cancel(ctx context.Context, namespace, workflowID string) error {
	return nil
}

func (f *fakeWorkflowSubmitter) GetStatus(ctx context.Context, namespace, workflowID string) (*deploycontracts.WorkflowStatus, error) {
	return &deploycontracts.WorkflowStatus{Phase: "Succeeded"}, nil
}
