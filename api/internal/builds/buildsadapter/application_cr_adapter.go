// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildsadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/builds"
	"github.com/getloopscale/loop-runtime/api/internal/k8s/workload"
	"github.com/getloopscale/loop-runtime/pkg/correlation"
)

// ApplicationCRAdapter bridges workload.ApplicationCRManager to build.ApplicationCREnsurer.
type ApplicationCRAdapter struct {
	Mgr *workload.ApplicationCRManager
}

func (a *ApplicationCRAdapter) EnsureLoopRuntimeApplication(ctx context.Context, input builds.ApplicationCRInput) error {
	// Audit gap M08 #10 / §6.C.1: propagate the originating correlation_id
	// from ctx into the CR annotation so the operator reconcile can attach
	// it back into audit_log rows + structured logs.
	return a.Mgr.EnsureLoopRuntimeApplication(ctx, workload.LoopRuntimeApplicationInput{
		TenantID:      input.TenantID,
		Project:       input.Project,
		Environment:   input.Environment,
		AppName:       input.AppName,
		ServiceName:   input.ServiceName,
		ServicePort:   input.ServicePort,
		CorrelationID: correlation.IDFromContext(ctx),
	})
}

// The port this adapter satisfies, asserted at compile time.
var _ builds.ApplicationCREnsurer = (*ApplicationCRAdapter)(nil)
