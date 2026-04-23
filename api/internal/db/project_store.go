// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// PostgresProjectStore creates projects in PostgreSQL.
// It implements domain.ProjectStore (DIP).
type PostgresProjectStore struct {
	DB DBTX
}

// GenerateProjectID generates a project ID in the format "proj-{8 hex chars}".
func GenerateProjectID() string {
	b := make([]byte, 4) // 4 bytes = 8 hex chars
	_, _ = rand.Read(b)
	return "proj-" + hex.EncodeToString(b)
}

// CreateProject inserts a new project into the database.
// Returns domain.ErrProjectExists if the (tenant_id, name) unique constraint is violated.
func (s *PostgresProjectStore) CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.ProjectResult, error) {
	id := GenerateProjectID()

	_, err := s.DB.Exec(ctx,
		"INSERT INTO projects (id, tenant_id, name, created_by) VALUES ($1, $2, $3, $4)",
		id, input.TenantID, input.Name, input.CreatedBy,
	)
	if err != nil {
		var uv *UniqueViolationError
		if errors.As(err, &uv) {
			return domain.ProjectResult{}, domain.ErrProjectExists
		}
		return domain.ProjectResult{}, fmt.Errorf("project store: insert failed: %w", err)
	}

	return domain.ProjectResult{
		ID:     id,
		Name:   input.Name,
		TeamID: input.TenantID,
	}, nil
}

// EnsureProject creates a project if it doesn't exist (idempotent).
// Used by the deploy flow to guarantee the project is registered in the
// projects table. Without this, a project deleted via `loopzero project delete`
// and redeployed would not appear in `loopzero project list`.
func (s *PostgresProjectStore) EnsureProject(ctx context.Context, tx DBTX, tenantID, name, createdBy string) error {
	id := GenerateProjectID()
	_, err := tx.Exec(ctx,
		`INSERT INTO projects (id, tenant_id, name, created_by)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tenant_id, name) DO NOTHING`,
		id, tenantID, name, createdBy,
	)
	if err != nil {
		return fmt.Errorf("project store: ensure project failed: %w", err)
	}
	return nil
}

// ListByTenant returns all projects for a tenant, ordered by name.
func (s *PostgresProjectStore) ListByTenant(ctx context.Context, tenantID string) ([]domain.ProjectResult, error) {
	rows, err := s.DB.Query(ctx,
		"SELECT id, name, tenant_id, created_by, created_at FROM projects WHERE tenant_id = $1 ORDER BY name",
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("project store: list by tenant: %w", err)
	}
	defer rows.Close()

	var results []domain.ProjectResult
	for rows.Next() {
		var r domain.ProjectResult
		if err := rows.Scan(&r.ID, &r.Name, &r.TeamID, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("project store: list by tenant scan: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project store: list by tenant rows error: %w", err)
	}

	// Return empty slice instead of nil for consistent JSON serialization.
	if results == nil {
		results = []domain.ProjectResult{}
	}
	return results, nil
}

// GetByName retrieves a project by tenant and name.
// Returns nil when not found (not an error — caller decides).
func (s *PostgresProjectStore) GetByName(ctx context.Context, tenantID, name string) (*domain.ProjectResult, error) {
	rows, err := s.DB.Query(ctx,
		"SELECT id, name, tenant_id, created_by, created_at FROM projects WHERE tenant_id = $1 AND name = $2",
		tenantID, name,
	)
	if err != nil {
		return nil, fmt.Errorf("project store: get by name: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("project store: get by name rows error: %w", err)
		}
		return nil, nil
	}

	var result domain.ProjectResult
	if err := rows.Scan(&result.ID, &result.Name, &result.TeamID, &result.CreatedBy, &result.CreatedAt); err != nil {
		return nil, fmt.Errorf("project store: get by name scan: %w", err)
	}
	return &result, nil
}

// DeleteProject removes a project from the database.
func (s *PostgresProjectStore) DeleteProject(ctx context.Context, tenantID, projectID string) error {
	result, err := s.DB.Exec(ctx,
		"DELETE FROM projects WHERE tenant_id = $1 AND id = $2",
		tenantID, projectID,
	)
	if err != nil {
		return fmt.Errorf("project store: delete failed: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("project store: project not found")
	}
	return nil
}
