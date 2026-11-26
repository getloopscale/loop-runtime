// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// scaling_types.go — public types for the app_scaling_config
// domain (per-app HPA / scale-to-zero configuration) that survive
// the T2.1 (Batch A #7) deletion of the legacy `scaling_store.go`.
// The 1 prod call site uses the constructor — preserved here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/pkg/naming"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// newInvalidArgument wraps a string message as LoopRuntimeError so handler
// validation produces actionable client feedback. Status 400 inferred
// from INVALID_ARGUMENT code by the HTTP error mapper.
func newInvalidArgument(msg string) *loopruntimeerrors.LoopRuntimeError {
	return loopruntimeerrors.NewLoopRuntimeError("INVALID_ARGUMENT", msg)
}

// ScalingConfig represents per-app scaling configuration persisted
// in app_scaling_config.
//
// Plan T2.1 of `pre-launch-gaps-plan.md`. Replaces the previous
// stub-202 path where `loopscale scale` returned acknowledged but did
// nothing persistent.
type ScalingConfig struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id"`
	ProjectID    string `json:"project_id"`
	Environment  string `json:"environment"`
	AppName      string `json:"app_name"`
	MinReplicas  int32  `json:"min_replicas"`
	MaxReplicas  int32  `json:"max_replicas"`
	TargetCPUPct *int32 `json:"target_cpu_pct,omitempty"`
	// TargetMemoryPct is the memory utilization target of the HPA. Nil scales on CPU alone.
	TargetMemoryPct *int32    `json:"target_memory_pct,omitempty"`
	ScaleToZero     bool      `json:"scale_to_zero"`
	UpdatedAt       time.Time `json:"updated_at"`
	UpdatedBy       string    `json:"updated_by"`
	ConfigHash      string    `json:"config_hash"`
}

// CanonicalHash computes a stable hash of the config body fields
// used by the API handler to short-circuit unchanged requests with
// `applied: false`.
//
// Format: "min={min}|max={max}|cpu={cpu_or_NIL}|s2z={true|false}", followed by
// "|mem={mem}" when a memory target is set. A config without one keeps the hash
// it had before the memory target existed.
func (c *ScalingConfig) CanonicalHash() string {
	cpu := "NIL"
	if c.TargetCPUPct != nil {
		cpu = fmt.Sprintf("%d", *c.TargetCPUPct)
	}
	body := fmt.Sprintf("min=%d|max=%d|cpu=%s|s2z=%t",
		c.MinReplicas, c.MaxReplicas, cpu, c.ScaleToZero)
	if c.TargetMemoryPct != nil {
		body += fmt.Sprintf("|mem=%d", *c.TargetMemoryPct)
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Validate checks invariants that DB CHECK constraints also enforce,
// but surfaces them as LoopRuntimeError for actionable client feedback.
func (c *ScalingConfig) Validate() error {
	if c.MinReplicas < 0 {
		return newInvalidArgument("min_replicas must be >= 0")
	}
	if c.MaxReplicas < c.MinReplicas {
		return newInvalidArgument(
			fmt.Sprintf("max_replicas (%d) must be >= min_replicas (%d)",
				c.MaxReplicas, c.MinReplicas))
	}
	if c.TargetCPUPct != nil {
		v := *c.TargetCPUPct
		if v < 1 || v > 100 {
			return newInvalidArgument(
				fmt.Sprintf("target_cpu_pct (%d) must be between 1 and 100", v))
		}
	}
	if c.TargetMemoryPct != nil {
		v := *c.TargetMemoryPct
		if v < 1 || v > 100 {
			return newInvalidArgument(
				fmt.Sprintf("target_memory_pct (%d) must be between 1 and 100", v))
		}
	}
	if c.ScaleToZero && c.MinReplicas != 0 {
		return newInvalidArgument(
			"scale_to_zero=true requires min_replicas=0")
	}
	// The environment is a segment of the workload namespace, so it must be
	// the RFC 1123 label pkg/naming (the namespace builder's own validator)
	// accepts. LoopRuntime supports arbitrary tenant-created environments; existence
	// is enforced by withEnvironmentLock at the route, this is syntax only.
	if err := naming.ValidateKubernetesName(c.Environment); err != nil {
		return newInvalidArgument(fmt.Sprintf("environment %q: %v", c.Environment, err))
	}
	return nil
}

// ErrScalingConfigNotFound is returned when GetByApp finds no row.
var ErrScalingConfigNotFound = errors.New("scaling config not found")

// ScalingStore is the persistence interface for app_scaling_config.
//
// 6 methods covering the lifecycle of scaling configuration.
type ScalingStore interface {
	// GetByApp returns the scaling config for a specific app, or
	// ErrScalingConfigNotFound if no row exists.
	GetByApp(ctx context.Context, tenantID, projectID, environment, appName string) (*ScalingConfig, error)

	// Upsert creates or updates the scaling config for an app,
	// returning the final state plus a flag indicating whether a
	// change was made. If the incoming config has an unchanged
	// ConfigHash relative to the stored row, Upsert returns
	// (existing, false, nil) without DB write.
	Upsert(ctx context.Context, tx db.DBTX, config *ScalingConfig) (final *ScalingConfig, changed bool, err error)

	// ListByEnv returns all scaling configs for a (tenant, project,
	// env).
	ListByEnv(ctx context.Context, tenantID, projectID, environment string) ([]ScalingConfig, error)

	// Delete removes a scaling config row. Idempotent — returns nil
	// if absent.
	Delete(ctx context.Context, tx db.DBTX, tenantID, projectID, environment, appName string) error

	// DeleteByProject removes ALL scaling config rows for the given
	// project. Used by ProjectDeleter to cascade so a future project
	// of the same name does not inherit replicas / HPA state from
	// its predecessor (which would surface as REPLICAS_EXCEED_PLAN
	// on a clean redeploy). Idempotent.
	DeleteByProject(ctx context.Context, tx db.DBTX, tenantID, projectID string) error

	// CountByTenant returns total scaling rows for a tenant. Used
	// for plan-cap enforcement at API layer (e.g., free-tier limit).
	CountByTenant(ctx context.Context, tenantID string) (int, error)
}

// PostgresScalingStore is the production implementation backed by
// sqlc + pgx.
//
// T2.1 Batch A #7 (plan 2026-05-29-sqlc-migration v1.3): db field
// type preserved (db.DBTX); methods (scaling_wrapper.go) dispatch
// through SqlcBridge to sqlc-generated queries.
type PostgresScalingStore struct {
	db db.DBTX
}

// NewPostgresScalingStore returns a Postgres-backed ScalingStore.
// Preserved verbatim from legacy.
func NewPostgresScalingStore(db db.DBTX) *PostgresScalingStore {
	return &PostgresScalingStore{db: db}
}

// Compile-time assertion: PostgresScalingStore satisfies ScalingStore
// 1:1.
var _ ScalingStore = (*PostgresScalingStore)(nil)
