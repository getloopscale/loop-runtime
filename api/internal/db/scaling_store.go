// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// newInvalidArgument is the package-local helper for validation errors.
// Wraps NewLoopZeroError with VALIDATION_ERROR code + HTTP 400.
func newInvalidArgument(msg string) *loopzeroerrors.LoopZeroError {
	return loopzeroerrors.NewLoopZeroError("VALIDATION_ERROR", msg, http.StatusBadRequest)
}

// ScalingConfig represents an app's scaling configuration in the database.
//
// Plan T2.1 of `pre-launch-gaps-plan.md`. Replaces the previous stub-202 path
// where `loopzero scale` returned acknowledged but did nothing persistent.
type ScalingConfig struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	ProjectID     string    `json:"project_id"`
	Environment   string    `json:"environment"`
	AppName       string    `json:"app_name"`
	MinReplicas   int32     `json:"min_replicas"`
	MaxReplicas   int32     `json:"max_replicas"`
	TargetCPUPct  *int32    `json:"target_cpu_pct,omitempty"`
	ScaleToZero   bool      `json:"scale_to_zero"`
	UpdatedAt     time.Time `json:"updated_at"`
	UpdatedBy     string    `json:"updated_by"`
	ConfigHash    string    `json:"config_hash"`
}

// CanonicalHash computes a stable hash of the config body fields used by the
// API handler to short-circuit unchanged requests with `applied: false`.
//
// The hash MUST be stable across repeated computations of the same config.
// Format: "min={min}|max={max}|cpu={cpu_or_NIL}|s2z={true|false}".
func (c *ScalingConfig) CanonicalHash() string {
	cpu := "NIL"
	if c.TargetCPUPct != nil {
		cpu = fmt.Sprintf("%d", *c.TargetCPUPct)
	}
	body := fmt.Sprintf("min=%d|max=%d|cpu=%s|s2z=%t",
		c.MinReplicas, c.MaxReplicas, cpu, c.ScaleToZero)
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Validate checks invariants that DB CHECK constraints also enforce, but
// surfaces them as LoopZeroError for actionable client feedback.
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
	if c.ScaleToZero && c.MinReplicas != 0 {
		return newInvalidArgument(
			"scale_to_zero=true requires min_replicas=0")
	}
	if c.Environment != "production" && c.Environment != "staging" && c.Environment != "preview" {
		return newInvalidArgument(
			fmt.Sprintf("environment must be one of [production, staging, preview], got %q", c.Environment))
	}
	return nil
}

// ErrScalingConfigNotFound is returned when GetByApp finds no row.
var ErrScalingConfigNotFound = errors.New("scaling config not found")

// ScalingStore is the persistence interface for app_scaling_config.
//
// Plan T2.1: 5 methods covering the lifecycle of scaling configuration.
type ScalingStore interface {
	// GetByApp returns the scaling config for a specific app, or
	// ErrScalingConfigNotFound if no row exists.
	GetByApp(ctx context.Context, tenantID, projectID, environment, appName string) (*ScalingConfig, error)

	// Upsert creates or updates the scaling config for an app, returning the
	// final state plus a flag indicating whether a change was made.
	// If the incoming config has an unchanged ConfigHash relative to the
	// stored row, Upsert returns (existing, false, nil) without DB write.
	Upsert(ctx context.Context, tx DBTX, config *ScalingConfig) (final *ScalingConfig, changed bool, err error)

	// ListByEnv returns all scaling configs for a (tenant, project, env).
	ListByEnv(ctx context.Context, tenantID, projectID, environment string) ([]ScalingConfig, error)

	// Delete removes a scaling config row. Idempotent — returns nil if absent.
	Delete(ctx context.Context, tx DBTX, tenantID, projectID, environment, appName string) error

	// DeleteByProject removes ALL scaling config rows for the given project.
	// Used by ProjectDeleter to cascade so a future project of the same name
	// does not inherit replicas / HPA state from its predecessor (which would
	// surface as REPLICAS_EXCEED_PLAN on a clean redeploy). Idempotent.
	DeleteByProject(ctx context.Context, tx DBTX, tenantID, projectID string) error

	// CountByTenant returns total scaling rows for a tenant. Used for plan-cap
	// enforcement at API layer (e.g., free-tier limit).
	CountByTenant(ctx context.Context, tenantID string) (int, error)
}

// PostgresScalingStore is the production implementation backed by pgx.
type PostgresScalingStore struct {
	db DBTX
}

// NewPostgresScalingStore returns a Postgres-backed ScalingStore.
func NewPostgresScalingStore(db DBTX) *PostgresScalingStore {
	return &PostgresScalingStore{db: db}
}

// GetByApp implements ScalingStore.
func (s *PostgresScalingStore) GetByApp(ctx context.Context, tenantID, projectID, environment, appName string) (*ScalingConfig, error) {
	const q = `
SELECT id, tenant_id, project_id, environment, app_name,
       min_replicas, max_replicas, target_cpu_pct, scale_to_zero,
       updated_at, updated_by, config_hash
  FROM app_scaling_config
 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND app_name = $4`

	var c ScalingConfig
	err := s.db.QueryRow(ctx, q, tenantID, projectID, environment, appName).Scan(
		&c.ID, &c.TenantID, &c.ProjectID, &c.Environment, &c.AppName,
		&c.MinReplicas, &c.MaxReplicas, &c.TargetCPUPct, &c.ScaleToZero,
		&c.UpdatedAt, &c.UpdatedBy, &c.ConfigHash,
	)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return nil, ErrScalingConfigNotFound
		}
		return nil, fmt.Errorf("scaling_store: GetByApp: %w", err)
	}
	return &c, nil
}

