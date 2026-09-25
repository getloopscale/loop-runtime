// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/adapters"
	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/services/platform"
)

// newEnvironmentEnsurer builds the one EnvironmentEnsurer that deploy
// (--env <new>), project creation (the production env, ADR D4) and promote
// (--to <new>, #158) share, so a first use of a (project, env) tuple creates
// the Environment CR, namespace and ArgoCD App on every path. Nil when there
// is no Tenant CR manager.
func newEnvironmentEnsurer(cfg config.Config, k8sDeps *K8sDeps, logger *slog.Logger) *platform.EnvironmentEnsurer {
	if k8sDeps == nil || k8sDeps.TenantCRMgr == nil {
		return nil
	}
	mgr := k8sDeps.TenantCRMgr
	lister, adder := platform.NewEnvManagerAdapter(mgr.AddEnvironment,
		func(ctx context.Context, tenantID, project, envName string) (any, error) {
			envs, err := mgr.ListEnvironments(ctx, tenantID)
			if err != nil {
				return nil, err
			}
			return environmentDetail(envs, project, envName), nil
		})
	ensurer := &platform.EnvironmentEnsurer{Lister: lister, Adder: adder, Logger: logger}
	// The readiness poll (Environment CR status.phase == Active) costs 2-30s
	// per deploy, so it is opt-in.
	if cfg.EnvReadinessChecker {
		if mgmtDynamic := buildMgmtDynamicClient(cfg, logger); mgmtDynamic != nil {
			ensurer.ReadinessChecker = adapters.NewEnvironmentCRReader(mgmtDynamic, "loopzero-system")
			logger.Info("env readiness checker wired: EnsureEnvironment polls Environment CR status.phase")
		}
	}
	return ensurer
}

// environmentDetail is the lister's "present" value for a (project, env)
// tuple, or an untyped nil when absent: a nil *EnvironmentEntry inside `any`
// would read as present, and the tuple would never be added.
func environmentDetail(envs []ports.EnvironmentEntry, project, envName string) any {
	if entry := ports.FindEnvironment(envs, project, envName); entry != nil {
		return entry
	}
	return nil
}