// Upsert implements ScalingStore. Returns (final, changed, err).
// If incoming.config_hash matches stored.config_hash, returns (stored, false, nil).
func (s *PostgresScalingStore) Upsert(ctx context.Context, tx DBTX, incoming *ScalingConfig) (*ScalingConfig, bool, error) {
	if err := incoming.Validate(); err != nil {
		return nil, false, err
	}
	incoming.ConfigHash = incoming.CanonicalHash()

	// Read current state for hash comparison.
	existing, err := s.GetByApp(ctx, incoming.TenantID, incoming.ProjectID, incoming.Environment, incoming.AppName)
	if err != nil && !errors.Is(err, ErrScalingConfigNotFound) {
		return nil, false, err
	}
	if existing != nil && existing.ConfigHash == incoming.ConfigHash {
		// Idempotent short-circuit.
		return existing, false, nil
	}

	const upsert = `
INSERT INTO app_scaling_config
       (tenant_id, project_id, environment, app_name,
        min_replicas, max_replicas, target_cpu_pct, scale_to_zero,
        updated_by, config_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (tenant_id, project_id, environment, app_name)
DO UPDATE SET
       min_replicas = EXCLUDED.min_replicas,
       max_replicas = EXCLUDED.max_replicas,
       target_cpu_pct = EXCLUDED.target_cpu_pct,
       scale_to_zero = EXCLUDED.scale_to_zero,
       updated_at = now(),
       updated_by = EXCLUDED.updated_by,
       config_hash = EXCLUDED.config_hash
RETURNING id, updated_at`

	var id string
	var updatedAt time.Time
	err = tx.QueryRow(ctx, upsert,
		incoming.TenantID, incoming.ProjectID, incoming.Environment, incoming.AppName,
		incoming.MinReplicas, incoming.MaxReplicas, incoming.TargetCPUPct, incoming.ScaleToZero,
		incoming.UpdatedBy, incoming.ConfigHash,
	).Scan(&id, &updatedAt)
	if err != nil {
		return nil, false, fmt.Errorf("scaling_store: Upsert: %w", err)
	}
	incoming.ID = id
	incoming.UpdatedAt = updatedAt
	return incoming, true, nil
}

// ListByEnv implements ScalingStore.
func (s *PostgresScalingStore) ListByEnv(ctx context.Context, tenantID, projectID, environment string) ([]ScalingConfig, error) {
	const q = `
SELECT id, tenant_id, project_id, environment, app_name,
       min_replicas, max_replicas, target_cpu_pct, scale_to_zero,
       updated_at, updated_by, config_hash
  FROM app_scaling_config
 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
 ORDER BY app_name`

	rows, err := s.db.Query(ctx, q, tenantID, projectID, environment)
	if err != nil {
		return nil, fmt.Errorf("scaling_store: ListByEnv: %w", err)
	}
	defer rows.Close()

	var out []ScalingConfig
	for rows.Next() {
		var c ScalingConfig
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.ProjectID, &c.Environment, &c.AppName,
			&c.MinReplicas, &c.MaxReplicas, &c.TargetCPUPct, &c.ScaleToZero,
			&c.UpdatedAt, &c.UpdatedBy, &c.ConfigHash,
		); err != nil {
			return nil, fmt.Errorf("scaling_store: ListByEnv scan: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Delete implements ScalingStore. Idempotent — returns nil if row absent.
func (s *PostgresScalingStore) Delete(ctx context.Context, tx DBTX, tenantID, projectID, environment, appName string) error {
	const q = `DELETE FROM app_scaling_config
                WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND app_name = $4`
	_, err := tx.Exec(ctx, q, tenantID, projectID, environment, appName)
	if err != nil {
		return fmt.Errorf("scaling_store: Delete: %w", err)
	}
	return nil
}

// DeleteByProject implements ScalingStore. Cascade for project deletion:
// removes every scaling row tied to (tenant, project) regardless of
// environment / app. Idempotent — returns nil if no rows match.
func (s *PostgresScalingStore) DeleteByProject(ctx context.Context, tx DBTX, tenantID, projectID string) error {
	const q = `DELETE FROM app_scaling_config WHERE tenant_id = $1 AND project_id = $2`
	if _, err := tx.Exec(ctx, q, tenantID, projectID); err != nil {
		return fmt.Errorf("scaling_store: DeleteByProject: %w", err)
	}
	return nil
}

// CountByTenant implements ScalingStore.
func (s *PostgresScalingStore) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	const q = `SELECT count(*) FROM app_scaling_config WHERE tenant_id = $1`
	var n int
	if err := s.db.QueryRow(ctx, q, tenantID).Scan(&n); err != nil {
		return 0, fmt.Errorf("scaling_store: CountByTenant: %w", err)
	}
	return n, nil
}

// (use ErrNoRows directly via errors.Is — convention from query_executor.go)
